package signalr

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// NewTraceContextPacket mirrors TraceContextUtils.GenerateTraceContext plus
// HubRelayTraceContextPacket(TraceContext): TraceId is 16 random bytes (32 hex
// chars), ParentId carries the current correlation id (8 random bytes / 16 hex
// chars), TraceFlags is zero, and TraceState is a non-nil empty map.
func NewTraceContextPacket() (TraceContextPacket, error) {
	traceID, err := randomTraceHex(16)
	if err != nil {
		return TraceContextPacket{}, err
	}
	parentID, err := randomTraceHex(8)
	if err != nil {
		return TraceContextPacket{}, err
	}
	return TraceContextPacket{
		TraceID:    &traceID,
		ParentID:   &parentID,
		TraceFlags: 0,
		TraceState: map[string]string{},
	}, nil
}

// NormalizeTraceContextPacket preserves an existing trace when usable and
// fills the fields Windows receivers require before converting a Hub Relay
// trace packet back into TraceContext.
func NormalizeTraceContextPacket(in TraceContextPacket) (TraceContextPacket, error) {
	out := in
	if out.TraceID == nil || len(*out.TraceID) != 32 {
		traceID, err := randomTraceHex(16)
		if err != nil {
			return TraceContextPacket{}, err
		}
		out.TraceID = &traceID
	}
	if out.ParentID == nil || len(*out.ParentID) != 16 {
		parentID, err := randomTraceHex(8)
		if err != nil {
			return TraceContextPacket{}, err
		}
		out.ParentID = &parentID
	}
	if out.TraceState == nil {
		out.TraceState = map[string]string{}
	}
	return out, nil
}

func randomTraceHex(bytes int) (string, error) {
	if bytes <= 0 {
		return "", errors.New("signalr: trace id size must be positive")
	}
	raw := make([]byte, bytes)
	for {
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		allZero := true
		for _, b := range raw {
			if b != 0 {
				allZero = false
				break
			}
		}
		if !allZero {
			return hex.EncodeToString(raw), nil
		}
	}
}
