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
)

const (
	pathEnrollDevice = "/DeviceAuthProxy/EnrollDevice"

	ClientTypeWEA = "WEA"
	ClientTypePL  = "PL"
	ClientTypeLTW = "LTW"

	CertificateCTSelfSigned = "CTSelfSigned"
	CertificateSelfSigned   = "SelfSigned"
	CertificatePKI          = "PKI"
)

type EnrollmentMetadata struct {
	Capabilities   []string `json:"capabilities"`
	ClientType     string   `json:"clientType"`
	ClientVersion  string   `json:"clientVersion"`
	ConsentVersion int      `json:"consentVersion"`
	CustomData     string   `json:"customData"`
	DisplayName    string   `json:"displayName"`
	IsEnabled      bool     `json:"isEnabled"`
	Manufacture    string   `json:"manufacture"`
	ModelName      string   `json:"modelName"`
	ModelVersion   string   `json:"modelVersion"`
	OSName         string   `json:"osName"`
	OSVersion      string   `json:"osVersion"`
}

type EnrollmentRequest struct {
	Certificates map[string][]string `json:"certificates"`
	Metadata     EnrollmentMetadata  `json:"metadata"`
}

type EnrollmentAccountInfo struct {
	AccountKey string `json:"accountKey,omitempty"`
	FirstName  string `json:"firstName,omitempty"`
	LastName   string `json:"lastName,omitempty"`
	SignInName string `json:"signInName,omitempty"`
}

type EnrollmentResponse struct {
	AccountCert          string                `json:"accountCert"`
	AccountInfo          EnrollmentAccountInfo `json:"accountInfo,omitempty"`
	Devices              []DeviceInfoItem      `json:"devices,omitempty"`
	RootCertificateChain []string              `json:"rootCertificateChain,omitempty"`
}

// WindowsCompatibleEnrollmentMetadata mirrors DeviceMetadataProvider plus the
// DeviceMetadata constructor defaults used by CrossDevice. The service schema
// only exposes Windows/Android/iOS OS names, so the compatibility profile uses
// the Windows/WEA identity of the first-party CrossDevice client.
func WindowsCompatibleEnrollmentMetadata(clientVersion, displayName, osVersion string, capabilities []string) EnrollmentMetadata {
	if capabilities == nil {
		capabilities = []string{}
	}
	return EnrollmentMetadata{
		Capabilities:   capabilities,
		ClientType:     ClientTypeWEA,
		ClientVersion:  clientVersion,
		ConsentVersion: 0,
		CustomData:     "",
		DisplayName:    displayName,
		IsEnabled:      true,
		Manufacture:    "Unknown",
		ModelName:      "Unknown",
		ModelVersion:   "Unknown",
		OSName:         "Windows",
		OSVersion:      osVersion,
	}
}

// EnrollmentRequestWithTrustIdentity mirrors TrustHelper.EnrollDeviceInternalAsync:
// enrollment registers the separate trust_<dcgClientId> certificate as SelfSigned.
func EnrollmentRequestWithTrustIdentity(metadata EnrollmentMetadata, trust *TrustIdentity) (EnrollmentRequest, error) {
	if trust == nil || trust.ClientID == "" || len(trust.CertificateDER) == 0 {
		return EnrollmentRequest{}, errors.New("dcgauth: trust identity is required for enrollment")
	}
	if err := validateEnrollmentMetadata(metadata); err != nil {
		return EnrollmentRequest{}, err
	}
	return EnrollmentRequest{
		Certificates: map[string][]string{
			CertificateSelfSigned: {trust.CertificateBase64()},
		},
		Metadata: metadata,
	}, nil
}

// EnrollDevice performs the source-confirmed DeviceAuthProxy enrollment call.
// partnerDeviceKey is optional; the normal silent enrollment path supplies an
// empty value, in which case the query parameter is omitted.
func (c *Client) EnrollDevice(ctx context.Context, msaToken, dcgToken, partnerDeviceKey string, request EnrollmentRequest) (EnrollmentResponse, error) {
	if msaToken == "" {
		return EnrollmentResponse{}, errors.New("dcgauth: MSA token is required")
	}
	if dcgToken == "" {
		return EnrollmentResponse{}, errors.New("dcgauth: DCG token is required")
	}
	if err := validateEnrollmentRequest(request); err != nil {
		return EnrollmentResponse{}, err
	}

	endpoint, err := c.enrollEndpoint(partnerDeviceKey)
	if err != nil {
		return EnrollmentResponse{}, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return EnrollmentResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return EnrollmentResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+msaToken)
	req.Header.Set(HeaderAuthorizationType, UserIdentityTypeMSA)
	req.Header.Set("Dcg-Token", dcgToken)
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
		return EnrollmentResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return EnrollmentResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return EnrollmentResponse{}, &HTTPError{
			StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After"),
			Body: strings.TrimSpace(string(body)),
		}
	}
	var out EnrollmentResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return EnrollmentResponse{}, fmt.Errorf("dcgauth: decode enrollment response: %w", err)
	}
	if out.AccountCert == "" {
		return EnrollmentResponse{}, errors.New("dcgauth: enrollment response has no account certificate")
	}
	return out, nil
}

func (c *Client) enrollEndpoint(partnerDeviceKey string) (string, error) {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = ProdServiceBase
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("dcgauth: invalid base URL: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + pathEnrollDevice
	q := u.Query()
	q.Set("api-version", DeviceManagementAPIVersion)
	if partnerDeviceKey != "" {
		q.Set("pop-device-key", partnerDeviceKey)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func validateEnrollmentRequest(request EnrollmentRequest) error {
	if len(request.Certificates) == 0 {
		return errors.New("dcgauth: enrollment certificates are required")
	}
	for kind, certs := range request.Certificates {
		if kind == "" || len(certs) == 0 {
			return errors.New("dcgauth: enrollment certificate entry is empty")
		}
		for _, cert := range certs {
			if cert == "" {
				return errors.New("dcgauth: enrollment certificate is empty")
			}
		}
	}
	return validateEnrollmentMetadata(request.Metadata)
}

func validateEnrollmentMetadata(metadata EnrollmentMetadata) error {
	switch metadata.ClientType {
	case ClientTypeWEA, ClientTypePL, ClientTypeLTW:
	default:
		return fmt.Errorf("dcgauth: unsupported client type %q", metadata.ClientType)
	}
	if metadata.ClientVersion == "" {
		return errors.New("dcgauth: client version is required")
	}
	if metadata.DisplayName == "" {
		return errors.New("dcgauth: display name is required")
	}
	switch metadata.OSName {
	case "Windows", "Android", "iOS":
	default:
		return fmt.Errorf("dcgauth: unsupported OS name %q", metadata.OSName)
	}
	if metadata.OSVersion == "" {
		return errors.New("dcgauth: OS version is required")
	}
	return nil
}
