package clipboard

import (
	"context"
	"testing"

	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
)

type richMemoryLocal struct{ content clipclient.Content }

func (l *richMemoryLocal) ReadText(context.Context) (string, error) { return l.content.Text, nil }
func (l *richMemoryLocal) WriteText(_ context.Context, s string) error {
	l.content = clipclient.TextContent(s)
	return nil
}
func (l *richMemoryLocal) ReadContent(context.Context) (clipclient.Content, error) {
	return l.content.Clone(), nil
}
func (l *richMemoryLocal) WriteContent(_ context.Context, c clipclient.Content) error {
	l.content = c.Clone()
	return nil
}

func TestRichEchoBarrierIncludesFormatAndHTMLFormatting(t *testing.T) {
	base := &richMemoryLocal{content: clipclient.TextContent("hello")}
	l := newTrackedLocalClipboard(base, "hello")
	html := clipclient.Content{Type: proto.ItemTextHTML, Text: "<b>hello</b>"}
	if err := l.WriteContent(context.Background(), html); err != nil {
		t.Fatal(err)
	}
	if l.MarkContentIfChanged(html) {
		t.Fatal("HTML written from phone echoed back")
	}
	if !l.MarkContentIfChanged(clipclient.Content{Type: proto.ItemTextHTML, Text: "<i>hello</i>"}) {
		t.Fatal("format-only local change was lost")
	}
	image := clipclient.Content{Type: proto.ItemImage, Image: []byte{1, 2, 3}}
	if !l.MarkContentIfChanged(image) {
		t.Fatal("image selection not observed")
	}
	if l.MarkContentIfChanged(image) {
		t.Fatal("duplicate image observed")
	}
	image.Image[0] = 4
	if !l.MarkContentIfChanged(image) {
		t.Fatal("changed image not observed")
	}
}
