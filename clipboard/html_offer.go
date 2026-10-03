package clipboard

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

//go:embed html_offer.py
var htmlOfferHelper string

func gtkPython(ctx context.Context) string {
	for _, candidate := range []string{"python3", "/usr/bin/python3"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = exec.CommandContext(probeCtx, path, "-c", "import gi; gi.require_version('Gtk','4.0'); gi.require_version('Gdk','4.0'); from gi.repository import Gtk, Gdk").Run()
		cancel()
		if err == nil {
			return path
		}
	}
	return ""
}

func writeHTMLOffer(ctx context.Context, python, text string) error {
	data, err := json.Marshal(text)
	if err != nil {
		return err
	}
	// The clipboard owner outlives the short CONTENT request context, just
	// like wl-copy/xclip. It exits as soon as another selection replaces it.
	cmd := exec.Command(python, "-c", htmlOfferHelper)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "READY\n" {
			err = errors.New("clipboard: HTML provider failed")
		}
		ready <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var startupErr error
	select {
	case startupErr = <-ready:
	case <-ctx.Done():
		startupErr = ctx.Err()
	case <-deadline.C:
		startupErr = errors.New("clipboard: HTML provider startup timed out")
	}
	_ = stdout.Close()
	if startupErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return startupErr
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
