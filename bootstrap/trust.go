package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
)

type TrustSyncResult struct {
	Account dcgauth.TrustRelationship
	Peers   []dcgauth.TrustRelationship
	Devices []servicedcg.DeviceInfo
}

func (r TrustSyncResult) Relationships() []dcgauth.TrustRelationship {
	out := make([]dcgauth.TrustRelationship, 0, 1+len(r.Peers))
	if r.Account.PartnerClientID != "" {
		out = append(out, r.Account)
	}
	out = append(out, r.Peers...)
	return out
}

// SyncTrust builds the local trust graph Windows derives after enrollment:
// the enrollment account certificate becomes the A2D relationship and the
// linked devices returned by DeviceAuthProxy become AsyncD2D relationships.
func SyncTrust(
	ctx context.Context,
	client *servicedcg.Client,
	msaToken string,
	selfDcgClientID string,
	accountCertificateBase64 string,
	msaCID string,
	now time.Time,
) (TrustSyncResult, error) {
	var out TrustSyncResult
	if client == nil {
		return out, errors.New("bootstrap: DCG service client is required")
	}
	if msaToken == "" {
		return out, errors.New("bootstrap: MSA token is required")
	}
	if selfDcgClientID == "" {
		return out, errors.New("bootstrap: self DCG client id is required")
	}
	if accountCertificateBase64 == "" {
		return out, errors.New("bootstrap: account certificate is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	account, err := dcgauth.AccountTrustFromCertificate(
		selfDcgClientID,
		accountCertificateBase64,
		msaCID,
		now,
	)
	if err != nil {
		return out, err
	}
	devices, err := client.ListDevices(ctx, msaToken)
	if err != nil {
		return out, err
	}
	peerItems := make([]dcgauth.DeviceInfoItem, 0, len(devices))
	for _, device := range devices {
		linked := device.IsLinked != nil && *device.IsLinked
		enabled := device.IsEnabled != nil && *device.IsEnabled
		peerItems = append(peerItems, dcgauth.DeviceInfoItem{
			ID:                    device.ID,
			DistinguishedDeviceID: device.DistinguishedDeviceID,
			IsLinked:              linked,
			IsEnabled:             enabled,
			Name:                  device.Name,
			ClientType:            device.ClientType,
			ClientVersion:         device.ClientVersion,
			OSName:                device.OSName,
			OSVersion:             device.OSVersion,
			CustomData:            device.CustomData,
			Capabilities:          append([]string(nil), device.Capabilities...),
			Certificates:          cloneCertificates(device.Certificates),
		})
	}
	peers, err := dcgauth.PeerTrustRelationships(peerItems, selfDcgClientID, msaCID, now)
	if err != nil {
		return out, err
	}
	out.Account = account
	out.Peers = peers
	out.Devices = devices
	return out, nil
}

func cloneCertificates(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for kind, certs := range in {
		out[kind] = append([]string(nil), certs...)
	}
	return out
}
