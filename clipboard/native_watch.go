package clipboard

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	NativeWatchHelperCommand = "__clipboard-watch-event"

	nativeWatchFrameVersion  = 1
	nativeWatchHeaderSize    = 10
	maxNativeWatchStateBytes = 64
	maxNativeWatchStderr     = 4096
)

var (
	ErrNativeWatchUnsupported = errors.New("clipboard: native watch is unsupported")
	nativeWatchMagic          = [4]byte{'P', 'L', 'C', 'W'}
)

type NativeTextEvent struct {
	Text  string
	State string
}

// WatchText blocks while a native clipboard watch is active and calls emit for
// each complete text selection event. Wayland uses wl-paste --watch and a
// hidden helper command in the current linkmyphone executable so clipboard
// bytes can be framed without invoking a shell.
func (l *NativeLocal) WatchText(
	ctx context.Context,
	emit func(NativeTextEvent) error,
) error {
	if l == nil || l.watch.name == "" {
		return ErrNativeWatchUnsupported
	}
	if emit == nil {
		return errors.New("clipboard: native watch callback is required")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("clipboard: resolve watch helper executable: %w", err)
	}

	cmd := newNativeWatchCommand(ctx, l.watch, executable)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("clipboard: open %s watch output: %w", l.backend, err)
	}
	var stderr cappedTextBuffer
	stderr.max = maxNativeWatchStderr
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("clipboard: start %s watch: %w", l.backend, err)
	}

	for {
		event, readErr := readNativeWatchFrame(stdout)
		if readErr == nil {
			if err := ctx.Err(); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
			if err := emit(event); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
			continue
		}

		if ctx.Err() != nil {
			_ = cmd.Wait()
			return ctx.Err()
		}

		if !errors.Is(readErr, io.EOF) &&
			!errors.Is(readErr, io.ErrUnexpectedEOF) {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if waitErr != nil {
			detail := strings.TrimSpace(stderr.String())
			if detail != "" {
				return fmt.Errorf(
					"clipboard: %s watch exited: %w: %s",
					l.backend,
					waitErr,
					detail,
				)
			}
			return fmt.Errorf("clipboard: %s watch exited: %w", l.backend, waitErr)
		}
		if errors.Is(readErr, io.EOF) {
			return fmt.Errorf("clipboard: %s watch exited unexpectedly", l.backend)
		}
		return fmt.Errorf("clipboard: decode %s watch event: %w", l.backend, readErr)
	}
}

func newNativeWatchCommand(
	ctx context.Context,
	spec commandSpec,
	executable string,
) *exec.Cmd {
	args := append([]string(nil), spec.args...)
	args = append(args, executable, NativeWatchHelperCommand)
	cmd := exec.CommandContext(ctx, spec.name, args...)

	// Keep wl-paste and the helper processes it spawns out of the terminal's
	// foreground process group. Ctrl+C should drive the module lifecycle first,
	// not asynchronously kill the watcher before Stop() cancels runCtx.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 500 * time.Millisecond
	return cmd
}

// RunNativeWatchHelper is invoked by the hidden helper command spawned by
// wl-paste --watch. wl-paste waits for each helper process before processing the
// next selection, so complete frames cannot interleave with each other.
func RunNativeWatchHelper(
	stdin io.Reader,
	stdout io.Writer,
	state string,
) error {
	if stdin == nil || stdout == nil {
		return errors.New("clipboard: native watch helper requires stdin and stdout")
	}

	data, err := io.ReadAll(io.LimitReader(
		stdin,
		MaxNativeClipboardTextBytes+1,
	))
	if err != nil {
		return writeNativeWatchErrorFrame(
			stdout,
			fmt.Errorf("read clipboard watch payload: %w", err),
		)
	}
	if len(data) > MaxNativeClipboardTextBytes {
		return writeNativeWatchErrorFrame(
			stdout,
			fmt.Errorf(
				"clipboard text exceeds %d bytes",
				MaxNativeClipboardTextBytes,
			),
		)
	}

	state = strings.TrimSpace(state)
	if state == "" {
		state = "data"
	}
	if state == "nil" {
		data = nil
	}
	if len(state) > maxNativeWatchStateBytes {
		state = "unknown"
	}

	return writeNativeWatchFrame(stdout, NativeTextEvent{
		Text:  string(data),
		State: state,
	})
}

func writeNativeWatchErrorFrame(w io.Writer, err error) error {
	message := "native clipboard watch helper failed"
	if err != nil {
		message = err.Error()
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	return writeNativeWatchFrame(w, NativeTextEvent{
		Text:  message,
		State: "error",
	})
}

func writeNativeWatchFrame(w io.Writer, event NativeTextEvent) error {
	state := []byte(event.State)
	payload := []byte(event.Text)
	if len(state) > maxNativeWatchStateBytes {
		return errors.New("clipboard: native watch state is too large")
	}
	if len(payload) > MaxNativeClipboardTextBytes {
		return fmt.Errorf(
			"clipboard: native watch payload exceeds %d bytes",
			MaxNativeClipboardTextBytes,
		)
	}

	header := make([]byte, nativeWatchHeaderSize)
	copy(header[:4], nativeWatchMagic[:])
	header[4] = nativeWatchFrameVersion
	header[5] = byte(len(state))
	binary.BigEndian.PutUint32(header[6:10], uint32(len(payload)))

	if err := writeAll(w, header); err != nil {
		return err
	}
	if err := writeAll(w, state); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func readNativeWatchFrame(r io.Reader) (NativeTextEvent, error) {
	var event NativeTextEvent
	header := make([]byte, nativeWatchHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return event, err
	}
	if string(header[:4]) != string(nativeWatchMagic[:]) {
		return event, errors.New("clipboard: invalid native watch frame magic")
	}
	if header[4] != nativeWatchFrameVersion {
		return event, fmt.Errorf(
			"clipboard: unsupported native watch frame version %d",
			header[4],
		)
	}

	stateLength := int(header[5])
	if stateLength > maxNativeWatchStateBytes {
		return event, errors.New("clipboard: invalid native watch state length")
	}
	payloadLength := int(binary.BigEndian.Uint32(header[6:10]))
	if payloadLength > MaxNativeClipboardTextBytes {
		return event, fmt.Errorf(
			"clipboard: native watch payload exceeds %d bytes",
			MaxNativeClipboardTextBytes,
		)
	}

	state := make([]byte, stateLength)
	if _, err := io.ReadFull(r, state); err != nil {
		return event, err
	}
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return event, err
	}

	event.State = string(state)
	event.Text = string(payload)
	if event.State == "error" {
		return NativeTextEvent{}, errors.New(event.Text)
	}
	if event.State == "nil" {
		event.Text = ""
	}
	return event, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

type cappedTextBuffer struct {
	mu  sync.Mutex
	max int
	b   strings.Builder
}

func (b *cappedTextBuffer) Write(p []byte) (int, error) {
	original := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 || b.b.Len() >= b.max {
		return original, nil
	}
	remaining := b.max - b.b.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.b.Write(p)
	return original, nil
}

func (b *cappedTextBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
