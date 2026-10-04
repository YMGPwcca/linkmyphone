// Package notifications contains the Phone Link notification APP contract.
package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/YMGPwcca/linkmyphone/protocol/app"
)

const (
	RouteConnect       = "/connect/v1"
	RouteNotifications = "/notifications"
	RoutePhoneContent  = "/legacy/phonecontent"
	ContentType        = "notifications"
	ContractVersion    = 3.99
)

const (
	OperationNew           int32 = 1
	OperationRemove        int32 = 2
	OperationExisting      int32 = 3
	OperationClear         int32 = 4
	OperationLaunch        int32 = 5
	OperationMedia         int32 = 6
	OperationSessionChange int32 = 7
)

type Action struct {
	Name        string `json:"actionName"`
	InlineReply bool   `json:"isActionInlineReply"`
	Index       int32  `json:"actionIndex"`
}

type Item struct {
	ID                int64    `json:"id"`
	Key               string   `json:"key"`
	PackageName       string   `json:"packageName"`
	AppName           string   `json:"appName"`
	Title             string   `json:"title"`
	Text              string   `json:"text"`
	BigText           string   `json:"bigText"`
	SubText           string   `json:"subText"`
	ConversationTitle string   `json:"conversationTitle"`
	TextLines         []string `json:"textLines"`
	PostTime          int64    `json:"postTime"`
	IsClearable       bool     `json:"isClearable"`
	IsOngoing         bool     `json:"isOngoing"`
	LargeIcon         string   `json:"largeIcon"`
	SmallIcon         string   `json:"smallIcon"`
	Actions           []Action `json:"actions"`
}

type Operation struct {
	Type int32
	Key  string
	Item *Item
}

type Batch struct {
	CorrelationVector string
	DedupeID          string
	Operations        []Operation
}

var ErrMalformed = errors.New("malformed notification batch")

const (
	maxBatchEntries = 4096
	maxBatchString  = 1 << 20
	maxBatchJSON    = 4 << 20
)

// DecodeBatch validates the three positionally-correlated notification arrays
// and decodes JSON only for operations which carry an item. It does not infer
// policy from a session-change key.
func DecodeBatch(values app.ValueSet) (Batch, error) {
	if values == nil {
		return Batch{}, fmt.Errorf("%w: nil values", ErrMalformed)
	}
	content, ok := values["contentType"].(string)
	if !ok || content != ContentType {
		return Batch{}, fmt.Errorf("%w: contentType", ErrMalformed)
	}
	keys, ok := values["notificationKeys"].([]string)
	if !ok {
		return Batch{}, fmt.Errorf("%w: notificationKeys type", ErrMalformed)
	}
	operations, ok := values["operations"].([]int32)
	if !ok {
		return Batch{}, fmt.Errorf("%w: operations type", ErrMalformed)
	}
	bodies, ok := values["notifications"].([]string)
	if !ok {
		return Batch{}, fmt.Errorf("%w: notifications type", ErrMalformed)
	}
	if len(keys) != len(operations) || len(keys) != len(bodies) {
		return Batch{}, fmt.Errorf("%w: correlated array lengths", ErrMalformed)
	}
	if len(keys) > maxBatchEntries {
		return Batch{}, fmt.Errorf("%w: too many operations", ErrMalformed)
	}
	batch := Batch{}
	if v, ok := values["correlationVector"].(string); ok {
		batch.CorrelationVector = v
	} else if _, present := values["correlationVector"]; present {
		return Batch{}, fmt.Errorf("%w: correlationVector type", ErrMalformed)
	}
	if v, ok := values["dedupeID"].(string); ok {
		batch.DedupeID = v
	} else if _, present := values["dedupeID"]; present {
		return Batch{}, fmt.Errorf("%w: dedupeID type", ErrMalformed)
	}
	batch.Operations = make([]Operation, len(keys))
	for i := range keys {
		if keys[i] == "" || len(keys[i]) > maxBatchString || len(bodies[i]) > maxBatchJSON {
			return Batch{}, fmt.Errorf("%w: operation size or empty key", ErrMalformed)
		}
		op := Operation{Type: operations[i], Key: keys[i]}
		switch op.Type {
		case OperationNew, OperationExisting:
			trimmed := bytes.TrimSpace([]byte(bodies[i]))
			if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
				return Batch{}, fmt.Errorf("%w: item body", ErrMalformed)
			}
			var item Item
			if err := json.Unmarshal(trimmed, &item); err != nil {
				return Batch{}, fmt.Errorf("%w: item JSON: %v", ErrMalformed, err)
			}
			if item.Key != op.Key {
				return Batch{}, fmt.Errorf("%w: item key mismatch", ErrMalformed)
			}
			op.Item = &item
		case OperationRemove:
			// Android serializes removals with an empty body. A non-empty body
			// is ignored for compatibility because removal identity is the key.
		case OperationSessionChange:
			// The key is a session identifier, not an Item JSON document.
		default:
			return Batch{}, fmt.Errorf("%w: unsupported operation %d", ErrMalformed, op.Type)
		}
		batch.Operations[i] = op
	}
	return batch, nil
}

type LocalInfo struct {
	DisplayName    string
	AppVersion     string
	InstallationID string
	RingName       string
}

func BaseValues(info LocalInfo, correlationVector string) app.ValueSet {
	return app.ValueSet{
		"displayName":       info.DisplayName,
		"appVersion":        info.AppVersion,
		"installationId":    info.InstallationID,
		"contractVersion":   float64(ContractVersion),
		"correlationVector": correlationVector,
	}
}

func ConnectRequest(info LocalInfo, correlationVector string, keys []string, postTimes []int64) (app.ValueSet, error) {
	if err := validateReconcileArrays(keys, postTimes); err != nil {
		return nil, err
	}
	v := BaseValues(info, correlationVector)
	v["contentType"] = "connect"
	v["configuration"] = app.ValueSet{"AudioInfoSyncEnabled": false}
	v["attributes"] = app.ValueSet{
		"displayName":    info.DisplayName,
		"appVersion":     info.AppVersion,
		"installationId": info.InstallationID,
		"flightRing":     info.RingName,
	}
	v["sequenceNumbers"] = app.ValueSet{}
	addReconcile(v, keys, postTimes)
	return v, nil
}

func ReconcileRequest(info LocalInfo, correlationVector string, keys []string, postTimes []int64) (app.ValueSet, error) {
	if err := validateReconcileArrays(keys, postTimes); err != nil {
		return nil, err
	}
	v := BaseValues(info, correlationVector)
	v["contentType"] = ContentType
	addReconcile(v, keys, postTimes)
	return v, nil
}

func DismissRequest(info LocalInfo, correlationVector, key string) (app.ValueSet, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty key", ErrMalformed)
	}
	v := BaseValues(info, correlationVector)
	v["contentType"], v["operation"], v["key"] = ContentType, OperationRemove, key
	return v, nil
}

func ClearRequest(info LocalInfo, correlationVector string, keys []string) (app.ValueSet, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: clear requires keys", ErrMalformed)
	}
	if len(keys) > maxBatchEntries {
		return nil, fmt.Errorf("%w: too many keys", ErrMalformed)
	}
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("%w: empty key", ErrMalformed)
		}
	}
	v := BaseValues(info, correlationVector)
	v["contentType"], v["operation"], v["notificationKeysCount"], v["notificationKeys"] = ContentType, OperationClear, int32(len(keys)), append([]string(nil), keys...)
	return v, nil
}

func ActionRequest(info LocalInfo, correlationVector, key string, index int32, reply *string) (app.ValueSet, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty key", ErrMalformed)
	}
	if index < -1 {
		return nil, fmt.Errorf("%w: invalid action index", ErrMalformed)
	}
	if reply != nil && *reply == "" {
		return nil, fmt.Errorf("%w: empty reply", ErrMalformed)
	}
	v := BaseValues(info, correlationVector)
	v["contentType"], v["operation"], v["key"] = ContentType, OperationLaunch, key
	if index >= 0 {
		v["actionIndex"] = index
	}
	if reply != nil {
		v["inlineReplyMessage"] = *reply
	}
	return v, nil
}

func validateReconcileArrays(keys []string, postTimes []int64) error {
	if len(keys) != len(postTimes) {
		return fmt.Errorf("%w: keys/postTimes lengths", ErrMalformed)
	}
	if len(keys) > maxBatchEntries {
		return fmt.Errorf("%w: too many keys", ErrMalformed)
	}
	for _, key := range keys {
		if len(key) > maxBatchString {
			return fmt.Errorf("%w: key too large", ErrMalformed)
		}
	}
	return nil
}

func addReconcile(v app.ValueSet, keys []string, postTimes []int64) {
	v["operation"] = OperationExisting
	v["notificationKeysCount"] = int32(len(keys))
	v["notificationKeys"] = append([]string(nil), keys...)
	v["postTimes"] = append([]int64(nil), postTimes...)
}
