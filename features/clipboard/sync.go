package clipboard

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	clipclient "github.com/YMGPwcca/phonelink-linux/clipboard"
	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

const remoteClipboardSettleWindow = 3 * time.Second

type remoteWriteEvent struct {
	size int
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
	remoteWrite      chan remoteWriteEvent
}

func newTrackedLocalClipboard(base clipclient.Local, initial string) *trackedLocalClipboard {
	return &trackedLocalClipboard{
		base:             base,
		lastExactHash:    sha256.Sum256([]byte(initial)),
		lastTrackingHash: clipboardTrackingHash(initial),
		initialized:      true,
		suppress:         make(map[[32]byte]time.Time),
		remoteWrite:      make(chan remoteWriteEvent, 8),
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
	}
	l.mu.Unlock()
	if err != nil {
		return err
	}

	select {
	case l.remoteWrite <- remoteWriteEvent{size: len([]byte(text))}:
	default:
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
				correlationID, err := client.PublishLocalTextGeneration(
					ctx,
					job.text,
					"",
					job.generation,
				)
				result := publishResult{
					generation:    job.generation,
					size:          len([]byte(job.text)),
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

func discardQueuedPublishes(queue chan publishJob) {
	for {
		select {
		case <-queue:
		default:
			return
		}
	}
}

func (i *instance) run(ctx context.Context, clientErr <-chan error) {
	defer close(i.done)
	defer close(i.errors)

	ticker := time.NewTicker(i.cfg.PollInterval())
	defer ticker.Stop()

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

		case result, ok := <-i.publishResults:
			if !ok {
				return
			}
			if errors.Is(result.err, clipclient.ErrPublicationSuperseded) {
				continue
			}
			if result.err != nil {
				i.fail(fmt.Errorf("clipboard module: publish Linux clipboard: %w", result.err))
				return
			}
			kernel.Report(i.reporter, kernel.Event{
				ModuleID: i.moduleID,
				Level:    "sync",
				Message:  "Linux -> phone published text",
				Fields: map[string]string{
					"bytes":          fmt.Sprint(result.size),
					"correlation_id": shortID(result.correlationID),
					"generation":     fmt.Sprint(result.generation),
				},
			})

		case event := <-i.local.remoteWrite:
			// The phone publication reserved the shared generation before its
			// CONTENT pull. Drop any local jobs that were still waiting; an
			// already in-flight older job is rejected by the client's floor.
			discardQueuedPublishes(i.publishQueue)
			generation := i.client.CurrentGeneration()
			kernel.Report(i.reporter, kernel.Event{
				ModuleID: i.moduleID,
				Level:    "sync",
				Message:  "phone -> Linux applied text",
				Fields: map[string]string{
					"bytes":      fmt.Sprint(event.size),
					"generation": fmt.Sprint(generation),
				},
			})

		case <-ticker.C:
			text, err := i.local.ReadText(ctx)
			if err != nil {
				i.fail(fmt.Errorf("clipboard module: poll Linux clipboard: %w", err))
				return
			}
			if !i.local.MarkIfChanged(text) {
				continue
			}
			generation := i.client.ReserveLocalGeneration()
			queueLatestPublish(i.publishQueue, publishJob{
				generation: generation,
				text:       text,
			})
		}
	}
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
