package phonehost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	"github.com/YMGPwcca/linkmyphone/bootstrap"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
	signalrtransport "github.com/YMGPwcca/linkmyphone/transport/signalr"
)

func noJitter(d time.Duration) time.Duration { return d }

func TestSupervisorRecoversAndPinsPeerAfterTeardown(t *testing.T) {
	var opened, generations int
	var closed bool
	var waits []time.Duration
	open := func(ctx context.Context, cfg Config, _ kernel.Reporter) (*Session, error) {
		opened++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("startup has no deadline")
		}
		if opened < 3 {
			return nil, &servicedcg.HTTPError{StatusCode: 503}
		}
		if opened == 4 && (!closed || cfg.Target != "original-phone") {
			t.Fatalf("replacement opened before teardown or selected a new phone: closed=%v target=%q", closed, cfg.Target)
		}
		return &Session{Target: servicedcg.DeviceInfo{ID: "original-phone"}, cancel: func() { closed = true }}, nil
	}
	run := func(ctx context.Context, s *Session) error {
		generations++
		if generations == 1 {
			return fmt.Errorf("host: %w", &recoveryError{errors.New("lost relay")})
		}
		return nil
	}
	err := supervise(context.Background(), Config{StatePath: "unused", ReconnectMaxDelay: 2 * time.Second}, nil, run, open,
		func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }, time.Now, noJitter)
	if err != nil || opened != 4 || generations != 2 || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second, 2 * time.Second}) {
		t.Fatalf("err=%v opens=%d generations=%d delays=%v", err, opened, generations, waits)
	}
}

func TestSupervisorStableSessionResetsBackoff(t *testing.T) {
	clock := time.Unix(1700000000, 0)
	var opens, runs int
	var waits []time.Duration
	open := func(context.Context, Config, kernel.Reporter) (*Session, error) {
		opens++
		if opens <= 2 {
			return nil, errors.New("network offline")
		}
		return &Session{Target: servicedcg.DeviceInfo{ID: "phone"}}, nil
	}
	err := supervise(context.Background(), Config{StatePath: "unused"}, nil,
		func(context.Context, *Session) error {
			runs++
			if runs == 2 {
				return nil
			}
			clock = clock.Add(time.Minute)
			return &recoveryError{errors.New("refresh due")}
		}, open, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }, func() time.Time { return clock }, noJitter)
	if err != nil || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second, time.Second}) {
		t.Fatalf("err=%v waits=%v", err, waits)
	}
}

func TestSupervisorPermanentFailuresDoNotRetry(t *testing.T) {
	for _, failure := range []error{
		permanent(errors.New("bad state")), &bootstrap.PersistenceError{Err: errors.New("disk full")}, bootstrap.ErrIdentityMismatch,
		&msa.OAuthError{Code: "invalid_grant", StatusCode: 400}, &dcgauth.HTTPError{StatusCode: 403},
		&servicedcg.HTTPError{StatusCode: 400}, &signalrtransport.HTTPError{StatusCode: 403},
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			err := supervise(context.Background(), Config{StatePath: "unused"}, nil, func(context.Context, *Session) error { t.Fatal("runner called"); return nil },
				func(context.Context, Config, kernel.Reporter) (*Session, error) {
					return nil, fmt.Errorf("stage: %w", failure)
				},
				func(context.Context, time.Duration) error { t.Fatal("retried permanent failure"); return nil }, time.Now, noJitter)
			if !errors.Is(err, failure) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestSupervisorFeatureFailureIsTerminal(t *testing.T) {
	failure := errors.New("invalid module configuration")
	err := supervise(context.Background(), Config{StatePath: "unused"}, nil,
		func(context.Context, *Session) error { return failure },
		func(context.Context, Config, kernel.Reporter) (*Session, error) { return &Session{}, nil },
		func(context.Context, time.Duration) error { t.Fatal("retried feature failure"); return nil }, time.Now, noJitter)
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
}

func TestSupervisorCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opens := 0
	err := supervise(ctx, Config{StatePath: "unused"}, nil, func(context.Context, *Session) error { return nil },
		func(context.Context, Config, kernel.Reporter) (*Session, error) {
			opens++
			cancel()
			return nil, context.Canceled
		},
		func(context.Context, time.Duration) error { t.Fatal("wait after cancellation"); return nil }, time.Now, noJitter)
	if err != nil || opens != 1 {
		t.Fatalf("err=%v opens=%d", err, opens)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if !errors.Is(waitRetry(cancelled, time.Hour), context.Canceled) {
		t.Fatal("backoff did not cancel")
	}
}

func TestSupervisorStartupDeadlineCanRecover(t *testing.T) {
	opens := 0
	err := supervise(context.Background(), Config{StatePath: "unused", SessionOpenTimeout: time.Millisecond}, nil,
		func(context.Context, *Session) error { return nil },
		func(ctx context.Context, _ Config, _ kernel.Reporter) (*Session, error) {
			opens++
			if opens == 1 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &Session{}, nil
		}, func(context.Context, time.Duration) error { return nil }, time.Now, noJitter)
	if err != nil || opens != 2 {
		t.Fatalf("err=%v opens=%d", err, opens)
	}
}

func TestSupervisorUnauthorizedRelayForcesNewOpenAndHonorsThrottle(t *testing.T) {
	opens := 0
	var waits []time.Duration
	err := supervise(context.Background(), Config{StatePath: "unused"}, nil, func(context.Context, *Session) error { return nil },
		func(context.Context, Config, kernel.Reporter) (*Session, error) {
			opens++
			if opens == 1 {
				return nil, &signalrtransport.HTTPError{StatusCode: 401}
			}
			if opens == 2 {
				return nil, &servicedcg.HTTPError{StatusCode: 429, RetryAfter: "120"}
			}
			return &Session{}, nil
		}, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }, time.Now, noJitter)
	if err != nil || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Minute}) {
		t.Fatalf("err=%v waits=%v", err, waits)
	}
}

func TestSupervisorRepeatedUnauthorizedRelayStopsAfterOneFreshAttempt(t *testing.T) {
	opens := 0
	err := supervise(context.Background(), Config{StatePath: "unused"}, nil, func(context.Context, *Session) error { return nil },
		func(context.Context, Config, kernel.Reporter) (*Session, error) {
			opens++
			return nil, &signalrtransport.HTTPError{StatusCode: 401}
		},
		func(context.Context, time.Duration) error { return nil }, time.Now, noJitter)
	var hub *signalrtransport.HTTPError
	if !errors.As(err, &hub) || opens != 2 {
		t.Fatalf("err=%v opens=%d", err, opens)
	}
}

func TestRefreshDeadlineUsesEarliestExpiryAndCapsMargin(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		msa  int
		dcg  time.Time
		want time.Duration
	}{
		{3600, now.Add(2 * time.Hour), 58 * time.Minute},
		{3600, now.Add(10 * time.Minute), 8 * time.Minute},
		{30, now.Add(time.Hour), 24 * time.Second},
		{0, time.Time{}, 28 * time.Minute},
	} {
		if got := refreshDeadline(now, tc.msa, tc.dcg, DefaultRefreshMargin).Sub(now); got != tc.want {
			t.Fatalf("got=%v want=%v", got, tc.want)
		}
	}
}

func TestHealthMonitorDetectsRefreshResumeAndPeerLoss(t *testing.T) {
	for _, reason := range []string{"refresh", "resume", "peer"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &Session{errors: make(chan error, 1)}
			now := time.Unix(1700000000, 0)
			at := now.Add(time.Hour)
			calls := 0
			clock := func() time.Time {
				calls++
				if calls > 1 {
					if reason == "resume" {
						return now.Add(time.Minute)
					}
					if reason == "refresh" {
						return now.Add(time.Second)
					}
				}
				return now
			}
			if reason == "refresh" {
				at = now
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.monitorHealth(ctx, at, time.Millisecond, func() bool { return reason != "peer" }, clock)
			}()
			select {
			case err := <-s.Errors():
				var recoverable *recoveryError
				if !errors.As(err, &recoverable) || !strings.Contains(err.Error(), map[string]string{"refresh": "refresh", "resume": "resume", "peer": "peer"}[reason]) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("no recovery requested")
			}
			<-done
		})
	}
}

func TestRetryPolicyJitterHeadersAndRedaction(t *testing.T) {
	for range 100 {
		if d := jitterDelay(time.Second); d < 500*time.Millisecond || d > time.Second {
			t.Fatalf("jitter=%v", d)
		}
	}
	now := time.Unix(1700000000, 0).UTC()
	err := fmt.Errorf("outer: %w", &dcgauth.HTTPError{StatusCode: 503, RetryAfter: now.Add(90 * time.Second).Format(http.TimeFormat), Body: "secret-token"})
	if d := retryAfter(err, now); d != 90*time.Second {
		t.Fatalf("Retry-After=%v", d)
	}
	if strings.Contains(retryReason(err), "secret") {
		t.Fatal("error body logged")
	}
	if d := retryAfter(&msa.OAuthError{StatusCode: 429}, now); d != time.Minute {
		t.Fatalf("429 fallback=%v", d)
	}
}
