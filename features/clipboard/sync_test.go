package clipboard

import (
	"context"
	"sync"
	"testing"
	"time"

	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
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
	discardQueuedPublishesBefore(queue, 3)
	select {
	case job := <-queue:
		if job.generation != 4 {
			t.Fatalf("newer job was not preserved: %#v", job)
		}
	default:
		t.Fatal("newer queued job was incorrectly discarded")
	}

	queueLatestPublish(queue, publishJob{generation: 4, text: "four"})
	discardQueuedPublishesBefore(queue, 5)
	select {
	case job := <-queue:
		t.Fatalf("older queued job survived remote generation: %#v", job)
	default:
	}
}

func TestRemoteApplyQueueKeepsLatestGeneration(t *testing.T) {
	queue := make(chan clipclient.RemoteApplyEvent, 1)
	queueLatestRemoteApply(queue, clipclient.RemoteApplyEvent{Generation: 2, Bytes: 2})
	queueLatestRemoteApply(queue, clipclient.RemoteApplyEvent{Generation: 3, Bytes: 3})

	select {
	case event := <-queue:
		if event.Generation != 3 || event.Bytes != 3 {
			t.Fatalf("event=%#v", event)
		}
	default:
		t.Fatal("expected remote apply event")
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

func TestNativeTextEventQueueKeepsLatestSelection(t *testing.T) {
	queue := make(chan clipclient.NativeTextEvent, 1)
	queueLatestNativeTextEvent(queue, clipclient.NativeTextEvent{
		Text:  "one",
		State: "data",
	})
	queueLatestNativeTextEvent(queue, clipclient.NativeTextEvent{
		Text:  "two",
		State: "data",
	})
	queueLatestNativeTextEvent(queue, clipclient.NativeTextEvent{
		Text:  "",
		State: "nil",
	})

	select {
	case event := <-queue:
		if event.Text != "" || event.State != "nil" {
			t.Fatalf("event=%#v", event)
		}
	default:
		t.Fatal("expected latest native clipboard event")
	}
}

func TestNativeClearDebouncerCancelsTransientNilBeforeData(t *testing.T) {
	debouncer := nativeClearDebouncer{duration: 10 * time.Millisecond}
	defer debouncer.Stop()

	debouncer.Schedule()
	debouncer.Cancel()

	select {
	case <-debouncer.C():
		t.Fatal("transient nil event survived cancellation")
	case <-time.After(20 * time.Millisecond):
	}
	if debouncer.Fire() {
		t.Fatal("canceled transient nil event remained pending")
	}
}

func TestNativeClearDebouncerEmitsGenuineClear(t *testing.T) {
	debouncer := nativeClearDebouncer{duration: 5 * time.Millisecond}
	defer debouncer.Stop()

	debouncer.Schedule()
	select {
	case <-debouncer.C():
	case <-time.After(time.Second):
		t.Fatal("genuine nil event did not reach debounce deadline")
	}
	if !debouncer.Fire() {
		t.Fatal("genuine nil event was not pending at deadline")
	}
	if debouncer.Fire() {
		t.Fatal("clear event fired more than once")
	}
}

func TestNativeClearDebouncerResetsOnRepeatedNil(t *testing.T) {
	debouncer := nativeClearDebouncer{duration: 20 * time.Millisecond}
	defer debouncer.Stop()

	debouncer.Schedule()
	time.Sleep(10 * time.Millisecond)
	debouncer.Schedule()

	select {
	case <-debouncer.C():
		t.Fatal("repeated nil did not reset debounce deadline")
	case <-time.After(12 * time.Millisecond):
	}
	select {
	case <-debouncer.C():
	case <-time.After(time.Second):
		t.Fatal("reset debounce deadline did not eventually fire")
	}
}
