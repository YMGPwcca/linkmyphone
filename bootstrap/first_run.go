package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

type FirstRunConfig struct {
	Auth     *dcgauth.Client
	Services *servicedcg.Client
	MSAToken msa.OAuthToken

	AppVersion    string
	ClientProfile dcgheaders.Profile
	DisplayName   string
	RingName      string
	OSVersion     string
	MSACID        string

	LogicalDeviceID string
	StatePath       string

	HubURL             string
	OnConnectedTimeout time.Duration
	HTTPClient         *http.Client
	RelayConfig        relay.Config
	DialHub            HubDialer
	Now                func() time.Time
}

type FirstRunResult struct {
	Enrollment *EnrollResult
	Trust      TrustSyncResult
	State      authstate.Snapshot
	StatePath  string
	Cloud      *CloudRelay
}

// BootstrapFirstRun wires stages 1 through 4 of the Linux bootstrap:
// enroll the DCG identity, build/persist trust state, then establish the
// account-level SignalR relay connection.
func BootstrapFirstRun(ctx context.Context, cfg FirstRunConfig) (*FirstRunResult, error) {
	result := &FirstRunResult{}
	if err := cfg.ClientProfile.Validate(); err != nil {
		return result, err
	}
	if cfg.ClientProfile.Canonical() == dcgheaders.ProfilePhoneLink && cfg.StatePath == "" {
		return result, errors.New("bootstrap: Phone Link enrollment requires an explicit isolated state path")
	}
	if cfg.Auth == nil || cfg.Services == nil {
		return result, errors.New("bootstrap: auth and DCG service clients are required")
	}
	if cfg.MSAToken.AccessToken == "" {
		return result, errors.New("bootstrap: MSA access token is required")
	}
	if cfg.DisplayName == "" {
		return result, errors.New("bootstrap: display name is required")
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	logicalDeviceID := cfg.LogicalDeviceID
	if logicalDeviceID == "" {
		var err error
		logicalDeviceID, err = authstate.NewLogicalDeviceID()
		if err != nil {
			return result, err
		}
	}
	clientInfo, err := dcgheaders.NewClientInfo(
		cfg.ClientProfile,
		logicalDeviceID,
		cfg.AppVersion,
		cfg.RingName,
		cfg.OSVersion,
	)
	if err != nil {
		return result, err
	}

	// The Linux device-code path uses the migrated first-party DCG scope.
	cfg.Auth.AuthorizationPortal = dcgheaders.PortalFirstParty
	cfg.Services.AuthorizationPortal = dcgheaders.PortalFirstParty
	cfg.Auth.ClientInfo = clientInfo
	cfg.Services.ClientInfo = clientInfo
	cfg.Auth.Now = nowFn

	enroller := &Enroller{
		Auth: cfg.Auth,
		DCG:  cfg.Services,
		Now:  nowFn,
	}
	enrollment, err := enroller.EnrollWithMSAToken(
		ctx,
		cfg.MSAToken.AccessToken,
		servicedcg.MetadataForPC(cfg.ClientProfile, cfg.AppVersion, cfg.DisplayName, cfg.OSVersion),
	)
	if err != nil {
		return result, err
	}
	result.Enrollment = enrollment

	trust, err := SyncTrust(
		ctx,
		cfg.Services,
		cfg.MSAToken.AccessToken,
		enrollment.Identity.DeviceID,
		enrollment.EnrollResponse.AccountCert,
		cfg.MSACID,
		nowFn(),
	)
	if err != nil {
		return result, err
	}
	result.Trust = trust

	snapshot, err := BuildStateSnapshot(
		cfg.MSAToken,
		enrollment,
		trust,
		logicalDeviceID,
		cfg.ClientProfile,
	)
	if err != nil {
		return result, err
	}
	result.State = snapshot

	statePath := cfg.StatePath
	if statePath == "" {
		statePath, err = authstate.DefaultPath()
		if err != nil {
			return result, err
		}
	}
	result.StatePath = statePath
	if err := authstate.Save(statePath, snapshot); err != nil {
		return result, err
	}

	general, err := enrollment.Token.GeneralAccessToken()
	if err != nil {
		return result, err
	}
	cloud, err := OpenCloudRelay(ctx, CloudConfig{
		Services:           cfg.Services,
		MSAAccessToken:     cfg.MSAToken.AccessToken,
		DCGAccessToken:     general.Token,
		ClientInfo:         clientInfo,
		HubURL:             cfg.HubURL,
		OnConnectedTimeout: cfg.OnConnectedTimeout,
		HTTPClient:         cfg.HTTPClient,
		RelayConfig:        cfg.RelayConfig,
		DialHub:            cfg.DialHub,
	})
	if err != nil {
		// Enrollment and state persistence have already succeeded. Return the
		// partial result so the caller can resume instead of enrolling again.
		return result, err
	}
	result.Cloud = cloud
	return result, nil
}
