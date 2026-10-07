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
		`{"poll_interval_ms":null}`,
		`{"request_timeout_ms":null}`,
		`{"publish_initial":null}`,
		`null`,
		`[]`,
	} {
		if _, err := DecodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("expected config error for %s", raw)
		}
	}
}

func TestConfigDefaultsAndExplicitFalse(t *testing.T) {
	for _, raw := range []string{`{}`, `{"publish_initial":false}`} {
		cfg, err := DecodeConfig(json.RawMessage(raw))
		if err != nil || cfg != DefaultConfig() {
			t.Fatalf("config %s: got %#v, err=%v", raw, cfg, err)
		}
	}
}
