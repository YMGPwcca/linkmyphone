package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	psignalr "github.com/YMGPwcca/linkmyphone/protocol/signalr"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

const (
	DefaultPeerWakeTimeout  = 45 * time.Second
	DefaultPeerWakeTTL      = 60 * time.Second
	DefaultPartnerFlushTimeout = 10 * time.Second
)

type PeerOnlineOptions struct {
	Timeout              time.Duration
	WakeTTL              time.Duration
	IgnoreDeviceDisabled bool
	RequestNewSession    bool
}

// EnsurePeerOnline checks Hub Relay presence first. If the target is not
// present, it sends the source-confirmed signed Dispatcher/Wake payload and
// waits for OnPartnerConnected (or other partner traffic that marks presence).
// The returned bool reports whether a wake request was sent.
func EnsurePeerOnline(
	ctx context.Context,
	cloud *CloudRelay,
	services *servicedcg.Client,
	trustIdentity *dcgauth.TrustIdentity,
	localDcgClientID string,
	targetDcgClientID string,
	dcgToken string,
	options PeerOnlineOptions,
) (bool, error) {
	if cloud == nil || cloud.Relay == nil {
		return false, errors.New("bootstrap: connected cloud relay is required")
	}
	if services == nil {
		return false, errors.New("bootstrap: DCG service client is required")
	}
	if trustIdentity == nil {
		return false, errors.New("bootstrap: trust identity is required")
	}
	if localDcgClientID == "" || targetDcgClientID == "" {
		return false, errors.New("bootstrap: local and target DCG client ids are required")
	}
	if dcgToken == "" {
		return false, errors.New("bootstrap: DCG token is required")
	}
	if cloud.Relay.PartnerConnected(targetDcgClientID) {
		return false, nil
	}

	// Windows flushes the Hub partner mapping with SendConnectedAsync before
	// issuing a wake. This is not just a reciprocal response to
	// OnPartnerConnected; WakePartnerAndWaitForConnectionInternalAsync calls
	// FlushConnectionAsync first and awaits the Hub Completion.
	flushCtx, flushCancel := context.WithTimeout(ctx, DefaultPartnerFlushTimeout)
	err := cloud.Relay.FlushPartner(flushCtx, targetDcgClientID, psignalr.TraceContextPacket{})
	flushCancel()
	if err != nil {
		return false, fmt.Errorf("bootstrap: flush partner before wake: %w", err)
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultPeerWakeTimeout
	}
	ttl := options.WakeTTL
	if ttl <= 0 {
		ttl = DefaultPeerWakeTTL
	}

	if err := WakePartner(
		ctx,
		services,
		trustIdentity,
		localDcgClientID,
		targetDcgClientID,
		dcgToken,
		WakeOptions{
			Environment:          "Prod",
			HubRegion:            cloud.Region,
			IgnoreDeviceDisabled: options.IgnoreDeviceDisabled,
			RequestNewSession:    options.RequestNewSession,
			TimeToLive:           ttl,
		},
	); err != nil {
		return true, fmt.Errorf("bootstrap: wake peer: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cloud.Relay.WaitPartnerConnected(waitCtx, targetDcgClientID)
	}()

	select {
	case err := <-waitDone:
		if err != nil {
			return true, fmt.Errorf("bootstrap: wait for peer presence: %w", err)
		}
		return true, nil
	case err := <-cloud.Errors():
		if err == nil {
			err = errors.New("relay read loop exited")
		}
		return true, fmt.Errorf("bootstrap: relay failed while waiting for peer: %w", err)
	case <-waitCtx.Done():
		return true, fmt.Errorf("bootstrap: wait for peer presence: %w", waitCtx.Err())
	}
}
