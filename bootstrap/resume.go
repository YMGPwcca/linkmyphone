package bootstrap

import (
	"context"
	"errors"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
)

type ResumeAuthResult struct {
	MSAToken      msa.OAuthToken
	Identity      *dcgauth.Identity
	TrustIdentity *dcgauth.TrustIdentity
	ServicesToken dcgauth.AccessToken
	State         authstate.Snapshot
}

// ResumeAuth refreshes the Microsoft token, restores the persisted crypto
// identities and signs the existing DCG identity back in. It deliberately does
// not call CreateIdentity, so normal restarts keep the same DCG client id.
func ResumeAuth(
	ctx context.Context,
	msaClient *msa.DeviceCodeClient,
	dcgClient *dcgauth.Client,
	snapshot authstate.Snapshot,
) (ResumeAuthResult, error) {
	var out ResumeAuthResult
	if msaClient == nil || dcgClient == nil {
		return out, errors.New("bootstrap: MSA and DCG auth clients are required")
	}
	if snapshot.MSARefreshToken == "" {
		return out, errors.New("bootstrap: persisted MSA refresh token is required")
	}

	// Resume uses the same migrated first-party MSA scope as the Linux
	// device-code path.
	dcgClient.AuthorizationPortal = "FirstPartyAppsRepo"

	identity, err := snapshot.Identity.Identity()
	if err != nil {
		return out, err
	}
	trustIdentity, err := snapshot.TrustIdentity.Trust()
	if err != nil {
		return out, err
	}
	msaToken, err := msaClient.Refresh(
		ctx,
		snapshot.MSARefreshToken,
		msa.ScopeWithOfflineAccess(dcgauth.MigratedProdMSAScope),
	)
	if err != nil {
		return out, err
	}
	rawDCGToken, err := dcgClient.SignInIdentity(ctx, msaToken.AccessToken, identity)
	if err != nil {
		return out, err
	}
	general, err := rawDCGToken.GeneralAccessToken()
	if err != nil {
		return out, err
	}

	updated := snapshot
	if msaToken.RefreshToken != "" {
		updated.MSARefreshToken = msaToken.RefreshToken
	}
	updated.ServicesToken = authstate.ServicesTokenFromDCG(general)

	out = ResumeAuthResult{
		MSAToken:      msaToken,
		Identity:      identity,
		TrustIdentity: trustIdentity,
		ServicesToken: general,
		State:         updated,
	}
	return out, nil
}
