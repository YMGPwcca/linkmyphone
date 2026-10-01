// Package wsclient provides the minimal RFC 6455 client needed by the SignalR
// Relay Hub transport. It has no third-party dependencies.
package wsclient

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	OpcodeContinuation = byte(0x0)
	OpcodeText         = byte(0x1)
	OpcodeBinary       = byte(0x2)
	OpcodeClose        = byte(0x8)
	OpcodePing         = byte(0x9)
	OpcodePong         = byte(0xa)

	defaultMaxMessage = 16 << 20
	websocketGUID      = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

var ErrProtocol = errors.New("websocket protocol error")

type Conn struct {
	conn       net.Conn
	reader     *bufio.Reader
	writeMu    sync.Mutex
	readMu     sync.Mutex
	maxMessage int
}

func Dial(ctx context.Context, rawURL string, headers http.Header) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("websocket: URL scheme must be ws or wss")
	}
	if u.Host == "" {
		return nil, errors.New("websocket: missing host")
	}

	hostname := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	netConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(hostname, port))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = netConn.Close()
		}
	}()

	if u.Scheme == "wss" {
		tlsConn := tls.Client(netConn, &tls.Config{ServerName: hostname, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		netConn = tlsConn
	}
	if deadline, has := ctx.Deadline(); has {
		_ = netConn.SetDeadline(deadline)
		defer netConn.SetDeadline(time.Time{})
	}

	var keyRaw [16]byte
	if _, err := rand.Read(keyRaw[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw[:])
	h := make(http.Header)
	for k, vv := range headers {
		for _, v := range vv {
			h.Add(k, v)
		}
	}
	h.Set("Upgrade", "websocket")
	h.Set("Connection", "Upgrade")
	h.Set("Sec-WebSocket-Version", "13")
	h.Set("Sec-WebSocket-Key", key)

	httpURL := *u
	if u.Scheme == "wss" {
		httpURL.Scheme = "https"
	} else {
		httpURL.Scheme = "http"
	}
	if httpURL.Path == "" {
		httpURL.Path = "/"
	}
	req := &http.Request{Method: http.MethodGet, URL: &httpURL, Host: u.Host, Header: h}
	if err := req.Write(netConn); err != nil {
		return nil, err
	}
	br := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("websocket: upgrade failed: %s", resp.Status)
	}
	if !headerHasToken(resp.Header, "Connection", "upgrade") ||
		!strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("%w: invalid upgrade response", ErrProtocol)
	}
	hash := sha1.Sum([]byte(key + websocketGUID))
	wantAccept := base64.StdEncoding.EncodeToString(hash[:])
	if resp.Header.Get("Sec-WebSocket-Accept") != wantAccept {
		return nil, fmt.Errorf("%w: invalid Sec-WebSocket-Accept", ErrProtocol)
	}

	ok = true
	return &Conn{conn: netConn, reader: br, maxMessage: defaultMaxMessage}, nil
}

func (c *Conn) Close() error {
	c.writeMu.Lock()
	_ = c.writeFrame(true, OpcodeClose, nil)
	c.writeMu.Unlock()
	return c.conn.Close()
}

func (c *Conn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
func (c *Conn) WriteText(payload []byte) error     { return c.WriteMessage(OpcodeText, payload) }
func (c *Conn) WriteBinary(payload []byte) error   { return c.WriteMessage(OpcodeBinary, payload) }

func (c *Conn) WriteMessage(opcode byte, payload []byte) error {
	if opcode != OpcodeText && opcode != OpcodeBinary {
		return errors.New("websocket: invalid data opcode")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(true, opcode, payload)
}

func (c *Conn) writeFrame(fin bool, opcode byte, payload []byte) error {
	if len(payload) > c.maxMessage {
		return errors.New("websocket: message too large")
	}
	first := opcode
	if fin {
		first |= 0x80
	}
	header := make([]byte, 0, 14)
	header = append(header, first)
	n := len(payload)
	switch {
	case n <= 125:
		header = append(header, 0x80|byte(n))
	case n <= 65535:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		var tmp [8]byte
		binary.BigEndian.PutUint64(tmp[:], uint64(n))
		header = append(header, tmp[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i&3]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

func (c *Conn) ReadMessage() (byte, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var messageOpcode byte
	var message []byte
	fragmented := false
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case OpcodePing:
			c.writeMu.Lock()
			err := c.writeFrame(true, OpcodePong, payload)
			c.writeMu.Unlock()
			if err != nil {
				return 0, nil, err
			}
			continue
		case OpcodePong:
			continue
		case OpcodeClose:
			return 0, nil, io.EOF
		case OpcodeText, OpcodeBinary:
			if fragmented {
				return 0, nil, fmt.Errorf("%w: new data frame during fragmentation", ErrProtocol)
			}
			messageOpcode = opcode
			message = append(message[:0], payload...)
			if fin {
				return messageOpcode, message, nil
			}
			fragmented = true
		case OpcodeContinuation:
			if !fragmented {
				return 0, nil, fmt.Errorf("%w: unexpected continuation", ErrProtocol)
			}
			if len(message)+len(payload) > c.maxMessage {
				return 0, nil, errors.New("websocket: message too large")
			}
			message = append(message, payload...)
			if fin {
				return messageOpcode, message, nil
			}
		default:
			return 0, nil, fmt.Errorf("%w: unsupported opcode %d", ErrProtocol, opcode)
		}
	}
}

func (c *Conn) readFrame() (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.reader, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, fmt.Errorf("%w: RSV bits set", ErrProtocol)
	}
	opcode := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	if masked {
		return false, 0, nil, fmt.Errorf("%w: server frame is masked", ErrProtocol)
	}
	length := uint64(h[1] & 0x7f)
	if length == 126 {
		var b [2]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(b[:]))
	}
	if length == 127 {
		var b [8]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(b[:])
		if length>>63 != 0 {
			return false, 0, nil, fmt.Errorf("%w: invalid length", ErrProtocol)
		}
	}
	if opcode >= 8 && (!fin || length > 125) {
		return false, 0, nil, fmt.Errorf("%w: invalid control frame", ErrProtocol)
	}
	if length > uint64(c.maxMessage) {
		return false, 0, nil, errors.New("websocket: message too large")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return false, 0, nil, err
	}
	return fin, opcode, payload, nil
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, line := range h.Values(name) {
		for _, part := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
