package kernel

import (
	"encoding/json"
	"errors"
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
		ID:      "phonelink.clipboard",
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

func TestFeatureStoreRejectsNonObjectConfig(t *testing.T) {
	store, err := OpenFeatureStore(filepath.Join(t.TempDir(), "features.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Create(FeatureRecord{
		ID:      "phonelink.clipboard",
		Enabled: true,
		Config:  json.RawMessage(`["bad"]`),
	})
	if err == nil {
		t.Fatal("expected config validation error")
	}
}
