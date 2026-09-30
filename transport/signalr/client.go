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
	"time"

	"github.com/YMGPwcca/phonelink-linux/transport/wsclient"
)

type Config struct {
	HubURL      string
	AccessToken string
	Headers     http.Header
	HTTPClient  *http.Client
}

type Client struct {
	ws *wsclient.Conn
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
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = ws.Close()
		}
	}()
	if err := ws.WriteText([]byte("{\"protocol\":\"messagepack\",\"version\":1}\x1e")); err != nil {
		return nil, err
	}
	op, payload, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	if op != wsclient.OpcodeText {
		return nil, errors.New("signalr: expected text handshake response")
	}
	if err := validateHandshake(payload); err != nil {
		return nil, err
	}
	ok = true
	return &Client{ws: ws}, nil
}

func (c *Client) Close() error                  { return c.ws.Close() }
func (c *Client) SendBinary(frame []byte) error { return c.ws.WriteBinary(frame) }
func (c *Client) ReadBinary() ([]byte, error) {
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("signalr negotiate: HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
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

func validateHandshake(payload []byte) error {
	if len(payload) == 0 || payload[len(payload)-1] != 0x1e {
		return errors.New("signalr: malformed handshake response")
	}
	var v struct {
		Error string `json:"error"`
	}
	body := payload[:len(payload)-1]
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}
	if v.Error != "" {
		return fmt.Errorf("signalr handshake: %s", v.Error)
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
