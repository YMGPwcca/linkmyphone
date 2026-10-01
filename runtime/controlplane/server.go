package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	DefaultProbeTimeout = 200 * time.Millisecond
	DefaultMaxClients   = 16
)

type Server struct {
	path     string
	listener net.Listener
	handler  Handler

	clients chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

func Listen(path string, handler Handler) (*Server, error) {
	if path == "" {
		return nil, errors.New("controlplane: socket path is required")
	}
	if handler == nil {
		return nil, errors.New("controlplane: handler is required")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("controlplane: create socket directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("controlplane: secure socket directory: %w", err)
	}

	if _, err := os.Lstat(path); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultProbeTimeout)
		_, probeErr := Call(ctx, path, Request{
			Version:   ProtocolVersion,
			Operation: OperationList,
		})
		cancel()
		if probeErr == nil {
			return nil, ErrAlreadyRunning
		}
		if !errors.Is(probeErr, ErrUnavailable) {
			return nil, fmt.Errorf("controlplane: probe existing socket: %w", probeErr)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("controlplane: remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("controlplane: inspect socket path: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("controlplane: listen: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("controlplane: secure socket: %w", err)
	}

	return &Server{
		path:     path,
		listener: listener,
		handler:  handler,
		clients:  make(chan struct{}, DefaultMaxClients),
	}, nil
}

func (s *Server) Serve(ctx context.Context) error {
	if s == nil || s.listener == nil {
		return errors.New("controlplane: server is not listening")
	}

	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				s.wg.Wait()
				return ctx.Err()
			}
			return fmt.Errorf("controlplane: accept: %w", err)
		}

		select {
		case s.clients <- struct{}{}:
			s.wg.Add(1)
			go func() {
				defer func() {
					<-s.clients
					s.wg.Done()
					_ = conn.Close()
				}()
				s.handleConnection(conn)
			}()
		default:
			_ = json.NewEncoder(conn).Encode(Failure(errors.New("controlplane: too many concurrent clients")))
			_ = conn.Close()
		}
	}
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.once.Do(func() {
		if s.listener != nil {
			if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				closeErr = err
			}
		}
		s.wg.Wait()
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) && closeErr == nil {
			closeErr = err
		}
	})
	return closeErr
}

func (s *Server) handleConnection(conn net.Conn) {
	decoder := json.NewDecoder(io.LimitReader(conn, MaxRequestBytes))
	decoder.DisallowUnknownFields()

	var request Request
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Failure(fmt.Errorf("controlplane: decode request: %w", err)))
		return
	}
	if request.Version != ProtocolVersion {
		_ = json.NewEncoder(conn).Encode(Failure(fmt.Errorf(
			"controlplane: incompatible request version %d",
			request.Version,
		)))
		return
	}
	response := s.handler.HandleControl(request)
	if response.Version == 0 {
		response.Version = ProtocolVersion
	}
	_ = json.NewEncoder(conn).Encode(response)
}
