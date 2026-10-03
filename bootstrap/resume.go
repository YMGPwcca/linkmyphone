package bootstrap

import (
	"context"
	"errors"
	"fmt"

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

var ErrIdentityMismatch = dcgauth.ErrIdentityMismatch

// ResumeAuth refreshes the Microsoft token, restores the persisted crypto
// identities and signs the existing DCG identity back in. It deliberately does
// not call CreateIdentity, so normal restarts keep the same DCG client id.
func ResumeAuth(
	ctx context.Context,
	msaClient *msa.DeviceCodeClient,
	dcgClient *dcgauth.Client,
	snapshot authstate.Snapshot,
) (ResumeAuthResult, error) {
	return resumeAuth(ctx, msaClient, dcgClient, snapshot, nil)
}

// ResumeAuthAndSave checkpoints a rotated MSA refresh token before making any
// further cloud call. A failed DCG SignIn must not lose the next credential.
func ResumeAuthAndSave(ctx context.Context, msaClient *msa.DeviceCodeClient, dcgClient *dcgauth.Client, snapshot authstate.Snapshot, path string) (ResumeAuthResult, error) {
	return resumeAuth(ctx, msaClient, dcgClient, snapshot, func(updated authstate.Snapshot) error {
		if err := authstate.Save(path, updated); err != nil {
			return &PersistenceError{Err: err}
		}
		return nil
	})
}

type PersistenceError struct{ Err error }

func (e *PersistenceError) Error() string {
	return fmt.Sprintf("bootstrap: persist refreshed authentication: %v", e.Err)
}
func (e *PersistenceError) Unwrap() error { return e.Err }

func resumeAuth(ctx context.Context, msaClient *msa.DeviceCodeClient, dcgClient *dcgauth.Client, snapshot authstate.Snapshot, save func(authstate.Snapshot) error) (ResumeAuthResult, error) {
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
	updated := snapshot
	if msaToken.RefreshToken != "" {
		updated.MSARefreshToken = msaToken.RefreshToken
	}
	if save != nil {
		if err := save(updated); err != nil {
			return out, err
		}
	}
	rawDCGToken, err := dcgClient.SignInIdentity(ctx, msaToken.AccessToken, identity)
	if err != nil {
		return out, err
	}
	general, err := rawDCGToken.GeneralAccessToken()
	if err != nil {
		return out, err
	}

	if general.DeviceID != identity.DeviceID {
		return out, ErrIdentityMismatch
	}
	updated.ServicesToken = authstate.ServicesTokenFromDCG(general)
	if save != nil {
		if err := save(updated); err != nil {
			return out, err
		}
	}

	out = ResumeAuthResult{
		MSAToken:      msaToken,
		Identity:      identity,
		TrustIdentity: trustIdentity,
		ServicesToken: general,
		State:         updated,
	}
	return out, nil
}
