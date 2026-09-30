package dcgauth

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestAccountTrustFromCertificate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, cert, der, err := newSigningCertificate("account-dcg", now)
	if err != nil {
		t.Fatal(err)
	}
	relationship, err := AccountTrustFromCertificate(
		"self-dcg",
		base64.StdEncoding.EncodeToString(der),
		"cid",
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if relationship.SelfClientID != "trust_self-dcg" ||
		relationship.PartnerClientID != "trust_account-dcg" ||
		relationship.PartnerDcgClientID != "account-dcg" ||
		relationship.TrustType != TrustTypeA2D ||
		relationship.Attributes[TrustAttributeProdDcgClientID] != "account-dcg" {
		t.Fatalf("relationship=%#v", relationship)
	}
	if relationship.PartnerKeyExpirationTime.After(cert.NotAfter) {
		t.Fatalf("expiration=%v cert=%v", relationship.PartnerKeyExpirationTime, cert.NotAfter)
	}
}

func TestPeerTrustRelationshipsPreferPKI(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, _, selfDER, err := newSigningCertificate("self-cert", now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, pkiDER, err := newSigningCertificate("peer-pki", now)
	if err != nil {
		t.Fatal(err)
	}
	items := []DeviceInfoItem{
		{ID: "self", IsLinked: true},
		{
			ID:       "phone",
			IsLinked: true,
			OSName:   "Android",
			Certificates: map[string][]string{
				CertificateSelfSigned: {base64.StdEncoding.EncodeToString(selfDER)},
				CertificatePKI:        {base64.StdEncoding.EncodeToString(pkiDER)},
			},
		},
		{ID: "unlinked", IsLinked: false},
	}
	relationships, err := PeerTrustRelationships(items, "self", "cid", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 1 {
		t.Fatalf("relationships=%#v", relationships)
	}
	got := relationships[0]
	if got.PartnerClientID != "trust_phone" ||
		got.PartnerDcgClientID != "phone" ||
		got.TrustType != TrustTypeAsyncD2D ||
		got.PartnerCertificate != base64.StdEncoding.EncodeToString(pkiDER) {
		t.Fatalf("relationship=%#v", got)
	}
}
