package dcgauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type Identity struct {
	DeviceID       string
	PrivateKey     *ecdsa.PrivateKey
	Certificate    *x509.Certificate
	CertificateDER []byte
}

type Extras struct {
	Data     string
	SourceID string
	Scope    string
}

// NewIdentity mirrors the Windows DCG identity primitive: a random device id,
// an ECDSA P-384 signing key, and a self-signed SHA-384 certificate whose
// subject common name is the device id.
func NewIdentity(now time.Time) (*Identity, error) {
	deviceID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: deviceID},
		NotBefore:             now.Add(-DefaultCertificateClockDrift),
		NotAfter:              now.Add(DefaultCertificateLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
		SignatureAlgorithm:    x509.ECDSAWithSHA384,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Identity{
		DeviceID:       deviceID,
		PrivateKey:     key,
		Certificate:    cert,
		CertificateDER: der,
	}, nil
}

// SignNonceJWT mirrors CryptoManager.CreateJwtTokenForNonceInner. The token is
// issued by the certificate CN, contains Nonce and the base64 DER certificate,
// and is signed with ES384.
func (i *Identity) SignNonceJWT(nonce string, now time.Time) (string, error) {
	if nonce == "" {
		return "", errors.New("dcgauth: empty nonce")
	}
	return i.signJWT(now, map[string]any{
		"Nonce":       nonce,
		"Certificate": base64.StdEncoding.EncodeToString(i.CertificateDER),
	})
}

// SignExtrasJWT mirrors the general certificate-signed JWT used by crypto
// payloads. Empty optional claims are omitted.
func (i *Identity) SignExtrasJWT(extras Extras, now time.Time) (string, error) {
	claims := make(map[string]any)
	if extras.Data != "" {
		claims["Data"] = extras.Data
	}
	if extras.SourceID != "" {
		claims["SourceId"] = extras.SourceID
	}
	if extras.Scope != "" {
		claims["Scope"] = extras.Scope
	}
	return i.signJWT(now, claims)
}

func (i *Identity) signJWT(now time.Time, claims map[string]any) (string, error) {
	if i == nil || i.PrivateKey == nil || i.Certificate == nil || i.DeviceID == "" {
		return "", errors.New("dcgauth: incomplete identity")
	}
	payload := make(map[string]any, len(claims)+3)
	for k, v := range claims {
		payload[k] = v
	}
	payload["iss"] = i.DeviceID
	payload["nbf"] = now.Add(-DefaultJWTClockDrift).Unix()
	payload["exp"] = now.Add(DefaultJWTLifetime).Unix()

	headerJSON, err := json.Marshal(map[string]any{
		"alg": "ES384",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(payloadJSON)
	sum := sha512.Sum384([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, i.PrivateKey, sum[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 96)
	r.FillBytes(signature[:48])
	s.FillBytes(signature[48:])
	return signingInput + "." + enc.EncodeToString(signature), nil
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
