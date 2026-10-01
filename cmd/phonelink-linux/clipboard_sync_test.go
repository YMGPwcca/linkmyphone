package main

import (
	"context"
	"testing"

	clipclient "github.com/YMGPwcca/phonelink-linux/clipboard"
)

type syncFakeLocal struct {
	text string
}

func (l *syncFakeLocal) ReadText(context.Context) (string, error) {
	return l.text, nil
}

func (l *syncFakeLocal) WriteText(_ context.Context, text string) error {
	l.text = text
	return nil
}

func TestTrackedLocalClipboardSuppressesRemoteEcho(t *testing.T) {
	base := &syncFakeLocal{text: "initial"}
	var local clipclient.Local = base
	tracked := newTrackedLocalClipboard(local, "initial")

	if tracked.MarkIfChanged("initial") {
		t.Fatal("initial clipboard must not be reported as changed")
	}
	if err := tracked.WriteText(context.Background(), "from phone"); err != nil {
		t.Fatal(err)
	}
	if tracked.MarkIfChanged("from phone") {
		t.Fatal("remote write must not be echoed back as a local change")
	}
	if !tracked.MarkIfChanged("copied on Linux") {
		t.Fatal("new local text must be reported as changed")
	}
	if tracked.MarkIfChanged("copied on Linux") {
		t.Fatal("unchanged local text must not be reported twice")
	}

	select {
	case size := <-tracked.remoteWrite:
		if size != len([]byte("from phone")) {
			t.Fatalf("remote write size=%d", size)
		}
	default:
		t.Fatal("expected remote write notification")
	}
}
