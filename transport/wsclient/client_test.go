package wsclient

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDialAndBinary(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			done <- err
			return
		}
		key := req.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + websocketGUID))
		accept := base64.StdEncoding.EncodeToString(sum[:])
		fmt.Fprintf(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)

		op, payload, err := readClientFrame(br)
		if err != nil {
			done <- err
			return
		}
		if op != OpcodeBinary || string(payload) != "hello" {
			done <- fmt.Errorf("bad client frame op=%d payload=%q", op, payload)
			return
		}
		if err := writeServerFrame(c, OpcodeBinary, []byte("world")); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := Dial(ctx, "ws://"+ln.Addr().String()+"/relay", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteBinary([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	op, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if op != OpcodeBinary || string(payload) != "world" {
		t.Fatalf("op=%d payload=%q", op, payload)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func readClientFrame(r *bufio.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("client frame not masked")
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var x [2]byte
		if _, err := io.ReadFull(r, x[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(x[:]))
	} else if n == 127 {
		var x [8]byte
		if _, err := io.ReadFull(r, x[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(x[:])
	}
	var mask [4]byte
	if _, err := io.ReadFull(r, mask[:]); err != nil {
		return 0, nil, err
	}
	p := make([]byte, int(n))
	if _, err := io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	for i := range p {
		p[i] ^= mask[i&3]
	}
	return h[0] & 0xf, p, nil
}

func writeServerFrame(w io.Writer, op byte, p []byte) error {
	if len(p) > 125 {
		return fmt.Errorf("test payload too large")
	}
	_, err := w.Write(append([]byte{0x80 | op, byte(len(p))}, p...))
	return err
}
