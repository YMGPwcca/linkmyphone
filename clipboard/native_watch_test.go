package clipboard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNativeWatchHelperRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	if err := RunNativeWatchHelper(
		strings.NewReader("hello"),
		&wire,
		"data",
	); err != nil {
		t.Fatal(err)
	}

	event, err := readNativeWatchFrame(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if event.Text != "hello" || event.State != "data" {
		t.Fatalf("event=%#v", event)
	}
}

func TestNativeWatchHelperClearEvent(t *testing.T) {
	var wire bytes.Buffer
	if err := RunNativeWatchHelper(
		strings.NewReader("ignored"),
		&wire,
		"nil",
	); err != nil {
		t.Fatal(err)
	}

	event, err := readNativeWatchFrame(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if event.Text != "" || event.State != "nil" {
		t.Fatalf("event=%#v", event)
	}
}

func TestNativeWatchHelperOversizeBecomesErrorFrame(t *testing.T) {
	var wire bytes.Buffer
	input := io.LimitReader(
		strings.NewReader(strings.Repeat("x", MaxNativeClipboardTextBytes+1)),
		MaxNativeClipboardTextBytes+1,
	)
	if err := RunNativeWatchHelper(input, &wire, "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeWatchFrame(&wire); err == nil {
		t.Fatal("expected oversize watch frame error")
	}
}

func TestNativeWatchFrameRejectsBadMagic(t *testing.T) {
	var wire bytes.Buffer
	if err := writeNativeWatchFrame(&wire, NativeTextEvent{
		Text:  "hello",
		State: "data",
	}); err != nil {
		t.Fatal(err)
	}
	raw := wire.Bytes()
	raw[0] = 'X'
	if _, err := readNativeWatchFrame(bytes.NewReader(raw)); err == nil {
		t.Fatal("expected invalid magic error")
	}
}

func TestNativeWatchFrameRejectsTruncatedPayload(t *testing.T) {
	var wire bytes.Buffer
	if err := writeNativeWatchFrame(&wire, NativeTextEvent{
		Text:  "hello",
		State: "data",
	}); err != nil {
		t.Fatal(err)
	}
	raw := wire.Bytes()
	raw = raw[:len(raw)-1]
	if _, err := readNativeWatchFrame(bytes.NewReader(raw)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v", err)
	}
}

func TestNativeWatchUnsupportedBackend(t *testing.T) {
	local := &NativeLocal{backend: "xclip"}
	err := local.WatchText(
		context.Background(),
		func(NativeTextEvent) error { return nil },
	)
	if !errors.Is(err, ErrNativeWatchUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestNativeWatchFramesRemainDelimited(t *testing.T) {
	var wire bytes.Buffer
	for _, event := range []NativeTextEvent{
		{Text: "one", State: "data"},
		{Text: "", State: "nil"},
		{Text: "three", State: "sensitive"},
	} {
		if err := writeNativeWatchFrame(&wire, event); err != nil {
			t.Fatal(err)
		}
	}

	for index, want := range []NativeTextEvent{
		{Text: "one", State: "data"},
		{Text: "", State: "nil"},
		{Text: "three", State: "sensitive"},
	} {
		got, err := readNativeWatchFrame(&wire)
		if err != nil {
			t.Fatalf("frame %d: %v", index, err)
		}
		if got != want {
			t.Fatalf("frame %d: got=%#v want=%#v", index, got, want)
		}
	}
}

func TestNativeWatchCommandUsesOwnProcessGroup(t *testing.T) {
	cmd := newNativeWatchCommand(
		context.Background(),
		commandSpec{
			name: "wl-paste",
			args: []string{"--type", "text", "--watch"},
		},
		"/tmp/phonelink-linux",
	)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr=%#v", cmd.SysProcAttr)
	}
	if cmd.SysProcAttr.Pdeathsig == 0 {
		t.Fatalf("watch command has no parent-death signal: %#v", cmd.SysProcAttr)
	}
	if cmd.Cancel == nil {
		t.Fatal("watch command has no lifecycle-aware cancellation")
	}
	if cmd.WaitDelay <= 0 {
		t.Fatalf("WaitDelay=%s", cmd.WaitDelay)
	}
	wantTail := []string{"/tmp/phonelink-linux", NativeWatchHelperCommand}
	if len(cmd.Args) < len(wantTail)+1 {
		t.Fatalf("Args=%#v", cmd.Args)
	}
	gotTail := cmd.Args[len(cmd.Args)-len(wantTail):]
	for index := range wantTail {
		if gotTail[index] != wantTail[index] {
			t.Fatalf("Args tail=%#v want=%#v", gotTail, wantTail)
		}
	}
}
