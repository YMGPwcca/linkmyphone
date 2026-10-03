package phonehost

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/auth/msa"
	"github.com/YMGPwcca/linkmyphone/bootstrap"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	servicedcg "github.com/YMGPwcca/linkmyphone/services/dcg"
	signalrtransport "github.com/YMGPwcca/linkmyphone/transport/signalr"
)

const (
	DefaultReconnectMinDelay  = time.Second
	DefaultReconnectMaxDelay  = time.Minute
	DefaultSessionOpenTimeout = 2 * time.Minute
	DefaultRefreshMargin      = 2 * time.Minute
	DefaultHealthInterval     = 5 * time.Second
	DefaultResumeThreshold    = 20 * time.Second
	stableSessionDuration     = 30 * time.Second
)

type recoveryError struct{ err error }

func (e *recoveryError) Error() string { return e.err.Error() }
func (e *recoveryError) Unwrap() error { return e.err }

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }
func permanent(err error) error         { return &permanentError{err: err} }

// Supervise opens successive validated sessions in the same process. run owns
// each session's feature lifecycle and must finish teardown before returning.
// Only host failures cause recovery; feature/config/control-plane failures do
// not become an infinite retry loop. Credentials and the selected peer survive.
func Supervise(ctx context.Context, cfg Config, reporter kernel.Reporter, run func(context.Context, *Session) error) error {
	return supervise(ctx, cfg, reporter, run, Open, waitRetry, time.Now, jitterDelay)
}

func supervise(ctx context.Context, cfg Config, reporter kernel.Reporter, run func(context.Context, *Session) error,
	open func(context.Context, Config, kernel.Reporter) (*Session, error),
	wait func(context.Context, time.Duration) error, now func() time.Time, jitter func(time.Duration) time.Duration,
) error {
	if err := normalizeConfig(&cfg); err != nil {
		return err
	}
	if run == nil {
		return errors.New("phonehost: session runner is required")
	}
	delay := cfg.ReconnectMinDelay
	unauthorizedRetries := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		attemptCtx, cancelOpen := context.WithTimeout(ctx, cfg.SessionOpenTimeout)
		session, err := open(attemptCtx, cfg, reporter)
		cancelOpen()
		if err == nil {
			// Never silently switch phones if discovery changes after a reconnect.
			cfg.Target = session.Target.ID
			started := now()
			generationCtx, cancelGeneration := context.WithCancel(ctx)
			err = run(generationCtx, session)
			cancelGeneration()
			_ = session.Close()
			if ctx.Err() != nil || err == nil {
				return nil
			}
			var recoverable *recoveryError
			if !errors.As(err, &recoverable) {
				return err
			}
			if now().Sub(started) >= stableSessionDuration {
				delay = cfg.ReconnectMinDelay
				unauthorizedRetries = 0
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if isPermanentFailure(err) {
			return err
		}
		var hubFailure *signalrtransport.HTTPError
		if errors.As(err, &hubFailure) && hubFailure.StatusCode == http.StatusUnauthorized {
			if unauthorizedRetries >= 1 {
				return fmt.Errorf("phonehost: relay still rejects freshly refreshed credentials: %w", err)
			}
			unauthorizedRetries++
		}
		sleep := jitter(delay)
		if floor := retryAfter(err, now()); floor > sleep {
			sleep = floor
		}
		report(reporter, "session recovering", map[string]string{
			"retry_in": sleep.String(), "reason": retryReason(err),
		})
		if err := wait(ctx, sleep); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if delay < cfg.ReconnectMaxDelay {
			if delay > cfg.ReconnectMaxDelay/2 {
				delay = cfg.ReconnectMaxDelay
			} else {
				delay *= 2
			}
		}
	}
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Equal jitter retains a lower bound instead of retrying immediately under load.
func jitterDelay(delay time.Duration) time.Duration {
	if delay < 2 {
		return delay
	}
	return delay/2 + time.Duration(rand.Int64N(int64(delay-delay/2)+1))
}

func isPermanentFailure(err error) bool {
	var local *permanentError
	var persistence *bootstrap.PersistenceError
	if errors.As(err, &local) || errors.As(err, &persistence) {
		return true
	}
	if errors.Is(err, bootstrap.ErrIdentityMismatch) {
		return true
	}
	var oauth *msa.OAuthError
	if errors.As(err, &oauth) {
		switch oauth.Code {
		case "temporarily_unavailable", "server_error", "slow_down":
			return false
		case "invalid_grant", "interaction_required", "login_required", "consent_required", "access_denied", "invalid_client", "unauthorized_client", "invalid_scope", "invalid_request":
			return true
		}
		return oauth.StatusCode >= 400 && oauth.StatusCode < 500 && oauth.StatusCode != 408 && oauth.StatusCode != 429
	}
	var auth *dcgauth.HTTPError
	if errors.As(err, &auth) {
		return permanentHTTP(auth.StatusCode)
	}
	var service *servicedcg.HTTPError
	if errors.As(err, &service) {
		return permanentHTTP(service.StatusCode)
	}
	var hub *signalrtransport.HTTPError
	// A relay 401 gets a fresh MSA/DCG credential on the next Open, as in
	// Phone Link's ForceRefresh policy. A 403 cannot be repaired by retries.
	if errors.As(err, &hub) {
		return permanentHTTP(hub.StatusCode) && hub.StatusCode != 401
	}
	return false
}

func permanentHTTP(code int) bool { return code >= 400 && code < 500 && code != 408 && code != 429 }

func retryAfter(err error, now time.Time) time.Duration {
	var header string
	var code int
	var auth *dcgauth.HTTPError
	var service *servicedcg.HTTPError
	var oauth *msa.OAuthError
	var hub *signalrtransport.HTTPError
	switch {
	case errors.As(err, &auth):
		header, code = auth.RetryAfter, auth.StatusCode
	case errors.As(err, &service):
		header, code = service.RetryAfter, service.StatusCode
	case errors.As(err, &oauth):
		header, code = oauth.RetryAfter, oauth.StatusCode
	case errors.As(err, &hub):
		header, code = hub.RetryAfter, hub.StatusCode
	}
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 32); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil && at.After(now) {
		return at.Sub(now)
	}
	if code == http.StatusTooManyRequests {
		return time.Minute
	}
	return 0
}

// Do not log HTTP bodies, bearer tokens, account payloads or OAuth descriptions.
func retryReason(err error) string {
	var oauth *msa.OAuthError
	if errors.As(err, &oauth) {
		return "Microsoft authentication: " + oauth.Code
	}
	var hub *signalrtransport.HTTPError
	if errors.As(err, &hub) {
		return fmt.Sprintf("relay HTTP %d", hub.StatusCode)
	}
	var auth *dcgauth.HTTPError
	if errors.As(err, &auth) {
		return fmt.Sprintf("DCG authentication HTTP %d", auth.StatusCode)
	}
	var service *servicedcg.HTTPError
	if errors.As(err, &service) {
		return fmt.Sprintf("DCG service HTTP %d", service.StatusCode)
	}
	return "session interrupted or cloud unavailable"
}

func refreshDeadline(issuedAt time.Time, msaLifetime int, dcgExpiry time.Time, margin time.Duration) time.Time {
	lifetime := 30 * time.Minute // Conservative fallback if MSA omits expires_in.
	if msaLifetime > 0 {
		lifetime = time.Duration(msaLifetime) * time.Second
	}
	expiry := issuedAt.Add(lifetime)
	if !dcgExpiry.IsZero() && dcgExpiry.Before(expiry) {
		expiry = dcgExpiry
	}
	if remaining := expiry.Sub(issuedAt); margin > remaining/5 {
		margin = remaining / 5
	}
	return expiry.Add(-margin)
}

func (s *Session) monitorHealth(ctx context.Context, refreshAt time.Time, interval time.Duration,
	partnerConnected func() bool, now func() time.Time,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := now().Round(0) // Wall time includes suspend; Go monotonic timers do not.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := now().Round(0)
			var err error
			switch {
			case !refreshAt.IsZero() && !current.Before(refreshAt.Round(0)):
				err = errors.New("phonehost: scheduled authentication refresh")
			case current.Sub(last) > DefaultResumeThreshold:
				err = errors.New("phonehost: resume or clock gap detected")
			case !partnerConnected():
				err = errors.New("phonehost: target peer disconnected")
			}
			if err != nil {
				s.reportRuntimeError(err)
				return
			}
			last = current
		}
	}
}
