package dcgauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/YMGPwcca/phonelink-linux/dcgheaders"
)

const (
	pathGenerateNonce  = "/Auth/GenerateNonce"
	pathCreateIdentity = "/Auth/CreateIdentity"
	pathSignIn         = "/Auth/SignIn"
	pathRotateKeys     = "/Auth/RotateKeys"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	BaseURL             string
	HTTP                HTTPDoer
	ClientInfo          dcgheaders.ClientInfo
	AuthorizationPortal string
	ExtraHeaders        http.Header
	Now                 func() time.Time
}

type NonceResponse struct {
	EpochTimeStamp *int64 `json:"epochTimeStamp,omitempty"`
	Nonce          string `json:"nonce"`
}

type TokenResponse struct {
	AccessToken           string `json:"accessToken"`
	DeviceID              string `json:"deviceId"`
	EpochExpirationTime   *int64 `json:"epochExpirationTime,omitempty"`
	KeyValidRemainingDays *int64 `json:"keyValidRemainingDays,omitempty"`
	TenantID              string `json:"tenantId,omitempty"`
}

type AccessToken struct {
	Token                 string
	Scope                 string
	DeviceID              string
	ExpiresAt             time.Time
	KeyValidRemainingDays int64
	TenantID              string
}

// GeneralAccessToken applies the exact semantics used by the Windows
// AuthServiceCryptoHelper after CreateIdentity succeeds: the returned service
// token is stored under the "general" scope for the newly-created DCG device.
func (r TokenResponse) GeneralAccessToken() (AccessToken, error) {
	if r.AccessToken == "" {
		return AccessToken{}, errors.New("dcgauth: token response has no access token")
	}
	if r.DeviceID == "" {
		return AccessToken{}, errors.New("dcgauth: token response has no device id")
	}
	if r.EpochExpirationTime == nil {
		return AccessToken{}, errors.New("dcgauth: token response has no expiration time")
	}
	keyDays := int64(-1)
	if r.KeyValidRemainingDays != nil {
		keyDays = *r.KeyValidRemainingDays
	}
	return AccessToken{
		Token:                 r.AccessToken,
		Scope:                 ServicesScopeGeneral,
		DeviceID:              r.DeviceID,
		ExpiresAt:             time.Unix(*r.EpochExpirationTime, 0).UTC(),
		KeyValidRemainingDays: keyDays,
		TenantID:              r.TenantID,
	}, nil
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("dcgauth: auth service returned HTTP %d: %s", e.StatusCode, e.Body)
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	return &Client{
		BaseURL:             baseURL,
		HTTP:                http.DefaultClient,
		AuthorizationPortal: dcgheaders.PortalLegacyMSM,
		Now:                 time.Now,
	}
}

// GenerateNonce performs the source-confirmed POST /Auth/GenerateNonce request.
func (c *Client) GenerateNonce(ctx context.Context, msaToken, deviceID string) (NonceResponse, error) {
	if deviceID == "" {
		return NonceResponse{}, errors.New("dcgauth: device id is required")
	}
	var out NonceResponse
	err := c.postJSON(ctx, msaToken, pathGenerateNonce, map[string]string{
		"deviceId": deviceID,
	}, &out)
	if err == nil && out.Nonce == "" {
		err = errors.New("dcgauth: auth service returned empty nonce")
	}
	return out, err
}

// CreateIdentity submits the certificate ownership JWT to POST /Auth/CreateIdentity.
func (c *Client) CreateIdentity(ctx context.Context, msaToken, certificateJWT string) (TokenResponse, error) {
	if certificateJWT == "" {
		return TokenResponse{}, errors.New("dcgauth: certificate JWT is required")
	}
	var out TokenResponse
	err := c.postJSON(ctx, msaToken, pathCreateIdentity, map[string]string{
		"certificateJWT": certificateJWT,
	}, &out)
	return out, err
}

// SignIn submits a nonce JWT from an existing identity to POST /Auth/SignIn.
func (c *Client) SignIn(ctx context.Context, msaToken, certificateJWT string) (TokenResponse, error) {
	if certificateJWT == "" {
		return TokenResponse{}, errors.New("dcgauth: certificate JWT is required")
	}
	var out TokenResponse
	err := c.postJSON(ctx, msaToken, pathSignIn, map[string]string{
		"certificateJWT": certificateJWT,
	}, &out)
	return out, err
}

// BootstrapIdentity performs GenerateNonce -> certificate JWT -> CreateIdentity.
// Persistence is deliberately left to the caller so the new private key is only
// committed after CreateIdentity succeeds, matching the Windows flow.
func (c *Client) BootstrapIdentity(ctx context.Context, msaToken string) (*Identity, TokenResponse, error) {
	id, err := NewIdentity(c.now())
	if err != nil {
		return nil, TokenResponse{}, err
	}
	nonce, err := c.GenerateNonce(ctx, msaToken, id.DeviceID)
	if err != nil {
		return nil, TokenResponse{}, err
	}
	jwt, err := id.SignNonceJWT(nonce.Nonce, c.now())
	if err != nil {
		return nil, TokenResponse{}, err
	}
	token, err := c.CreateIdentity(ctx, msaToken, jwt)
	if err != nil {
		return nil, TokenResponse{}, err
	}
	if token.DeviceID != "" && token.DeviceID != id.DeviceID {
		return nil, TokenResponse{}, fmt.Errorf(
			"dcgauth: create identity returned device id %q, want %q",
			token.DeviceID,
			id.DeviceID,
		)
	}
	return id, token, nil
}

// SignInIdentity refreshes a DCG services token using the persisted identity.
// Windows rejects a successful response whose deviceId differs from the active
// local identity, so this implementation does the same.
func (c *Client) SignInIdentity(ctx context.Context, msaToken string, identity *Identity) (TokenResponse, error) {
	if identity == nil || identity.DeviceID == "" {
		return TokenResponse{}, errors.New("dcgauth: identity is required")
	}
	nonce, err := c.GenerateNonce(ctx, msaToken, identity.DeviceID)
	if err != nil {
		return TokenResponse{}, err
	}
	jwt, err := identity.SignNonceJWT(nonce.Nonce, c.now())
	if err != nil {
		return TokenResponse{}, err
	}
	token, err := c.SignIn(ctx, msaToken, jwt)
	if err != nil {
		return TokenResponse{}, err
	}
	if token.DeviceID != "" && token.DeviceID != identity.DeviceID {
		return TokenResponse{}, fmt.Errorf(
			"dcgauth: sign-in returned device id %q, want %q",
			token.DeviceID,
			identity.DeviceID,
		)
	}
	return token, nil
}

func (c *Client) postJSON(ctx context.Context, msaToken, path string, body, out any) error {
	if msaToken == "" {
		return errors.New("dcgauth: MSA token is required")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint, err := c.endpoint(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderUserIdentityType, UserIdentityTypeMSA)
	req.Header.Set(HeaderUserIdentityToken, msaToken)
	req.Header.Set("Authorization", "Bearer "+msaToken)
	req.Header.Set(HeaderAuthorizationType, UserIdentityTypeMSA)
	c.ClientInfo.ApplyHTTP(req.Header, c.AuthorizationPortal)
	for key, values := range c.ExtraHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(responseBody)),
		}
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("dcgauth: decode auth response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(path string) (string, error) {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("dcgauth: invalid base URL: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	query := u.Query()
	query.Set("api-version", AuthAPIVersion)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
