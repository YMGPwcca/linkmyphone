package clipboard

import (
	_ "embed"
	"sync"

	"github.com/YMGPwcca/phonelink-linux/runtime/kernel"
)

//go:embed manifest.json
var manifestJSON []byte

var (
	manifestOnce sync.Once
	manifestValue kernel.Manifest
	manifestErr error
)

func Manifest() (kernel.Manifest, error) {
	manifestOnce.Do(func() {
		manifestValue, manifestErr = kernel.ParseManifestJSON(manifestJSON)
	})
	return manifestValue, manifestErr
}
