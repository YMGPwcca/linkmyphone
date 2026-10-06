package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type Config struct {
	RequestTimeoutMS int  `json:"request_timeout_ms"`
	RemoteActions    bool `json:"remote_actions"`
	ShowExisting     bool `json:"show_existing"`
}

func DefaultConfig() Config { return Config{RequestTimeoutMS: 10000, RemoteActions: true} }
func DecodeConfig(raw json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return cfg, nil
	}
	if trimmed[0] != '{' {
		return Config{}, errors.New("notifications module: config must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("notifications module: decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("notifications module: trailing JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return Config{}, fmt.Errorf("notifications module: decode config fields: %w", err)
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Config{}, fmt.Errorf("notifications module: config field %s must not be null", name)
		}
	}
	if cfg.RequestTimeoutMS < 100 || cfg.RequestTimeoutMS > 120000 {
		return Config{}, errors.New("notifications module: request_timeout_ms must be between 100 and 120000")
	}
	return cfg, nil
}
func (c Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutMS) * time.Millisecond
}
