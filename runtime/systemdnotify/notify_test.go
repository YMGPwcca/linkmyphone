package systemdnotify

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotifyWithoutSocketIsNoOp(t *testing.T) {
	t.Setenv(notifySocketEnv, "")
	sent, err := Notify("READY=1")
	if err != nil {
		t.Fatal(err)
	}
	if sent {
		t.Fatal("notification unexpectedly reported as sent")
	}
}

func TestReadySendsSystemdDatagram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.sock")
	address := &net.UnixAddr{Name: path, Net: "unixgram"}
	listener, err := net.ListenUnixgram("unixgram", address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv(notifySocketEnv, path)

	if err := Ready("runtime ready\nwithout multiline status"); err != nil {
		t.Fatal(err)
	}

	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1024)
	n, _, err := listener.ReadFromUnix(buffer)
	if err != nil {
		t.Fatal(err)
	}
	got := string(buffer[:n])
	if !strings.Contains(got, "READY=1") {
		t.Fatalf("notification=%q", got)
	}
	if strings.Contains(got, "without\nmultiline") {
		t.Fatalf("status was not sanitized: %q", got)
	}
	if !strings.Contains(got, "STATUS=runtime ready without multiline status") {
		t.Fatalf("notification=%q", got)
	}
}

func TestNotifyRejectsEmptyStateWhenSocketConfigured(t *testing.T) {
	t.Setenv(notifySocketEnv, "/tmp/unused-systemd-notify.sock")
	if _, err := Notify("  "); err == nil {
		t.Fatal("expected empty state error")
	}
}
