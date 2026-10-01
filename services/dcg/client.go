package dcg

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

	"github.com/YMGPwcca/phonelink-linux/dcgheaders"
)

const (
	ProdServiceBase = "https://dcg.microsoft.com/"

	EnvironmentDiscoveryAPIVersion = "1.0.0"
	TransportConfigAPIVersion      = "1.0.0"
	DeviceManagementAPIVersion     = "1.5.0"
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
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("dcg: service returned HTTP %d: %s", e.StatusCode, e.Body)
}

type EnvironmentResponse struct {
	ServiceEnvironment string `json:"serviceEnvironment"`
}

type RegionInfo struct {
	Region string `json:"region"`
}

type ShardsResponse struct {
	AssignedShards []RegionInfo `json:"assignedShards"`
}

type DeviceInfo struct {
	Capabilities          []string            `json:"capabilities,omitempty"`
	Certificates          map[string][]string `json:"certificates,omitempty"`
	ClientType            string              `json:"clientType,omitempty"`
	ClientVersion         string              `json:"clientVersion,omitempty"`
	ConsentVersion        *int                `json:"consentVersion,omitempty"`
	CustomData            string              `json:"customData,omitempty"`
	DistinguishedDeviceID string              `json:"distinguishedDeviceId,omitempty"`
	GlobalDeviceID        string              `json:"globalDeviceId,omitempty"`
	ID                    string              `json:"id,omitempty"`
	IsEnabled             *bool               `json:"isEnabled,omitempty"`
	IsLinked              *bool               `json:"isLinked,omitempty"`
	LastSeenTime          *int64              `json:"lastSeenTime,omitempty"`
	Manufacture           string              `json:"manufacture,omitempty"`
	ModelName             string              `json:"modelName,omitempty"`
	ModelVersion          string              `json:"modelVersion,omitempty"`
	Name                  string              `json:"name,omitempty"`
	OSName                string              `json:"osName,omitempty"`
	OSVersion             string              `json:"osVersion,omitempty"`
	RegistrationTime      *int64              `json:"registrationTime,omitempty"`
}

type DeviceMetadata struct {
	Capabilities          []string `json:"capabilities"`
	ClientType            string   `json:"clientType"`
	ClientVersion         string   `json:"clientVersion"`
	ConsentVersion        int      `json:"consentVersion"`
	CustomData            string   `json:"customData"`
	DisplayName           string   `json:"displayName"`
	DistinguishedDeviceID string   `json:"distinguishedDeviceId,omitempty"`
	GlobalDeviceID        string   `json:"globalDeviceId,omitempty"`
	IsEnabled             bool     `json:"isEnabled"`
	Manufacture           string   `json:"manufacture"`
	ModelName             string   `json:"modelName"`
	ModelVersion          string   `json:"modelVersion"`
	OSName                string   `json:"osName"`
	OSVersion             string   `json:"osVersion"`
}

type EnrollRequest struct {
	Certificates map[string][]string `json:"certificates"`
	Metadata     DeviceMetadata      `json:"metadata"`
}

type AccountInfo struct {
	AccountKey string `json:"accountKey,omitempty"`
	FirstName  string `json:"firstName,omitempty"`
	LastName   string `json:"lastName,omitempty"`
	SignInName string `json:"signInName,omitempty"`
}

type EnrollResponse struct {
	AccountCert          string       `json:"accountCert,omitempty"`
	AccountInfo          *AccountInfo `json:"accountInfo,omitempty"`
	Devices              []DeviceInfo `json:"devices,omitempty"`
	RootCertificateChain []string     `json:"rootCertificateChain,omitempty"`
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	return &Client{
		BaseURL:             baseURL,
		HTTP:                http.DefaultClient,
		AuthorizationPortal: dcgheaders.PortalLegacyMSM,
	}
}

// DiscoverEnvironment mirrors EnvironmentDiscovery/UserServiceEnvironment.
// Windows defaults to production when this service fails for non-auth reasons.
func (c *Client) DiscoverEnvironment(ctx context.Context, msaToken string) (EnvironmentResponse, error) {
	var out EnvironmentResponse
	err := c.doJSON(ctx, http.MethodGet, "/EnvironmentDiscovery/UserServiceEnvironment",
		map[string]string{"api-version": EnvironmentDiscoveryAPIVersion}, msaToken, "", nil, &out)
	return out, err
}

// AssignedSignalRShards returns the account's SignalR region assignments.
func (c *Client) AssignedSignalRShards(ctx context.Context, msaToken string) (ShardsResponse, error) {
	var out ShardsResponse
	err := c.doJSON(ctx, http.MethodGet, "/TransportConfiguration/user/transportconfiguration/SignalR",
		map[string]string{"api-version": TransportConfigAPIVersion}, msaToken, "", nil, &out)
	return out, err
}

// ListDevices is the cloud source used by Windows to discover linked peer
// DcgClientIds. DeviceInfo.ID is the DCG client id.
func (c *Client) ListDevices(ctx context.Context, msaToken string) ([]DeviceInfo, error) {
	var out []DeviceInfo
	err := c.doJSON(ctx, http.MethodGet, "/DeviceAuthProxy/GetDeviceInfoList",
		map[string]string{
			"api-version": DeviceManagementAPIVersion,
			"filter":      "",
			"top":         "20",
		}, msaToken, "", nil, &out)
	return out, err
}

// EnrollDevice registers local metadata and its trust certificate. The DCG
// token is the "general" token returned by CreateIdentity/SignIn.
func (c *Client) EnrollDevice(ctx context.Context, msaToken, dcgToken, popDeviceKey string, request EnrollRequest) (EnrollResponse, error) {
	if dcgToken == "" {
		return EnrollResponse{}, errors.New("dcg: DCG token is required")
	}
	if request.Metadata.ClientType == "" ||
		request.Metadata.ClientVersion == "" ||
		request.Metadata.DisplayName == "" ||
		request.Metadata.OSName == "" ||
		request.Metadata.OSVersion == "" {
		return EnrollResponse{}, errors.New("dcg: incomplete device metadata")
	}
	if len(request.Certificates) == 0 {
		return EnrollResponse{}, errors.New("dcg: at least one device certificate is required")
	}
	query := map[string]string{
		"api-version": DeviceManagementAPIVersion,
	}
	if popDeviceKey != "" {
		query["pop-device-key"] = popDeviceKey
	}
	var out EnrollResponse
	err := c.doJSON(ctx, http.MethodPost, "/DeviceAuthProxy/EnrollDevice",
		query, msaToken, dcgToken, request, &out)
	return out, err
}

// LinkedPeers returns linked devices other than self. Windows uses the Id field
// as the peer DcgClientId and syncs its certificate into local trust storage.
func LinkedPeers(devices []DeviceInfo, selfDcgClientID string) []DeviceInfo {
	out := make([]DeviceInfo, 0, len(devices))
	for _, device := range devices {
		if device.IsLinked == nil || !*device.IsLinked {
			continue
		}
		if selfDcgClientID != "" && strings.EqualFold(device.ID, selfDcgClientID) {
			continue
		}
		out = append(out, device)
	}
	return out
}

func (c *Client) doJSON(
	ctx context.Context,
	method, path string,
	query map[string]string,
	msaToken, dcgToken string,
	body any,
	out any,
) error {
	if msaToken == "" {
		return errors.New("dcg: MSA token is required")
	}
	endpoint, err := c.endpoint(path, query)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+msaToken)
	req.Header.Set("Authorization-Type", "MSA")
	c.ClientInfo.ApplyHTTP(req.Header, c.AuthorizationPortal)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if dcgToken != "" {
		req.Header.Set("Dcg-Token", dcgToken)
	}
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

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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
		return fmt.Errorf("dcg: decode service response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(path string, query map[string]string) (string, error) {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("dcg: invalid base URL: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	q := u.Query()
	for key, value := range query {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// MetadataForClipboardPC produces the Windows-side metadata shape used by the
// CrossDevice runtime, while leaving app version, display name, and OS version
// explicit so callers do not silently spoof a package build.
func MetadataForClipboardPC(clientVersion, displayName, osVersion string) DeviceMetadata {
	return DeviceMetadata{
		Capabilities:   []string{"CLIPBOARD"},
		ClientType:     "WEA",
		ClientVersion:  clientVersion,
		DisplayName:    displayName,
		IsEnabled:      true,
		Manufacture:    "Unknown",
		ModelName:      "Unknown",
		ModelVersion:   "Unknown",
		OSName:         "Windows",
		OSVersion:      osVersion,
		ConsentVersion: 0,
		CustomData:     "",
	}
}

