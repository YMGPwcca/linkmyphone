package notifications

import (
	"context"
	"encoding/json"
	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
	"testing"
)

func TestCrossDeviceCannotClaimNotificationReadiness(t *testing.T) {
	module, err := New(&phonehost.Session{Profile: dcgheaders.ProfileCrossDevice})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := module.Start(context.Background(), nil, nil)
	if err == nil || instance != nil {
		t.Fatal("unassociated WEA session claimed notification readiness")
	}
}
func TestReceiveOnlyConfigAndMalformedConfiguration(t *testing.T) {
	cfg, err := DecodeConfig(json.RawMessage(`{"remote_actions":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RemoteActions {
		t.Fatal("explicit receive-only configuration enabled phone mutations")
	}
	for _, raw := range []string{`{"remote_actions":"false"}`, `{"request_timeout_ms":0}`, `{"show_existing":false,"unknown":1}`, `{} {}`, `null`} {
		if _, err := DecodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid config accepted: %s", raw)
		}
	}
}
