package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/services/dcg"
)

type EnrollResult struct {
	Identity       *dcgauth.Identity
	TrustIdentity  *dcgauth.TrustIdentity
	Token          dcgauth.TokenResponse
	EnrollResponse dcg.EnrollResponse
}

type Enroller struct {
	Auth *dcgauth.Client
	DCG  *dcg.Client
	Now  func() time.Time
}

// EnrollWithMSAToken performs the source-confirmed silent production enrollment
// sequence after an MSA DCG token has already been acquired:
//
// GenerateNonce -> CreateIdentity -> trust certificate -> EnrollDevice.
//
// The caller owns persistence of both private keys. Keeping that explicit avoids
// silently writing sensitive key material before the surrounding application
// has selected a secure storage backend.
func (e *Enroller) EnrollWithMSAToken(
	ctx context.Context,
	msaToken string,
	metadata dcg.DeviceMetadata,
) (*EnrollResult, error) {
	if e == nil || e.Auth == nil || e.DCG == nil {
		return nil, errors.New("bootstrap: auth and DCG clients are required")
	}
	identity, token, err := e.Auth.BootstrapIdentity(ctx, msaToken)
	if err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, errors.New("bootstrap: CreateIdentity returned an empty DCG token")
	}
	trustIdentity, err := dcgauth.NewTrustIdentity(identity.DeviceID, e.now())
	if err != nil {
		return nil, err
	}
	enrollResponse, err := e.DCG.EnrollDevice(ctx, msaToken, token.AccessToken, "", dcg.EnrollRequest{
		Metadata: metadata,
		Certificates: map[string][]string{
			"SelfSigned": {trustIdentity.CertificateBase64()},
		},
	})
	if err != nil {
		return nil, err
	}
	if enrollResponse.AccountCert == "" {
		return nil, errors.New("bootstrap: EnrollDevice returned an empty account certificate")
	}
	return &EnrollResult{
		Identity:       identity,
		TrustIdentity:  trustIdentity,
		Token:          token,
		EnrollResponse: enrollResponse,
	}, nil
}

func (e *Enroller) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
