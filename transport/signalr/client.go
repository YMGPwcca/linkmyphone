// Package signalr connects to a SignalR hub using the MessagePack Hub Protocol.
package signalr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	psignalr "github.com/YMGPwcca/linkmyphone/protocol/signalr"
	"github.com/YMGPwcca/linkmyphone/transport/wsclient"
)

const (
	DefaultHandshakeTimeout  = 15 * time.Second
	DefaultKeepAliveInterval = 15 * time.Second
	DefaultServerTimeout     = 45 * time.Second
)

type HTTPError struct {
	StatusCode int
	RetryAfter string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("signalr: HTTP %d", e.StatusCode) }

type Config struct {
	HubURL            string
	AccessToken       string
	Headers           http.Header
	HTTPClient        *http.Client
	HandshakeTimeout  time.Duration
	KeepAliveInterval time.Duration
	ServerTimeout     time.Duration
}

type Client struct {
	ws            *wsclient.Conn
	pending       []byte
	serverTimeout time.Duration
	done          chan struct{}
	keepAliveDone chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

type negotiateResponse struct {
	ConnectionToken     string `json:"connectionToken"`
	URL                 string `json:"url"`
	AccessToken         string `json:"accessToken"`
	Error               string `json:"error"`
	AvailableTransports []struct {
		Transport       string   `json:"transport"`
		TransferFormats []string `json:"transferFormats"`
	} `json:"availableTransports"`
}

func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.HubURL == "" {
		return nil, errors.New("signalr: empty hub URL")
	}
	if cfg.HandshakeTimeout == 0 {
		cfg.HandshakeTimeout = DefaultHandshakeTimeout
	}
	if cfg.KeepAliveInterval == 0 {
		cfg.KeepAliveInterval = DefaultKeepAliveInterval
	}
	if cfg.ServerTimeout == 0 {
		cfg.ServerTimeout = DefaultServerTimeout
	}
	if cfg.HandshakeTimeout <= 0 || cfg.KeepAliveInterval <= 0 || cfg.ServerTimeout <= cfg.KeepAliveInterval {
		return nil, errors.New("signalr: timeouts must be positive and server timeout must exceed keepalive interval")
	}
	ctx, cancelHandshake := context.WithTimeout(ctx, cfg.HandshakeTimeout)
	defer cancelHandshake()
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	hubURL := cfg.HubURL
	token := cfg.AccessToken
	var nr negotiateResponse
	for redirects := 0; redirects < 5; redirects++ {
		var err error
		nr, err = negotiate(ctx, httpClient, hubURL, token, cfg.Headers)
		if err != nil {
			return nil, err
		}
		if nr.Error != "" {
			return nil, fmt.Errorf("signalr negotiate: %s", nr.Error)
		}
		if nr.URL == "" {
			break
		}
		hubURL = nr.URL
		if nr.AccessToken != "" {
			token = nr.AccessToken
		}
		if redirects == 4 {
			return nil, errors.New("signalr: too many negotiate redirects")
		}
	}
	if nr.ConnectionToken == "" {
		return nil, errors.New("signalr: negotiate response missing connection token")
	}
	if !supportsWebSockets(nr) {
		return nil, errors.New("signalr: server did not offer WebSockets/Binary")
	}
	wsURL, err := websocketURL(hubURL, nr.ConnectionToken)
	if err != nil {
		return nil, err
	}
	headers := cloneHeader(cfg.Headers)
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	ws, err := wsclient.Dial(ctx, wsURL, headers)
	if err != nil {
		var upgrade *wsclient.HTTPError
		if errors.As(err, &upgrade) {
			return nil, &HTTPError{StatusCode: upgrade.StatusCode, RetryAfter: upgrade.RetryAfter}
		}
		return nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stopCancel()
	ok := false
	defer func() {
		if !ok {
			_ = ws.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := ws.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	if err := ws.WriteText([]byte("{\"protocol\":\"messagepack\",\"version\":1}\x1e")); err != nil {
		return nil, err
	}
	op, payload, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	if op != wsclient.OpcodeText && op != wsclient.OpcodeBinary {
		return nil, fmt.Errorf("signalr: unexpected handshake websocket opcode %d", op)
	}
	pending, err := consumeHandshake(payload)
	if err != nil {
		return nil, err
	}
	if len(pending) != 0 && op != wsclient.OpcodeBinary {
		return nil, errors.New("signalr: text handshake response carried trailing hub data")
	}
	if !stopCancel() || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := ws.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	ok = true
	client := &Client{ws: ws, pending: pending, serverTimeout: cfg.ServerTimeout, done: make(chan struct{}), keepAliveDone: make(chan struct{})}
	go client.keepAlive(cfg.KeepAliveInterval)
	return client, nil
}

func (c *Client) Close() error {
	err := c.close()
	<-c.keepAliveDone
	return err
}

func (c *Client) close() error {
	c.closeOnce.Do(func() { close(c.done); c.closeErr = c.ws.Close() })
	return c.closeErr
}

func (c *Client) keepAlive(interval time.Duration) {
	defer close(c.keepAliveDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	frame := psignalr.FramePing()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.SendBinary(frame); err != nil {
				_ = c.close()
				return
			}
		}
	}
}

func (c *Client) SendBinary(frame []byte) error {
	err := c.ws.WriteBinary(frame)
	if err != nil {
		_ = c.close()
	}
	return err
}
func (c *Client) ReadBinary() ([]byte, error) {
	if err := c.ws.SetReadDeadline(time.Now().Add(c.serverTimeout)); err != nil {
		return nil, err
	}
	if len(c.pending) != 0 {
		p := c.pending
		c.pending = nil
		return p, nil
	}
	for {
		op, p, err := c.ws.ReadMessage()
		if err != nil {
			return nil, err
		}
		if op == wsclient.OpcodeBinary {
			return p, nil
		}
		if op == wsclient.OpcodeText {
			return nil, errors.New("signalr: unexpected text message after handshake")
		}
	}
}

func negotiate(ctx context.Context, client *http.Client, hubURL, token string, headers http.Header) (negotiateResponse, error) {
	var out negotiateResponse
	u, err := url.Parse(hubURL)
	if err != nil {
		return out, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/negotiate"
	q := u.Query()
	q.Set("negotiateVersion", "1")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(nil))
	if err != nil {
		return out, err
	}
	for k, vv := range headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return out, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func supportsWebSockets(n negotiateResponse) bool {
	for _, t := range n.AvailableTransports {
		if t.Transport != "WebSockets" {
			continue
		}
		for _, f := range t.TransferFormats {
			if f == "Binary" {
				return true
			}
		}
	}
	return false
}

func websocketURL(hubURL, token string) (string, error) {
	u, err := url.Parse(hubURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("signalr: unsupported hub URL scheme")
	}
	q := u.Query()
	q.Set("id", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func consumeHandshake(payload []byte) ([]byte, error) {
	recordSeparator := bytes.IndexByte(payload, 0x1e)
	if recordSeparator < 0 {
		return nil, errors.New("signalr: malformed handshake response")
	}
	var v struct {
		Error string `json:"error"`
	}
	body := payload[:recordSeparator]
	if len(body) != 0 {
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, fmt.Errorf("signalr: decode handshake response: %w", err)
		}
		if v.Error != "" {
			return nil, fmt.Errorf("signalr handshake: %s", v.Error)
		}
	}
	return append([]byte(nil), payload[recordSeparator+1:]...), nil
}

func validateHandshake(payload []byte) error {
	pending, err := consumeHandshake(payload)
	if err != nil {
		return err
	}
	if len(pending) != 0 {
		return errors.New("signalr: unexpected trailing data after handshake")
	}
	return nil
}

func cloneHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vv := range h {
		out[k] = append([]string(nil), vv...)
	}
	return out
}
