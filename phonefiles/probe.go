// Package phonefiles performs a single read-only metadata probe using an
// already-open LinkMyPhone PLATFORM session. It never enrolls a new identity.
package phonefiles

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	clipproto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
	"github.com/YMGPwcca/linkmyphone/protocol/dcg"
	proto "github.com/YMGPwcca/linkmyphone/protocol/phonefiles"
	"github.com/YMGPwcca/linkmyphone/protocol/platform"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
	"github.com/YMGPwcca/linkmyphone/transport/relay"
)

const MaxPlatformFrameBytes = proto.MaxResponseBytes + (64 << 10)

type Result struct {
	DeviceResourceStatus uint64
	Status uint64
	Files int
	HasMoreBatches bool
	BatchIndex uint64
	EstimatedTotalItems uint64
	PayloadBytes int
	Elapsed time.Duration
}

// Probe sends one DRM GET for the first FullSync batch. The subscription is
// installed before sending. Only the matching peer's original request ID is
// accepted, so unrelated clipboard/notification traffic is left alone.
func Probe(ctx context.Context, session *phonehost.Session) (Result, error) {
	if session == nil || session.Target.ID == "" {
		return Result{}, errors.New("phonefiles: active linked phone session required")
	}
	requestID, err := newUUID()
	if err != nil { return Result{}, err }
	platformID, err := newUUID()
	if err != nil { return Result{}, err }
	body, err := proto.MarshalFirstBatch(requestID)
	if err != nil { return Result{}, err }
	wrapped := clipproto.MarshalDeviceResourceMessage(clipproto.DeviceResourceMessage{
		ResourceType: clipproto.DeviceResourceType(proto.ResourceType),
		RequestType: clipproto.DeviceResourceRequestGET,
		Payload: body,
		ResourcePath: proto.ResourcePath,
	})
	wire, err := platform.Marshal(platform.NewDeviceResourceRequest(wrapped, platformID))
	if err != nil { return Result{}, err }

	endpoint, err := session.Subscribe("phone-files-probe", matcher(session.Target.ID, platformID), 1)
	if err != nil { return Result{}, err }
	defer endpoint.Close()

	started := time.Now()
	if err := endpoint.Send(ctx, session.Target.ID, "", dcg.TransportMessageTypePlatform, wire); err != nil {
		return Result{}, fmt.Errorf("phonefiles: send metadata GET: %w", err)
	}
	select {
	case <-ctx.Done():
		return Result{}, fmt.Errorf("phonefiles: metadata GET timed out or canceled: %w", ctx.Err())
	case incoming, ok := <-endpoint.Received():
		if !ok { return Result{}, errors.New("phonefiles: session subscription closed") }
		if len(incoming.Payload) > MaxPlatformFrameBytes {
			return Result{}, errors.New("phonefiles: platform response exceeds size limit")
		}
		pm, err := platform.Unmarshal(incoming.Payload)
		if err != nil { return Result{}, fmt.Errorf("phonefiles: decode platform response: %w", err) }
		if reason, ok := pm.Header(platform.HeaderRejectedReason); ok && reason != "" && reason != "None" {
			return Result{}, errors.New("phonefiles: PLATFORM request was rejected")
		}
		if len(pm.Payload) > proto.MaxResponseBytes + 1024 {
			return Result{}, errors.New("phonefiles: DeviceResourceManager response exceeds size limit")
		}
		drm, err := clipproto.UnmarshalDeviceResourceResponse(pm.Payload)
		if err != nil { return Result{}, fmt.Errorf("phonefiles: decode DRM response: %w", err) }
		result := Result{DeviceResourceStatus: uint64(drm.ResponseType), PayloadBytes: len(drm.Payload), Elapsed: time.Since(started)}
		if drm.ResponseType != clipproto.DeviceResourceResponseSuccess {
			return result, nil
		}
		summary, err := proto.ParseResponse(drm.Payload, requestID)
		if err != nil { return Result{}, err }
		result.Status = summary.Status
		result.Files = summary.Files
		result.HasMoreBatches = summary.HasMoreBatches
		result.BatchIndex = summary.BatchIndex
		result.EstimatedTotalItems = summary.EstimatedTotalItems
		return result, nil
	}
}

func matcher(target, originalRequestID string) phonehost.Matcher {
	return func(message relay.Received) bool {
		if message.Source != target || message.TransportMessageType != dcg.TransportMessageTypePlatform {
			return false
		}
		if len(message.Payload) > MaxPlatformFrameBytes { return false }
		pm, err := platform.Unmarshal(message.Payload)
		if err != nil { return false }
		route, _ := pm.Header(platform.HeaderRoute)
		id, _ := pm.Header(platform.HeaderOriginalRequestID)
		return route == platform.RouteInternalResponse && id == originalRequestID
	}
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { return "", fmt.Errorf("phonefiles: generate request ID: %w", err) }
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
