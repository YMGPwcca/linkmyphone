package clipboard

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeLocal struct {
	mu   sync.Mutex
	text string
}

func (l *fakeLocal) ReadText(context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text, nil
}

func (l *fakeLocal) WriteText(_ context.Context, text string) error {
	l.mu.Lock()
	l.text = text
	l.mu.Unlock()
	return nil
}

func TestTrackedLocalClipboardSuppressesRemoteEcho(t *testing.T) {
	base := &fakeLocal{text: "initial"}
	tracked := newTrackedLocalClipboard(base, "initial")

	if tracked.MarkIfChanged("initial") {
		t.Fatal("initial clipboard must not be reported as changed")
	}
	if err := tracked.WriteText(context.Background(), "from phone"); err != nil {
		t.Fatal(err)
	}
	if tracked.MarkIfChanged("from phone") {
		t.Fatal("remote write must not be echoed back")
	}
	if !tracked.MarkIfChanged("copied on Linux") {
		t.Fatal("new local text must be reported")
	}

	select {
	case event := <-tracked.remoteWrite:
		if event.size != len([]byte("from phone")) {
			t.Fatalf("size=%d", event.size)
		}
	default:
		t.Fatal("expected remote write event")
	}
}

func TestTrackingHashNormalizesOnlyEchoComparison(t *testing.T) {
	if clipboardTrackingHash("phone text") != clipboardTrackingHash("phone text\n") {
		t.Fatal("terminal newline should normalize for echo tracking")
	}
	if clipboardTrackingHash("a\r\nb") != clipboardTrackingHash("a\nb") {
		t.Fatal("CRLF should normalize for echo tracking")
	}

	base := &fakeLocal{text: "abc"}
	tracked := newTrackedLocalClipboard(base, "abc")
	if !tracked.MarkIfChanged("abc\n") {
		t.Fatal("exact newline-only local change must remain publishable")
	}
}

func TestPublishQueueIsLatestWinsAndDiscardable(t *testing.T) {
	queue := make(chan publishJob, 1)
	queueLatestPublish(queue, publishJob{generation: 1, text: "one"})
	queueLatestPublish(queue, publishJob{generation: 2, text: "two"})
	queueLatestPublish(queue, publishJob{generation: 3, text: "three"})

	select {
	case job := <-queue:
		if job.generation != 3 || job.text != "three" {
			t.Fatalf("job=%#v", job)
		}
	default:
		t.Fatal("expected queued publish")
	}

	queueLatestPublish(queue, publishJob{generation: 4, text: "four"})
	discardQueuedPublishes(queue)
	select {
	case job := <-queue:
		t.Fatalf("unexpected queued job=%#v", job)
	default:
	}
}

type blockingLocal struct {
	started chan struct{}
	release chan struct{}
	text    string
}

func (l *blockingLocal) ReadText(context.Context) (string, error) {
	return l.text, nil
}

func (l *blockingLocal) WriteText(_ context.Context, text string) error {
	select {
	case <-l.started:
	default:
		close(l.started)
	}
	<-l.release
	l.text = text
	return nil
}

func TestTrackedLocalClipboardSerializesReadDuringRemoteWrite(t *testing.T) {
	base := &blockingLocal{
		started: make(chan struct{}),
		release: make(chan struct{}),
		text:    "old",
	}
	tracked := newTrackedLocalClipboard(base, "old")

	done := make(chan error, 1)
	go func() {
		done <- tracked.WriteText(context.Background(), "remote")
	}()
	select {
	case <-base.started:
	case <-time.After(time.Second):
		t.Fatal("write did not start")
	}

	readDone := make(chan string, 1)
	go func() {
		text, _ := tracked.ReadText(context.Background())
		readDone <- text
	}()

	select {
	case <-readDone:
		t.Fatal("read escaped write serialization")
	case <-time.After(20 * time.Millisecond):
	}

	close(base.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not finish")
	}
	select {
	case text := <-readDone:
		if text != "remote" {
			t.Fatalf("text=%q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
}
