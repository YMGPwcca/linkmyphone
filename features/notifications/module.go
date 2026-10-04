package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	client "github.com/YMGPwcca/linkmyphone/notifications"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	wire "github.com/YMGPwcca/linkmyphone/protocol/notifications"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

type Module struct {
	session  *phonehost.Session
	manifest kernel.Manifest
}

func New(session *phonehost.Session) (*Module, error) {
	if session == nil {
		return nil, errors.New("notifications module: phone host session is required")
	}
	manifest, err := Manifest()
	if err != nil {
		return nil, err
	}
	return &Module{session: session, manifest: manifest}, nil
}
func (m *Module) Manifest() kernel.Manifest                { return m.manifest }
func (m *Module) ValidateConfig(raw json.RawMessage) error { _, err := DecodeConfig(raw); return err }

func (m *Module) Start(ctx context.Context, raw json.RawMessage, reporter kernel.Reporter) (kernel.Instance, error) {
	cfg, err := DecodeConfig(raw)
	if err != nil {
		return nil, err
	}
	if m.session.Profile.Canonical() != dcgheaders.ProfilePhoneLink {
		return nil, errors.New("notifications module: full push requires a phonelink enrollment; existing CrossDevice identities cannot be reclassified")
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	native, err := client.NewDesktop(runCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	endpoint, err := m.session.Subscribe(m.manifest.ID, func(message relay.Received) bool {
		return message.Source == m.session.Target.ID && message.TransportMessageType == dcg.TransportMessageTypeApp
	}, phonehost.DefaultSubscriptionQueue)
	if err != nil {
		cancel()
		_ = native.Close()
		return nil, fmt.Errorf("notifications module: subscribe APP transport: %w", err)
	}
	reply := cfg.RemoteActions && native.SupportsActions() && client.ReplyAvailable(ctx)
	protocolClient, err := client.New(endpoint, native, client.Config{
		Target:         m.session.Target.ID,
		Info:           wire.LocalInfo{DisplayName: "LinkMyPhone", AppVersion: m.session.AppVersion, InstallationID: m.session.InstallationID, RingName: m.session.RingName},
		RequestTimeout: cfg.RequestTimeout(), RemoteActions: cfg.RemoteActions, ShowExisting: cfg.ShowExisting, ReplyEnabled: reply,
		OnEvent: func(message string, fields map[string]string) {
			kernel.Report(reporter, kernel.Event{ModuleID: m.manifest.ID, Level: "info", Message: message, Fields: fields})
		},
	})
	if err != nil {
		cancel()
		endpoint.Close()
		_ = native.Close()
		return nil, err
	}
	instance := &instance{moduleID: m.manifest.ID, cancel: cancel, endpoint: endpoint, done: make(chan struct{}), errors: make(chan error, 1), remoteActions: cfg.RemoteActions, actions: cfg.RemoteActions && native.SupportsActions(), reply: reply}
	go func() {
		defer close(instance.done)
		defer close(instance.errors)
		err := protocolClient.Run(runCtx)
		if err != nil && runCtx.Err() == nil {
			instance.errors <- err
		}
	}()
	status, err := protocolClient.Connect(ctx)
	if err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = instance.Stop(stopCtx)
		stopCancel()
		return nil, fmt.Errorf("notifications module: connect/reconcile: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cancel()
		endpoint.Close()
		<-instance.done
		return nil, err
	}
	select {
	case <-instance.done:
		return nil, errors.New("notifications module: client stopped during startup")
	default:
	}
	kernel.Report(reporter, kernel.Event{ModuleID: m.manifest.ID, Level: "info", Message: "notification APP session ready", Fields: map[string]string{"ecr": fmt.Sprint(status.ECR), "remote_actions": fmt.Sprint(cfg.RemoteActions), "reply": fmt.Sprint(reply)}})
	return instance, nil
}

type instance struct {
	moduleID                      string
	cancel                        context.CancelFunc
	endpoint                      *phonehost.Endpoint
	done                          chan struct{}
	errors                        chan error
	once                          sync.Once
	remoteActions, actions, reply bool
}

func (i *instance) Capabilities() []kernel.LiveCapability {
	ids := []string{"notifications.receive"}
	if i.remoteActions {
		ids = append(ids, "notifications.dismiss")
	}
	if i.actions {
		ids = append(ids, "notifications.actions")
	}
	if i.reply {
		ids = append(ids, "notifications.reply")
	}
	caps := make([]kernel.LiveCapability, 0, len(ids))
	for _, id := range ids {
		caps = append(caps, kernel.LiveCapability{ID: id, ContractVersion: "1.0.0", ProviderID: i.moduleID})
	}
	return caps
}
func (i *instance) Errors() <-chan error { return i.errors }
func (i *instance) Stop(ctx context.Context) error {
	i.once.Do(func() { i.cancel(); i.endpoint.Close() })
	select {
	case <-i.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
