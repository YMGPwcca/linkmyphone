package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"sync"
	"testing"
	"time"

	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
	"golang.org/x/image/bmp"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(37))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPrepareImageShrinksAndPreservesSmallPNG(t *testing.T) {
	small := testPNG(t, 12, 10)
	c, err := PrepareImage(small)
	if err != nil || !bytes.Equal(c.Image, small) {
		t.Fatal("small PNG changed", err)
	}
	large := testPNG(t, 800, 700)
	if len(large) <= MaxClipboardImageBytes {
		t.Fatal("fixture must require resizing")
	}
	c, err = PrepareImage(large)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(c.Image))
	if err != nil || cfg.Width >= 800 || cfg.Height >= 700 {
		t.Fatal("image was not reduced", cfg, err)
	}
}

func TestAndroidJPEGContentIsNormalizedToPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 9))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	resp := proto.Response{Status: proto.ResponseOK, Items: []proto.Item{{Type: proto.ItemImage, ImageBytes: b.Bytes()}}}
	c, err := contentFromResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(c.Image, []byte("\x89PNG")) {
		t.Fatal("JPEG was advertised as PNG without conversion")
	}
}

func TestContentRejectsMalformedImageAndUTF16Overflow(t *testing.T) {
	if _, err := PrepareImage([]byte("not an image")); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatal(err)
	}
	data := testPNG(t, 5, 6)
	if err := (Content{Type: proto.ItemImage, Image: data[:len(data)-9]}).Validate(); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatal(err)
	}
	if err := TextContent(string(bytes.Repeat([]byte("😀"), 65536))).Validate(); !errors.Is(err, ErrContentTooLarge) {
		t.Fatal(err)
	}
	if err := TextContent(string([]byte{0xff})).Validate(); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatal(err)
	}
}

type contentLocal struct {
	mu     sync.Mutex
	value  Content
	writes int
}

func (l *contentLocal) ReadText(ctx context.Context) (string, error) {
	c, err := l.ReadContent(ctx)
	return c.Text, err
}
func (l *contentLocal) WriteText(ctx context.Context, s string) error {
	return l.WriteContent(ctx, TextContent(s))
}
func (l *contentLocal) ReadContent(context.Context) (Content, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value.Clone(), nil
}
func (l *contentLocal) WriteContent(_ context.Context, c Content) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.value = c.Clone()
	l.writes++
	return nil
}

func latestSent(t *testing.T, fr *fakeRelay, after int) relay.Received {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		fr.mu.Lock()
		if len(fr.sent) > after {
			v := fr.sent[len(fr.sent)-1]
			fr.mu.Unlock()
			return v
		}
		fr.mu.Unlock()
		select {
		case <-timer.C:
			t.Fatal("request not sent")
		case <-ticker.C:
		}
	}
}

func TestTypedContentPullAndPublishedSnapshots(t *testing.T) {
	for _, content := range []Content{TextContent(""), {Type: proto.ItemTextHTML, Text: "<b>Tiếng Việt 😀</b>"}, {Type: proto.ItemImage, Image: testPNG(t, 220, 200)}} {
		t.Run(content.MIME(), func(t *testing.T) {
			fr := newFakeRelay()
			local := &contentLocal{value: TextContent("old")}
			c := New(fr, local, Config{Target: "phone", SelfDcgClientID: "linux", RequestTimeout: time.Second})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go c.Run(ctx)
			done := make(chan error, 1)
			go func() { done <- c.pullToLocal(ctx, "cid", 0) }()
			sent := latestSent(t, fr, 0)
			pm, err := platform.Unmarshal(sent.Payload)
			if err != nil {
				t.Fatal(err)
			}
			rid, _ := pm.Header(platform.HeaderRequestID)
			response := proto.DeviceResourceResponse{ResponseType: proto.DeviceResourceResponseSuccess, Payload: proto.MarshalResponse(contentResponse("cid", content))}
			wire, err := platform.Marshal(platform.NewInternalResponse(proto.MarshalDeviceResourceResponse(response), rid))
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the existing DCG fragment/reassembly boundary with a
			// payload larger than the default 64 KiB relay fragment size.
			parts, err := dcg.FragmentPayload(wire, 64<<10, 1, "s", int(dcg.TransportMessageTypePlatform))
			if err != nil {
				t.Fatal(err)
			}
			assembler := dcg.NewReassembler(0, 0)
			var joined []byte
			for _, part := range parts {
				var complete bool
				joined, complete, err = assembler.Add("phone", part)
				if err != nil {
					t.Fatal(err)
				}
				if complete && !bytes.Equal(joined, wire) {
					t.Fatal("reassembly changed payload")
				}
			}
			fr.recv <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypePlatform, Payload: joined}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("pull did not finish")
			}
			got, _ := local.ReadContent(ctx)
			if !got.Equal(content) {
				t.Fatal("remote representation was lost")
			}
			cid, err := c.PublishLocalContent(ctx, content, "snapshot")
			if err != nil {
				t.Fatal(err)
			}
			// Caller-owned image bytes must not mutate an already published offer.
			if len(content.Image) > 0 {
				content.Image[0] ^= 0xff
			}
			_ = local.WriteText(ctx, "new selection")
			request := platform.NewDeviceResourceRequest(proto.MarshalDeviceResourceMessage(proto.WrapClipboardRequest(proto.NewContentRequest(cid))), "request")
			if err := c.handleIncomingRequest(ctx, relay.Received{Source: "phone"}, request); err != nil {
				t.Fatal(err)
			}
			reply := latestSent(t, fr, 2)
			parsed, err := platform.Unmarshal(reply.Payload)
			if err != nil {
				t.Fatal(err)
			}
			drm, err := proto.UnmarshalDeviceResourceResponse(parsed.Payload)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := proto.UnmarshalResponse(drm.Payload)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := contentFromResponse(resp)
			if err != nil {
				t.Fatal(err)
			}
			if !snapshot.Equal(got) {
				t.Fatal("snapshot served current or mutated content")
			}
		})
	}
}

func TestRequestTimeoutIncludesSend(t *testing.T) {
	c := New(&blockingSendRelay{}, &fakeLocal{}, Config{Target: "phone", RequestTimeout: 20 * time.Millisecond})
	started := time.Now()
	_, err := c.GetContent(context.Background(), "cid")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("send escaped request deadline", err)
	}
}

type blockingSendRelay struct{}

func (*blockingSendRelay) Send(ctx context.Context, _ string, _ string, _ dcg.TransportMessageType, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}
func (*blockingSendRelay) Received() <-chan relay.Received { return nil }

func TestTypedSnapshotAggregateBudgetRetiresOldest(t *testing.T) {
	c := New(newFakeRelay(), nil, Config{Target: "phone"})
	data := make([]byte, MaxClipboardImageBytes)
	now := time.Now()
	for n := 0; n < 16; n++ {
		content := Content{Type: proto.ItemImage, Image: data}
		c.published[string(rune('a'+n))] = publishedSnapshot{content: &content, createdAt: now.Add(time.Duration(n) * time.Second)}
	}
	c.pruneSnapshotBytesLocked(MaxClipboardImageBytes)
	if len(c.published) != 15 {
		t.Fatal("aggregate image budget not enforced", len(c.published))
	}
	if _, ok := c.published["a"]; ok {
		t.Fatal("oldest image snapshot retained")
	}
	if _, ok := c.retired["a"]; !ok {
		t.Fatal("evicted correlation not retired")
	}
}

func TestIncomingImagesPreserveDimensionsAndOutboundIsBounded(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(testPNG(t, 1000, 900)))
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"bmp", "jpeg", "phone-sized-jpeg"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			if format == "bmp" {
				err = bmp.Encode(&encoded, img)
			} else {
				quality := 100
				if format == "phone-sized-jpeg" {
					quality = 70
				}
				err = jpeg.Encode(&encoded, img, &jpeg.Options{Quality: quality})
			}
			if err != nil {
				t.Fatal(err)
			}
			if format == "phone-sized-jpeg" && encoded.Len() > MaxClipboardImageBytes {
				t.Fatal("phone fixture must fit its JPEG transfer budget")
			}
			if format != "phone-sized-jpeg" && encoded.Len() <= MaxClipboardImageBytes {
				t.Fatal("fixture must exceed old incoming limit")
			}
			c, err := contentFromResponse(proto.Response{Status: proto.ResponseOK, Items: []proto.Item{{Type: proto.ItemImage, ImageBytes: encoded.Bytes()}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Validate(); err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(c.Image))
			if err != nil || cfg.Width != 1000 || cfg.Height != 900 {
				t.Fatal("incoming normalization changed dimensions", cfg, err)
			}
			if len(c.Image) <= MaxClipboardImageBytes {
				t.Fatal("fixture must expand beyond the outbound PNG budget")
			}
			out, err := prepareOutboundContent(c)
			if err != nil || len(out.Image) > MaxClipboardImageBytes {
				t.Fatal("outbound transfer was not bounded", err)
			}
		})
	}
}

func TestIncomingImageInputAndBMPDimensionBudgets(t *testing.T) {
	resp := proto.Response{Status: proto.ResponseOK, Items: []proto.Item{{Type: proto.ItemImage, ImageBytes: make([]byte, MaxImageInputBytes+1)}}}
	if _, err := contentFromResponse(resp); !errors.Is(err, ErrContentTooLarge) {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.LittleEndian.PutUint32(data[18:22], MaxImagePixels)
	binary.LittleEndian.PutUint32(data[22:26], 2)
	if _, err := PrepareImage(data); !errors.Is(err, ErrContentTooLarge) {
		t.Fatal("BMP allocation was not bounded", err)
	}
	if _, err := PrepareImage([]byte("BMcorrupt")); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatal(err)
	}
}

func TestLargeDesktopImageOutboundSnapshotsAndLiveFallback(t *testing.T) {
	original := Content{Type: proto.ItemImage, Image: testPNG(t, 800, 700)}
	local := &contentLocal{value: original.Clone()}
	fr := newFakeRelay()
	c := New(fr, local, Config{Target: "phone", SelfDcgClientID: "linux"})
	ctx := context.Background()
	cid, err := c.PublishLocalContent(ctx, original, "snapshot-large")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{cid, "unpublished-live-request"} {
		request := platform.NewDeviceResourceRequest(proto.MarshalDeviceResourceMessage(proto.WrapClipboardRequest(proto.NewContentRequest(id))), "request")
		if err := c.handleIncomingRequest(ctx, relay.Received{Source: "phone"}, request); err != nil {
			t.Fatal(err)
		}
		fr.mu.Lock()
		sent := fr.sent[len(fr.sent)-1]
		fr.mu.Unlock()
		pm, err := platform.Unmarshal(sent.Payload)
		if err != nil {
			t.Fatal(err)
		}
		drm, err := proto.UnmarshalDeviceResourceResponse(pm.Payload)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := proto.UnmarshalResponse(drm.Payload)
		if err != nil || resp.Status != proto.ResponseOK || len(resp.Items) != 1 {
			t.Fatal("outbound CONTENT response failed", resp.Status, err)
		}
		data := resp.Items[0].ImageBytes
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || len(data) > MaxClipboardImageBytes || cfg.Width >= 800 || cfg.Height >= 700 {
			t.Fatal("outbound PNG not prepared", id, len(data), cfg, err)
		}
	}
	got, _ := local.ReadContent(ctx)
	if !got.Equal(original) {
		t.Fatal("outbound preparation changed the desktop selection")
	}
}
