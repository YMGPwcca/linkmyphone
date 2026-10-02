package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/YMGPwcca/linkmyphone/auth/dcgauth"
	"github.com/YMGPwcca/linkmyphone/services/dcg"
)

type WakeOptions struct {
	Environment          string
	HubRegion            string
	IgnoreDeviceDisabled bool
	RequestNewSession    bool
	CancelPrevious       bool
	TimeToLive           time.Duration
}

// WakePartner builds the source-confirmed signed dispatcher wake payload. The
// signature uses the trust_<localDcgClientId> identity created during device
// enrollment, not the DCG auth identity certificate.
func WakePartner(
	ctx context.Context,
	client *dcg.Client,
	trustIdentity *dcgauth.TrustIdentity,
	localDcgClientID string,
	targetDcgClientID string,
	dcgToken string,
	options WakeOptions,
) error {
	if client == nil {
		return errors.New("bootstrap: DCG service client is required")
	}
	if trustIdentity == nil {
		return errors.New("bootstrap: trust identity is required")
	}
	if localDcgClientID == "" || targetDcgClientID == "" {
		return errors.New("bootstrap: local and target DCG client ids are required")
	}
	environment := options.Environment
	if environment == "" {
		environment = "Prod"
	}
	data := map[string]string{
		"DCG-Environment":            environment,
		"IgnoreDeviceDisabledStatus": dotNetBool(options.IgnoreDeviceDisabled),
		"DCG-RequestNewSession":      dotNetBool(options.RequestNewSession),
	}
	if options.HubRegion != "" {
		data["DCG-HubRegion"] = options.HubRegion
	}
	if options.CancelPrevious {
		data["DCG-NoOp"] = "True"
	}
	rawData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	jwt, err := trustIdentity.SignExtrasJWT(dcgauth.Extras{
		Data:     string(rawData),
		SourceID: localDcgClientID,
		Scope:    "wake",
	}, time.Now())
	if err != nil {
		return err
	}
	data["DCG-CryptoWakeJwt"] = jwt

	ttl := int64(options.TimeToLive / time.Second)
	if ttl < 0 {
		ttl = 0
	}
	return client.Wake(ctx, dcgToken, dcg.WakeRequest{
		CollapseKey: "YPPWake",
		Data:        data,
		DeviceID:    targetDcgClientID,
		Priority:    "high",
		TTL:         ttl,
	})
}

func dotNetBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
