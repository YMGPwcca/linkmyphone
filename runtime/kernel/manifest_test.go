package kernel

import (
	"strings"
	"testing"
)

const validManifestJSON = `{
  "schema_version": "1.0",
  "id": "linkmyphone.test",
  "version": "1.2.3",
  "kind": "builtin",
  "runtime": {"api_version": "1.0"},
  "capabilities": {
    "potential": [
      {"id": "test.echo", "contract_version": "1.0.0"}
    ]
  },
  "dependencies": {"required": [], "optional": []},
  "permissions": {"requested": ["ipc.local"]},
  "configuration_schema": "config.schema.json",
  "metadata": {
    "display_name": "Test",
    "description": "Test module",
    "diagnostic_label": "linkmyphone.test"
  }
}`

func TestParseManifestJSON(t *testing.T) {
	manifest, err := ParseManifestJSON([]byte(validManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "linkmyphone.test" || !manifest.DeclaresCapability("test.echo", "1.0.0") {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestParseManifestRejectsUnknownField(t *testing.T) {
	source := strings.Replace(validManifestJSON, `"schema_version": "1.0",`, `"schema_version": "1.0", "typo": true,`, 1)
	if _, err := ParseManifestJSON([]byte(source)); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestManifestRejectsDuplicateCapability(t *testing.T) {
	source := strings.Replace(
		validManifestJSON,
		`{"id": "test.echo", "contract_version": "1.0.0"}`,
		`{"id": "test.echo", "contract_version": "1.0.0"}, {"id": "test.echo", "contract_version": "1.0.0"}`,
		1,
	)
	if _, err := ParseManifestJSON([]byte(source)); err == nil {
		t.Fatal("expected duplicate capability error")
	}
}

func TestManifestRejectsUnsafeConfigurationSchema(t *testing.T) {
	source := strings.Replace(validManifestJSON, "config.schema.json", "../config.schema.json", 1)
	if _, err := ParseManifestJSON([]byte(source)); err == nil {
		t.Fatal("expected unsafe path error")
	}
}
