package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
)

func TestNativeOfferPreferenceAndOutputBounds(t *testing.T) {
	if got := preferredNativeTarget([]string{"text/plain", "text/html", "image/png"}); got != "image/png" {
		t.Fatal(got)
	}
	for _, mime := range []string{"image/bmp", "image/x-bmp", "image/x-ms-bmp"} {
		if got := preferredNativeTarget([]string{"text/plain", mime}); got != mime {
			t.Fatal("BMP offer lost", got)
		}
	}
	if got := preferredNativeTarget([]string{"text/uri-list"}); got != "" {
		t.Fatal("file selections are not clipboard text")
	}
	if _, err := boundedCommandOutput(context.Background(), "head", []string{"-c", "1000", "/dev/zero"}, 32); !errors.Is(err, ErrContentTooLarge) {
		t.Fatal(err)
	}
}

func TestNativeX11RichContentIntegration(t *testing.T) {
	if os.Getenv("LINKMYPHONE_NATIVE_INTEGRATION") != "1" {
		t.Skip("set LINKMYPHONE_NATIVE_INTEGRATION=1 with an X11 display")
	}
	path, err := exec.LookPath("xclip")
	if err != nil {
		t.Fatal(err)
	}
	l := &NativeLocal{backend: "xclip", read: commandSpec{name: path, args: []string{"-selection", "clipboard", "-out", "-target", "UTF8_STRING"}}, write: commandSpec{name: path, args: []string{"-selection", "clipboard", "-in"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if gtkPython(ctx) == "" {
		out, err := exec.CommandContext(ctx, "/usr/bin/python3", "-c", "import gi; gi.require_version('Gtk','4.0'); from gi.repository import Gtk").CombinedOutput()
		t.Fatalf("integration suite requires Python GI and GTK4: %v %s", err, out)
	}
	defer l.WriteText(context.Background(), "")
	for _, want := range []Content{TextContent(""), TextContent("Tiếng Việt\n😀"), {Type: proto.ItemTextHTML, Text: "<b>Tiếng Việt 😀</b>"}, {Type: proto.ItemImage, Image: testPNG(t, 220, 200)}} {
		if err := l.WriteContent(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, err := l.ReadContent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Fatal("native MIME transfer changed content", want.MIME())
		}
		if want.Type == proto.ItemTextHTML && l.gtkRuntime != "" {
			plain, err := l.ReadText(ctx)
			if err != nil || plain != "Tiếng Việt 😀" {
				t.Fatal("HTML plain-text fallback unavailable", err)
			}
		}
		if want.Type == proto.ItemImage && !bytes.Equal(got.Image, want.Image) {
			t.Fatal("image changed")
		}
	}
}
