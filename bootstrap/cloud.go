package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/YMGPwcca/phonelink-linux/dcgheaders"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
	signalrtransport "github.com/YMGPwcca/phonelink-linux/transport/signalr"
)

const (
	DefaultRelayHubEndpoint   = "relayhub/"
	DefaultOnConnectedTimeout = 10 * time.Second
	DefaultHeartbeatSeconds   = 15
)

type HubDialer func(context.Context, signalrtransport.Config) (relay.Hub, error)

type CloudConfig struct {
	Services           *servicedcg.Client
	MSAAccessToken     string
	DCGAccessToken     string
	ClientInfo         dcgheaders.ClientInfo
	HubURL             string
	OnConnectedTimeout time.Duration
	HTTPClient         *http.Client
	RelayConfig        relay.Config
	DialHub            HubDialer
}

type CloudRelay struct {
	Relay    *relay.Client
	Region   string
	Partners []string

	cancel    context.CancelFunc
	runErr    chan error
	closeOnce sync.Once
	closeErr  error
}

func (c *CloudRelay) Errors() <-chan error {
	if c == nil {
		return nil
	}
	return c.runErr
}

func (c *CloudRelay) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.Relay != nil {
			c.closeErr = c.Relay.Close()
		}
	})
	return c.closeErr
}

// OpenCloudRelay mirrors the peerless account connection bootstrap:
// fetch assigned SignalR shards using the MSA token, connect the account-level
// relay hub using the DCG "general" token, wait for OnConnected, and reject a
// connection whose resolved region differs from the selected shard.
func OpenCloudRelay(ctx context.Context, cfg CloudConfig) (*CloudRelay, error) {
	if cfg.Services == nil {
		return nil, errors.New("bootstrap: DCG services client is required")
	}
	if cfg.MSAAccessToken == "" {
		return nil, errors.New("bootstrap: MSA access token is required")
	}
	if cfg.DCGAccessToken == "" {
		return nil, errors.New("bootstrap: DCG access token is required")
	}
	if err := validateClientInfo(cfg.ClientInfo); err != nil {
		return nil, err
	}
	timeout := cfg.OnConnectedTimeout
	if timeout <= 0 {
		timeout = DefaultOnConnectedTimeout
	}
	hubURL := cfg.HubURL
	if hubURL == "" {
		base := cfg.Services.BaseURL
		if base == "" {
			base = servicedcg.ProdServiceBase
		}
		hubURL = strings.TrimRight(base, "/") + "/" + DefaultRelayHubEndpoint
	}
	dial := cfg.DialHub
	if dial == nil {
		dial = func(ctx context.Context, cfg signalrtransport.Config) (relay.Hub, error) {
			return signalrtransport.Dial(ctx, cfg)
		}
	}

	shards, err := cfg.Services.AssignedSignalRShards(ctx, cfg.MSAAccessToken)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: get SignalR shards: %w", err)
	}
	if len(shards.AssignedShards) == 0 {
		return nil, errors.New("bootstrap: account has no assigned SignalR shards")
	}

	var lastErr error
	for _, shard := range shards.AssignedShards {
		region := strings.TrimSpace(shard.Region)
		if region == "" {
			continue
		}
		traceParent, traceState, err := newSignalRTrace()
		if err != nil {
			return nil, err
		}
		headers := (dcgheaders.SignalRInfo{
			ClientInfo:       cfg.ClientInfo,
			HubRegion:        region,
			TraceParent:      traceParent,
			TraceState:       traceState,
			HeartbeatSeconds: DefaultHeartbeatSeconds,
		}).Headers()

		hub, err := dial(ctx, signalrtransport.Config{
			HubURL:      hubURL,
			AccessToken: cfg.DCGAccessToken,
			Headers:     headers,
			HTTPClient:  cfg.HTTPClient,
		})
		if err != nil {
			lastErr = fmt.Errorf("region %s: %w", region, err)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}

		relayClient := relay.New(hub, cfg.RelayConfig)
		runCtx, runCancel := context.WithCancel(context.Background())
		runErr := make(chan error, 1)
		go func() {
			runErr <- relayClient.Run(runCtx)
		}()

		waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
		type connectedResult struct {
			region   string
			partners []string
			err      error
		}
		connected := make(chan connectedResult, 1)
		go func() {
			payload, err := relayClient.WaitHubConnected(waitCtx)
			connected <- connectedResult{
				region:   payload.RegionName,
				partners: append([]string(nil), payload.Partners...),
				err:      err,
			}
		}()

		var result connectedResult
		connectionFailed := false
		select {
		case result = <-connected:
			if result.err != nil {
				lastErr = fmt.Errorf("region %s: wait OnConnected: %w", region, result.err)
				connectionFailed = true
			}
		case err := <-runErr:
			if err == nil {
				err = errors.New("relay read loop exited")
			}
			lastErr = fmt.Errorf("region %s: %w", region, err)
			connectionFailed = true
		case <-waitCtx.Done():
			lastErr = fmt.Errorf("region %s: wait OnConnected: %w", region, waitCtx.Err())
			connectionFailed = true
		}
		waitCancel()

		if connectionFailed {
			runCancel()
			_ = relayClient.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if !strings.EqualFold(region, result.region) {
			lastErr = fmt.Errorf(
				"region %s: Hub Relay resolved region %q",
				region,
				result.region,
			)
			runCancel()
			_ = relayClient.Close()
			continue
		}

		return &CloudRelay{
			Relay:    relayClient,
			Region:   region,
			Partners: result.partners,
			cancel:   runCancel,
			runErr:   runErr,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no non-empty SignalR shard was returned")
	}
	return nil, fmt.Errorf("bootstrap: no usable SignalR shard: %w", lastErr)
}

func validateClientInfo(info dcgheaders.ClientInfo) error {
	switch {
	case info.LogicalDeviceID == "":
		return errors.New("bootstrap: DCG logical device id is required")
	case info.AppVersion == "":
		return errors.New("bootstrap: DCG app version is required")
	case info.AppID == "":
		return errors.New("bootstrap: DCG app id is required")
	case info.SessionID == "":
		return errors.New("bootstrap: DCG app session id is required")
	case info.RingName == "":
		return errors.New("bootstrap: DCG ring name is required")
	case info.OS == "":
		return errors.New("bootstrap: DCG OS is required")
	case info.OSVersion == "":
		return errors.New("bootstrap: DCG OS version is required")
	default:
		return nil
	}
}

func newSignalRTrace() (traceParent, traceState string, err error) {
	traceID, err := randomHex(16)
	if err != nil {
		return "", "", err
	}
	correlationID, err := randomHex(8)
	if err != nil {
		return "", "", err
	}
	return "00-" + traceID + "-" + correlationID + "-00",
		"yppHttpParent=" + correlationID,
		nil
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
