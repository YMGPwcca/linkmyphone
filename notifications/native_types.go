package notifications

import (
	"context"
	"errors"
)

type DesktopAction struct {
	ID    string
	Label string
}

// DesktopNotification is the desktop representation of a phone notification.
type DesktopNotification struct {
	ReplaceID uint32
	AppName   string
	Summary   string
	Body      string
	Icon      []byte
	Actions   []DesktopAction
	Silent    bool
	Resident  bool
}

// NativeEventKind identifies an event emitted by the desktop notification
// service.
type NativeEventKind uint8

const (
	NativeClosed NativeEventKind = iota + 1
	NativeAction
	NativeActivationToken
	NativeReset
	NativeFailure
	NativeUnavailable
)

// NativeEvent is a desktop notification-service event. Reason is the
// org.freedesktop.Notifications close reason when Kind is NativeClosed.
type NativeEvent struct {
	Kind            NativeEventKind
	ID              uint32
	Reason          uint32
	Action          string
	ActivationToken string
	Err             error
}

// NativeBackend is the desktop notification and event bridge.
type NativeBackend interface {
	Notify(context.Context, DesktopNotification) (uint32, error)
	CloseNotification(context.Context, uint32) error
	Events() <-chan NativeEvent
	SupportsActions() bool
	Close() error
}

// ReplyPrompt is the bounded, private input passed to the native reply
// prompt.
type ReplyPrompt struct {
	AppName         string
	Title           string
	Body            string
	ActionLabel     string
	ActivationToken string
}

var (
	errNativeClosed        = errors.New("notifications: desktop backend is closed")
	errInvalidNotification = errors.New("notifications: invalid desktop notification")
)
