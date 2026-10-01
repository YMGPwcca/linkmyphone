package clipboard

import (
	"context"
	"testing"
	"time"

	clipclient "github.com/YMGPwcca/phonelink-linux/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
)

type silentRelay struct {
	received chan relay.Received
}

func newSilentRelay() *silentRelay {
	return &silentRelay{received: make(chan relay.Received)}
}

func (r *silentRelay) Send(
	context.Context,
	string,
	string,
	dcg.TransportMessageType,
	[]byte,
) error {
	return nil
}

func (r *silentRelay) Received() <-chan relay.Received {
	return r.received
}

func TestInstanceStopDoesNotSpendWholeRequestTimeoutOnFeatureOff(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		<-runCtx.Done()
		close(done)
	}()

	client := clipclient.New(newSilentRelay(), nil, clipclient.Config{
		Target:          "phone",
		SelfDcgClientID: "desktop",
		RequestTimeout:  time.Second,
	})

	instance := &instance{
		moduleID:          "phonelink.clipboard",
		client:            client,
		requestTimeout:    time.Second,
		featureOffTimeout: 20 * time.Millisecond,
		cancel:            cancel,
		done:              done,
		errors:            make(chan error, 1),
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer stopCancel()

	start := time.Now()
	if err := instance.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
		t.Fatalf("Stop took %s; FEATURE_OFF consumed too much shutdown budget", elapsed)
	}
}

func TestInstanceStopIsIdempotentAfterCancellation(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		<-runCtx.Done()
		close(done)
	}()

	client := clipclient.New(newSilentRelay(), nil, clipclient.Config{
		Target:          "phone",
		SelfDcgClientID: "desktop",
		RequestTimeout:  time.Second,
	})

	instance := &instance{
		moduleID:          "phonelink.clipboard",
		client:            client,
		requestTimeout:    time.Second,
		featureOffTimeout: 10 * time.Millisecond,
		cancel:            cancel,
		done:              done,
		errors:            make(chan error, 1),
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), time.Second)
	defer ctxCancel()
	if err := instance.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := instance.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
