package clipboard

import (
	"encoding/json"
	"testing"
)

func TestManifestAndDefaultConfig(t *testing.T) {
	manifest, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "phonelink.clipboard" {
		t.Fatalf("manifest id=%q", manifest.ID)
	}
	for _, capability := range []string{
		"clipboard.text.read",
		"clipboard.text.write",
		"clipboard.text.bidirectional",
	} {
		if !manifest.DeclaresCapability(capability, "1.0.0") {
			t.Fatalf("missing capability %s", capability)
		}
	}

	raw, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollIntervalMS != 500 || cfg.RequestTimeoutMS != 10000 || cfg.PublishInitial {
		t.Fatalf("config=%#v", cfg)
	}
}

func TestConfigRejectsUnknownAndOutOfRangeFields(t *testing.T) {
	for _, raw := range []string{
		`{"unknown":true}`,
		`{"poll_interval_ms":1}`,
		`{"request_timeout_ms":1}`,
	} {
		if _, err := DecodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("expected config error for %s", raw)
		}
	}
}
