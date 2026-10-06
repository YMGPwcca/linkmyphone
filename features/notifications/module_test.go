package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/dcgheaders"
	client "github.com/YMGPwcca/linkmyphone/notifications"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
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
	for _, raw := range []string{`{"remote_actions":"false"}`, `{"request_timeout_ms":0}`, `{"show_existing":false,"unknown":1}`, `{"remote_actions":null}`, `{"show_existing":null}`, `{"request_timeout_ms":null}`, `{} {}`, `null`} {
		if _, err := DecodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid config accepted: %s", raw)
		}
	}
}

func TestConfigDefaultsAndExplicitFalse(t *testing.T) {
	cfg, err := DecodeConfig(json.RawMessage(`{}`))
	if err != nil || cfg != DefaultConfig() {
		t.Fatalf("defaults: got %#v, err=%v", cfg, err)
	}
	cfg, err = DecodeConfig(json.RawMessage(`{"remote_actions":false,"show_existing":false}`))
	if err != nil || cfg.RemoteActions || cfg.ShowExisting || cfg.RequestTimeoutMS != 10000 {
		t.Fatalf("explicit false: got %#v, err=%v", cfg, err)
	}
}

func TestDesktopStartupHonorsTimeoutAndCallerCancellation(t *testing.T) {
	for _, name := range []string{"request timeout", "caller cancellation", "caller deadline"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "caller deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer deadlineCancel()
			}
			module, err := New(&phonehost.Session{Profile: dcgheaders.ProfilePhoneLink})
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			module.newDesktop = func(startupCtx context.Context) (client.NativeBackend, error) {
				deadline, ok := startupCtx.Deadline()
				if !ok || time.Until(deadline) > 110*time.Millisecond {
					t.Error("desktop startup has no bounded request/caller deadline")
				}
				close(entered)
				select {
				case <-startupCtx.Done():
					return nil, startupCtx.Err()
				case <-time.After(time.Second):
					return nil, errors.New("desktop startup did not cancel")
				}
			}
			if name == "caller cancellation" {
				go func() { <-entered; cancel() }()
			}
			instance, err := module.Start(ctx, json.RawMessage(`{"request_timeout_ms":100}`), nil)
			want := error(context.DeadlineExceeded)
			if name == "caller cancellation" {
				want = context.Canceled
			}
			if instance != nil || !errors.Is(err, want) {
				t.Fatalf("startup: instance=%v err=%v, want %v", instance, err, want)
			}
		})
	}
}
