package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ManifestSchemaVersion = "1.0"
	RuntimeAPIVersion      = "1.0"

	maxCapabilities = 128
	maxDependencies = 64
	maxPermissions  = 64
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$`)
	semverPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	apiVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

type CapabilityDeclaration struct {
	ID              string `json:"id"`
	ContractVersion string `json:"contract_version"`
}

type Dependency struct {
	ID             string `json:"id"`
	MinimumVersion string `json:"minimum_version"`
}

type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Kind          string `json:"kind"`

	Runtime struct {
		APIVersion string `json:"api_version"`
	} `json:"runtime"`

	Capabilities struct {
		Potential []CapabilityDeclaration `json:"potential"`
	} `json:"capabilities"`

	Dependencies struct {
		Required []Dependency `json:"required"`
		Optional []Dependency `json:"optional"`
	} `json:"dependencies"`

	Permissions struct {
		Requested []string `json:"requested"`
	} `json:"permissions"`

	ConfigurationSchema *string `json:"configuration_schema"`

	Metadata struct {
		DisplayName     string `json:"display_name"`
		Description     string `json:"description"`
		DiagnosticLabel string `json:"diagnostic_label"`
	} `json:"metadata"`
}

func ParseManifestJSON(source []byte) (Manifest, error) {
	var manifest Manifest
	if len(source) == 0 {
		return manifest, errors.New("kernel: empty module manifest")
	}
	if len(source) > 1<<20 {
		return manifest, errors.New("kernel: module manifest exceeds 1 MiB")
	}

	dec := json.NewDecoder(bytes.NewReader(source))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("kernel: decode module manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("kernel: module manifest contains trailing JSON")
		}
		return Manifest{}, fmt.Errorf("kernel: decode module manifest trailer: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	switch {
	case m.SchemaVersion != ManifestSchemaVersion:
		return fmt.Errorf("kernel: unsupported manifest schema %q", m.SchemaVersion)
	case !validIdentifier(m.ID):
		return fmt.Errorf("kernel: invalid module id %q", m.ID)
	case !semverPattern.MatchString(m.Version):
		return fmt.Errorf("kernel: invalid module version %q", m.Version)
	case m.Kind != "builtin":
		return fmt.Errorf("kernel: unsupported module kind %q", m.Kind)
	case !apiVersionPattern.MatchString(m.Runtime.APIVersion):
		return fmt.Errorf("kernel: invalid runtime API version %q", m.Runtime.APIVersion)
	case !apiCompatible(RuntimeAPIVersion, m.Runtime.APIVersion):
		return fmt.Errorf("kernel: module %s requires incompatible runtime API %s", m.ID, m.Runtime.APIVersion)
	}

	if len(m.Capabilities.Potential) > maxCapabilities {
		return fmt.Errorf("kernel: module %s declares too many capabilities", m.ID)
	}
	if len(m.Dependencies.Required)+len(m.Dependencies.Optional) > maxDependencies {
		return fmt.Errorf("kernel: module %s declares too many dependencies", m.ID)
	}
	if len(m.Permissions.Requested) > maxPermissions {
		return fmt.Errorf("kernel: module %s requests too many permissions", m.ID)
	}

	capabilities := make(map[string]struct{}, len(m.Capabilities.Potential))
	for _, capability := range m.Capabilities.Potential {
		if !validIdentifier(capability.ID) || !semverPattern.MatchString(capability.ContractVersion) {
			return fmt.Errorf("kernel: module %s has invalid capability declaration", m.ID)
		}
		if _, exists := capabilities[capability.ID]; exists {
			return fmt.Errorf("kernel: module %s declares duplicate capability %s", m.ID, capability.ID)
		}
		capabilities[capability.ID] = struct{}{}
	}

	dependencies := make(map[string]struct{}, len(m.Dependencies.Required)+len(m.Dependencies.Optional))
	for _, dependency := range append(append([]Dependency(nil), m.Dependencies.Required...), m.Dependencies.Optional...) {
		if !validIdentifier(dependency.ID) || !semverPattern.MatchString(dependency.MinimumVersion) {
			return fmt.Errorf("kernel: module %s has invalid dependency", m.ID)
		}
		if dependency.ID == m.ID {
			return fmt.Errorf("kernel: module %s cannot depend on itself", m.ID)
		}
		if _, exists := dependencies[dependency.ID]; exists {
			return fmt.Errorf("kernel: module %s declares duplicate dependency %s", m.ID, dependency.ID)
		}
		dependencies[dependency.ID] = struct{}{}
	}

	permissions := make(map[string]struct{}, len(m.Permissions.Requested))
	for _, permission := range m.Permissions.Requested {
		if !validIdentifier(permission) {
			return fmt.Errorf("kernel: module %s has invalid permission %q", m.ID, permission)
		}
		if _, exists := permissions[permission]; exists {
			return fmt.Errorf("kernel: module %s declares duplicate permission %s", m.ID, permission)
		}
		permissions[permission] = struct{}{}
	}

	if m.ConfigurationSchema != nil && !safeRelativePath(*m.ConfigurationSchema) {
		return fmt.Errorf("kernel: module %s has unsafe configuration schema path", m.ID)
	}
	if strings.TrimSpace(m.Metadata.DisplayName) == "" ||
		strings.TrimSpace(m.Metadata.Description) == "" ||
		strings.TrimSpace(m.Metadata.DiagnosticLabel) == "" {
		return fmt.Errorf("kernel: module %s metadata is incomplete", m.ID)
	}
	return nil
}

func (m Manifest) DeclaresCapability(id, contractVersion string) bool {
	for _, capability := range m.Capabilities.Potential {
		if capability.ID == id && capability.ContractVersion == contractVersion {
			return true
		}
	}
	return false
}

func validIdentifier(value string) bool {
	return len(value) <= 128 && identifierPattern.MatchString(value)
}

func safeRelativePath(value string) bool {
	if value == "" || len(value) > 260 || filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func apiCompatible(host, required string) bool {
	var hostMajor, hostMinor, requiredMajor, requiredMinor int
	if _, err := fmt.Sscanf(host, "%d.%d", &hostMajor, &hostMinor); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(required, "%d.%d", &requiredMajor, &requiredMinor); err != nil {
		return false
	}
	return hostMajor == requiredMajor && hostMinor >= requiredMinor
}
