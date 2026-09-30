package signalr

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
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func TestDialHandshakeAndBinary(t *testing.T) {
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/negotiate") {
			if r.Method != http.MethodPost {
				http.Error(w, "method", 405)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"connectionToken":"ct","availableTransports":[{"transport":"WebSockets","transferFormats":["Binary"]}]}`)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			panic("no hijack")
		}
		c, brw, err := hj.Hijack()
		if err != nil {
			done <- err
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + guid))
		fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
		brw.Flush()
		go func() {
			defer c.Close()
			op, p, err := readMasked(brw.Reader)
			if err != nil {
				done <- err
				return
			}
			if op != 1 || string(p) != "{\"protocol\":\"messagepack\",\"version\":1}\x1e" {
				done <- fmt.Errorf("bad handshake %d %q", op, p)
				return
			}
			writePlain(c, 1, []byte("{}\x1e"))
			op, p, err = readMasked(brw.Reader)
			if err != nil {
				done <- err
				return
			}
			if op != 2 || string(p) != "abc" {
				done <- fmt.Errorf("bad data %d %q", op, p)
				return
			}
			writePlain(c, 2, []byte("xyz"))
			done <- nil
		}()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{HubURL: server.URL + "/relayhub/", AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SendBinary([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	p, err := c.ReadBinary()
	if err != nil {
		t.Fatal(err)
	}
	if string(p) != "xyz" {
		t.Fatalf("payload=%q", p)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func readMasked(r *bufio.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("unmasked")
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

func writePlain(w net.Conn, op byte, p []byte) {
	_, _ = w.Write(append([]byte{0x80 | op, byte(len(p))}, p...))
}
