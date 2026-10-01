package clipboard

import (
	"errors"
	"os/exec"
	"testing"
)

func TestDetectNativeLocalPrefersWayland(t *testing.T) {
	available := map[string]bool{
		"wl-paste": true,
		"wl-copy":  true,
		"xclip":    true,
		"xsel":     true,
	}
	lookPath := func(name string) (string, error) {
		if available[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("missing")
	}
	getenv := func(name string) string {
		switch name {
		case "WAYLAND_DISPLAY":
			return "wayland-0"
		case "DISPLAY":
			return ":0"
		default:
			return ""
		}
	}

	local, err := detectNativeLocal(lookPath, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if local.BackendName() != "wl-clipboard" {
		t.Fatalf("backend=%q", local.BackendName())
	}
	if !local.SupportsWatch() {
		t.Fatal("Wayland wl-clipboard backend should support event watching")
	}
}

func TestDetectNativeLocalFallsBackToXclip(t *testing.T) {
	available := map[string]bool{
		"xclip": true,
	}
	lookPath := func(name string) (string, error) {
		if available[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("missing")
	}
	getenv := func(name string) string {
		if name == "DISPLAY" {
			return ":0"
		}
		return ""
	}

	local, err := detectNativeLocal(lookPath, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if local.BackendName() != "xclip" {
		t.Fatalf("backend=%q", local.BackendName())
	}
	if local.SupportsWatch() {
		t.Fatal("xclip backend unexpectedly reports native watch support")
	}
}

func TestDetectNativeLocalFallsBackToXsel(t *testing.T) {
	available := map[string]bool{
		"xsel": true,
	}
	lookPath := func(name string) (string, error) {
		if available[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("missing")
	}
	getenv := func(name string) string {
		if name == "DISPLAY" {
			return ":0"
		}
		return ""
	}

	local, err := detectNativeLocal(lookPath, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if local.BackendName() != "xsel" {
		t.Fatalf("backend=%q", local.BackendName())
	}
	if local.SupportsWatch() {
		t.Fatal("xsel backend unexpectedly reports native watch support")
	}
}

func TestDetectNativeLocalReportsMissingBackend(t *testing.T) {
	lookPath := func(string) (string, error) {
		return "", errors.New("missing")
	}
	getenv := func(string) string { return "" }

	if _, err := detectNativeLocal(lookPath, getenv); err == nil {
		t.Fatal("expected missing backend error")
	}
}

func TestNativeReadRecognizesEmptyWaylandClipboard(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("Nothing is copied\n")}
	if !nativeReadIsEmptyClipboard("wl-clipboard", err) {
		t.Fatal("empty Wayland clipboard was not recognized")
	}
}

func TestNativeReadDoesNotHideOtherWaylandFailures(t *testing.T) {
	for _, detail := range []string{
		"Failed to connect to a Wayland server",
		"Clipboard content is not available as requested type \"text\"",
	} {
		err := &exec.ExitError{Stderr: []byte(detail + "\n")}
		if nativeReadIsEmptyClipboard("wl-clipboard", err) {
			t.Fatalf("unexpected empty clipboard match for %q", detail)
		}
	}
}

func TestNativeReadDoesNotApplyWaylandEmptySemanticToX11(t *testing.T) {
	err := &exec.ExitError{Stderr: []byte("Nothing is copied\n")}
	if nativeReadIsEmptyClipboard("xclip", err) {
		t.Fatal("Wayland empty clipboard semantic leaked into xclip backend")
	}
}
