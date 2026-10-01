package signalr

import (
	"errors"
	"fmt"
)

const (
	CompletionResultError  = 1
	CompletionResultVoid   = 2
	CompletionResultValue  = 3
)

type Completion struct {
	InvocationID string
	ResultKind   int
	Error        string
	Result       any
}

func ParseCompletion(body []byte) (Completion, error) {
	var out Completion
	mt, values, err := ParseHubMessage(body)
	if err != nil {
		return out, err
	}
	if mt != HubMessageTypeCompletion {
		return out, errors.New("signalr: not a Completion message")
	}
	if len(values) < 4 {
		return out, ErrMessagePack
	}
	invocationID, ok := values[2].(string)
	if !ok || invocationID == "" {
		return out, errors.New("signalr: Completion missing invocation id")
	}
	resultKind64, ok := asInt64(values[3])
	if !ok {
		return out, ErrMessagePack
	}
	out.InvocationID = invocationID
	out.ResultKind = int(resultKind64)

	switch out.ResultKind {
	case CompletionResultError:
		if len(values) != 5 {
			return Completion{}, ErrMessagePack
		}
		errorText, ok := values[4].(string)
		if !ok || errorText == "" {
			return Completion{}, ErrMessagePack
		}
		out.Error = errorText
	case CompletionResultVoid:
		if len(values) != 4 {
			return Completion{}, ErrMessagePack
		}
	case CompletionResultValue:
		if len(values) != 5 {
			return Completion{}, ErrMessagePack
		}
		out.Result = values[4]
	default:
		return Completion{}, fmt.Errorf("signalr: invalid Completion result kind %d", out.ResultKind)
	}
	return out, nil
}
