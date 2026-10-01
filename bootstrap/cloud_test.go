package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/YMGPwcca/phonelink-linux/dcgheaders"
	psignalr "github.com/YMGPwcca/phonelink-linux/protocol/signalr"
	"github.com/YMGPwcca/phonelink-linux/transport/relay"
	signalrtransport "github.com/YMGPwcca/phonelink-linux/transport/signalr"
	servicedcg "github.com/YMGPwcca/phonelink-linux/services/dcg"
)

type cloudFakeHub struct {
	reads     chan []byte
	sent      chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newCloudFakeHub(frame []byte) *cloudFakeHub {
	h := &cloudFakeHub{
		reads:  make(chan []byte, 8),
		sent:   make(chan []byte, 16),
		closed: make(chan struct{}),
	}
	h.reads <- frame
	return h
}

func (h *cloudFakeHub) SendBinary(payload []byte) error {
	h.sent <- append([]byte(nil), payload...)
	return nil
}

func (h *cloudFakeHub) ReadBinary() ([]byte, error) {
	select {
	case frame := <-h.reads:
		return frame, nil
	case <-h.closed:
		return nil, errors.New("closed")
	}
}

func (h *cloudFakeHub) Close() error {
	h.closeOnce.Do(func() { close(h.closed) })
	return nil
}

func TestOpenCloudRelayRetriesMismatchedRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TransportConfiguration/user/transportconfiguration/SignalR" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.URL.Query().Get("api-version") != servicedcg.TransportConfigAPIVersion {
			t.Fatalf("query=%q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msa-token" {
			t.Fatalf("Authorization=%q", got)
		}
		_, _ = w.Write([]byte(`{"assignedShards":[{"region":"eastus"},{"region":"westus"}]}`))
	}))
	defer server.Close()

	info, err := dcgheaders.NewCrossDeviceClientInfo(
		"logical",
		"1.26072.116.0",
		"Public",
		"10.0.26100",
	)
	if err != nil {
		t.Fatal(err)
	}

	dials := 0
	dial := func(ctx context.Context, cfg signalrtransport.Config) (relay.Hub, error) {
		dials++
		if cfg.AccessToken != "dcg-general" {
			t.Fatalf("token=%q", cfg.AccessToken)
		}
		if cfg.Headers.Get("DCG-PartnerId") != "" {
			t.Fatalf("account connection must not have partner id: %q", cfg.Headers.Get("DCG-PartnerId"))
		}
		wantRegion := "eastus"
		resolvedRegion := "wrong-region"
		partners := []string(nil)
		if dials == 2 {
			wantRegion = "westus"
			resolvedRegion = "westus"
			partners = []string{"phone"}
		}
		if got := cfg.Headers.Get("DCG-HubRegion"); got != wantRegion {
			t.Fatalf("region=%q want=%q", got, wantRegion)
		}
		if got := cfg.Headers.Get("DCG-HeartBeatFrequency"); got != "15" {
			t.Fatalf("heartbeat=%q", got)
		}
		if cfg.Headers.Get("traceparent") == "" ||
			cfg.Headers.Get("tracestate") == "" ||
			cfg.Headers.Get("DCG-TraceState") == "" {
			t.Fatalf("missing trace headers: %#v", cfg.Headers)
		}
		return newCloudFakeHub(hubOnConnectedFrame(t, resolvedRegion, partners)), nil
	}

	service := servicedcg.NewClient(server.URL)
	cloud, err := OpenCloudRelay(context.Background(), CloudConfig{
		Services:       service,
		MSAAccessToken: "msa-token",
		DCGAccessToken: "dcg-general",
		ClientInfo:     info,
		DialHub:        dial,
		OnConnectedTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cloud.Close()
	if dials != 2 || cloud.Region != "westus" ||
		len(cloud.Partners) != 1 || cloud.Partners[0] != "phone" {
		t.Fatalf("dials=%d cloud=%#v", dials, cloud)
	}
}

func TestOpenCloudRelayRejectsNoShards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assignedShards":[]}`))
	}))
	defer server.Close()
	info, err := dcgheaders.NewCrossDeviceClientInfo("logical", "1", "Public", "10")
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenCloudRelay(context.Background(), CloudConfig{
		Services:       servicedcg.NewClient(server.URL),
		MSAAccessToken: "msa",
		DCGAccessToken: "dcg",
		ClientInfo:     info,
	})
	if err == nil {
		t.Fatal("expected no-shards error")
	}
}

func hubOnConnectedFrame(t *testing.T, region string, partners []string) []byte {
	t.Helper()
	body := make([]byte, 0, 128)
	body = append(body, 0x96, 0x01, 0x80, 0xc0)
	body = appendMPString(body, psignalr.TargetOnConnected)
	body = append(body, 0x91, 0x82)
	body = appendMPString(body, "RegionName")
	body = appendMPString(body, region)
	body = appendMPString(body, "Partners")
	if len(partners) >= 16 {
		t.Fatal("test helper only supports short partner arrays")
	}
	body = append(body, 0x90|byte(len(partners)))
	for _, partner := range partners {
		body = appendMPString(body, partner)
	}
	body = append(body, 0x90)
	frame, err := psignalr.Frame(body)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func appendMPString(dst []byte, value string) []byte {
	if len(value) < 32 {
		dst = append(dst, 0xa0|byte(len(value)))
		return append(dst, value...)
	}
	dst = append(dst, 0xd9, byte(len(value)))
	return append(dst, value...)
}
