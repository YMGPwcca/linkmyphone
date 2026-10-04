package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	psignalr "github.com/YMGPwcca/linkmyphone/protocol/signalr"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
	signalrtransport "github.com/YMGPwcca/linkmyphone/transport/signalr"
)

func TestEnsurePeerOnlineWakesAndWaitsForPresence(t *testing.T) {
	hub := newCloudFakeHub(hubOnConnectedFrame(t, "westus", nil))
	wakeCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/TransportConfiguration/user/transportconfiguration/SignalR":
			_, _ = w.Write([]byte(`{"assignedShards":[{"region":"westus"}]}`))

		case "/Dispatcher/Wake":
			wakeCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer dcg-general" {
				t.Fatalf("Authorization=%q", got)
			}
			var body servicedcg.WakeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.DeviceID != "phone" ||
				body.CollapseKey != "YPPWake" ||
				body.Priority != "high" ||
				body.TTL != 60 ||
				body.Data["DCG-Environment"] != "Prod" ||
				body.Data["DCG-HubRegion"] != "westus" ||
				body.Data["DCG-RequestNewSession"] != "True" ||
				body.Data["DCG-CryptoWakeJwt"] == "" {
				t.Fatalf("wake=%#v", body)
			}
			w.WriteHeader(http.StatusOK)
			hub.reads <- hubOnPartnerConnectedFrame(t, "phone", "westus")

		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	info, err := dcgheaders.NewClientInfo(dcgheaders.ProfileCrossDevice, "logical", "1", "Public", "10")
	if err != nil {
		t.Fatal(err)
	}
	service := servicedcg.NewClient(server.URL)
	service.ClientInfo = info

	cloud, err := OpenCloudRelay(context.Background(), CloudConfig{
		Services:       service,
		MSAAccessToken: "msa",
		DCGAccessToken: "dcg-general",
		ClientInfo:     info,
		DialHub: func(context.Context, signalrtransport.Config) (relay.Hub, error) {
			return hub, nil
		},
		OnConnectedTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cloud.Close()

	trust, err := dcgauth.NewTrustIdentity("local", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	flushResult := make(chan error, 1)
	go func() {
		select {
		case frame := <-hub.sent:
			frames, err := psignalr.SplitFrames(frame)
			if err != nil || len(frames) != 1 {
				flushResult <- err
				return
			}
			inv, err := psignalr.ParseInvocation(frames[0])
			if err != nil {
				flushResult <- err
				return
			}
			if inv.Target != psignalr.TargetSendConnectedAsync || inv.InvocationID == nil {
				flushResult <- errors.New("expected pre-wake SendConnectedAsync")
				return
			}
			hub.reads <- hubCompletionVoidFrame(t, *inv.InvocationID)
			flushResult <- nil
		case <-time.After(time.Second):
			flushResult <- errors.New("timed out waiting for pre-wake SendConnectedAsync")
		}
	}()

	woke, err := EnsurePeerOnline(
		context.Background(),
		cloud,
		service,
		trust,
		"local",
		"phone",
		"dcg-general",
		PeerOnlineOptions{
			Timeout:           time.Second,
			WakeTTL:           60 * time.Second,
			RequestNewSession: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !woke {
		t.Fatal("expected wake request")
	}
	if wakeCalls != 1 {
		t.Fatalf("wakeCalls=%d", wakeCalls)
	}
	if err := <-flushResult; err != nil {
		t.Fatal(err)
	}
	if !cloud.Relay.PartnerConnected("phone") {
		t.Fatal("phone should be present after OnPartnerConnected")
	}
}

func TestEnsurePeerOnlineSkipsWakeWhenAlreadyPresent(t *testing.T) {
	hub := newCloudFakeHub(hubOnConnectedFrame(t, "westus", []string{"phone"}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/TransportConfiguration/user/transportconfiguration/SignalR":
			_, _ = w.Write([]byte(`{"assignedShards":[{"region":"westus"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	info, err := dcgheaders.NewClientInfo(dcgheaders.ProfileCrossDevice, "logical", "1", "Public", "10")
	if err != nil {
		t.Fatal(err)
	}
	service := servicedcg.NewClient(server.URL)
	cloud, err := OpenCloudRelay(context.Background(), CloudConfig{
		Services:       service,
		MSAAccessToken: "msa",
		DCGAccessToken: "dcg-general",
		ClientInfo:     info,
		DialHub: func(context.Context, signalrtransport.Config) (relay.Hub, error) {
			return hub, nil
		},
		OnConnectedTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cloud.Close()

	trust, err := dcgauth.NewTrustIdentity("local", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	woke, err := EnsurePeerOnline(
		context.Background(),
		cloud,
		service,
		trust,
		"local",
		"phone",
		"dcg-general",
		PeerOnlineOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if woke {
		t.Fatal("did not expect wake request for an already-present partner")
	}
}

func hubOnPartnerConnectedFrame(t *testing.T, source, region string) []byte {
	t.Helper()
	body := make([]byte, 0, 96)
	body = append(body, 0x96, 0x01, 0x80, 0xc0)
	body = appendMPString(body, psignalr.TargetOnPartnerConnected)
	body = append(body, 0x93)
	body = appendMPString(body, source)
	body = append(body, 0xc0)
	body = appendMPString(body, region)
	body = append(body, 0x90)

	frame, err := psignalr.Frame(body)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func hubCompletionVoidFrame(t *testing.T, invocationID string) []byte {
	t.Helper()
	body := []byte{0x94, 0x03, 0x80}
	body = appendMPString(body, invocationID)
	body = append(body, byte(psignalr.CompletionResultVoid))
	frame, err := psignalr.Frame(body)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}
