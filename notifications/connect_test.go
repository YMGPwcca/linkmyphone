package notifications

import (
	"context"
	"errors"
	"github.com/YMGPwcca/linkmyphone/protocol/app"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
	"testing"
	"time"
)

func TestConnectRequiresCorrelatedPermissionAndTypedCapabilities(t *testing.T) {
	for _, permission := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "granted"}[permission], func(t *testing.T) {
			c, _, transport := newFixtureClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("client did not stop")
				}
			}()
			connected := make(chan error, 1)
			go func() { _, err := c.Connect(ctx); connected <- err }()
			var request sentMessage
			select {
			case request = <-transport.sent:
			case <-time.After(time.Second):
				t.Fatal("connect not sent")
			}
			id, _ := request.message.Header(platform.HeaderRequestID)
			values := app.ValueSet{"result": int32(0), "permissions": app.ValueSet{"notifications": permission}, "capabilities": app.ValueSet{"notifications": int32(2)}}
			payload, err := app.MarshalMessage(app.NewResponse(id, values))
			if err != nil {
				t.Fatal(err)
			}
			wrong := relay.Received{Source: "other-phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
			transport.received <- wrong
			barrierValues := app.ValueSet{"contentType": "notifications", "notificationKeys": []string{}, "operations": []int32{}, "notifications": []string{}}
			barrier, err := app.MarshalMessage(app.NewRequest("/legacy/phonecontent", "barrier", barrierValues))
			if err != nil {
				t.Fatal(err)
			}
			transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: barrier}
			select {
			case <-transport.sent:
			case <-time.After(time.Second):
				t.Fatal("receive barrier not processed")
			}
			select {
			case err := <-connected:
				t.Fatalf("another peer satisfied connect: %v", err)
			default:
			}
			transport.received <- relay.Received{Source: "phone", TransportMessageType: dcg.TransportMessageTypeApp, Payload: payload}
			select {
			case err := <-connected:
				if permission && err != nil {
					t.Fatal(err)
				}
				if !permission && !errors.Is(err, ErrPermission) {
					t.Fatalf("permission error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("connect did not complete")
			}
			c.stateMu.Lock()
			ready := c.ready
			c.stateMu.Unlock()
			if ready != permission {
				t.Fatalf("permission=%t ready=%t", permission, ready)
			}
		})
	}
}
