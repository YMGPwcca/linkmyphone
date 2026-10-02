package clipboard

import (
	"encoding/json"
	"testing"
)

func TestConfigRejectsUnknownAndOutOfRangeFields(t *testing.T) {
	for _, raw := range []string{
		`{"unknown":true}`,
		`{"poll_interval_ms":1}`,
		`{"request_timeout_ms":1}`,
		`null`,
		`[]`,
	} {
		if _, err := DecodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("expected config error for %s", raw)
		}
	}
}
