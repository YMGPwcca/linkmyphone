package phonehost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
	"github.com/YMGPwcca/linkmyphone/bootstrap"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

const (
	DefaultAppVersion = "1.26072.116.0"
	DefaultRingName   = "Public"
	DefaultOSVersion  = "10.0.26100"
)

type Config struct {
	StatePath          string
	AppVersion         string
	RingName           string
	OSVersion          string
	Target             string
	SignalRTimeout     time.Duration
	WakeTimeout        time.Duration
	WakeTTL            time.Duration
	RequestTimeout     time.Duration
	ReconnectMinDelay  time.Duration
	ReconnectMaxDelay  time.Duration
	SessionOpenTimeout time.Duration
	RefreshMargin      time.Duration
}

type Session struct {
	cloud           *bootstrap.CloudRelay
	router          *Router
	runCtx          context.Context
	cancel          context.CancelFunc
	routingOnce     sync.Once
	errors          chan error
	closeOnce       sync.Once
	closeErr        error
	Target          servicedcg.DeviceInfo
	SelfDcgClientID string
	Region          string
	Profile         dcgheaders.Profile
	AppVersion      string
	RingName        string
	InstallationID  string
}

func (s *Session) Subscribe(name string, matcher Matcher, queueSize int) (*Endpoint, error) {
	if s == nil || s.router == nil {
		return nil, errors.New("phonehost: session router is unavailable")
	}
	s.ensureRouting()
	return s.router.Subscribe(name, matcher, queueSize)
}

func (s *Session) ensureRouting() {
	if s == nil || s.router == nil {
		return
	}
	s.routingOnce.Do(func() {
		go func() {
			if err := s.router.Run(s.runCtx); err != nil &&
				!errors.Is(err, context.Canceled) {
				s.reportRuntimeError(err)
			}
		}()
	})
}

func (s *Session) Errors() <-chan error {
	if s == nil {
		return nil
	}
	return s.errors
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.router != nil {
			s.router.Close()
		}
		if s.cloud != nil {
			s.closeErr = s.cloud.Close()
		}
	})
	return s.closeErr
}

func Open(ctx context.Context, cfg Config, reporter kernel.Reporter) (*Session, error) {
	if err := normalizeConfig(&cfg); err != nil {
		return nil, permanent(err)
	}

	snapshot, err := authstate.Load(cfg.StatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, permanent(errors.New("phonehost: no persisted enrollment found; run bootstrap-probe first"))
		}
		return nil, permanent(fmt.Errorf("phonehost: load state: %w", err))
	}
	if snapshot.Enrollment.AccountCert == "" {
		return nil, permanent(errors.New("phonehost: persisted enrollment has no account certificate"))
	}
	if snapshot.MSARefreshToken == "" {
		return nil, permanent(errors.New("phonehost: no refresh credential; run bootstrap-probe first"))
	}
	if _, err := snapshot.Identity.Identity(); err != nil {
		return nil, permanent(err)
	}
	if _, err := snapshot.TrustIdentity.Trust(); err != nil {
		return nil, permanent(err)
	}

	clientInfo, err := dcgheaders.NewClientInfo(
		snapshot.ClientProfile,
		snapshot.LogicalDeviceID,
		cfg.AppVersion,
		cfg.RingName,
		cfg.OSVersion,
	)
	if err != nil {
		return nil, permanent(fmt.Errorf("phonehost: build client profile info: %w", err))
	}

	msaClient := msa.NewDeviceCodeClient()
	authClient := dcgauth.NewClient(dcgauth.ProdServiceBase)
	serviceClient := servicedcg.NewClient(servicedcg.ProdServiceBase)
	configureDCGClients(authClient, serviceClient, clientInfo)

	report(reporter, "resuming Microsoft and DCG identity", nil)
	issuedAt := time.Now()
	resumed, err := bootstrap.ResumeAuthAndSave(ctx, msaClient, authClient, snapshot, cfg.StatePath)
	if err != nil {
		return nil, fmt.Errorf("phonehost: resume authentication: %w", err)
	}
	snapshot = resumed.State
	report(reporter, "authentication ready", map[string]string{
		"dcg_client_id": shortID(resumed.Identity.DeviceID),
	})

	report(reporter, "refreshing linked-device trust", nil)
	trust, err := bootstrap.SyncTrust(
		ctx,
		serviceClient,
		resumed.MSAToken.AccessToken,
		resumed.Identity.DeviceID,
		snapshot.Enrollment.AccountCert,
		"",
		time.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("phonehost: trust sync: %w", err)
	}
	snapshot.TrustRelationships = append([]dcgauth.TrustRelationship(nil), trust.Relationships()...)
	if err := authstate.Save(cfg.StatePath, snapshot); err != nil {
		return nil, permanent(fmt.Errorf("phonehost: persist trust state: %w", err))
	}

	target, err := SelectPeer(trust.Devices, resumed.Identity.DeviceID, cfg.Target)
	if err != nil {
		return nil, permanent(err)
	}
	report(reporter, "target selected", map[string]string{
		"name":          PeerDisplayName(target),
		"os":            PeerOSName(target),
		"dcg_client_id": shortID(target.ID),
	})

	report(reporter, "connecting Hub Relay", nil)
	cloud, err := bootstrap.OpenCloudRelay(ctx, bootstrap.CloudConfig{
		Services:           serviceClient,
		MSAAccessToken:     resumed.MSAToken.AccessToken,
		DCGAccessToken:     resumed.ServicesToken.Token,
		ClientInfo:         clientInfo,
		OnConnectedTimeout: cfg.SignalRTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("phonehost: SignalR bootstrap: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = cloud.Close()
		}
	}()

	woke, err := bootstrap.EnsurePeerOnline(
		ctx,
		cloud,
		serviceClient,
		resumed.TrustIdentity,
		resumed.Identity.DeviceID,
		target.ID,
		resumed.ServicesToken.Token,
		bootstrap.PeerOnlineOptions{
			Timeout:           cfg.WakeTimeout,
			WakeTTL:           cfg.WakeTTL,
			RequestNewSession: true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("phonehost: peer presence: %w", err)
	}
	fields := map[string]string{"region": cloud.Region}
	if woke {
		fields["wake"] = "sent"
	} else {
		fields["wake"] = "not-needed"
	}
	report(reporter, "peer online", fields)

	if _, err := bootstrap.ValidatePlatformSession(
		ctx,
		cloud.Relay,
		target.ID,
		cfg.RequestTimeout,
	); err != nil {
		return nil, fmt.Errorf("phonehost: SessionValidation: %w", err)
	}
	report(reporter, "PLATFORM session ready", nil)

	runCtx, runCancel := context.WithCancel(context.Background())
	session := &Session{
		cloud:           cloud,
		runCtx:          runCtx,
		cancel:          runCancel,
		errors:          make(chan error, 8),
		Target:          target,
		SelfDcgClientID: resumed.Identity.DeviceID,
		Region:          cloud.Region,
		Profile:         snapshot.ClientProfile,
		AppVersion:      cfg.AppVersion,
		RingName:        cfg.RingName,
		InstallationID:  snapshot.LogicalDeviceID,
	}
	session.router = newRouter(cloud.Relay)
	// The host is the sole owner of the raw relay receive stream even when no
	// feature is enabled. Keep draining unmatched application traffic so a
	// zero-module runtime (or disabling the final module) cannot backpressure
	// relay ACK/completion processing.
	session.ensureRouting()
	partnerDisconnects := cloud.Relay.PartnerDisconnects(target.ID)
	go session.monitorHealth(runCtx, refreshDeadline(issuedAt, resumed.MSAToken.ExpiresIn, resumed.ServicesToken.ExpiresAt, cfg.RefreshMargin),
		DefaultHealthInterval, func() bool {
			return cloud.Relay.PartnerConnected(target.ID) && cloud.Relay.PartnerDisconnects(target.ID) == partnerDisconnects
		}, time.Now)

	go func() {
		select {
		case <-runCtx.Done():
			return
		case err, ok := <-cloud.Errors():
			if !ok {
				return
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				session.reportRuntimeError(fmt.Errorf("phonehost: Hub Relay: %w", err))
			}
		}
	}()

	ok = true
	return session, nil
}

func (s *Session) reportRuntimeError(err error) {
	if s == nil || err == nil {
		return
	}
	select {
	case s.errors <- &recoveryError{err: err}:
	default:
	}
}

func normalizeConfig(cfg *Config) error {
	if cfg.StatePath == "" {
		path, err := authstate.DefaultPath()
		if err != nil {
			return fmt.Errorf("phonehost: resolve state path: %w", err)
		}
		cfg.StatePath = path
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = DefaultAppVersion
	}
	if cfg.RingName == "" {
		cfg.RingName = DefaultRingName
	}
	if cfg.OSVersion == "" {
		cfg.OSVersion = DefaultOSVersion
	}
	if cfg.SignalRTimeout <= 0 {
		cfg.SignalRTimeout = bootstrap.DefaultOnConnectedTimeout
	}
	if cfg.WakeTimeout <= 0 {
		cfg.WakeTimeout = bootstrap.DefaultPeerWakeTimeout
	}
	if cfg.WakeTTL <= 0 {
		cfg.WakeTTL = bootstrap.DefaultPeerWakeTTL
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.ReconnectMinDelay == 0 {
		cfg.ReconnectMinDelay = DefaultReconnectMinDelay
	}
	if cfg.ReconnectMaxDelay == 0 {
		cfg.ReconnectMaxDelay = DefaultReconnectMaxDelay
	}
	if cfg.SessionOpenTimeout == 0 {
		cfg.SessionOpenTimeout = DefaultSessionOpenTimeout
	}
	if cfg.RefreshMargin == 0 {
		cfg.RefreshMargin = DefaultRefreshMargin
	}
	if cfg.ReconnectMinDelay <= 0 || cfg.ReconnectMaxDelay < cfg.ReconnectMinDelay || cfg.SessionOpenTimeout <= 0 || cfg.RefreshMargin <= 0 {
		return errors.New("phonehost: recovery durations must be positive and max reconnect delay must be >= min delay")
	}
	return nil
}

func configureDCGClients(authClient *dcgauth.Client, serviceClient *servicedcg.Client, info dcgheaders.ClientInfo) {
	authClient.AuthorizationPortal = dcgheaders.PortalFirstParty
	authClient.ClientInfo = info
	serviceClient.AuthorizationPortal = dcgheaders.PortalFirstParty
	serviceClient.ClientInfo = info
}

func SelectPeer(devices []servicedcg.DeviceInfo, selfID, selector string) (servicedcg.DeviceInfo, error) {
	peers := servicedcg.LinkedPeers(devices, selfID)
	if len(peers) == 0 {
		return servicedcg.DeviceInfo{}, errors.New("phonehost: no linked peers returned by DeviceInfoList")
	}

	selector = strings.TrimSpace(selector)
	if selector == "" {
		android := make([]servicedcg.DeviceInfo, 0, 1)
		for _, peer := range peers {
			if strings.EqualFold(strings.TrimSpace(peer.OSName), "Android") {
				android = append(android, peer)
			}
		}
		switch len(android) {
		case 0:
			return servicedcg.DeviceInfo{}, errors.New("phonehost: no linked Android device found; select a target explicitly")
		case 1:
			return android[0], nil
		default:
			return servicedcg.DeviceInfo{}, fmt.Errorf(
				"phonehost: %d linked Android devices found; select a target explicitly",
				len(android),
			)
		}
	}

	for _, peer := range peers {
		if strings.EqualFold(peer.ID, selector) {
			return peer, nil
		}
	}
	exact := make([]servicedcg.DeviceInfo, 0, 1)
	for _, peer := range peers {
		if strings.EqualFold(strings.TrimSpace(peer.Name), selector) ||
			strings.EqualFold(strings.TrimSpace(peer.ModelName), selector) {
			exact = append(exact, peer)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return servicedcg.DeviceInfo{}, fmt.Errorf("phonehost: target %q is ambiguous", selector)
	}

	selectorLower := strings.ToLower(selector)
	partial := make([]servicedcg.DeviceInfo, 0, 1)
	for _, peer := range peers {
		haystack := strings.ToLower(peer.ID + "\n" + peer.Name + "\n" + peer.ModelName)
		if strings.Contains(haystack, selectorLower) {
			partial = append(partial, peer)
		}
	}
	if len(partial) == 1 {
		return partial[0], nil
	}
	if len(partial) > 1 {
		return servicedcg.DeviceInfo{}, fmt.Errorf("phonehost: target %q matches multiple linked devices", selector)
	}
	return servicedcg.DeviceInfo{}, fmt.Errorf("phonehost: target %q was not found among linked devices", selector)
}

func PeerDisplayName(peer servicedcg.DeviceInfo) string {
	if name := strings.TrimSpace(peer.Name); name != "" {
		return name
	}
	if model := strings.TrimSpace(peer.ModelName); model != "" {
		return model
	}
	return "unnamed"
}

func PeerOSName(peer servicedcg.DeviceInfo) string {
	if osName := strings.TrimSpace(peer.OSName); osName != "" {
		return osName
	}
	return "unknown OS"
}

func report(reporter kernel.Reporter, message string, fields map[string]string) {
	kernel.Report(reporter, kernel.Event{
		ModuleID: "runtime.phonehost",
		Level:    "info",
		Message:  message,
		Fields:   fields,
	})
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:8] + "..." + value[len(value)-4:]
}
