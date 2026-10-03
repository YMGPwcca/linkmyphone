package msa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAuthority = "https://login.microsoftonline.com/consumers/oauth2/v2.0"
	DeviceCodeGrant  = "urn:ietf:params:oauth:grant-type:device_code"
)

// DeviceCodeClient implements the standard Microsoft identity-platform v2
// device authorization grant. The Windows CrossDevice source uses WAM instead,
// so this client is an interoperability experiment for Linux rather than a
// source-confirmed replacement for WAM.
type DeviceCodeClient struct {
	Authority string
	ClientID  string
	HTTP      interface {
		Do(*http.Request) (*http.Response, error)
	}
	Sleep func(context.Context, time.Duration) error
	Now   func() time.Time
}

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
	issuedAt        time.Time
}

type OAuthToken struct {
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

type OAuthError struct {
	Code          string `json:"error"`
	Description   string `json:"error_description"`
	CorrelationID string `json:"correlation_id,omitempty"`
	ErrorCodes    []int  `json:"error_codes,omitempty"`
	Timestamp     string `json:"timestamp,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	StatusCode    int    `json:"-"`
	RetryAfter    string `json:"-"`
}

func (e *OAuthError) Error() string {
	if e.Code == "http_error" {
		return fmt.Sprintf("msa: token endpoint HTTP %d", e.StatusCode)
	}
	if e.Description == "" {
		return "msa: oauth error: " + e.Code
	}
	return fmt.Sprintf("msa: oauth error %s: %s", e.Code, e.Description)
}

func NewDeviceCodeClient() *DeviceCodeClient {
	return &DeviceCodeClient{
		Authority: DefaultAuthority,
		ClientID:  ClientID,
		HTTP:      http.DefaultClient,
		Sleep:     sleepContext,
		Now:       time.Now,
	}
}

func (c *DeviceCodeClient) Start(ctx context.Context, scope string) (DeviceCode, error) {
	if strings.TrimSpace(scope) == "" {
		return DeviceCode{}, errors.New("msa: scope is required")
	}
	form := url.Values{
		"client_id": {c.clientID()},
		"scope":     {scope},
	}
	var out DeviceCode
	if err := c.postForm(ctx, "/devicecode", form, &out); err != nil {
		return DeviceCode{}, err
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.VerificationURI == "" {
		return DeviceCode{}, errors.New("msa: incomplete device-code response")
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	out.issuedAt = c.now()
	return out, nil
}

func (c *DeviceCodeClient) Poll(ctx context.Context, code DeviceCode) (OAuthToken, error) {
	if code.DeviceCode == "" {
		return OAuthToken{}, errors.New("msa: device code is required")
	}
	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	issuedAt := code.issuedAt
	if issuedAt.IsZero() {
		issuedAt = c.now()
	}
	expiresAt := issuedAt.Add(time.Duration(code.ExpiresIn) * time.Second)

	for {
		if code.ExpiresIn > 0 && !c.now().Before(expiresAt) {
			return OAuthToken{}, &OAuthError{Code: "expired_token", Description: "device code expired"}
		}
		if err := c.sleep(ctx, interval); err != nil {
			return OAuthToken{}, err
		}
		form := url.Values{
			"grant_type":  {DeviceCodeGrant},
			"client_id":   {c.clientID()},
			"device_code": {code.DeviceCode},
		}
		var out OAuthToken
		err := c.postForm(ctx, "/token", form, &out)
		if err == nil {
			if out.AccessToken == "" {
				return OAuthToken{}, errors.New("msa: token response has no access token")
			}
			return out, nil
		}
		var oauthErr *OAuthError
		if !errors.As(err, &oauthErr) {
			return OAuthToken{}, err
		}
		switch oauthErr.Code {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return OAuthToken{}, oauthErr
		}
	}
}

func (c *DeviceCodeClient) Refresh(ctx context.Context, refreshToken, scope string) (OAuthToken, error) {
	if refreshToken == "" {
		return OAuthToken{}, errors.New("msa: refresh token is required")
	}
	form := url.Values{
		"client_id":     {c.clientID()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if strings.TrimSpace(scope) != "" {
		form.Set("scope", scope)
	}
	var out OAuthToken
	if err := c.postForm(ctx, "/token", form, &out); err != nil {
		return OAuthToken{}, err
	}
	if out.AccessToken == "" {
		return OAuthToken{}, errors.New("msa: refresh response has no access token")
	}
	return out, nil
}

func ScopeWithOfflineAccess(resourceScope string) string {
	parts := strings.Fields(resourceScope)
	for _, part := range parts {
		if part == "offline_access" {
			return strings.Join(parts, " ")
		}
	}
	parts = append(parts, "offline_access")
	return strings.Join(parts, " ")
}

func (c *DeviceCodeClient) postForm(ctx context.Context, suffix string, form url.Values, out any) error {
	authority := strings.TrimRight(c.Authority, "/")
	if authority == "" {
		authority = DefaultAuthority
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authority+suffix, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var oauthErr OAuthError
		if err := json.Unmarshal(body, &oauthErr); err == nil && oauthErr.Code != "" {
			oauthErr.StatusCode = resp.StatusCode
			oauthErr.RetryAfter = resp.Header.Get("Retry-After")
			return &oauthErr
		}
		return &OAuthError{Code: "http_error", StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("msa: decode oauth response: %w", err)
	}
	return nil
}

func (c *DeviceCodeClient) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return ClientID
}

func (c *DeviceCodeClient) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *DeviceCodeClient) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	return sleepContext(ctx, d)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d DeviceCode) String() string {
	return d.Message + " (expires in " + strconv.Itoa(d.ExpiresIn) + "s)"
}
