package dcg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const DispatcherAPIVersion = "1.5.0"

type WakeRequest struct {
	CollapseKey string            `json:"collapseKey"`
	Data        map[string]string `json:"data"`
	DeviceID    string            `json:"deviceId"`
	Priority    string            `json:"priority"`
	TTL         int64             `json:"ttl"`
}

// Wake sends the source-confirmed Dispatcher/Wake request. Unlike device
// management operations this endpoint is authenticated with the DCG identity
// access token as a normal Bearer token, not the MSA token.
func (c *Client) Wake(ctx context.Context, dcgToken string, request WakeRequest) error {
	if dcgToken == "" {
		return errors.New("dcg: DCG token is required")
	}
	if request.DeviceID == "" {
		return errors.New("dcg: wake target device id is required")
	}
	if request.CollapseKey == "" {
		request.CollapseKey = "YPPWake"
	}
	if request.Priority == "" {
		request.Priority = "high"
	}
	if request.Data == nil {
		request.Data = make(map[string]string)
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	endpoint, err := c.endpoint("/Dispatcher/Wake", map[string]string{
		"api-version": DispatcherAPIVersion,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+dcgToken)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}
	return nil
}
