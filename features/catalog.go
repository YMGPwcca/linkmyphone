package features

import (
	"encoding/json"
	"fmt"
	"sort"

	clipboardfeature "github.com/YMGPwcca/linkmyphone/features/clipboard"
	"github.com/YMGPwcca/linkmyphone/runtime/kernel"
	"github.com/YMGPwcca/linkmyphone/runtime/phonehost"
)

type Definition struct {
	Manifest      kernel.Manifest
	DefaultConfig json.RawMessage
	Validate      func(json.RawMessage) error
	Build         func(*phonehost.Session) (kernel.Module, error)
}

func Catalog() ([]Definition, error) {
	clipboardManifest, err := clipboardfeature.Manifest()
	if err != nil {
		return nil, err
	}
	defaultClipboardConfig, err := json.Marshal(clipboardfeature.DefaultConfig())
	if err != nil {
		return nil, err
	}

	definitions := []Definition{
		{
			Manifest:      clipboardManifest,
			DefaultConfig: defaultClipboardConfig,
			Validate: func(raw json.RawMessage) error {
				_, err := clipboardfeature.DecodeConfig(raw)
				return err
			},
			Build: func(session *phonehost.Session) (kernel.Module, error) {
				return clipboardfeature.New(session)
			},
		},
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Manifest.ID < definitions[j].Manifest.ID
	})
	return definitions, nil
}

func Find(id string) (Definition, error) {
	definitions, err := Catalog()
	if err != nil {
		return Definition{}, err
	}
	for _, definition := range definitions {
		if definition.Manifest.ID == id {
			return definition, nil
		}
	}
	return Definition{}, fmt.Errorf("features: unknown module %q", id)
}
