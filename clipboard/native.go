package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const MaxNativeClipboardTextBytes = 4 << 20

type commandSpec struct {
	name string
	args []string
}

// NativeLocal implements Local using common Linux clipboard command-line
// providers. Commands are executed directly without a shell.
type NativeLocal struct {
	backend string
	read    commandSpec
	write   commandSpec
	watch   commandSpec
}

func DetectNativeLocal() (*NativeLocal, error) {
	return detectNativeLocal(exec.LookPath, os.Getenv)
}

func (l *NativeLocal) BackendName() string {
	if l == nil {
		return ""
	}
	return l.backend
}

func (l *NativeLocal) SupportsWatch() bool {
	return l != nil && l.watch.name != ""
}

func (l *NativeLocal) ReadText(ctx context.Context) (string, error) {
	if l == nil || l.read.name == "" {
		return "", errors.New("clipboard: native read backend is unavailable")
	}
	cmd := exec.CommandContext(ctx, l.read.name, l.read.args...)
	out, err := cmd.Output()
	if err != nil {
		if nativeReadIsEmptyClipboard(l.backend, err) {
			return "", nil
		}
		return "", nativeCommandError("read", l.backend, err)
	}
	if len(out) > MaxNativeClipboardTextBytes {
		return "", fmt.Errorf(
			"clipboard: native clipboard text exceeds %d bytes",
			MaxNativeClipboardTextBytes,
		)
	}
	return string(out), nil
}

func (l *NativeLocal) WriteText(ctx context.Context, text string) error {
	if l == nil || l.write.name == "" {
		return errors.New("clipboard: native write backend is unavailable")
	}
	if len([]byte(text)) > MaxNativeClipboardTextBytes {
		return fmt.Errorf(
			"clipboard: native clipboard text exceeds %d bytes",
			MaxNativeClipboardTextBytes,
		)
	}
	cmd := exec.CommandContext(ctx, l.write.name, l.write.args...)
	cmd.Stdin = strings.NewReader(text)

	// wl-copy forks by default and serves the selection from its background
	// child. Do not use Output/CombinedOutput here: the forked child can inherit
	// the capture pipe descriptors and keep Go waiting for EOF until the
	// selection is replaced. With nil Stdout/Stderr, os/exec connects them to
	// the null device and Run only waits for the invoked parent process.
	if err := cmd.Run(); err != nil {
		return nativeCommandError("write", l.backend, err)
	}
	return nil
}

func detectNativeLocal(
	lookPath func(string) (string, error),
	getenv func(string) string,
) (*NativeLocal, error) {
	has := func(name string) bool {
		_, err := lookPath(name)
		return err == nil
	}

	wayland := strings.TrimSpace(getenv("WAYLAND_DISPLAY")) != ""
	x11 := strings.TrimSpace(getenv("DISPLAY")) != ""

	if wayland && has("wl-paste") && has("wl-copy") {
		return &NativeLocal{
			backend: "wl-clipboard",
			read: commandSpec{
				name: "wl-paste",
				args: []string{"--no-newline", "--type", "text"},
			},
			write: commandSpec{
				name: "wl-copy",
				args: []string{"--type", "text/plain;charset=utf-8"},
			},
			watch: commandSpec{
				name: "wl-paste",
				args: []string{"--type", "text", "--watch"},
			},
		}, nil
	}

	if x11 && has("xclip") {
		return &NativeLocal{
			backend: "xclip",
			read: commandSpec{
				name: "xclip",
				args: []string{"-selection", "clipboard", "-out", "-target", "UTF8_STRING"},
			},
			write: commandSpec{
				name: "xclip",
				args: []string{"-selection", "clipboard", "-in"},
			},
		}, nil
	}

	if x11 && has("xsel") {
		return &NativeLocal{
			backend: "xsel",
			read: commandSpec{
				name: "xsel",
				args: []string{"--clipboard", "--output"},
			},
			write: commandSpec{
				name: "xsel",
				args: []string{"--clipboard", "--input"},
			},
		}, nil
	}

	// Some launchers sanitize DISPLAY/WAYLAND_DISPLAY while still preserving
	// access to the compositor/X server. Fall back to installed tools so the
	// actual command can provide the useful connection error.
	if has("wl-paste") && has("wl-copy") {
		return &NativeLocal{
			backend: "wl-clipboard",
			read:    commandSpec{name: "wl-paste", args: []string{"--no-newline", "--type", "text"}},
			write: commandSpec{
				name: "wl-copy",
				args: []string{"--type", "text/plain;charset=utf-8"},
			},
			watch: commandSpec{
				name: "wl-paste",
				args: []string{"--type", "text", "--watch"},
			},
		}, nil
	}
	if has("xclip") {
		return &NativeLocal{
			backend: "xclip",
			read:    commandSpec{name: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "UTF8_STRING"}},
			write:   commandSpec{name: "xclip", args: []string{"-selection", "clipboard", "-in"}},
		}, nil
	}
	if has("xsel") {
		return &NativeLocal{
			backend: "xsel",
			read:    commandSpec{name: "xsel", args: []string{"--clipboard", "--output"}},
			write:   commandSpec{name: "xsel", args: []string{"--clipboard", "--input"}},
		}, nil
	}

	return nil, errors.New(
		"clipboard: no supported Linux clipboard backend found; install wl-clipboard, xclip, or xsel",
	)
}

func nativeReadIsEmptyClipboard(backend string, err error) bool {
	if backend != "wl-clipboard" {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return strings.TrimSpace(string(exitErr.Stderr)) == "Nothing is copied"
}

func nativeCommandError(operation, backend string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
			if len(detail) > 256 {
				detail = detail[:256] + "..."
			}
			return fmt.Errorf(
				"clipboard: %s %s failed: %w: %s",
				backend,
				operation,
				err,
				detail,
			)
		}
	}
	return fmt.Errorf("clipboard: %s %s failed: %w", backend, operation, err)
}
