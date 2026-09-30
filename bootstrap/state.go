package bootstrap

import (
	"errors"

	"github.com/YMGPwcca/phonelink-linux/auth/dcgauth"
	"github.com/YMGPwcca/phonelink-linux/auth/msa"
	authstate "github.com/YMGPwcca/phonelink-linux/auth/state"
)

// BuildStateSnapshot converts a completed enrollment/trust bootstrap into the
// persistent Linux state needed on later launches.
func BuildStateSnapshot(
	msaToken msa.OAuthToken,
	enroll *EnrollResult,
	trust TrustSyncResult,
	logicalDeviceID string,
) (authstate.Snapshot, error) {
	var out authstate.Snapshot
	if enroll == nil || enroll.Identity == nil || enroll.TrustIdentity == nil {
		return out, errors.New("bootstrap: complete enrollment result is required")
	}
	if logicalDeviceID == "" {
		var err error
		logicalDeviceID, err = authstate.NewLogicalDeviceID()
		if err != nil {
			return out, err
		}
	}
	identityPair, err := authstate.KeyPairFromIdentity(enroll.Identity)
	if err != nil {
		return out, err
	}
	trustPair, err := authstate.KeyPairFromTrustIdentity(enroll.TrustIdentity)
	if err != nil {
		return out, err
	}
	general, err := enroll.Token.GeneralAccessToken()
	if err != nil {
		return out, err
	}

	var accountInfo dcgauth.EnrollmentAccountInfo
	if enroll.EnrollResponse.AccountInfo != nil {
		accountInfo = dcgauth.EnrollmentAccountInfo{
			AccountKey: enroll.EnrollResponse.AccountInfo.AccountKey,
			FirstName:  enroll.EnrollResponse.AccountInfo.FirstName,
			LastName:   enroll.EnrollResponse.AccountInfo.LastName,
			SignInName: enroll.EnrollResponse.AccountInfo.SignInName,
		}
	}

	out = authstate.Snapshot{
		Version:         authstate.CurrentVersion,
		LogicalDeviceID: logicalDeviceID,
		MSARefreshToken: msaToken.RefreshToken,
		Identity:        identityPair,
		TrustIdentity:   trustPair,
		ServicesToken:   authstate.ServicesTokenFromDCG(general),
		Enrollment: authstate.Enrollment{
			AccountCert:          enroll.EnrollResponse.AccountCert,
			AccountInfo:          accountInfo,
			RootCertificateChain: append([]string(nil), enroll.EnrollResponse.RootCertificateChain...),
		},
		TrustRelationships: append([]dcgauth.TrustRelationship(nil), trust.Relationships()...),
	}
	return out, nil
}
