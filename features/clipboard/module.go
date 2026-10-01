package clipboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	clipclient "github.com/YMGPwcca/phonelink-linux/clipboard"
	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
	"github.com/YMGPwcca/phonelink-linux/runtime/phonehost"
)

const defaultFeatureOffTimeout = time.Second

type Module struct {
	session  *phonehost.Session
	manifest kernel.Manifest
}

func New(session *phonehost.Session) (*Module, error) {
	if session == nil {
		return nil, errors.New("clipboard module: phone host session is required")
	}
	manifest, err := Manifest()
	if err != nil {
		return nil, err
	}
	return &Module{session: session, manifest: manifest}, nil
}

func (m *Module) Manifest() kernel.Manifest {
	return m.manifest
}

func (m *Module) ValidateConfig(raw json.RawMessage) error {
	_, err := DecodeConfig(raw)
	return err
}

func (m *Module) Start(
	ctx context.Context,
	raw json.RawMessage,
	reporter kernel.Reporter,
) (kernel.Instance, error) {
	cfg, err := DecodeConfig(raw)
	if err != nil {
		return nil, err
	}

	native, err := clipclient.DetectNativeLocal()
	if err != nil {
		return nil, err
	}
	initialText, err := native.ReadText(ctx)
	if err != nil {
		return nil, fmt.Errorf("clipboard module: read initial Linux clipboard: %w", err)
	}

	local := newTrackedLocalClipboard(native, initialText)
	endpoint, err := m.session.Subscribe(
		m.manifest.ID,
		matcherForTarget(m.session.Target.ID),
		phonehost.DefaultSubscriptionQueue,
	)
	if err != nil {
		return nil, fmt.Errorf("clipboard module: subscribe transport: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	remoteApplied := make(chan clipclient.RemoteApplyEvent, 8)
	client := clipclient.New(endpoint, local, clipclient.Config{
		Target:          m.session.Target.ID,
		SelfDcgClientID: m.session.SelfDcgClientID,
		RequestTimeout:  cfg.RequestTimeout(),
		OnRemoteApplied: func(event clipclient.RemoteApplyEvent) {
			queueLatestRemoteApply(remoteApplied, event)
		},
	})
	instance := &instance{
		moduleID:          m.manifest.ID,
		cfg:               cfg,
		reporter:          reporter,
		client:            client,
		local:             local,
		watcher:           native,
		endpoint:          endpoint,
		remoteApplied:     remoteApplied,
		cancel:            cancel,
		done:              make(chan struct{}),
		errors:            make(chan error, 1),
		requestTimeout:    cfg.RequestTimeout(),
		featureOffTimeout: defaultFeatureOffTimeout,
	}

	clientErr := make(chan error, 1)
	go func() {
		clientErr <- client.Run(runCtx)
	}()

	kernel.Report(reporter, kernel.Event{
		ModuleID: m.manifest.ID,
		Level:    "info",
		Message:  "native clipboard ready",
		Fields: map[string]string{
			"backend": native.BackendName(),
		},
	})

	featureCtx, cancelFeature := context.WithTimeout(ctx, cfg.RequestTimeout())
	status, featureErr := client.PushFeatureState(featureCtx, clipproto.RequestFeatureOn)
	cancelFeature()
	if featureErr != nil {
		kernel.Report(reporter, kernel.Event{
			ModuleID: m.manifest.ID,
			Level:    "warning",
			Message:  "FEATURE_ON synchronization failed",
			Fields:   map[string]string{"error": featureErr.Error()},
		})
	} else {
		kernel.Report(reporter, kernel.Event{
			ModuleID: m.manifest.ID,
			Level:    "info",
			Message:  "FEATURE_ON synchronized",
			Fields:   map[string]string{"status": fmt.Sprint(status)},
		})
	}

	if cfg.PublishInitial {
		generation := client.ReserveLocalGeneration()
		correlationID, err := client.PublishLocalTextGeneration(
			ctx,
			initialText,
			"",
			generation,
		)
		if err != nil {
			cancel()
			endpoint.Close()
			return nil, fmt.Errorf("clipboard module: publish initial clipboard: %w", err)
		}
		kernel.Report(reporter, kernel.Event{
			ModuleID: m.manifest.ID,
			Level:    "sync",
			Message:  "Linux -> phone published text",
			Fields: map[string]string{
				"bytes":          fmt.Sprint(len([]byte(initialText))),
				"correlation_id": shortID(correlationID),
			},
		})
	}
	if err := ctx.Err(); err != nil {
		cancel()
		endpoint.Close()
		return nil, fmt.Errorf("clipboard module: start canceled: %w", err)
	}

	instance.publishQueue, instance.publishResults = startClipboardPublisher(runCtx, client)
	go instance.run(runCtx, clientErr)

	return instance, nil
}

type instance struct {
	moduleID string
	cfg      Config
	reporter kernel.Reporter

	client        *clipclient.Client
	local         *trackedLocalClipboard
	watcher       localTextWatcher
	endpoint      *phonehost.Endpoint
	remoteApplied chan clipclient.RemoteApplyEvent

	publishQueue   chan publishJob
	publishResults <-chan publishResult

	requestTimeout    time.Duration
	featureOffTimeout time.Duration

	cancel   context.CancelFunc
	stopOnce sync.Once
	done     chan struct{}
	errors   chan error
}

func (i *instance) Capabilities() []kernel.LiveCapability {
	return []kernel.LiveCapability{
		{ID: "clipboard.text.read", ContractVersion: "1.0.0", ProviderID: i.moduleID},
		{ID: "clipboard.text.write", ContractVersion: "1.0.0", ProviderID: i.moduleID},
		{ID: "clipboard.text.bidirectional", ContractVersion: "1.0.0", ProviderID: i.moduleID},
	}
}

func (i *instance) Errors() <-chan error {
	return i.errors
}

func (i *instance) Stop(ctx context.Context) error {
	i.stopOnce.Do(func() {
		running := true
		select {
		case <-i.done:
			running = false
		default:
		}

		// FEATURE_OFF is advisory shutdown synchronization, not a prerequisite
		// for reclaiming local resources. Never let an unresponsive phone spend
		// the module's entire Stop deadline before cancellation can begin.
		if running && ctx.Err() == nil {
			timeout := i.featureOffTimeout
			if timeout <= 0 || timeout > i.requestTimeout {
				timeout = i.requestTimeout
			}
			if deadline, ok := ctx.Deadline(); ok {
				remaining := time.Until(deadline)
				if remaining < timeout {
					timeout = remaining
				}
			}
			if timeout > 0 {
				featureCtx, cancel := context.WithTimeout(ctx, timeout)
				if _, err := i.client.PushFeatureState(featureCtx, clipproto.RequestFeatureOff); err != nil &&
					!errors.Is(err, context.Canceled) &&
					!errors.Is(err, context.DeadlineExceeded) {
					kernel.Report(i.reporter, kernel.Event{
						ModuleID: i.moduleID,
						Level:    "warning",
						Message:  "FEATURE_OFF synchronization failed",
						Fields:   map[string]string{"error": err.Error()},
					})
				}
				cancel()
			}
		}

		i.cancel()
		if i.endpoint != nil {
			i.endpoint.Close()
		}
	})

	select {
	case <-i.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
