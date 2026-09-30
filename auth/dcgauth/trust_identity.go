package dcgauth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"time"
)

const TrustClientPrefix = "trust_"

// TrustIdentity is the separate crypto identity Windows registers in device
// metadata. Its certificate CN is "trust_" + the local DCG client id; it is
// distinct from the certificate used to own the DCG auth identity itself.
type TrustIdentity struct {
	ClientID       string
	PrivateKey     *ecdsa.PrivateKey
	Certificate    *x509.Certificate
	CertificateDER []byte
}

func NewTrustIdentity(dcgClientID string, now time.Time) (*TrustIdentity, error) {
	if dcgClientID == "" {
		return nil, errors.New("dcgauth: DCG client id is required")
	}
	clientID := TrustClientPrefix + dcgClientID
	key, cert, der, err := newSigningCertificate(clientID, now)
	if err != nil {
		return nil, err
	}
	return &TrustIdentity{
		ClientID:       clientID,
		PrivateKey:     key,
		Certificate:    cert,
		CertificateDER: der,
	}, nil
}

func (i *TrustIdentity) CertificateBase64() string {
	if i == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(i.CertificateDER)
}
