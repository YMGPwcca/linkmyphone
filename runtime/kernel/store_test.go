package kernel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFeatureStoreCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "features.json")
	store, err := OpenFeatureStore(path)
	if err != nil {
		t.Fatal(err)
	}

	record := FeatureRecord{
		ID:      "linkmyphone.clipboard",
		Enabled: true,
		Config:  json.RawMessage(`{"poll_interval_ms":500}`),
	}
	if err := store.Create(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(record); !errors.Is(err, ErrFeatureExists) {
		t.Fatalf("duplicate create err=%v", err)
	}

	got, err := store.Read(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || string(got.Config) != string(record.Config) {
		t.Fatalf("record=%#v", got)
	}

	disabled := false
	config := json.RawMessage(`{"poll_interval_ms":250}`)
	got, err = store.Update(record.ID, &disabled, &config)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || string(got.Config) != string(config) {
		t.Fatalf("updated=%#v", got)
	}

	reopened, err := OpenFeatureStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = reopened.Read(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || string(got.Config) != string(config) {
		t.Fatalf("reopened=%#v", got)
	}

	if err := reopened.Delete(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Read(record.ID); !errors.Is(err, ErrFeatureNotFound) {
		t.Fatalf("read deleted err=%v", err)
	}
}

func TestFeatureStoreEnforcesPrivatePermissions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new directory"
		if existing {
			name = "existing permissive directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "feature-state")
			path := filepath.Join(dir, "features.json")
			if existing {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"schema_version":1,"features":[]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			store, err := OpenFeatureStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Create(FeatureRecord{ID: "linkmyphone.clipboard", Config: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			for target, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
				info, err := os.Stat(target)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != want {
					t.Fatalf("%s: permissions=%o, want %o", target, info.Mode().Perm(), want)
				}
			}
		})
	}
}

func TestFeatureStoreFailedReplacementRollsBackAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "features.json")
	store, err := OpenFeatureStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// A directory appearing at the destination makes the final rename fail.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(FeatureRecord{ID: "linkmyphone.clipboard", Config: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("save unexpectedly replaced a directory")
	}
	if _, err := store.Read("linkmyphone.clipboard"); !errors.Is(err, ErrFeatureNotFound) {
		t.Fatalf("failed save left a record: %v", err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".features-*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("failed save left temporary files: %v, err=%v", temps, err)
	}
}

func TestFeatureStoreRejectsNonObjectConfig(t *testing.T) {
	store, err := OpenFeatureStore(filepath.Join(t.TempDir(), "features.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []json.RawMessage{
		json.RawMessage(`["bad"]`),
		json.RawMessage(`null`),
	} {
		err = store.Create(FeatureRecord{
			ID:      "linkmyphone.clipboard",
			Enabled: true,
			Config:  config,
		})
		if err == nil {
			t.Fatalf("expected config validation error for %s", config)
		}
	}
}
