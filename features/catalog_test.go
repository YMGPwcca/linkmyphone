package features

import (
	"encoding/json"
	"testing"
)

func TestCatalogContainsValidatedClipboardModule(t *testing.T) {
	definitions, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 {
		t.Fatalf("definitions=%d", len(definitions))
	}
	definition := definitions[0]
	if definition.Manifest.ID != "phonelink.clipboard" {
		t.Fatalf("id=%q", definition.Manifest.ID)
	}
	if err := definition.Manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := definition.Validate(definition.DefaultConfig); err != nil {
		t.Fatal(err)
	}

	var config map[string]any
	if err := json.Unmarshal(definition.DefaultConfig, &config); err != nil {
		t.Fatal(err)
	}
	if _, ok := config["poll_interval_ms"]; !ok {
		t.Fatalf("default config=%v", config)
	}
}

func TestFindRejectsUnknownFeature(t *testing.T) {
	if _, err := Find("phonelink.missing"); err == nil {
		t.Fatal("expected unknown feature error")
	}
}
