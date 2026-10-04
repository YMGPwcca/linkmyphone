package state

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
)

const CurrentVersion = 1

type Snapshot struct {
	Version            int                         `json:"version"`
	ClientProfile      dcgheaders.Profile          `json:"clientProfile"`
	LogicalDeviceID    string                      `json:"logicalDeviceId"`
	MSARefreshToken    string                      `json:"msaRefreshToken,omitempty"`
	Identity           KeyPair                     `json:"identity"`
	TrustIdentity      KeyPair                     `json:"trustIdentity"`
	ServicesToken      ServicesToken               `json:"servicesToken"`
	Enrollment         Enrollment                  `json:"enrollment"`
	TrustRelationships []dcgauth.TrustRelationship `json:"trustRelationships,omitempty"`
}

type KeyPair struct {
	ID              string `json:"id"`
	PrivateKeyPKCS8 string `json:"privateKeyPkcs8"`
	CertificateDER  string `json:"certificateDer"`
}

type ServicesToken struct {
	Token                 string    `json:"token"`
	Scope                 string    `json:"scope"`
	DeviceID              string    `json:"deviceId"`
	ExpiresAt             time.Time `json:"expiresAt"`
	KeyValidRemainingDays int64     `json:"keyValidRemainingDays"`
	TenantID              string    `json:"tenantId,omitempty"`
}

type Enrollment struct {
	AccountCert          string                        `json:"accountCert,omitempty"`
	AccountInfo          dcgauth.EnrollmentAccountInfo `json:"accountInfo,omitempty"`
	RootCertificateChain []string                      `json:"rootCertificateChain,omitempty"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "linkmyphone", "state.json"), nil
}

func NewLogicalDeviceID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw[:]), nil
}

func KeyPairFromIdentity(identity *dcgauth.Identity) (KeyPair, error) {
	if identity == nil {
		return KeyPair{}, errors.New("state: identity is required")
	}
	return encodeKeyPair(identity.DeviceID, identity.PrivateKey, identity.CertificateDER)
}

func KeyPairFromTrustIdentity(identity *dcgauth.TrustIdentity) (KeyPair, error) {
	if identity == nil {
		return KeyPair{}, errors.New("state: trust identity is required")
	}
	return encodeKeyPair(identity.ClientID, identity.PrivateKey, identity.CertificateDER)
}

func (p KeyPair) Identity() (*dcgauth.Identity, error) {
	key, cert, der, err := p.decode()
	if err != nil {
		return nil, err
	}
	return &dcgauth.Identity{
		DeviceID:       p.ID,
		PrivateKey:     key,
		Certificate:    cert,
		CertificateDER: der,
	}, nil
}

func (p KeyPair) Trust() (*dcgauth.TrustIdentity, error) {
	key, cert, der, err := p.decode()
	if err != nil {
		return nil, err
	}
	return &dcgauth.TrustIdentity{
		ClientID:       p.ID,
		PrivateKey:     key,
		Certificate:    cert,
		CertificateDER: der,
	}, nil
}

func ServicesTokenFromDCG(token dcgauth.AccessToken) ServicesToken {
	return ServicesToken{
		Token:                 token.Token,
		Scope:                 token.Scope,
		DeviceID:              token.DeviceID,
		ExpiresAt:             token.ExpiresAt,
		KeyValidRemainingDays: token.KeyValidRemainingDays,
		TenantID:              token.TenantID,
	}
}

func (t ServicesToken) DCG() dcgauth.AccessToken {
	return dcgauth.AccessToken{
		Token:                 t.Token,
		Scope:                 t.Scope,
		DeviceID:              t.DeviceID,
		ExpiresAt:             t.ExpiresAt,
		KeyValidRemainingDays: t.KeyValidRemainingDays,
		TenantID:              t.TenantID,
	}
}

func EnrollmentFromResponse(response dcgauth.EnrollmentResponse) Enrollment {
	return Enrollment{
		AccountCert:          response.AccountCert,
		AccountInfo:          response.AccountInfo,
		RootCertificateChain: append([]string(nil), response.RootCertificateChain...),
	}
}

func Save(path string, snapshot Snapshot) error {
	if path == "" {
		return errors.New("state: path is required")
	}
	if snapshot.Version == 0 {
		snapshot.Version = CurrentVersion
	}
	if snapshot.Version != CurrentVersion {
		return fmt.Errorf("state: unsupported version %d", snapshot.Version)
	}
	snapshot.ClientProfile = snapshot.ClientProfile.Canonical()
	if err := snapshot.ClientProfile.Validate(); err != nil {
		return fmt.Errorf("state: client profile: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	ok = true
	return nil
}

func Load(path string) (Snapshot, error) {
	var out Snapshot
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("state: decode snapshot: %w", err)
	}
	if out.Version != CurrentVersion {
		return Snapshot{}, fmt.Errorf("state: unsupported version %d", out.Version)
	}
	out.ClientProfile = out.ClientProfile.Canonical()
	if err := out.ClientProfile.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("state: client profile: %w", err)
	}
	return out, nil
}

func encodeKeyPair(id string, key *ecdsa.PrivateKey, certDER []byte) (KeyPair, error) {
	if id == "" || key == nil || len(certDER) == 0 {
		return KeyPair{}, errors.New("state: incomplete key pair")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		ID:              id,
		PrivateKeyPKCS8: base64.StdEncoding.EncodeToString(keyDER),
		CertificateDER:  base64.StdEncoding.EncodeToString(certDER),
	}, nil
}

func (p KeyPair) decode() (*ecdsa.PrivateKey, *x509.Certificate, []byte, error) {
	if p.ID == "" || p.PrivateKeyPKCS8 == "" || p.CertificateDER == "" {
		return nil, nil, nil, errors.New("state: incomplete persisted key pair")
	}
	keyDER, err := base64.StdEncoding.DecodeString(p.PrivateKeyPKCS8)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("state: decode private key: %w", err)
	}
	rawKey, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("state: parse private key: %w", err)
	}
	key, ok := rawKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, nil, errors.New("state: persisted key is not ECDSA")
	}
	certDER, err := base64.StdEncoding.DecodeString(p.CertificateDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("state: decode certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("state: parse certificate: %w", err)
	}
	if cert.Subject.CommonName != p.ID {
		return nil, nil, nil, fmt.Errorf("state: certificate CN %q does not match id %q", cert.Subject.CommonName, p.ID)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.X.Cmp(key.PublicKey.X) != 0 || pub.Y.Cmp(key.PublicKey.Y) != 0 {
		return nil, nil, nil, errors.New("state: certificate and private key do not match")
	}
	return key, cert, certDER, nil
}
