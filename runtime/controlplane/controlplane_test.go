package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

func TestServerRoundTripAndSocketPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	handler := HandlerFunc(func(request Request) Response {
		if request.Operation != OperationGet || request.ID != "phonelink.test" {
			return Failure(errors.New("unexpected request"))
		}
		record := kernel.FeatureRecord{ID: request.ID, Enabled: true}
		snapshot := kernel.Snapshot{
			ID:      request.ID,
			Version: "1.0.0",
			Enabled: true,
			State:   kernel.StateReady,
			Epoch:   2,
		}
		response := Success()
		response.Record = &record
		response.Snapshot = &snapshot
		return response
	})

	server, err := Listen(path, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode=%#o want=0600", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx)
	}()

	response, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationGet,
		ID:        "phonelink.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Record == nil || response.Snapshot == nil {
		t.Fatalf("response=%#v", response)
	}
	if response.Snapshot.State != kernel.StateReady || response.Snapshot.Epoch != 2 {
		t.Fatalf("snapshot=%#v", response.Snapshot)
	}

	if _, err := Listen(path, handler); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Listen err=%v", err)
	}

	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists err=%v", err)
	}
}

func TestCallUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	_, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationList,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestListenReplacesStaleNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	server, err := Listen(path, HandlerFunc(func(Request) Response {
		return Success()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("mode=%v is not a Unix socket", info.Mode())
	}
}

func TestSwitchHandlerTransitionsWithoutReplacingSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	switcher := NewSwitchHandler(HandlerFunc(func(Request) Response {
		return Failure(errors.New("runtime is starting"))
	}))
	server, err := Listen(path, switcher)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx)
	}()

	first, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationList,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.OK || first.Error != "runtime is starting" {
		t.Fatalf("starting response=%#v", first)
	}

	switcher.Set(HandlerFunc(func(Request) Response {
		response := Success()
		response.Snapshots = []kernel.Snapshot{{
			ID:      "phonelink.test",
			State:   kernel.StateReady,
			Enabled: true,
		}}
		return response
	}))

	second, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationList,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.OK || len(second.Snapshots) != 1 ||
		second.Snapshots[0].State != kernel.StateReady {
		t.Fatalf("ready response=%#v", second)
	}
}

func TestServerCloseStopsServeWithoutContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	server, err := Listen(path, HandlerFunc(func(Request) Response {
		return Success()
	}))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after Server.Close")
	}
}

func TestHandlerPanicReturnsFailureAndServerSurvives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	server, err := Listen(path, HandlerFunc(func(request Request) Response {
		if request.Operation == OperationDelete {
			panic("boom")
		}
		return Success()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx)
	}()

	panicResponse, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationDelete,
		ID:        "phonelink.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if panicResponse.OK || panicResponse.Error == "" {
		t.Fatalf("panic response=%#v", panicResponse)
	}

	healthy, err := Call(context.Background(), path, Request{
		Version:   ProtocolVersion,
		Operation: OperationList,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !healthy.OK {
		t.Fatalf("server did not survive handler panic: %#v", healthy)
	}
}

func TestSocketPathIsScopedToFeatureStore(t *testing.T) {
	// Keep this deliberately short so this test exercises the preferred XDG
	// branch. The long-path fallback is covered separately below.
	runtimeDir := "/tmp/pll-xdg"
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	storeDir := t.TempDir()
	first := SocketPathForStore(filepath.Join(storeDir, "features.json"))
	second := SocketPathForStore(filepath.Join(storeDir, "other.json"))
	if first == second {
		t.Fatalf("socket collision: %q", first)
	}
	if filepath.Dir(first) != filepath.Join(runtimeDir, "phonelink-linux") {
		t.Fatalf("runtime dir=%q", filepath.Dir(first))
	}
	if len(filepath.Base(first)) != 24+len(".sock") {
		t.Fatalf("socket name=%q", filepath.Base(first))
	}
}

func TestSocketPathRemainsShortForDeepFeatureStore(t *testing.T) {
	longRuntimeDir := filepath.Join(
		t.TempDir(),
		"an-intentionally-very-long-runtime-directory-name",
		"another-long-component",
	)
	t.Setenv("XDG_RUNTIME_DIR", longRuntimeDir)

	deep := t.TempDir()
	for index := 0; index < 12; index++ {
		deep = filepath.Join(deep, "very-long-feature-store-directory-name")
	}
	storePath := filepath.Join(deep, "features.json")
	socketPath := SocketPathForStore(storePath)
	if len(socketPath) >= maxUnixSocketPathLength {
		t.Fatalf("socket path too long (%d): %q", len(socketPath), socketPath)
	}
	if !strings.HasPrefix(socketPath, "/tmp/phonelink-linux-") {
		t.Fatalf("long XDG runtime path did not use short fallback: %q", socketPath)
	}

	server, err := Listen(socketPath, HandlerFunc(func(Request) Response {
		return Success()
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}
