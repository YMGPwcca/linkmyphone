package systemdnotify

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

const notifySocketEnv = "NOTIFY_SOCKET"

// Notify sends one sd_notify-compatible state datagram when the process was
// launched with NOTIFY_SOCKET. Interactive/non-systemd runs are a no-op.
func Notify(state string) (bool, error) {
	socket := strings.TrimSpace(os.Getenv(notifySocketEnv))
	if socket == "" {
		return false, nil
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return false, errors.New("systemdnotify: state is empty")
	}

	address := &net.UnixAddr{
		Name: socket,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix("unixgram", nil, address)
	if err != nil {
		return true, fmt.Errorf("systemdnotify: connect notify socket: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(state)); err != nil {
		return true, fmt.Errorf("systemdnotify: send state: %w", err)
	}
	return true, nil
}

// Ready marks the service ready only after the runtime control plane and
// feature lifecycle have completed startup.
func Ready(status string) error {
	state := "READY=1"
	if status = strings.TrimSpace(status); status != "" {
		state += "\nSTATUS=" + sanitizeStatus(status)
	}
	_, err := Notify(state)
	return err
}

// Stopping announces lifecycle-aware shutdown before module teardown starts.
func Stopping(status string) error {
	state := "STOPPING=1"
	if status = strings.TrimSpace(status); status != "" {
		state += "\nSTATUS=" + sanitizeStatus(status)
	}
	_, err := Notify(state)
	return err
}

func sanitizeStatus(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}
