package dcgauth

import (
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TrustAttributeLegacyDcgClientID = "dcg_client_id"
	TrustAttributeBetaDcgClientID   = "dcg_beta_client_id"
	TrustAttributeProdDcgClientID   = "dcg_prod_client_id"
	TrustAttributeDogfoodDcgClientID = "dcg_df_client_id"
)

type TrustType int

const (
	TrustTypeUnknown TrustType = iota
	TrustTypeClassicPairingD2D
	TrustTypeAccountPairingD2D
	TrustTypeAsyncD2D
	TrustTypeA2D
)

type TrustRelationship struct {
	SelfClientID             string            `json:"selfClientId"`
	PartnerClientID          string            `json:"partnerClientId"`
	PartnerDcgClientID       string            `json:"partnerDcgClientId"`
	PartnerCertificate       string            `json:"partnerCertificate"`
	PartnerKeyExpirationTime time.Time         `json:"partnerKeyExpirationTime"`
	RelationshipLastAccessTime time.Time       `json:"relationshipLastAccessTime"`
	MSACID                   string            `json:"msaCid,omitempty"`
	TrustType                TrustType         `json:"trustType"`
	Attributes               map[string]string `json:"attributes"`
}

// AccountTrustFromCertificate mirrors TrustHelper.UpdateTrustWithAccountAsync.
// The account certificate subject CN is the account DCG client id.
func AccountTrustFromCertificate(selfDcgClientID, accountCertificateBase64, msaCID string, now time.Time) (TrustRelationship, error) {
	if selfDcgClientID == "" {
		return TrustRelationship{}, errors.New("dcgauth: self DCG client id is required")
	}
	cert, normalized, err := parseTrustCertificate(accountCertificateBase64)
	if err != nil {
		return TrustRelationship{}, fmt.Errorf("dcgauth: account certificate: %w", err)
	}
	accountDcgClientID := cert.Subject.CommonName
	if accountDcgClientID == "" {
		return TrustRelationship{}, errors.New("dcgauth: account certificate has no common name")
	}
	return newTrustRelationship(
		CryptoTrustClientID(selfDcgClientID),
		CryptoTrustClientID(accountDcgClientID),
		accountDcgClientID,
		normalized,
		cert,
		msaCID,
		TrustTypeA2D,
		now,
	)
}

// PeerTrustRelationships mirrors the fresh-store path in
// TrustHelper.SyncTrustStorageWithRemotePeerMetadataAsync. Linked peers other
// than self are registered as AsyncD2D using PKI first, then SelfSigned cert.
func PeerTrustRelationships(items []DeviceInfoItem, selfDcgClientID, msaCID string, now time.Time) ([]TrustRelationship, error) {
	peers := LinkedPeerDevices(items, selfDcgClientID)
	out := make([]TrustRelationship, 0, len(peers))
	seen := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		if _, ok := seen[strings.ToLower(peer.ID)]; ok {
			continue
		}
		certText, ok := PeerTrustCertificate(peer)
		if !ok {
			continue
		}
		cert, normalized, err := parseTrustCertificate(certText)
		if err != nil {
			return nil, fmt.Errorf("dcgauth: peer %q certificate: %w", peer.ID, err)
		}
		relationship, err := newTrustRelationship(
			CryptoTrustClientID(selfDcgClientID),
			CryptoTrustClientID(peer.ID),
			peer.ID,
			normalized,
			cert,
			msaCID,
			TrustTypeAsyncD2D,
			now,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, relationship)
		seen[strings.ToLower(peer.ID)] = struct{}{}
	}
	return out, nil
}

func newTrustRelationship(selfClientID, partnerClientID, partnerDcgClientID, certBase64 string, cert *x509.Certificate, msaCID string, trustType TrustType, now time.Time) (TrustRelationship, error) {
	if selfClientID == "" || partnerClientID == "" || partnerDcgClientID == "" {
		return TrustRelationship{}, errors.New("dcgauth: incomplete trust relationship ids")
	}
	if cert == nil {
		return TrustRelationship{}, errors.New("dcgauth: partner certificate is required")
	}
	if !cert.NotAfter.After(now) {
		return TrustRelationship{}, errors.New("dcgauth: partner certificate is expired")
	}
	expiration := cert.NotAfter.UTC()
	maxExpiration := now.Add(DefaultCertificateLifetime)
	if expiration.After(maxExpiration) {
		expiration = maxExpiration
	}
	return TrustRelationship{
		SelfClientID:               selfClientID,
		PartnerClientID:            partnerClientID,
		PartnerDcgClientID:         partnerDcgClientID,
		PartnerCertificate:         certBase64,
		PartnerKeyExpirationTime:   expiration,
		RelationshipLastAccessTime: now.UTC(),
		MSACID:                     msaCID,
		TrustType:                  trustType,
		Attributes: map[string]string{
			TrustAttributeProdDcgClientID: partnerDcgClientID,
		},
	}, nil
}

func parseTrustCertificate(value string) (*x509.Certificate, string, error) {
	if value == "" {
		return nil, "", errors.New("empty certificate")
	}
	der, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", err
	}
	return cert, base64.StdEncoding.EncodeToString(cert.Raw), nil
}
