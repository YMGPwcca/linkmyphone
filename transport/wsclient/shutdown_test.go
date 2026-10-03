package wsclient

import (
	"net"
	"sync"
	"testing"
	"time"
)

type observedWriter struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *observedWriter) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(b)
}

func TestCloseInterruptsBlockedWriter(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	writer := &observedWriter{Conn: client, started: make(chan struct{})}
	conn := &Conn{conn: writer, maxMessage: 4096}
	written := make(chan error, 1)
	go func() { written <- conn.WriteBinary([]byte("network has stopped reading")) }()
	<-writer.started
	closed := make(chan error, 1)
	go func() { closed <- conn.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close blocked behind writer")
	}
	select {
	case err := <-written:
		if err == nil {
			t.Fatal("blocked write succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("write not interrupted")
	}
}
