package dcgauth

import (
	"crypto/ecdsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestNewIdentityUsesP384AndDeviceIDCN(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	id, err := NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	if id.DeviceID == "" || id.Certificate.Subject.CommonName != id.DeviceID {
		t.Fatalf("device=%q cn=%q", id.DeviceID, id.Certificate.Subject.CommonName)
	}
	if id.PrivateKey.Curve.Params().Name != "P-384" {
		t.Fatalf("curve=%q", id.PrivateKey.Curve.Params().Name)
	}
	if id.Certificate.KeyUsage&1 == 0 {
		t.Fatalf("certificate does not allow digital signatures")
	}
}

func TestNonceJWTClaimsAndSignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	id, err := NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := id.SignNonceJWT("nonce-123", now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("parts=%d", len(parts))
	}
	var header map[string]any
	headerBytes, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "ES384" || header["typ"] != "JWT" {
		t.Fatalf("header=%#v", header)
	}
	var payload map[string]any
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["iss"] != id.DeviceID || payload["Nonce"] != "nonce-123" {
		t.Fatalf("payload=%#v", payload)
	}
	cert, ok := payload["Certificate"].(string)
	if !ok || cert != base64.StdEncoding.EncodeToString(id.CertificateDER) {
		t.Fatalf("certificate claim mismatch")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 96 {
		t.Fatalf("signature len=%d err=%v", len(sig), err)
	}
	sum := sha512.Sum384([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sig[:48])
	s := new(big.Int).SetBytes(sig[48:])
	if !ecdsa.Verify(&id.PrivateKey.PublicKey, sum[:], r, s) {
		t.Fatal("signature verification failed")
	}
}

func TestExtrasJWTOmitsEmptyClaims(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	id, err := NewIdentity(now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := id.SignExtrasJWT(Extras{SourceID: "desktop", Scope: "wake"}, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["SourceId"] != "desktop" || payload["Scope"] != "wake" {
		t.Fatalf("payload=%#v", payload)
	}
	if _, ok := payload["Data"]; ok {
		t.Fatalf("unexpected Data claim: %#v", payload)
	}
}
