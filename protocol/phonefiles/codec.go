// Package phonefiles implements the minimal read-only Phone Files GET wire
// format. No file content, names, paths or raw metadata are retained.
package phonefiles

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	ResourcePath = "/filesmetadata"
	ResourceType = 1 // ProtoDeviceResourceType.FILES_METADATA
	StatusSuccess = 1
	MaxResponseBytes = 4 << 20
	MaxMetadataItems = 5000
)

type MetadataSummary struct {
	Status uint64
	RequestID string
	Files int
	HasMoreBatches bool
	BatchIndex uint64
	EstimatedTotalItems uint64
}

// MarshalFirstBatch requests a full metadata sync, with batched-response
// support. No continuation token is supplied, and no further batches are
// fetched by this probe. SeqNum=-1 matches Windows' initial sync cursor.
func MarshalFirstBatch(requestID string) ([]byte, error) {
	if requestID == "" || len(requestID) > 128 {
		return nil, errors.New("phonefiles: invalid request ID")
	}
	sync := appendVarintField(nil, 1, 1) // FullSync
	sync = appendVarintField(sync, 2, ^uint64(0)) // SeqNum -1 (int64)
	sync = appendVarintField(sync, 4, 1) // SupportsBatchedResponse=true
	out := appendBytesField(nil, 2, sync) // FilesMetadataSyncRequest
	out = appendBytesField(out, 4, []byte(requestID)) // RequestId
	return out, nil
}

// ParseResponse extracts only an aggregate, validating nested protobuf
// framing without materializing individual FileMetadata content.
func ParseResponse(payload []byte, expectedID string) (MetadataSummary, error) {
	var result MetadataSummary
	if len(payload) > MaxResponseBytes {
		return result, errors.New("phonefiles: response exceeds 4 MiB limit")
	}
	var statusSeen, syncSeen, fullSeen bool
	err := walk(payload, func(field, wire int, data []byte, number uint64) error {
		switch field {
		case 1:
			if wire != 0 { return errors.New("phonefiles: invalid status wire type") }
			result.Status, statusSeen = number, true
		case 3:
			if wire != 2 { return errors.New("phonefiles: invalid sync response wire type") }
			syncSeen = true
			if err := walk(data, func(field, wire int, data []byte, number uint64) error {
				if field != 1 { return nil }
				if wire != 2 { return errors.New("phonefiles: invalid full-sync wire type") }
				fullSeen = true
				return walk(data, func(field, wire int, data []byte, number uint64) error {
					switch field {
					case 1:
						if wire != 2 { return errors.New("phonefiles: invalid metadata wire type") }
						result.Files++
						if result.Files > MaxMetadataItems { return errors.New("phonefiles: too many metadata items") }
					case 4:
						if wire != 0 { return errors.New("phonefiles: invalid has-more wire type") }
						result.HasMoreBatches = number != 0
					case 6:
						if wire != 0 { return errors.New("phonefiles: invalid batch index wire type") }
						result.BatchIndex = number
					case 7:
						if wire != 0 { return errors.New("phonefiles: invalid item estimate wire type") }
						result.EstimatedTotalItems = number
					}
					return nil
				})
			}); err != nil { return err }
		case 5:
			if wire != 2 { return errors.New("phonefiles: invalid response ID wire type") }
			if len(data) > 128 { return errors.New("phonefiles: response ID too long") }
			result.RequestID = string(data)
		}
		return nil
	})
	if err != nil { return MetadataSummary{}, err }
	if !statusSeen { return result, errors.New("phonefiles: response has no status") }
	if result.RequestID != "" && result.RequestID != expectedID {
		return MetadataSummary{}, errors.New("phonefiles: response request ID mismatch")
	}
	if result.Status == StatusSuccess && (!syncSeen || !fullSeen) {
		return result, errors.New("phonefiles: successful response has no full-sync payload")
	}
	return result, nil
}

func appendVarintField(dst []byte, field, number uint64) []byte {
	dst = binary.AppendUvarint(dst, field<<3)
	return binary.AppendUvarint(dst, number)
}

func appendBytesField(dst []byte, field uint64, value []byte) []byte {
	dst = binary.AppendUvarint(dst, field<<3|2)
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}

// walk rejects malformed or overflowing wire values and never copies values.
func walk(payload []byte, visit func(field, wire int, bytes []byte, number uint64) error) error {
	for len(payload) > 0 {
		key, n := binary.Uvarint(payload)
		if n <= 0 || key>>3 == 0 { return errors.New("phonefiles: malformed protobuf field") }
		payload = payload[n:]
		field, wire := int(key>>3), int(key&7)
		var bytesValue []byte
		var number uint64
		switch wire {
		case 0:
			number, n = binary.Uvarint(payload)
			if n <= 0 { return errors.New("phonefiles: malformed protobuf varint") }
			payload = payload[n:]
		case 1:
			if len(payload) < 8 { return errors.New("phonefiles: truncated fixed64") }
			bytesValue, payload = payload[:8], payload[8:]
		case 2:
			length, m := binary.Uvarint(payload)
			if m <= 0 { return errors.New("phonefiles: malformed length") }
			payload = payload[m:]
			if length > uint64(len(payload)) { return errors.New("phonefiles: truncated length field") }
			bytesValue, payload = payload[:int(length)], payload[int(length):]
		case 5:
			if len(payload) < 4 { return errors.New("phonefiles: truncated fixed32") }
			bytesValue, payload = payload[:4], payload[4:]
		default:
			return fmt.Errorf("phonefiles: unsupported protobuf wire type %d", wire)
		}
		if err := visit(field, wire, bytesValue, number); err != nil { return err }
	}
	return nil
}
