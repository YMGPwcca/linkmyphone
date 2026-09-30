package dcg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWakeWireShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/Dispatcher/Wake" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != "1.5.0" {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dcg-token" {
			t.Fatalf("Authorization=%q", got)
		}
		if got := r.Header.Get("Authorization-Type"); got != "" {
			t.Fatalf("unexpected Authorization-Type=%q", got)
		}
		var body WakeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DeviceID != "phone" ||
			body.CollapseKey != "YPPWake" ||
			body.Priority != "high" ||
			body.TTL != 30 ||
			body.Data["DCG-CryptoWakeJwt"] != "jwt" {
			t.Fatalf("body=%#v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Wake(context.Background(), "dcg-token", WakeRequest{
		DeviceID: "phone",
		TTL:      30,
		Data: map[string]string{
			"DCG-CryptoWakeJwt": "jwt",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
