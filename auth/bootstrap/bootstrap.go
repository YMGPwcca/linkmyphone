package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
)

type IdentityResult struct {
	Identity      *dcgauth.Identity
	ServicesToken dcgauth.AccessToken
	MSAToken      msa.OAuthToken
}

// DCGIdentityFromMSA bridges the Linux Microsoft-account login result into the
// source-confirmed DCG identity bootstrap:
// MSA access token -> GenerateNonce -> ES384 certificate JWT -> CreateIdentity.
func DCGIdentityFromMSA(ctx context.Context, client *dcgauth.Client, msaToken msa.OAuthToken) (IdentityResult, error) {
	if client == nil {
		return IdentityResult{}, errors.New("bootstrap: DCG auth client is required")
	}
	if msaToken.AccessToken == "" {
		return IdentityResult{}, errors.New("bootstrap: MSA access token is required")
	}

	identity, rawToken, err := client.BootstrapIdentity(ctx, msaToken.AccessToken)
	if err != nil {
		return IdentityResult{}, err
	}
	servicesToken, err := rawToken.GeneralAccessToken()
	if err != nil {
		return IdentityResult{}, fmt.Errorf("bootstrap: invalid CreateIdentity token response: %w", err)
	}
	if identity == nil || servicesToken.DeviceID != identity.DeviceID {
		return IdentityResult{}, fmt.Errorf(
			"bootstrap: DCG identity/token device mismatch: identity=%v token=%q",
			identity != nil,
			servicesToken.DeviceID,
		)
	}

	return IdentityResult{
		Identity:      identity,
		ServicesToken: servicesToken,
		MSAToken:      msaToken,
	}, nil
}
