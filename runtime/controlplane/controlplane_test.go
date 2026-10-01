package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
