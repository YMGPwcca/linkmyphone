package clipboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os/exec"
	"strings"
	"time"

	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
)

// SupportsRichContent means this provider can read/write MIME targets.
// xsel remains a plain-text fallback.
func (l *NativeLocal) SupportsRichContent() bool {
	return l != nil && (l.backend == "wl-clipboard" || l.backend == "xclip")
}

func (l *NativeLocal) ReadContent(ctx context.Context) (Content, error) {
	if !l.SupportsRichContent() {
		text, err := l.ReadText(ctx)
		c := TextContent(text)
		if err == nil {
			err = c.Validate()
		}
		return c, err
	}
	name, args := l.read.name, []string{"--list-types"}
	if l.backend == "xclip" {
		args = []string{"-selection", "clipboard", "-out", "-target", "TARGETS"}
	}
	offers, err := boundedCommandOutput(ctx, name, args, 32<<10)
	if err != nil {
		if nativeReadIsEmptyClipboard(l.backend, err) {
			return TextContent(""), nil
		}
		var exitErr *exec.ExitError
		if l.backend == "xclip" && errors.As(err, &exitErr) && strings.Contains(string(exitErr.Stderr), "target TARGETS not available") {
			return Content{}, ErrContentUnavailable
		}
		return Content{}, nativeCommandError("list formats", l.backend, err)
	}
	if len(bytes.TrimSpace(offers)) == 0 {
		return TextContent(""), nil
	}
	target := preferredNativeTarget(strings.Fields(string(offers)))
	if target == "" {
		return Content{}, ErrUnsupportedContent
	}
	args = []string{"--no-newline", "--type", target}
	if l.backend == "xclip" {
		args = []string{"-selection", "clipboard", "-out", "-target", target}
	}
	inputLimit := MaxImageInputBytes
	if target == "image/png" {
		inputLimit = MaxLocalImageBytes
	}
	data, err := boundedCommandOutput(ctx, name, args, inputLimit)
	if err != nil {
		if errors.Is(err, ErrContentTooLarge) {
			return Content{}, err
		}
		// The owner can disappear or replace its offer between TARGETS and
		// the actual transfer. Skip this observation and retry the next tick.
		return Content{}, ErrContentUnavailable
	}
	if strings.HasPrefix(target, "image/") {
		hash := sha256.Sum256(data)
		l.imageMu.Lock()
		defer l.imageMu.Unlock()
		if l.imageReady && l.imageHash == hash {
			return l.imageContent.Clone(), nil
		}
		c, err := normalizeImage(data, inputLimit)
		if err == nil {
			l.imageContent = c.Clone()
			l.imageHash = hash
			l.imageReady = true
		}
		return c, err
	}
	c := TextContent(string(data))
	if target == "text/html" {
		c.Type = proto.ItemTextHTML
	}
	if err := c.Validate(); err != nil {
		return Content{}, err
	}
	return c, nil
}

func preferredNativeTarget(offers []string) string {
	for _, preferred := range []string{"image/png", "image/jpeg", "image/gif", "image/bmp", "image/x-bmp", "image/x-ms-bmp", "text/html", "text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "TEXT", "STRING"} {
		for _, offered := range offers {
			if offered == preferred {
				return offered
			}
		}
	}
	return ""
}

func (l *NativeLocal) WriteContent(ctx context.Context, c Content) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Type == proto.ItemTextPlain {
		return l.WriteText(ctx, c.Text)
	}
	if !l.SupportsRichContent() {
		return ErrUnsupportedContent
	}
	if c.Type == proto.ItemTextHTML {
		l.gtkOnce.Do(func() { l.gtkRuntime = gtkPython(ctx) })
		if l.gtkRuntime != "" {
			return writeHTMLOffer(ctx, l.gtkRuntime, c.Text)
		}
	}
	args := []string{"--type", c.MIME()}
	if l.backend == "xclip" {
		args = []string{"-selection", "clipboard", "-in", "-target", c.MIME()}
	}
	cmd := exec.CommandContext(ctx, l.write.name, args...)
	if c.Type == proto.ItemImage {
		cmd.Stdin = bytes.NewReader(c.Image)
	} else {
		cmd.Stdin = strings.NewReader(c.Text)
	}
	// Clipboard owners fork. Inherit no capture pipes that the background
	// owner can keep open after the write command has exited.
	if err := cmd.Run(); err != nil {
		return nativeCommandError("write content", l.backend, err)
	}
	return nil
}

type limitedOutput struct {
	data bytes.Buffer
	max  int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.max-b.data.Len() {
		return 0, ErrContentTooLarge
	}
	return b.data.Write(p)
}

func boundedCommandOutput(ctx context.Context, name string, args []string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var output limitedOutput
	output.max = limit
	var stderr cappedTextBuffer
	stderr.max = 1024
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	cmd.WaitDelay = 250 * time.Millisecond
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitErr.Stderr = []byte(stderr.String())
		}
		return nil, err
	}
	return output.data.Bytes(), nil
}
