package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	authstate "github.com/YMGPwcca/linkmyphone/auth/state"
)

func resumeSnapshot(t *testing.T) authstate.Snapshot {
	t.Helper()
	identity, err := dcgauth.NewIdentity(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	trust, err := dcgauth.NewTrustIdentity(identity.DeviceID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pair, err := authstate.KeyPairFromIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	trustPair, err := authstate.KeyPairFromTrustIdentity(trust)
	if err != nil {
		t.Fatal(err)
	}
	return authstate.Snapshot{Version: authstate.CurrentVersion, LogicalDeviceID: "logical-id", MSARefreshToken: "old-refresh",
		Identity: pair, TrustIdentity: trustPair, ServicesToken: authstate.ServicesToken{Token: "old-dcg"}, Enrollment: authstate.Enrollment{AccountCert: "account-cert"}}
}

func TestResumeCheckpointSurvivesDCGFailureAndIsUsedOnRetry(t *testing.T) {
	snapshot := resumeSnapshot(t)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := authstate.Save(path, snapshot); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	msaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		n := refreshes.Add(1)
		want := "old-refresh"
		if n == 2 {
			want = "rotated-refresh"
		}
		if got := r.Form.Get("refresh_token"); got != want {
			t.Errorf("refresh=%q want=%q", got, want)
		}
		_ = json.NewEncoder(w).Encode(msa.OAuthToken{AccessToken: "access", RefreshToken: "rotated-refresh", ExpiresIn: 3600})
	}))
	defer msaServer.Close()
	var dcgRequests atomic.Int32
	dcgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dcgRequests.Add(1)
		checkpoint, err := authstate.Load(path)
		if err != nil || checkpoint.MSARefreshToken != "rotated-refresh" {
			t.Errorf("missing checkpoint before DCG call: err=%v", err)
		}
		http.Error(w, "temporary outage", http.StatusServiceUnavailable)
	}))
	defer dcgServer.Close()
	msaClient := msa.NewDeviceCodeClient()
	msaClient.Authority = msaServer.URL
	dcgClient := dcgauth.NewClient(dcgServer.URL)
	for range 2 {
		_, err := ResumeAuthAndSave(context.Background(), msaClient, dcgClient, snapshot, path)
		var httpErr *dcgauth.HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != 503 {
			t.Fatalf("err=%v", err)
		}
		snapshot, err = authstate.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.MSARefreshToken != "rotated-refresh" || snapshot.ServicesToken.Token != "old-dcg" || snapshot.LogicalDeviceID != "logical-id" || snapshot.Enrollment.AccountCert != "account-cert" {
			t.Fatal("checkpoint changed enrollment or failed to retain rotated credential")
		}
	}
	if dcgRequests.Load() != 2 {
		t.Fatal("unexpected DCG calls")
	}
}

func TestResumePersistenceFailureStopsBeforeDCG(t *testing.T) {
	snapshot := resumeSnapshot(t)
	msaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(msa.OAuthToken{AccessToken: "access", RefreshToken: "rotated"})
	}))
	defer msaServer.Close()
	var requests atomic.Int32
	dcgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer dcgServer.Close()
	msaClient := msa.NewDeviceCodeClient()
	msaClient.Authority = msaServer.URL
	path := filepath.Join(t.TempDir(), "directory-not-file")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := ResumeAuthAndSave(context.Background(), msaClient, dcgauth.NewClient(dcgServer.URL), snapshot, path)
	var persistence *PersistenceError
	if !errors.As(err, &persistence) || requests.Load() != 0 {
		t.Fatalf("err=%v DCG requests=%d", err, requests.Load())
	}
}

func TestResumeSavesNewDCGTokenAndRetainsRefreshIfOmitted(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-identity", true: "wrong-identity"}[mismatch], func(t *testing.T) {
			snapshot := resumeSnapshot(t)
			path := filepath.Join(t.TempDir(), "state.json")
			msaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(msa.OAuthToken{AccessToken: "msa-access", ExpiresIn: 3600})
			}))
			defer msaServer.Close()
			dcgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/Auth/GenerateNonce" {
					_, _ = w.Write([]byte("{\"nonce\":\"nonce\"}"))
					return
				}
				if r.URL.Path != "/Auth/SignIn" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					w.WriteHeader(400)
					return
				}
				id := snapshot.Identity.ID
				if mismatch {
					id = "other-identity"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "dcg-new", "deviceId": id, "epochExpirationTime": time.Now().Add(time.Hour).Unix()})
			}))
			defer dcgServer.Close()
			msaClient := msa.NewDeviceCodeClient()
			msaClient.Authority = msaServer.URL
			_, err := ResumeAuthAndSave(context.Background(), msaClient, dcgauth.NewClient(dcgServer.URL), snapshot, path)
			if mismatch && !errors.Is(err, ErrIdentityMismatch) {
				t.Fatalf("err=%v", err)
			}
			if !mismatch && err != nil {
				t.Fatal(err)
			}
			got, err := authstate.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			wantDCG := "dcg-new"
			if mismatch {
				wantDCG = "old-dcg"
			}
			if got.ServicesToken.Token != wantDCG || got.MSARefreshToken != "old-refresh" || got.Identity.ID != snapshot.Identity.ID {
				t.Fatal("incorrect credentials or identity after resume")
			}
		})
	}
}
