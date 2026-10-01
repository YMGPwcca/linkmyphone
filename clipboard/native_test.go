package clipboard

import (
	"errors"
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
