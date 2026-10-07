package notifications

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	notificationService = "org.freedesktop.Notifications"
	notificationPath    = dbus.ObjectPath("/org/freedesktop/Notifications")
	notificationIface   = "org.freedesktop.Notifications"
	busPath             = dbus.ObjectPath("/org/freedesktop/DBus")
	busIface            = "org.freedesktop.DBus"
)

const (
	nativeSignalQueue = 64
	nativeEventQueue  = 64
	maxIconBytes      = 2 << 20
	maxIconPixels     = 4_000_000
	maxActions        = 32
	maxActionBytes    = 8 << 10
	maxTextBytes      = 256 << 10
)

type desktopBackend struct {
	conn   *dbus.Conn
	object dbus.BusObject

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	signals chan *dbus.Signal
	events  chan NativeEvent

	mu             sync.RWMutex
	closed         bool
	owner          string
	supportsAction bool
	bodyMarkup     bool
	match          []dbus.MatchOption
	ownerMatch     []dbus.MatchOption
	closeOnce      sync.Once
}

// NewDesktop connects to the current user's notification service. It does not
// construct a fallback backend: callers receive an error when a real service
// is unavailable.
func NewDesktop(ctx context.Context) (NativeBackend, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backendCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stopStartupCancel := context.AfterFunc(ctx, cancel)
	defer stopStartupCancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(backendCtx))
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("notifications: connect session bus: %w", err)
	}
	fail := func(err error) (NativeBackend, error) {
		cancel()
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	owner, err := notificationOwner(ctx, conn)
	if err != nil {
		return fail(fmt.Errorf("notifications: find service owner: %w", err))
	}
	capabilities, err := notificationCapabilities(ctx, conn)
	if err != nil {
		return fail(fmt.Errorf("notifications: query capabilities: %w", err))
	}

	// The feature closes its rendered IDs before Close tears down this bus.
	// Constructor cancellation bounds startup, not the backend lifetime.
	b := &desktopBackend{
		conn:           conn,
		object:         conn.Object(notificationService, notificationPath),
		ctx:            backendCtx,
		cancel:         cancel,
		done:           make(chan struct{}),
		signals:        make(chan *dbus.Signal, nativeSignalQueue),
		events:         make(chan NativeEvent, nativeEventQueue),
		owner:          owner,
		supportsAction: containsCapability(capabilities, "actions"),
		bodyMarkup:     containsCapability(capabilities, "body-markup"),
	}
	b.match = notificationMatch(owner)
	b.ownerMatch = ownerChangedMatch()

	if err := conn.AddMatchSignalContext(ctx, b.match...); err != nil {
		cancel()
		return fail(fmt.Errorf("notifications: subscribe to service signals: %w", err))
	}
	if err := conn.AddMatchSignalContext(ctx, b.ownerMatch...); err != nil {
		removeCtx, removeCancel := context.WithCancel(context.Background())
		_ = conn.RemoveMatchSignalContext(removeCtx, b.match...)
		removeCancel()
		cancel()
		return fail(fmt.Errorf("notifications: subscribe to owner changes: %w", err))
	}
	conn.Signal(b.signals)
	if !stopStartupCancel() || ctx.Err() != nil {
		return fail(ctx.Err())
	}
	go b.run()
	return b, nil
}

func notificationOwner(ctx context.Context, conn *dbus.Conn) (string, error) {
	var owner string
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, notificationService)
	if err := call.Store(&owner); err != nil {
		return "", err
	}
	if owner == "" {
		return "", errors.New("service has no owner")
	}
	return owner, nil
}

func notificationCapabilities(ctx context.Context, conn *dbus.Conn) ([]string, error) {
	var capabilities []string
	object := conn.Object(notificationService, notificationPath)
	call := object.CallWithContext(ctx, notificationIface+".GetCapabilities", 0)
	if err := call.Store(&capabilities); err != nil {
		return nil, err
	}
	return capabilities, nil
}

func containsCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func notificationMatch(owner string) []dbus.MatchOption {
	return []dbus.MatchOption{
		dbus.WithMatchObjectPath(notificationPath),
		dbus.WithMatchInterface(notificationIface),
		dbus.WithMatchSender(owner),
	}
}

func ownerChangedMatch() []dbus.MatchOption {
	return []dbus.MatchOption{
		dbus.WithMatchInterface(busIface),
		dbus.WithMatchSender(busIface),
		dbus.WithMatchObjectPath(busPath),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, notificationService),
	}
}

func (b *desktopBackend) run() {
	defer close(b.done)
	defer close(b.events)
	defer b.conn.RemoveSignal(b.signals)
	defer b.conn.Close()
	defer b.cancel()
	defer b.markClosed()
	defer b.removeMatches()

	for {
		select {
		case <-b.ctx.Done():
			return
		case signal, ok := <-b.signals:
			if !ok {
				if b.ctx.Err() == nil {
					b.emit(NativeEvent{Kind: NativeFailure, Err: errors.New("notifications: D-Bus signal connection closed")})
				}
				return
			}
			if signal == nil {
				return
			}
			b.handleSignal(signal)
		}
	}
}

func (b *desktopBackend) markClosed() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

func (b *desktopBackend) removeMatches() {
	removeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = b.conn.RemoveMatchSignalContext(removeCtx, b.ownerMatch...)
	b.mu.RLock()
	match := append([]dbus.MatchOption(nil), b.match...)
	b.mu.RUnlock()
	if len(match) != 0 {
		_ = b.conn.RemoveMatchSignalContext(removeCtx, match...)
	}
}

func (b *desktopBackend) handleSignal(signal *dbus.Signal) {
	if signal.Path == busPath && signal.Name == busIface+".NameOwnerChanged" && signal.Sender == busIface {
		b.handleOwnerChanged(signal)
		return
	}
	if signal.Path != notificationPath || signal.Sender == "" {
		return
	}
	b.mu.RLock()
	owner := b.owner
	b.mu.RUnlock()
	if owner == "" || signal.Sender != owner {
		return
	}
	switch signal.Name {
	case notificationIface + ".NotificationClosed":
		if len(signal.Body) != 2 {
			return
		}
		id, okID := signal.Body[0].(uint32)
		reason, okReason := signal.Body[1].(uint32)
		if !okID || !okReason {
			return
		}
		b.emit(NativeEvent{Kind: NativeClosed, ID: id, Reason: reason})
	case notificationIface + ".ActionInvoked":
		if len(signal.Body) != 2 {
			return
		}
		id, okID := signal.Body[0].(uint32)
		action, okAction := signal.Body[1].(string)
		if !okID || !okAction || action == "" || len(action) > maxActionBytes {
			return
		}
		b.emit(NativeEvent{Kind: NativeAction, ID: id, Action: action})
	case notificationIface + ".ActivationToken":
		if len(signal.Body) != 2 {
			return
		}
		id, okID := signal.Body[0].(uint32)
		token, okToken := signal.Body[1].(string)
		if !okID || !okToken || len(token) > maxActionBytes {
			return
		}
		b.emit(NativeEvent{Kind: NativeActivationToken, ID: id, ActivationToken: token})
	}
}

func (b *desktopBackend) handleOwnerChanged(signal *dbus.Signal) {
	if len(signal.Body) != 3 {
		return
	}
	name, okName := signal.Body[0].(string)
	oldOwner, okOld := signal.Body[1].(string)
	newOwner, okNew := signal.Body[2].(string)
	if !okName || !okOld || !okNew || name != notificationService || oldOwner == newOwner {
		return
	}
	b.mu.Lock()
	if b.closed || b.owner == newOwner {
		b.mu.Unlock()
		return
	}
	previous := b.owner
	b.owner = newOwner
	oldMatch := b.match
	if newOwner == "" {
		b.match = nil
		b.supportsAction = false
		b.bodyMarkup = false
	} else {
		b.match = notificationMatch(newOwner)
	}
	b.mu.Unlock()

	removeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if previous != "" {
		if err := b.conn.RemoveMatchSignalContext(removeCtx, oldMatch...); err != nil {
			b.emit(NativeEvent{Kind: NativeFailure, Err: errors.New("notifications: failed to remove old service signal match")})
		}
	}
	if newOwner != "" {
		b.mu.RLock()
		newMatch := append([]dbus.MatchOption(nil), b.match...)
		b.mu.RUnlock()
		if err := b.conn.AddMatchSignalContext(removeCtx, newMatch...); err != nil {
			b.emit(NativeEvent{Kind: NativeFailure, Err: errors.New("notifications: failed to add new service signal match")})
		}
		capabilities, err := notificationCapabilities(removeCtx, b.conn)
		if err != nil {
			b.emit(NativeEvent{Kind: NativeFailure, Err: errors.New("notifications: failed to query service capabilities")})
		} else {
			b.mu.Lock()
			b.supportsAction = containsCapability(capabilities, "actions")
			b.bodyMarkup = containsCapability(capabilities, "body-markup")
			b.mu.Unlock()
		}
	}
	cancel()
	if newOwner == "" {
		b.emit(NativeEvent{Kind: NativeUnavailable})
	} else {
		b.emit(NativeEvent{Kind: NativeReset})
	}
}

func (b *desktopBackend) emit(event NativeEvent) {
	select {
	case b.events <- event:
	default:
		// Losing an action/close would leave desktop and phone state divergent.
		// Make overflow explicit; the feature can revoke its capabilities.
		select {
		case <-b.events:
		default:
		}
		select {
		case b.events <- NativeEvent{Kind: NativeFailure, Err: errors.New("notifications: native event queue overflow")}:
		default:
		}
	}
}

func (b *desktopBackend) Notify(ctx context.Context, notification DesktopNotification) (uint32, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateNotification(notification); err != nil {
		return 0, err
	}
	hints := make(map[string]dbus.Variant, 3)
	if notification.Silent {
		hints["suppress-sound"] = dbus.MakeVariant(true)
	}
	if notification.Resident {
		hints["resident"] = dbus.MakeVariant(true)
	}
	if len(notification.Icon) != 0 {
		icon, err := decodeNotificationIcon(notification.Icon)
		if err != nil {
			return 0, err
		}
		hints["image-data"] = dbus.MakeVariant(icon)
	}
	actions, err := notificationActions(notification.Actions, b.SupportsActions())
	if err != nil {
		return 0, err
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return 0, errNativeClosed
	}
	object := b.object
	body := notification.Body
	if b.bodyMarkup {
		body = html.EscapeString(body)
	}
	b.mu.RUnlock()
	var id uint32
	call := object.CallWithContext(ctx, notificationIface+".Notify", 0,
		notification.AppName, notification.ReplaceID, "", notification.Summary,
		body, actions, hints, int32(-1))
	if err := call.Store(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (b *desktopBackend) CloseNotification(ctx context.Context, id uint32) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == 0 {
		return errors.New("notifications: notification id must be non-zero")
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return errNativeClosed
	}
	object := b.object
	b.mu.RUnlock()
	return object.CallWithContext(ctx, notificationIface+".CloseNotification", 0, id).Store()
}

func (b *desktopBackend) Events() <-chan NativeEvent { return b.events }

func (b *desktopBackend) SupportsActions() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.supportsAction && !b.closed
}

func (b *desktopBackend) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		b.cancel()
		// Closing the connection also wakes an in-flight D-Bus call. The run
		// goroutine owns final signal/match cleanup and closes Events().
		_ = b.conn.Close()
	})
	<-b.done
	return nil
}

func validateNotification(notification DesktopNotification) error {
	if notification.AppName == "" || !validNativeText(notification.AppName, maxTextBytes) ||
		!validNativeText(notification.Summary, maxTextBytes) || !validNativeText(notification.Body, maxTextBytes) {
		return errInvalidNotification
	}
	if len(notification.Actions) > maxActions {
		return errInvalidNotification
	}
	for _, action := range notification.Actions {
		if action.ID == "" || action.Label == "" || !validNativeText(action.ID, maxActionBytes) || !validNativeText(action.Label, maxActionBytes) {
			return errInvalidNotification
		}
	}
	if len(notification.Icon) > maxIconBytes {
		return errInvalidNotification
	}
	return nil
}

func validNativeText(value string, maxBytes int) bool {
	return len(value) <= maxBytes && strings.IndexByte(value, 0) < 0
}

func notificationActions(actions []DesktopAction, supported bool) ([]string, error) {
	if !supported {
		return nil, nil
	}
	if len(actions) > maxActions {
		return nil, errInvalidNotification
	}
	result := make([]string, 0, len(actions)*2)
	for _, action := range actions {
		if action.ID == "" || action.Label == "" || len(action.ID) > maxActionBytes || len(action.Label) > maxActionBytes {
			return nil, errInvalidNotification
		}
		result = append(result, action.ID, action.Label)
	}
	return result, nil
}

type notificationImageData struct {
	Width         int32
	Height        int32
	RowStride     int32
	HasAlpha      bool
	BitsPerSample int32
	Channels      int32
	Data          []byte
}

func decodeNotificationIcon(encoded []byte) (notificationImageData, error) {
	if len(encoded) == 0 || len(encoded) > maxIconBytes {
		return notificationImageData{}, errors.New("notifications: icon exceeds size limit")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return notificationImageData{}, errors.New("notifications: icon is not a supported JPEG")
	}
	width, height := config.Width, config.Height
	if width <= 0 || height <= 0 || int64(width)*int64(height) > maxIconPixels {
		return notificationImageData{}, errors.New("notifications: icon exceeds pixel limit")
	}
	if width > math.MaxInt32/4 || height > math.MaxInt32/4 {
		return notificationImageData{}, errors.New("notifications: icon dimensions are invalid")
	}
	imageData, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		return notificationImageData{}, errors.New("notifications: icon could not be decoded")
	}
	bounds := imageData.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		return notificationImageData{}, errors.New("notifications: icon dimensions changed during decode")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), imageData, bounds.Min, draw.Src)
	return notificationImageData{
		Width: int32(width), Height: int32(height), RowStride: int32(width * 4), HasAlpha: true,
		BitsPerSample: 8, Channels: 4, Data: rgba.Pix,
	}, nil
}
