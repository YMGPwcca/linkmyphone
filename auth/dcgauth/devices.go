package dcgauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const pathGetDeviceInfoList = "/DeviceAuthProxy/GetDeviceInfoList"

type DeviceInfoItem struct {
	ID                    string              `json:"id"`
	DistinguishedDeviceID string              `json:"distinguishedDeviceId,omitempty"`
	IsLinked              bool                `json:"isLinked"`
	IsEnabled             bool                `json:"isEnabled"`
	Name                  string              `json:"name,omitempty"`
	ClientType            string              `json:"clientType,omitempty"`
	ClientVersion         string              `json:"clientVersion,omitempty"`
	OSName                string              `json:"osName,omitempty"`
	OSVersion             string              `json:"osVersion,omitempty"`
	CustomData            string              `json:"customData,omitempty"`
	Capabilities          []string            `json:"capabilities,omitempty"`
	Certificates          map[string][]string `json:"certificates,omitempty"`
}

// GetDeviceInfoList retrieves the account's DCG device metadata using the
// source-confirmed DeviceAuthProxy API. DeviceInfoItem.ID is the peer DCG
// client id consumed by the platform/trust layers.
func (c *Client) GetDeviceInfoList(ctx context.Context, msaToken string) ([]DeviceInfoItem, error) {
	if msaToken == "" {
		return nil, errors.New("dcgauth: MSA token is required")
	}
	endpoint, err := c.deviceInfoListEndpoint()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+msaToken)
	req.Header.Set(HeaderAuthorizationType, UserIdentityTypeMSA)
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
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	var items []DeviceInfoItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("dcgauth: decode device info list: %w", err)
	}
	return items, nil
}

// LinkedPeerDevices mirrors the Windows peer filtering step: only linked
// devices other than the local DCG client are candidate peers.
func LinkedPeerDevices(items []DeviceInfoItem, selfDcgClientID string) []DeviceInfoItem {
	out := make([]DeviceInfoItem, 0, len(items))
	for _, item := range items {
		if !item.IsLinked || item.ID == "" || strings.EqualFold(item.ID, selfDcgClientID) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// CryptoTrustClientID is the exact mapping used by TrustManager.
func CryptoTrustClientID(dcgClientID string) string {
	return "trust_" + dcgClientID
}

// PeerTrustCertificate returns the certificate Windows prefers when syncing
// linked peers into local async trust: PKI first, then SelfSigned.
func PeerTrustCertificate(item DeviceInfoItem) (string, bool) {
	for _, kind := range []string{"PKI", "SelfSigned"} {
		if certs := item.Certificates[kind]; len(certs) > 0 && certs[0] != "" {
			return certs[0], true
		}
	}
	return "", false
}

// SupportsAsyncTrust mirrors the yppCapabilities bit check used for non-iOS
// peers. iOS devices are treated as supporting async trust unconditionally.
func SupportsAsyncTrust(item DeviceInfoItem) bool {
	if strings.EqualFold(item.OSName, "iOS") {
		return true
	}
	if item.CustomData == "" {
		return false
	}
	var custom struct {
		YPPCapabilities int `json:"yppCapabilities"`
	}
	if err := json.Unmarshal([]byte(item.CustomData), &custom); err != nil {
		return false
	}
	return custom.YPPCapabilities&0x20 == 0x20
}

func (c *Client) deviceInfoListEndpoint() (string, error) {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("dcgauth: invalid base URL: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + pathGetDeviceInfoList
	q := u.Query()
	q.Set("api-version", DeviceManagementAPIVersion)
	q.Set("top", "20")
	q.Set("filter", "")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
