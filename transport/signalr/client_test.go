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
	server, done := newSignalRTestServer(t, func(c net.Conn, r *bufio.Reader) error {
		op, p, err := readMasked(r)
		if err != nil {
			return err
		}
		if op != 1 || string(p) != "{\"protocol\":\"messagepack\",\"version\":1}\x1e" {
			return fmt.Errorf("bad handshake request %d %q", op, p)
		}
		writePlain(c, 1, []byte("{}\x1e"))

		op, p, err = readMasked(r)
		if err != nil {
			return err
		}
		if op != 2 || string(p) != "abc" {
			return fmt.Errorf("bad data %d %q", op, p)
		}
		writePlain(c, 2, []byte("xyz"))
		return nil
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := Dial(ctx, Config{HubURL: server.URL + "/relayhub/", AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err := client.SendBinary([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	p, err := client.ReadBinary()
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

func TestDialAcceptsBinaryHandshakeAndPreservesCoalescedHubData(t *testing.T) {
	server, done := newSignalRTestServer(t, func(c net.Conn, r *bufio.Reader) error {
		op, p, err := readMasked(r)
		if err != nil {
			return err
		}
		if op != 1 || string(p) != "{\"protocol\":\"messagepack\",\"version\":1}\x1e" {
			return fmt.Errorf("bad handshake request %d %q", op, p)
		}

		// Azure SignalR may carry the JSON handshake response in a binary
		// WebSocket message. It can also coalesce the first MessagePack hub
		// payload after the record separator in that same message.
		writePlain(c, 2, append([]byte("{}\x1e"), []byte("first-hub-frame")...))
		return nil
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := Dial(ctx, Config{HubURL: server.URL + "/relayhub/", AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	p, err := client.ReadBinary()
	if err != nil {
		t.Fatal(err)
	}
	if string(p) != "first-hub-frame" {
		t.Fatalf("payload=%q", p)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConsumeHandshakeRejectsMissingRecordSeparator(t *testing.T) {
	if _, err := consumeHandshake([]byte("{}")); err == nil {
		t.Fatal("expected malformed handshake error")
	}
}

func newSignalRTestServer(
	t *testing.T,
	session func(net.Conn, *bufio.Reader) error,
) (*httptest.Server, <-chan error) {
	t.Helper()

	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/negotiate") {
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"connectionToken":"ct","availableTransports":[{"transport":"WebSockets","transferFormats":["Binary"]}]}`)
			return
		}

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			done <- fmt.Errorf("server does not support hijacking")
			return
		}
		conn, brw, err := hijacker.Hijack()
		if err != nil {
			done <- err
			return
		}

		key := r.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + guid))
		_, err = fmt.Fprintf(
			brw,
			"HTTP/1.1 101 Switching Protocols\r\n"+
				"Upgrade: websocket\r\n"+
				"Connection: Upgrade\r\n"+
				"Sec-WebSocket-Accept: %s\r\n"+
				"\r\n",
			base64.StdEncoding.EncodeToString(sum[:]),
		)
		if err == nil {
			err = brw.Flush()
		}
		if err != nil {
			_ = conn.Close()
			done <- err
			return
		}

		go func() {
			defer conn.Close()
			done <- session(conn, brw.Reader)
		}()
	}))

	return server, done
}

func readMasked(r *bufio.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("unmasked client frame")
	}

	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var x [2]byte
		if _, err := io.ReadFull(r, x[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(x[:]))
	case 127:
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
	payload := make([]byte, int(n))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return h[0] & 0x0f, payload, nil
}

func writePlain(w net.Conn, opcode byte, payload []byte) {
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) <= 125:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(payload)))
		header = append(header, 127)
		header = append(header, n[:]...)
	}
	_, _ = w.Write(append(header, payload...))
}
