package dcgauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewTrustIdentityUsesTrustPrefixedCN(t *testing.T) {
	id, err := NewTrustIdentity("dcg-id", time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if id.ClientID != "trust_dcg-id" {
		t.Fatalf("client id=%q", id.ClientID)
	}
	if id.Certificate.Subject.CommonName != "trust_dcg-id" {
		t.Fatalf("cn=%q", id.Certificate.Subject.CommonName)
	}
	decoded, err := base64.StdEncoding.DecodeString(id.CertificateBase64())
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(id.CertificateDER) {
		t.Fatal("base64 certificate mismatch")
	}
}

func TestTrustIdentitySignsWakeClaims(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	id, err := NewTrustIdentity("dcg-id", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := id.SignExtrasJWT(Extras{
		Data:     `{"DCG-Environment":"Prod"}`,
		SourceID: "dcg-id",
		Scope:    "wake",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token=%q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "trust_dcg-id" ||
		claims["SourceId"] != "dcg-id" ||
		claims["Scope"] != "wake" {
		t.Fatalf("claims=%#v", claims)
	}
}
