package kernel

import (
	"encoding/json"
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

func TestManifestRequiresSchemaFields(t *testing.T) {
	requirements := map[string][]string{
		"":             {"schema_version", "id", "version", "kind", "runtime", "capabilities", "dependencies", "permissions", "configuration_schema", "metadata"},
		"runtime":      {"api_version"},
		"capabilities": {"potential"},
		"dependencies": {"required", "optional"},
		"permissions":  {"requested"},
		"metadata":     {"display_name", "description", "diagnostic_label"},
	}
	for section, names := range requirements {
		for _, name := range names {
			for _, null := range []bool{false, true} {
				if section == "" && name == "configuration_schema" && null {
					continue
				}
				label := section + "." + name + " omitted"
				if null {
					label = section + "." + name + " null"
				}
				t.Run(label, func(t *testing.T) {
					var document map[string]any
					if err := json.Unmarshal([]byte(validManifestJSON), &document); err != nil {
						t.Fatal(err)
					}
					object := document
					if section != "" {
						object = document[section].(map[string]any)
					}
					if null {
						object[name] = nil
					} else {
						delete(object, name)
					}
					data, err := json.Marshal(document)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := ParseManifestJSON(data); err == nil {
						t.Fatalf("invalid manifest accepted: %s", data)
					}
				})
			}
		}
	}
}

func TestManifestAllowsEmptyDeclarationsAndNullConfigurationSchema(t *testing.T) {
	source := strings.Replace(validManifestJSON, `"configuration_schema": "config.schema.json"`, `"configuration_schema": null`, 1)
	source = strings.Replace(source, `{"id": "test.echo", "contract_version": "1.0.0"}`, "", 1)
	source = strings.Replace(source, `["ipc.local"]`, `[]`, 1)
	if _, err := ParseManifestJSON([]byte(source)); err != nil {
		t.Fatalf("valid empty declarations/null schema rejected: %v", err)
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
