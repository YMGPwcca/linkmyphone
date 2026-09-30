package dcgauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGenerateNonceWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost ||
			r.URL.Path != "/Auth/GenerateNonce" ||
			r.URL.Query().Get("api-version") != "1.1.0" {
			t.Fatalf("request=%s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get(HeaderUserIdentityType); got != "MSA" {
			t.Fatalf("UserIdentityType=%q", got)
		}
		if got := r.Header.Get(HeaderUserIdentityToken); got != "msa" {
			t.Fatalf("UserIdentityToken=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msa" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get(HeaderAuthorizationType); got != "MSA" {
			t.Fatalf("Authorization-Type=%q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["deviceId"] != "device" {
			t.Fatalf("body=%#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"epochTimeStamp\":123,\"nonce\":\"abc\"}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	out, err := client.GenerateNonce(context.Background(), "msa", "device")
	if err != nil {
		t.Fatal(err)
	}
	if out.Nonce != "abc" || out.EpochTimeStamp == nil || *out.EpochTimeStamp != 123 {
		t.Fatalf("out=%#v", out)
	}
}

func TestCreateIdentityAndSignInWireShapes(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		wantPath := "/Auth/CreateIdentity"
		if calls == 2 {
			wantPath = "/Auth/SignIn"
		}
		if r.URL.Path != wantPath {
			t.Fatalf("path=%q want=%q", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("api-version") != AuthAPIVersion {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["certificateJWT"] != "jwt" {
			t.Fatalf("body=%#v", body)
		}
		_, _ = w.Write([]byte("{\"accessToken\":\"dcg\",\"deviceId\":\"device\",\"epochExpirationTime\":456,\"keyValidRemainingDays\":300,\"tenantId\":\"tenant\"}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	created, err := client.CreateIdentity(context.Background(), "msa", "jwt")
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := client.SignIn(context.Background(), "msa", "jwt")
	if err != nil {
		t.Fatal(err)
	}
	if created.AccessToken != "dcg" || signedIn.DeviceID != "device" || calls != 2 {
		t.Fatalf("created=%#v signedIn=%#v calls=%d", created, signedIn, calls)
	}
}

func TestSignInIdentityRejectsMismatchedDevice(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0).UTC()
	identity, err := NewIdentity(fixed)
	if err != nil {
		t.Fatal(err)
	}
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			_, _ = w.Write([]byte("{\"nonce\":\"abc\"}"))
			return
		}
		_, _ = w.Write([]byte("{\"accessToken\":\"dcg\",\"deviceId\":\"other\"}"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.Now = func() time.Time { return fixed }
	_, err = client.SignInIdentity(context.Background(), "msa", identity)
	if err == nil || !strings.Contains(err.Error(), "returned device id") {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad nonce", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GenerateNonce(context.Background(), "msa", "device")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*HTTPError); !ok {
		t.Fatalf("err=%T %v", err, err)
	}
}
