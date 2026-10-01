package main

import (
	"context"
	"sync"
	"testing"
	"time"

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

type blockingSyncLocal struct {
	mu      sync.Mutex
	text    string
	started chan struct{}
	release chan struct{}
}

func (l *blockingSyncLocal) ReadText(context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text, nil
}

func (l *blockingSyncLocal) WriteText(_ context.Context, text string) error {
	select {
	case <-l.started:
	default:
		close(l.started)
	}
	<-l.release
	l.mu.Lock()
	l.text = text
	l.mu.Unlock()
	return nil
}

func TestTrackedLocalClipboardSuppressesPollRaceDuringRemoteWrite(t *testing.T) {
	base := &blockingSyncLocal{
		text:    "old local",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	tracked := newTrackedLocalClipboard(base, "old local")

	done := make(chan error, 1)
	go func() {
		done <- tracked.WriteText(context.Background(), "from phone")
	}()

	select {
	case <-base.started:
	case <-time.After(time.Second):
		t.Fatal("remote write did not start")
	}

	if tracked.MarkIfChanged("old local") {
		t.Fatal("old clipboard observed during remote write must not be published")
	}
	if tracked.MarkIfChanged("from phone") {
		t.Fatal("remote target observed during remote write must not be published")
	}

	close(base.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote write did not finish")
	}

	if tracked.MarkIfChanged("from phone") {
		t.Fatal("applied remote clipboard must not be published back")
	}
}

func TestQueueLatestClipboardTextCoalescesPendingChanges(t *testing.T) {
	queue := make(chan string, 1)
	queueLatestClipboardText(queue, "first")
	queueLatestClipboardText(queue, "second")
	queueLatestClipboardText(queue, "third")

	select {
	case got := <-queue:
		if got != "third" {
			t.Fatalf("queued=%q", got)
		}
	default:
		t.Fatal("expected queued clipboard text")
	}
}

func TestClipboardTrackingHashNormalizesProviderNewlineVariants(t *testing.T) {
	cases := [][2]string{
		{"phone text", "phone text\n"},
		{"line1\r\nline2", "line1\nline2"},
		{"line1\r\nline2\r\n", "line1\nline2\n"},
	}
	for _, pair := range cases {
		if clipboardTrackingHash(pair[0]) != clipboardTrackingHash(pair[1]) {
			t.Fatalf("tracking hash mismatch for %q and %q", pair[0], pair[1])
		}
	}
}

func TestTrackedLocalClipboardSuppressesRemoteTextWithProviderNewline(t *testing.T) {
	base := &syncFakeLocal{text: "initial"}
	tracked := newTrackedLocalClipboard(base, "initial")

	if err := tracked.WriteText(context.Background(), "fromphone"); err != nil {
		t.Fatal(err)
	}
	if tracked.MarkIfChanged("fromphone\n") {
		t.Fatal("provider-added terminal newline must not echo remote text")
	}
}

func TestTrackedLocalClipboardStillDetectsRealNewlineOnlyLocalChange(t *testing.T) {
	base := &syncFakeLocal{text: "abc"}
	tracked := newTrackedLocalClipboard(base, "abc")

	if !tracked.MarkIfChanged("abc\n") {
		t.Fatal("exact local newline change must still be publishable")
	}
	if tracked.MarkIfChanged("abc\n") {
		t.Fatal("unchanged exact local text must not publish twice")
	}
}
