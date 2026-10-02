package clipboard

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
)

const (
	remoteClipboardSettleWindow  = 3 * time.Second
	nativeClipboardClearDebounce = 100 * time.Millisecond
)

type localTextWatcher interface {
	SupportsWatch() bool
	WatchText(context.Context, func(clipclient.NativeTextEvent) error) error
}

type nativeClearDebouncer struct {
	duration time.Duration
	timer    *time.Timer
	channel  <-chan time.Time
	pending  bool
}

func (d *nativeClearDebouncer) Schedule() {
	if d.duration <= 0 {
		d.duration = nativeClipboardClearDebounce
	}
	if d.timer == nil {
		d.timer = time.NewTimer(d.duration)
	} else {
		d.stopTimer()
		d.timer.Reset(d.duration)
	}
	d.pending = true
	d.channel = d.timer.C
}

func (d *nativeClearDebouncer) Cancel() {
	d.pending = false
	d.channel = nil
	d.stopTimer()
}

func (d *nativeClearDebouncer) C() <-chan time.Time {
	return d.channel
}

func (d *nativeClearDebouncer) Fire() bool {
	if !d.pending {
		d.channel = nil
		return false
	}
	d.pending = false
	d.channel = nil
	return true
}

func (d *nativeClearDebouncer) Stop() {
	d.Cancel()
}

func (d *nativeClearDebouncer) stopTimer() {
	if d.timer == nil {
		return
	}
	if !d.timer.Stop() {
		select {
		case <-d.timer.C:
		default:
		}
	}
}

type trackedLocalClipboard struct {
	base clipclient.Local

	writeMu sync.Mutex
	mu      sync.Mutex

	lastExactHash    [32]byte
	lastTrackingHash [32]byte
	initialized      bool
	applying         bool
	suppress         map[[32]byte]time.Time
}

func newTrackedLocalClipboard(base clipclient.Local, initial string) *trackedLocalClipboard {
	return &trackedLocalClipboard{
		base:             base,
		lastExactHash:    sha256.Sum256([]byte(initial)),
		lastTrackingHash: clipboardTrackingHash(initial),
		initialized:      true,
		suppress:         make(map[[32]byte]time.Time),
	}
}

func (l *trackedLocalClipboard) ReadText(ctx context.Context) (string, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return l.base.ReadText(ctx)
}

func (l *trackedLocalClipboard) WriteText(ctx context.Context, text string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	nextExactHash := sha256.Sum256([]byte(text))
	nextTrackingHash := clipboardTrackingHash(text)
	now := time.Now()

	l.mu.Lock()
	l.pruneSuppressedLocked(now)
	l.applying = true
	if l.initialized {
		l.suppress[l.lastTrackingHash] = now.Add(remoteClipboardSettleWindow)
	}
	l.suppress[nextTrackingHash] = now.Add(remoteClipboardSettleWindow)
	l.mu.Unlock()

	err := l.base.WriteText(ctx, text)

	l.mu.Lock()
	l.applying = false
	if err == nil {
		l.lastExactHash = nextExactHash
		l.lastTrackingHash = nextTrackingHash
		l.initialized = true
	} else {
		delete(l.suppress, nextTrackingHash)
	}
	l.mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func (l *trackedLocalClipboard) MarkIfChanged(text string) bool {
	exactHash := sha256.Sum256([]byte(text))
	trackingHash := clipboardTrackingHash(text)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneSuppressedLocked(now)

	if l.applying {
		return false
	}
	if until, ok := l.suppress[trackingHash]; ok && now.Before(until) {
		l.lastExactHash = exactHash
		l.lastTrackingHash = trackingHash
		l.initialized = true
		delete(l.suppress, trackingHash)
		return false
	}
	if l.initialized && l.lastExactHash == exactHash {
		return false
	}
	l.lastExactHash = exactHash
	l.lastTrackingHash = trackingHash
	l.initialized = true
	return true
}

func clipboardTrackingHash(text string) [32]byte {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	return sha256.Sum256([]byte(normalized))
}

func (l *trackedLocalClipboard) pruneSuppressedLocked(now time.Time) {
	for hash, until := range l.suppress {
		if !now.Before(until) {
			delete(l.suppress, hash)
		}
	}
}

type publishJob struct {
	generation uint64
	text       string
	content    *clipclient.Content
}

type publishResult struct {
	generation    uint64
	size          int
	correlationID string
	err           error
}

func startClipboardPublisher(
	ctx context.Context,
	client *clipclient.Client,
) (chan publishJob, <-chan publishResult) {
	queue := make(chan publishJob, 1)
	results := make(chan publishResult, 4)

	go func() {
		defer close(results)
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-queue:
				content := clipclient.TextContent(job.text)
				if job.content != nil {
					content = *job.content
				}
				correlationID, err := client.PublishLocalContentGeneration(ctx, content, "", job.generation)
				result := publishResult{
					generation:    job.generation,
					size:          content.Size(),
					correlationID: correlationID,
					err:           err,
				}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return queue, results
}

func queueLatestPublish(queue chan publishJob, job publishJob) {
	select {
	case queue <- job:
		return
	default:
	}
	select {
	case <-queue:
	default:
	}
	select {
	case queue <- job:
	default:
	}
}

func discardQueuedPublishesBefore(queue chan publishJob, generation uint64) {
	select {
	case job := <-queue:
		if job.generation >= generation {
			select {
			case queue <- job:
			default:
			}
		}
	default:
	}
}

func queueLatestNativeTextEvent(
	queue chan clipclient.NativeTextEvent,
	event clipclient.NativeTextEvent,
) {
	select {
	case queue <- event:
		return
	default:
	}
	select {
	case <-queue:
	default:
	}
	select {
	case queue <- event:
	default:
	}
}

func queueLatestRemoteApply(
	queue chan clipclient.RemoteApplyEvent,
	event clipclient.RemoteApplyEvent,
) {
	select {
	case queue <- event:
		return
	default:
	}
	select {
	case <-queue:
	default:
	}
	select {
	case queue <- event:
	default:
	}
}

func (i *instance) run(ctx context.Context, clientErr <-chan error) {
	defer close(i.done)
	defer close(i.errors)
	defer i.cancel()
	if i.endpoint != nil {
		defer i.endpoint.Close()
	}

	localEvents := make(chan clipclient.NativeTextEvent, 1)
	var watchErr <-chan error
	var pollTicker *time.Ticker
	var pollC <-chan time.Time
	clearDebouncer := nativeClearDebouncer{
		duration: nativeClipboardClearDebounce,
	}

	startPolling := func(reason error) {
		if pollTicker != nil {
			return
		}
		pollTicker = time.NewTicker(i.cfg.PollInterval())
		pollC = pollTicker.C
		fields := map[string]string{
			"mode":          "poll",
			"poll_interval": i.cfg.PollInterval().String(),
		}
		if reason != nil {
			fields["watch_error"] = reason.Error()
			kernel.Report(i.reporter, kernel.Event{
				ModuleID: i.moduleID,
				Level:    "warning",
				Message:  "native clipboard watch unavailable; using polling fallback",
				Fields:   fields,
			})
			return
		}
		kernel.Report(i.reporter, kernel.Event{
			ModuleID: i.moduleID,
			Level:    "info",
			Message:  "local clipboard observer ready",
			Fields:   fields,
		})
	}
	defer func() {
		if pollTicker != nil {
			pollTicker.Stop()
		}
		clearDebouncer.Stop()
	}()

	if i.watcher != nil && i.watcher.SupportsWatch() {
		watchResult := make(chan error, 1)
		watchErr = watchResult
		go func() {
			watchResult <- i.watcher.WatchText(
				ctx,
				func(event clipclient.NativeTextEvent) error {
					queueLatestNativeTextEvent(localEvents, event)
					return nil
				},
			)
		}()
		kernel.Report(i.reporter, kernel.Event{
			ModuleID: i.moduleID,
			Level:    "info",
			Message:  "local clipboard observer ready",
			Fields: map[string]string{
				"mode": "wl-paste-watch",
			},
		})
	} else {
		startPolling(nil)
	}

	for {
		select {
		case <-ctx.Done():
			return

		case err := <-clientErr:
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			i.fail(err)
			return

		case err := <-watchErr:
			watchErr = nil
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}
			if err == nil {
				err = errors.New("native clipboard watcher exited unexpectedly")
			}
			startPolling(err)

		case event := <-localEvents:
			if event.State == "nil" {
				// A Wayland selection handoff can transiently publish a NULL
				// offer between the old owner disappearing and the new owner
				// advertising data. Delay an empty publication briefly so a
				// following data/sensitive event can supersede that transient.
				clearDebouncer.Schedule()
				continue
			}
			clearDebouncer.Cancel()
			i.observeLocalText(event.Text)

		case <-clearDebouncer.C():
			if clearDebouncer.Fire() {
				if i.rich {
					content, err := i.local.ReadContent(ctx)
					if err == nil {
						i.observeLocalContent(content)
					} else if !errors.Is(err, clipclient.ErrUnsupportedContent) && !errors.Is(err, clipclient.ErrContentTooLarge) && !errors.Is(err, clipclient.ErrContentUnavailable) {
						i.fail(err)
						return
					}
				} else {
					i.observeLocalText("")
				}
			}

		case result, ok := <-i.publishResults:
			if !ok {
				return
			}
			if errors.Is(result.err, clipclient.ErrPublicationSuperseded) {
				continue
			}
			if result.err != nil && (errors.Is(result.err, clipclient.ErrContentTooLarge) || errors.Is(result.err, clipclient.ErrUnsupportedContent)) {
				i.reportSkip(result.err)
				continue
			}
			if result.err != nil {
				i.fail(fmt.Errorf("clipboard module: publish Linux clipboard: %w", result.err))
				return
			}
			kernel.Report(i.reporter, kernel.Event{
				ModuleID: i.moduleID,
				Level:    "sync",
				Message:  "Linux -> phone published clipboard",
				Fields: map[string]string{
					"bytes":          fmt.Sprint(result.size),
					"correlation_id": shortID(result.correlationID),
					"generation":     fmt.Sprint(result.generation),
				},
			})

		case event := <-i.remoteApplied:
			// The protocol client reserved this generation when the phone
			// publication arrived. Remove only older queued local work; a local
			// copy observed after this phone event has a higher generation and
			// must survive.
			discardQueuedPublishesBefore(i.publishQueue, event.Generation)
			kernel.Report(i.reporter, kernel.Event{
				ModuleID: i.moduleID,
				Level:    "sync",
				Message:  "phone -> Linux applied clipboard",
				Fields: map[string]string{
					"bytes":      fmt.Sprint(event.Bytes),
					"mime_type":  event.MIME,
					"generation": fmt.Sprint(event.Generation),
				},
			})

		case <-pollC:
			content, err := i.local.ReadContent(ctx)
			if err != nil {
				if errors.Is(err, clipclient.ErrUnsupportedContent) || errors.Is(err, clipclient.ErrContentTooLarge) || errors.Is(err, clipclient.ErrContentUnavailable) {
					clearDebouncer.Cancel()
					if !i.localUnavailable {
						i.localUnavailable = true
						generation := i.client.ReserveLocalGeneration()
						discardQueuedPublishesBefore(i.publishQueue, generation)
						i.reportSkip(err)
					}
					continue
				}
				i.fail(fmt.Errorf("clipboard module: poll Linux clipboard: %w", err))
				return
			}
			if i.localUnavailable {
				i.localUnavailable = false
				i.local.mu.Lock()
				i.local.initialized = false
				i.local.mu.Unlock()
			}
			if content.Type == proto.ItemTextPlain && content.Text == "" && i.rich {
				if !clearDebouncer.pending {
					clearDebouncer.Schedule()
				}
				continue
			}
			clearDebouncer.Cancel()
			i.observeLocalContent(content)
		}
	}
}

func (i *instance) observeLocalText(text string) {
	if !i.local.MarkIfChanged(text) {
		return
	}
	generation := i.client.ReserveLocalGeneration()
	queueLatestPublish(i.publishQueue, publishJob{
		generation: generation,
		text:       text,
	})
}

func (i *instance) fail(err error) {
	select {
	case i.errors <- err:
	default:
	}
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:8] + "..." + value[len(value)-4:]
}

func (i *instance) reportSkip(err error) {
	kernel.Report(i.reporter, kernel.Event{ModuleID: i.moduleID, Level: "warning", Message: "clipboard content skipped", Fields: map[string]string{"reason": err.Error()}})
}
