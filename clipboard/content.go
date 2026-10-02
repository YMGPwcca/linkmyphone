package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"time"
	"unicode/utf8"

	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
)

const (
	MaxClipboardImageBytes = 1 << 20
	MaxClipboardTextUnits  = 131071 // Android's limit is < 131072 UTF-16 units.
	MaxImageInputBytes     = 16 << 20
	MaxImagePixels         = 32 << 20
)

var (
	ErrUnsupportedContent = errors.New("clipboard: unsupported content")
	ErrContentTooLarge    = errors.New("clipboard: content too large")
	ErrContentUnavailable = errors.New("clipboard: content unavailable")
)

// Content is one clipboard representation. HTML is a UTF-8 fragment, not
// Windows CF_HTML framing. Images on the wire are encoded PNG bytes.
type Content struct {
	Type  proto.ItemType
	Text  string
	Image []byte
}

type ContentLocal interface {
	ReadContent(context.Context) (Content, error)
	WriteContent(context.Context, Content) error
}

func TextContent(text string) Content { return Content{Type: proto.ItemTextPlain, Text: text} }
func (c Content) Size() int           { return len(c.Text) + len(c.Image) }
func (c Content) Clone() Content      { c.Image = bytes.Clone(c.Image); return c }
func (c Content) Equal(other Content) bool {
	return c.Type == other.Type && c.Text == other.Text && bytes.Equal(c.Image, other.Image)
}
func (c Content) MIME() string {
	switch c.Type {
	case proto.ItemTextPlain:
		return "text/plain;charset=utf-8"
	case proto.ItemTextHTML:
		return "text/html"
	case proto.ItemImage:
		return "image/png"
	default:
		return ""
	}
}

func (c Content) Validate() error {
	switch c.Type {
	case proto.ItemTextPlain, proto.ItemTextHTML:
		if len(c.Image) != 0 || !utf8.ValidString(c.Text) {
			return ErrUnsupportedContent
		}
		units := 0
		for _, r := range c.Text {
			units++
			if r > 0xffff {
				units++
			}
			if units > MaxClipboardTextUnits {
				return ErrContentTooLarge
			}
		}
		return nil
	case proto.ItemImage:
		if c.Text != "" || len(c.Image) == 0 {
			return ErrUnsupportedContent
		}
		if len(c.Image) > MaxClipboardImageBytes {
			return ErrContentTooLarge
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(c.Image))
		if err != nil {
			return fmt.Errorf("%w: invalid PNG", ErrUnsupportedContent)
		}
		if !validImageDimensions(cfg.Width, cfg.Height) {
			return ErrContentTooLarge
		}
		// Decode as well as reading the header: a truncated/corrupt image must
		// never replace a valid desktop clipboard.
		if _, err := png.Decode(bytes.NewReader(c.Image)); err != nil {
			return fmt.Errorf("%w: invalid PNG", ErrUnsupportedContent)
		}
		return nil
	default:
		return ErrUnsupportedContent
	}
}

func validImageDimensions(w, h int) bool {
	return w > 0 && h > 0 && w <= MaxImagePixels/h
}

// PrepareImage converts supported local image encodings to PNG, preserving
// the exact bytes of valid small PNGs. Its resizing algorithm is independent
// of the Windows implementation; only the interoperable size limit is shared.
func PrepareImage(data []byte) (Content, error) {
	if len(data) > MaxImageInputBytes {
		return Content{}, ErrContentTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Content{}, ErrUnsupportedContent
	}
	if !validImageDimensions(cfg.Width, cfg.Height) {
		return Content{}, ErrContentTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Content{}, ErrUnsupportedContent
	}
	if format == "png" && len(data) <= MaxClipboardImageBytes {
		return Content{Type: proto.ItemImage, Image: bytes.Clone(data)}, nil
	}
	for {
		var out bytes.Buffer
		if err := png.Encode(&out, img); err != nil {
			return Content{}, err
		}
		if out.Len() <= MaxClipboardImageBytes {
			return Content{Type: proto.ItemImage, Image: out.Bytes()}, nil
		}
		bounds := img.Bounds()
		if bounds.Dx() == 1 && bounds.Dy() == 1 {
			return Content{}, ErrContentTooLarge
		}
		ratio := math.Min(0.85, math.Sqrt(float64(MaxClipboardImageBytes)/float64(out.Len()))*0.9)
		w := max(1, int(float64(bounds.Dx())*ratio))
		h := max(1, int(float64(bounds.Dy())*ratio))
		next := image.NewNRGBA(image.Rect(0, 0, w, h))
		// Area averaging keeps transparency and avoids selecting only one
		// source pixel when a large image is reduced.
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				x0 := x * bounds.Dx() / w
				x1 := max(x0+1, (x+1)*bounds.Dx()/w)
				y0 := y * bounds.Dy() / h
				y1 := max(y0+1, (y+1)*bounds.Dy()/h)
				var r, g, b, a, n uint64
				for sy := y0; sy < y1; sy++ {
					for sx := x0; sx < x1; sx++ {
						rr, gg, bb, aa := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
						r += uint64(rr)
						g += uint64(gg)
						b += uint64(bb)
						a += uint64(aa)
						n++
					}
				}
				// RGBA() is premultiplied; use an RGBA pixel before Set converts
				// it back to the NRGBA storage used for PNG.
				next.Set(x, y, color.RGBA{R: byte(r / n >> 8), G: byte(g / n >> 8), B: byte(b / n >> 8), A: byte(a / n >> 8)})
			}
		}
		img = next
	}
}

func contentResponse(cid string, content Content) proto.Response {
	return contentResponseAt(cid, content, time.Now())
}

func contentResponseAt(cid string, content Content, created time.Time) proto.Response {
	it := proto.Item{Type: content.Type, CreatedTime: &proto.Timestamp{Seconds: created.Unix(), Nanos: int32(created.Nanosecond())}}
	if content.Type == proto.ItemImage {
		it.ImageBytes = bytes.Clone(content.Image)
	} else {
		text := content.Text
		it.Text = &text
	}
	return proto.Response{Status: proto.ResponseOK, CorrelationID: cid, Items: []proto.Item{it}}
}

func contentFromResponse(resp proto.Response) (Content, error) {
	if resp.Status != proto.ResponseOK {
		return Content{}, fmt.Errorf("%w: response status %d", ErrContentUnavailable, resp.Status)
	}
	// Prefer the richer representation if a peer sends multiple items.
	for _, kind := range []proto.ItemType{proto.ItemImage, proto.ItemTextHTML, proto.ItemTextPlain} {
		for _, item := range resp.Items {
			if item.Type != kind {
				continue
			}
			c := Content{Type: kind}
			if kind == proto.ItemImage {
				if len(item.ImageBytes) > MaxClipboardImageBytes {
					return Content{}, ErrContentTooLarge
				}
				var err error
				c, err = PrepareImage(item.ImageBytes)
				if err != nil {
					return Content{}, err
				}
			} else {
				if item.Text == nil {
					continue
				}
				c.Text = *item.Text
			}
			if err := c.Validate(); err != nil {
				return Content{}, err
			}
			return c, nil
		}
	}
	return Content{}, ErrUnsupportedContent
}

func readLocalContent(ctx context.Context, local Local) (Content, error) {
	if local == nil {
		return Content{}, ErrContentUnavailable
	}
	if rich, ok := local.(ContentLocal); ok {
		return rich.ReadContent(ctx)
	}
	text, err := local.ReadText(ctx)
	return TextContent(text), err
}

func isRecoverableContentError(err error) bool {
	return errors.Is(err, ErrUnsupportedContent) || errors.Is(err, ErrContentTooLarge) || errors.Is(err, ErrContentUnavailable)
}
