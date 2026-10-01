package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

const DefaultCallTimeout = 5 * time.Second

func Call(ctx context.Context, socketPath string, request Request) (Response, error) {
	if socketPath == "" {
		return Response{}, errors.New("controlplane: socket path is required")
	}
	if request.Version == 0 {
		request.Version = ProtocolVersion
	}

	callCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		callCtx, cancel = context.WithTimeout(ctx, DefaultCallTimeout)
	}
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(callCtx, "unix", socketPath)
	if err != nil {
		if isUnavailable(err) {
			return Response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return Response{}, fmt.Errorf("controlplane: dial runtime: %w", err)
	}
	defer conn.Close()

	if deadline, ok := callCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, fmt.Errorf("controlplane: send request: %w", err)
	}

	var response Response
	decoder := json.NewDecoder(conn)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("controlplane: read response: %w", err)
	}
	if response.Version != ProtocolVersion {
		return Response{}, fmt.Errorf(
			"controlplane: incompatible response version %d",
			response.Version,
		)
	}
	return response, nil
}

func isUnavailable(err error) bool {
	if errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENOTSOCK) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) &&
		(errors.Is(opErr.Err, os.ErrNotExist) ||
			errors.Is(opErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(opErr.Err, syscall.ENOTSOCK))
}
