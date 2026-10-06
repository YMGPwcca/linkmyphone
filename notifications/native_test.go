package notifications

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"testing"
	"time"
)

func TestDesktopStartupCancellationInterruptsBusAuthentication(t *testing.T) {
	for _, name := range []string{"deadline", "cancellation"} {
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			port := listener.Addr().(*net.TCPAddr).Port
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", fmt.Sprintf("tcp:host=127.0.0.1,port=%d", port))
			accepted := make(chan net.Conn, 1)
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				accepted <- conn
				// Consume auth bytes but never reply, simulating a stuck bus.
				_, _ = io.Copy(io.Discard, conn)
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer deadlineCancel()
			}
			result := make(chan error, 1)
			go func() {
				backend, err := NewDesktop(ctx)
				if backend != nil {
					_ = backend.Close()
				}
				result <- err
			}()
			var serverConn net.Conn
			select {
			case serverConn = <-accepted:
				defer serverConn.Close()
			case <-time.After(time.Second):
				t.Fatal("desktop did not connect to test bus")
			}
			want := error(context.DeadlineExceeded)
			if name == "cancellation" {
				want = context.Canceled
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("startup err=%v, want %v", err, want)
				}
			case <-time.After(time.Second):
				_ = serverConn.Close()
				<-result
				t.Fatal("startup cancellation did not interrupt bus authentication")
			}
			select {
			case <-serverDone:
			case <-time.After(time.Second):
				t.Fatal("cancelled startup left its bus connection open")
			}
		})
	}
}

func TestNotificationActionsOmitWhenUnsupported(t *testing.T) {
	actions, err := notificationActions([]DesktopAction{{ID: "reply", Label: "Reply"}}, false)
	if err != nil {
		t.Fatalf("notificationActions returned error: %v", err)
	}
	if actions != nil {
		t.Fatalf("unsupported action capability forwarded actions: %#v", actions)
	}
}

func TestNotificationActionsRejectEmptyName(t *testing.T) {
	_, err := notificationActions([]DesktopAction{{ID: "", Label: "Reply"}}, true)
	if err == nil {
		t.Fatal("notificationActions accepted an empty action id")
	}
}

func TestDecodeNotificationIconRejectsNonJPEG(t *testing.T) {
	_, err := decodeNotificationIcon([]byte("not an image"))
	if err == nil {
		t.Fatal("non-JPEG icon accepted")
	}
}

func TestDecodeNotificationIconProducesBoundedImageData(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 2, 3))
	input.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, input, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	decoded, err := decodeNotificationIcon(encoded.Bytes())
	if err != nil {
		t.Fatalf("decodeNotificationIcon: %v", err)
	}
	if decoded.Width != 2 || decoded.Height != 3 || decoded.RowStride != 8 || !decoded.HasAlpha || decoded.BitsPerSample != 8 || decoded.Channels != 4 {
		t.Fatalf("unexpected image-data metadata: %#v", decoded)
	}
	if len(decoded.Data) != 2*3*4 {
		t.Fatalf("image-data length = %d, want %d", len(decoded.Data), 2*3*4)
	}
}

func TestValidateNotificationRejectsOversizedIcon(t *testing.T) {
	if err := validateNotification(DesktopNotification{AppName: "Phone", Icon: make([]byte, maxIconBytes+1)}); err == nil {
		t.Fatal("validateNotification accepted an oversized icon")
	}
}
