package msa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRefreshNonJSONThrottlePreservesRetryAfterAndRedactsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("private account data"))
	}))
	defer server.Close()
	client := NewDeviceCodeClient()
	client.Authority = server.URL
	_, err := client.Refresh(context.Background(), "refresh", "scope")
	var oauth *OAuthError
	if !errors.As(err, &oauth) || oauth.StatusCode != 429 || oauth.RetryAfter != "90" || strings.Contains(err.Error(), "private") {
		t.Fatalf("err=%v", err)
	}
}

func TestDeviceCodeStartWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/devicecode" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("client_id") != ClientID ||
			r.Form.Get("scope") != "https://dcg.microsoft.com/DCG.ReadWrite offline_access" {
			t.Fatalf("form=%v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device",
			"user_code":        "ABCD",
			"verification_uri": "https://microsoft.com/devicelogin",
			"expires_in":       900,
			"interval":         5,
			"message":          "sign in",
		})
	}))
	defer server.Close()

	c := NewDeviceCodeClient()
	c.Authority = server.URL
	code, err := c.Start(context.Background(), ScopeWithOfflineAccess("https://dcg.microsoft.com/DCG.ReadWrite"))
	if err != nil {
		t.Fatal(err)
	}
	if code.DeviceCode != "device" || code.UserCode != "ABCD" {
		t.Fatalf("code=%#v", code)
	}
}

func TestPollHandlesPendingThenSuccess(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != DeviceCodeGrant ||
			r.Form.Get("device_code") != "device" {
			t.Fatalf("form=%v", r.Form)
		}
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "authorization_pending",
				"error_description": "pending",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "msa-token",
			"refresh_token": "refresh",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	c := NewDeviceCodeClient()
	c.Authority = server.URL
	c.Now = func() time.Time { return fixed }
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	token, err := c.Poll(context.Background(), DeviceCode{
		DeviceCode: "device",
		ExpiresIn:  900,
		Interval:   5,
		issuedAt:   fixed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || token.AccessToken != "msa-token" || token.RefreshToken != "refresh" {
		t.Fatalf("calls=%d token=%#v", calls, token)
	}
}

func TestRefreshWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{
			"client_id":     {ClientID},
			"grant_type":    {"refresh_token"},
			"refresh_token": {"refresh"},
			"scope":         {"scope"},
		}
		for key := range want {
			if r.Form.Get(key) != want.Get(key) {
				t.Fatalf("%s=%q want=%q", key, r.Form.Get(key), want.Get(key))
			}
		}
		_, _ = w.Write([]byte("{\"access_token\":\"new-token\",\"expires_in\":3600}"))
	}))
	defer server.Close()

	c := NewDeviceCodeClient()
	c.Authority = server.URL
	token, err := c.Refresh(context.Background(), "refresh", "scope")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "new-token" {
		t.Fatalf("token=%#v", token)
	}
}
