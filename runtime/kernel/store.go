package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const featureStoreSchemaVersion = 1

var (
	ErrFeatureExists   = errors.New("kernel: feature already exists")
	ErrFeatureNotFound = errors.New("kernel: feature not found")
)

type FeatureRecord struct {
	ID      string          `json:"id"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
}

type featureStoreFile struct {
	SchemaVersion int             `json:"schema_version"`
	Features      []FeatureRecord `json:"features"`
}

type FeatureStore struct {
	mu      sync.RWMutex
	path    string
	records map[string]FeatureRecord
}

func DefaultFeatureStorePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "linkmyphone", "features.json"), nil
}

func OpenFeatureStore(path string) (*FeatureStore, error) {
	if path == "" {
		return nil, errors.New("kernel: feature store path is required")
	}
	store := &FeatureStore{
		path:    path,
		records: make(map[string]FeatureRecord),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kernel: read feature store: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file featureStoreFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("kernel: decode feature store: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("kernel: feature store contains trailing JSON")
		}
		return nil, fmt.Errorf("kernel: decode feature store trailer: %w", err)
	}
	if file.SchemaVersion != featureStoreSchemaVersion {
		return nil, fmt.Errorf("kernel: unsupported feature store schema %d", file.SchemaVersion)
	}
	for _, record := range file.Features {
		if err := validateFeatureRecord(record); err != nil {
			return nil, err
		}
		if _, exists := store.records[record.ID]; exists {
			return nil, fmt.Errorf("kernel: duplicate feature record %s", record.ID)
		}
		store.records[record.ID] = cloneFeatureRecord(record)
	}
	return store, nil
}

func (s *FeatureStore) Path() string {
	return s.path
}

func (s *FeatureStore) Create(record FeatureRecord) error {
	if err := validateFeatureRecord(record); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[record.ID]; exists {
		return fmt.Errorf("%w: %s", ErrFeatureExists, record.ID)
	}
	s.records[record.ID] = cloneFeatureRecord(record)
	if err := s.saveLocked(); err != nil {
		delete(s.records, record.ID)
		return err
	}
	return nil
}

func (s *FeatureStore) Read(id string) (FeatureRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, exists := s.records[id]
	if !exists {
		return FeatureRecord{}, fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	return cloneFeatureRecord(record), nil
}

func (s *FeatureStore) List() []FeatureRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *FeatureStore) listLocked() []FeatureRecord {
	records := make([]FeatureRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, cloneFeatureRecord(record))
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})
	return records
}

func (s *FeatureStore) Update(id string, enabled *bool, config *json.RawMessage) (FeatureRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[id]
	if !exists {
		return FeatureRecord{}, fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	before := cloneFeatureRecord(record)
	if enabled != nil {
		record.Enabled = *enabled
	}
	if config != nil {
		record.Config = cloneRawJSON(*config)
	}
	if err := validateFeatureRecord(record); err != nil {
		return FeatureRecord{}, err
	}
	s.records[id] = record
	if err := s.saveLocked(); err != nil {
		s.records[id] = before
		return FeatureRecord{}, err
	}
	return cloneFeatureRecord(record), nil
}

func (s *FeatureStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrFeatureNotFound, id)
	}
	delete(s.records, id)
	if err := s.saveLocked(); err != nil {
		s.records[id] = record
		return err
	}
	return nil
}

func (s *FeatureStore) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("kernel: create feature store directory: %w", err)
	}

	file := featureStoreFile{
		SchemaVersion: featureStoreSchemaVersion,
		Features:      s.listLocked(),
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("kernel: encode feature store: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".features-*.tmp")
	if err != nil {
		return fmt.Errorf("kernel: create feature store temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("kernel: chmod feature store temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("kernel: write feature store temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("kernel: sync feature store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("kernel: close feature store temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("kernel: replace feature store: %w", err)
	}
	return nil
}

func validateFeatureRecord(record FeatureRecord) error {
	if !validIdentifier(record.ID) {
		return fmt.Errorf("kernel: invalid feature id %q", record.ID)
	}
	if len(record.Config) == 0 {
		record.Config = json.RawMessage("{}")
	}
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(record.Config))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("kernel: feature %s config must be a JSON object: %w", record.ID, err)
	}
	if value == nil {
		return fmt.Errorf("kernel: feature %s config must be a JSON object, not null", record.ID)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("kernel: feature %s config contains trailing JSON", record.ID)
	}
	return nil
}

func normalizeFeatureConfig(config json.RawMessage) json.RawMessage {
	if len(config) == 0 {
		return json.RawMessage("{}")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, config); err == nil {
		return append(json.RawMessage(nil), compact.Bytes()...)
	}
	return cloneRawJSON(config)
}

func cloneFeatureRecord(record FeatureRecord) FeatureRecord {
	record.Config = normalizeFeatureConfig(record.Config)
	return record
}

func cloneRawJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
