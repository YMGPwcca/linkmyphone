package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	sessionproto "github.com/YMGPwcca/linkmyphone/protocol/sessionvalidation"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

const DefaultSessionValidationTimeout = 10 * time.Second

var sessionValidationRequestID atomic.Uint64

type SessionValidationRelay interface {
	Send(context.Context, string, string, dcg.TransportMessageType, []byte) error
	Received() <-chan relay.Received
}

type SessionValidationResult struct {
	RequestID string
	SessionID string
	Headers   []platform.Header
	Response  sessionproto.Response
}

// ValidatePlatformSession mirrors PlatformConnectionSessionValidator's
// source-confirmed request: PLATFORM /SessionValidation with a protobuf payload
// advertising the local platform capabilities. The baseline Windows capability
// set always includes SessionValidation.
func ValidatePlatformSession(
	ctx context.Context,
	r SessionValidationRelay,
	targetDcgClientID string,
	timeout time.Duration,
) (SessionValidationResult, error) {
	var out SessionValidationResult
	if r == nil {
		return out, errors.New("bootstrap: session-validation relay is required")
	}
	if targetDcgClientID == "" {
		return out, errors.New("bootstrap: session-validation target is required")
	}
	if timeout <= 0 {
		timeout = DefaultSessionValidationTimeout
	}

	requestID := strconv.FormatUint(sessionValidationRequestID.Add(1), 10)
	payload := sessionproto.MarshalRequest(sessionproto.Request{
		Capabilities: []sessionproto.Capability{
			sessionproto.CapabilitySessionValidation,
		},
	})
	message := platform.NewSessionValidationRequest(payload, requestID)
	wire, err := platform.Marshal(message)
	if err != nil {
		return out, err
	}

	if err := r.Send(ctx, targetDcgClientID, "", dcg.TransportMessageTypePlatform, wire); err != nil {
		return out, fmt.Errorf("bootstrap: send SessionValidation: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			return out, fmt.Errorf("bootstrap: SessionValidation response: %w", waitCtx.Err())
		case incoming, ok := <-r.Received():
			if !ok {
				return out, errors.New("bootstrap: relay closed before SessionValidation response")
			}
			if incoming.Source != targetDcgClientID ||
				incoming.TransportMessageType != dcg.TransportMessageTypePlatform {
				continue
			}
			pm, err := platform.Unmarshal(incoming.Payload)
			if err != nil {
				// Ignore unrelated platform traffic that is not decodable by
				// this protocol version; the probe is waiting for one request.
				continue
			}
			route, _ := pm.Header(platform.HeaderRoute)
			if route != platform.RouteInternalResponse {
				continue
			}
			originalRequestID, _ := pm.Header(platform.HeaderOriginalRequestID)
			if originalRequestID != requestID {
				continue
			}
			if rejected, ok := pm.Header(platform.HeaderRejectedReason); ok && rejected != "" && rejected != "None" {
				return out, fmt.Errorf("bootstrap: SessionValidation rejected by peer: %s", rejected)
			}
			if len(pm.Payload) == 0 {
				return out, errors.New("bootstrap: SessionValidation response payload is empty")
			}
			response, err := sessionproto.UnmarshalResponse(pm.Payload)
			if err != nil {
				return out, fmt.Errorf("bootstrap: parse SessionValidation response: %w", err)
			}
			if !response.Has(sessionproto.CapabilitySessionValidation) {
				return out, errors.New("bootstrap: peer SessionValidation response lacks SessionValidation capability")
			}
			out = SessionValidationResult{
				RequestID: requestID,
				SessionID: incoming.SessionID,
				Headers:   append([]platform.Header(nil), pm.Headers...),
				Response:  response,
			}
			return out, nil
		}
	}
}
