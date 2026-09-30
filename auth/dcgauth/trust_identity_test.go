package dcgauth

import (
	"encoding/base64"
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
