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

const DefaultContextProbeTimeout = 8 * time.Second

type ContextProbeResult struct {
	RequestID                 string
	CorrelationID             string
	SessionID                 string
	Route                     string
	Headers                   []platform.Header
	RejectedReason            string
	MessageTag                int32
	ResourcePath              string
	DeviceResourceRequestType clipproto.DeviceResourceRequestType
	ClipboardRequestType      clipproto.RequestType
	ClipboardCorrelationID    string
}

// ProbeClipboardContextPublish sends the source-confirmed Windows clipboard
// publication shape through /Context/Publish, then observes the first relevant
// PLATFORM reaction from the Android peer. A /clipboard DRM request is answered
// with ResourceHandlerNotRegistered after it has been decoded so the probe does
// not leave the peer waiting for a response or mutate either clipboard.
func ProbeClipboardContextPublish(
	ctx context.Context,
	r SessionValidationRelay,
	targetDcgClientID string,
	selfDcgClientID string,
	timeout time.Duration,
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
	if timeout <= 0 {
		timeout = DefaultContextProbeTimeout
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

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
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
				out.ResourcePath = drm.ResourcePath
				out.DeviceResourceRequestType = drm.RequestType
				out.ClipboardRequestType = req.Type
				out.ClipboardCorrelationID = req.CorrelationID

				if incomingRequestID, ok := pm.Header(platform.HeaderRequestID); ok && incomingRequestID != "" {
					response := clipproto.DeviceResourceResponse{
						ResponseType: clipproto.DeviceResourceResponseResourceHandlerNotRegistered,
					}
					reply := platform.NewInternalResponse(
						clipproto.MarshalDeviceResourceResponse(response),
						incomingRequestID,
					)
					replyWire, err := platform.Marshal(reply)
					if err != nil {
						return out, fmt.Errorf("bootstrap: marshal Context probe DRM response: %w", err)
					}
					if err := r.Send(
						ctx,
						incoming.Source,
						"",
						dcg.TransportMessageTypePlatform,
						replyWire,
					); err != nil {
						return out, fmt.Errorf("bootstrap: send Context probe DRM response: %w", err)
					}
				}
				return out, nil

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
