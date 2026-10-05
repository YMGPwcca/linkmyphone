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
			text, details := notificationLogText(message, fields)
			kernel.Report(reporter, kernel.Event{ModuleID: m.manifest.ID, Level: "info", Message: text, Fields: details})
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
	mode := "Phone actions are off."
	if cfg.RemoteActions {
		mode = "You can dismiss them from the desktop."
		if native.SupportsActions() {
			mode = "You can dismiss them or use app buttons."
		}
		if reply {
			mode = "You can dismiss them, use app buttons or reply."
		}
	}
	readyMessage := "Connected to phone notifications. " + mode
	if !status.ECR {
		readyMessage += " Synced existing items."
	}
	kernel.Report(reporter, kernel.Event{ModuleID: m.manifest.ID, Level: "info", Message: readyMessage})
	return instance, nil
}

// notificationLogText renders feature events for the journal. The client keeps
// structured fields so callers can correlate a desktop action, phone response
// and later removal without logging Android notification keys or reply text.
func notificationLogText(message string, fields map[string]string) (string, map[string]string) {
	ref := fields["record_ref"]
	switch message {
	case "desktop notification dismissed":
		if fields["remote_request"] == "true" {
			return fmt.Sprintf("Dismissed notification #%s on desktop. Asking the phone to remove it.", ref), nil
		}
		return fmt.Sprintf("Dismissed notification #%s on desktop. Nothing was sent to the phone.", ref), nil
	case "phone accepted notification request":
		switch fields["operation"] {
		case "dismiss":
			return fmt.Sprintf("Phone accepted dismissal of notification #%s.", ref), nil
		case "clear":
			if fields["count"] == "1" {
				return "Phone accepted a request to clear one notification.", nil
			}
			return fmt.Sprintf("Phone accepted a request to clear %s notifications.", fields["count"]), nil
		case "launch":
			return fmt.Sprintf("Phone accepted a request to open notification #%s.", ref), nil
		case "button":
			return fmt.Sprintf("Phone accepted a button press on notification #%s.", ref), nil
		case "reply":
			return fmt.Sprintf("Phone accepted a reply to notification #%s.", ref), nil
		}
	case "notification state synchronized":
		items := fields["items"]
		if removedRef := fields["removed_record_ref"]; removedRef != "" {
			return fmt.Sprintf("Removed notification #%s after a phone update. %s left.", removedRef, items), nil
		}
		if removed := fields["removed"]; removed != "" {
			return fmt.Sprintf("Removed %s notifications after a phone update. %s left.", removed, items), nil
		}
		if fields["operations"] == "1" {
			return fmt.Sprintf("Phone notifications updated. %s in sync.", items), nil
		}
		return fmt.Sprintf("Phone sent %s notification changes. %s in sync.", fields["operations"], items), nil
	case "desktop notification action failed":
		return "Could not complete a notification action: " + fields["reason"], nil
	case "notification reply failed":
		return "Could not send the reply: " + fields["reason"], nil
	case "malformed APP envelope":
		return "Could not read a phone message: " + fields["reason"], nil
	case "notification push lacks request correlation":
		return "Ignored a phone notification update without a request ID.", nil
	case "malformed notification batch rejected":
		return "Rejected a phone notification update with invalid data.", nil
	case "notification batch failed":
		return "Could not apply a phone notification update: " + fields["reason"], nil
	case "notification state budget evicted oldest item":
		return "Notification cache is full. Dropped the oldest item on Linux; the phone was not changed.", nil
	case "desktop notification service reset":
		if fields["available"] == "true" {
			return "Desktop notifications are available again. Restored visible items.", nil
		}
		return "Desktop notifications are unavailable. Keeping phone items until they return.", nil
	}
	return message, fields
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
