package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	clipproto "github.com/YMGPwcca/phonelink-linux/protocol/clipboard"
	"github.com/YMGPwcca/phonelink-linux/protocol/dcg"
	"github.com/YMGPwcca/phonelink-linux/protocol/msaep"
	"github.com/YMGPwcca/phonelink-linux/protocol/platform"
)

const (
	DefaultContextProbeTimeout = 8 * time.Second
	MaxContextProbeTextBytes   = 4096
)

type ContextProbeOptions struct {
	Timeout     time.Duration
	ContentText *string
}

type ContextProbeClipboardRequest struct {
	Headers                   []platform.Header
	ResourcePath              string
	DeviceResourceRequestType clipproto.DeviceResourceRequestType
	ClipboardRequestType      clipproto.RequestType
	CorrelationID             string
}

type ContextProbeResult struct {
	RequestID           string
	CorrelationID       string
	SessionID           string
	Route               string
	Headers             []platform.Header
	RejectedReason      string
	MessageTag          int32
	ClipboardRequests   []ContextProbeClipboardRequest
	StatusFeatureOnSent bool
	ContentDeclined     bool
	ContentSent         bool
	ContentTextBytes    int
}

// ProbeClipboardContextPublish sends the source-confirmed Windows clipboard
// publication shape through /Context/Publish, then observes the first relevant
// PLATFORM reaction from the Android peer. Live Android behavior after a PC
// CLIPBOARD_CHANGE publication starts with /clipboard STATUS using the same
// correlation id. The probe answers STATUS with FEATURE_ON, matching the normal
// Windows-side resource handler, then keeps observing for CONTENT. CONTENT is
// answered with ResourceHandlerNotRegistered by default so no clipboard
// content is sent. When ContentText is explicitly supplied, the probe instead
// returns a normal text/plain clipboard response to validate PC-to-phone
// clipboard delivery end to end.
func ProbeClipboardContextPublish(
	ctx context.Context,
	r SessionValidationRelay,
	targetDcgClientID string,
	selfDcgClientID string,
	timeout time.Duration,
) (ContextProbeResult, error) {
	return ProbeClipboardContextPublishWithOptions(
		ctx,
		r,
		targetDcgClientID,
		selfDcgClientID,
		ContextProbeOptions{Timeout: timeout},
	)
}

func ProbeClipboardContextPublishWithOptions(
	ctx context.Context,
	r SessionValidationRelay,
	targetDcgClientID string,
	selfDcgClientID string,
	opts ContextProbeOptions,
) (ContextProbeResult, error) {
	var out ContextProbeResult
	if r == nil {
		return out, errors.New("bootstrap: context probe relay is required")
	}
	if targetDcgClientID == "" {
		return out, errors.New("bootstrap: context probe target is required")
	}
	if selfDcgClientID == "" {
		return out, errors.New("bootstrap: context probe self DCG client id is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultContextProbeTimeout
	}
	if opts.ContentText != nil && len([]byte(*opts.ContentText)) > MaxContextProbeTextBytes {
		return out, fmt.Errorf(
			"bootstrap: context probe text exceeds %d bytes",
			MaxContextProbeTextBytes,
		)
	}

	requestID, err := newContextProbeID()
	if err != nil {
		return out, fmt.Errorf("bootstrap: context probe request id: %w", err)
	}
	correlationID, err := newContextProbeID()
	if err != nil {
		return out, fmt.Errorf("bootstrap: context probe correlation id: %w", err)
	}
	messageID, err := newContextProbeID()
	if err != nil {
		return out, fmt.Errorf("bootstrap: context probe message id: %w", err)
	}

	pubsub := clipproto.MarshalPubSubPayload(
		clipproto.NewPCClipboardChangePublication(correlationID),
	)
	envelope := msaep.New(
		messageID,
		selfDcgClientID,
		int32(clipproto.ClipboardMessageTag),
		pubsub,
	)
	message := platform.NewContextPublish(msaep.Marshal(envelope), requestID)
	wire, err := platform.Marshal(message)
	if err != nil {
		return out, fmt.Errorf("bootstrap: marshal Context/Publish probe: %w", err)
	}

	out.RequestID = requestID
	out.CorrelationID = correlationID
	if err := r.Send(
		ctx,
		targetDcgClientID,
		"",
		dcg.TransportMessageTypePlatform,
		wire,
	); err != nil {
		return out, fmt.Errorf("bootstrap: send Context/Publish probe: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return out, nil
			}
			return out, waitCtx.Err()

		case incoming, ok := <-r.Received():
			if !ok {
				return out, errors.New("bootstrap: relay closed during Context/Publish probe")
			}
			if incoming.Source != targetDcgClientID ||
				incoming.TransportMessageType != dcg.TransportMessageTypePlatform {
				continue
			}

			pm, err := platform.Unmarshal(incoming.Payload)
			if err != nil {
				continue
			}
			route, _ := pm.Header(platform.HeaderRoute)

			switch route {
			case platform.RouteInternalResponse:
				originalRequestID, _ := pm.Header(platform.HeaderOriginalRequestID)
				if originalRequestID != requestID {
					continue
				}
				out.SessionID = incoming.SessionID
				out.Route = route
				out.Headers = append([]platform.Header(nil), pm.Headers...)
				out.RejectedReason, _ = pm.Header(platform.HeaderRejectedReason)
				return out, nil

			case platform.RouteDeviceResourceManager:
				drm, err := clipproto.UnmarshalDeviceResourceMessage(pm.Payload)
				if err != nil {
					return out, fmt.Errorf("bootstrap: parse Context probe DRM request: %w", err)
				}
				if drm.ResourcePath != clipproto.ResourcePath {
					continue
				}
				req, err := clipproto.UnmarshalRequest(drm.Payload)
				if err != nil {
					return out, fmt.Errorf("bootstrap: parse Context probe clipboard request: %w", err)
				}

				out.SessionID = incoming.SessionID
				out.Route = route
				out.Headers = append([]platform.Header(nil), pm.Headers...)
				out.ClipboardRequests = append(out.ClipboardRequests, ContextProbeClipboardRequest{
					Headers:                   append([]platform.Header(nil), pm.Headers...),
					ResourcePath:              drm.ResourcePath,
					DeviceResourceRequestType: drm.RequestType,
					ClipboardRequestType:      req.Type,
					CorrelationID:             req.CorrelationID,
				})

				incomingRequestID, hasRequestID := pm.Header(platform.HeaderRequestID)
				switch req.Type {
				case clipproto.RequestStatus:
					if !hasRequestID || incomingRequestID == "" {
						return out, errors.New("bootstrap: Context probe STATUS missing request id")
					}
					response := clipproto.DeviceResourceResponse{
						ResponseType: clipproto.DeviceResourceResponseSuccess,
						Payload:      clipproto.MarshalResponse(clipproto.NewFeatureOnResponse(req.CorrelationID)),
					}
					if err := sendContextProbeDRMResponse(
						ctx,
						r,
						incoming.Source,
						incomingRequestID,
						response,
					); err != nil {
						return out, err
					}
					out.StatusFeatureOnSent = true
					continue

				case clipproto.RequestContent:
					if !hasRequestID || incomingRequestID == "" {
						return out, errors.New("bootstrap: Context probe CONTENT missing request id")
					}
					if opts.ContentText != nil {
						response := clipproto.DeviceResourceResponse{
							ResponseType: clipproto.DeviceResourceResponseSuccess,
							Payload: clipproto.MarshalResponse(
								clipproto.NewTextResponse(
									req.CorrelationID,
									*opts.ContentText,
									nil,
								),
							),
						}
						if err := sendContextProbeDRMResponse(
							ctx,
							r,
							incoming.Source,
							incomingRequestID,
							response,
						); err != nil {
							return out, err
						}
						out.ContentSent = true
						out.ContentTextBytes = len([]byte(*opts.ContentText))
					} else {
						response := clipproto.DeviceResourceResponse{
							ResponseType: clipproto.DeviceResourceResponseResourceHandlerNotRegistered,
						}
						if err := sendContextProbeDRMResponse(
							ctx,
							r,
							incoming.Source,
							incomingRequestID,
							response,
						); err != nil {
							return out, err
						}
						out.ContentDeclined = true
					}
					return out, nil

				default:
					if hasRequestID && incomingRequestID != "" {
						response := clipproto.DeviceResourceResponse{
							ResponseType: clipproto.DeviceResourceResponseResourceHandlerNotRegistered,
						}
						if err := sendContextProbeDRMResponse(
							ctx,
							r,
							incoming.Source,
							incomingRequestID,
							response,
						); err != nil {
							return out, err
						}
					}
					return out, nil
				}

			case platform.RouteContextPublish:
				envelope, err := msaep.Unmarshal(pm.Payload)
				if err != nil {
					continue
				}
				out.SessionID = incoming.SessionID
				out.Route = route
				out.Headers = append([]platform.Header(nil), pm.Headers...)
				out.MessageTag = envelope.MessageTag
				return out, nil
			}
		}
	}
}

func sendContextProbeDRMResponse(
	ctx context.Context,
	r SessionValidationRelay,
	target string,
	requestID string,
	response clipproto.DeviceResourceResponse,
) error {
	reply := platform.NewInternalResponse(
		clipproto.MarshalDeviceResourceResponse(response),
		requestID,
	)
	replyWire, err := platform.Marshal(reply)
	if err != nil {
		return fmt.Errorf("bootstrap: marshal Context probe DRM response: %w", err)
	}
	if err := r.Send(
		ctx,
		target,
		"",
		dcg.TransportMessageTypePlatform,
		replyWire,
	); err != nil {
		return fmt.Errorf("bootstrap: send Context probe DRM response: %w", err)
	}
	return nil
}

func newContextProbeID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}
