package clipboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	defaultPollInterval   = 500 * time.Millisecond
	defaultRequestTimeout = 10 * time.Second
)

type Config struct {
	PollIntervalMS   int  `json:"poll_interval_ms,omitempty"`
	RequestTimeoutMS int  `json:"request_timeout_ms,omitempty"`
	PublishInitial   bool `json:"publish_initial,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		PollIntervalMS:   int(defaultPollInterval / time.Millisecond),
		RequestTimeoutMS: int(defaultRequestTimeout / time.Millisecond),
	}
}

func DecodeConfig(raw json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		return cfg, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return cfg, nil
	}
	if trimmed[0] != '{' {
		return Config{}, errors.New("clipboard module: config must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("clipboard module: decode config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("clipboard module: config contains trailing JSON")
		}
		return Config{}, fmt.Errorf("clipboard module: decode config trailer: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.PollIntervalMS < 50 || c.PollIntervalMS > 60000 {
		return errors.New("clipboard module: fallback poll_interval_ms must be between 50 and 60000")
	}
	if c.RequestTimeoutMS < 100 || c.RequestTimeoutMS > 120000 {
		return errors.New("clipboard module: request_timeout_ms must be between 100 and 120000")
	}
	return nil
}

func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalMS) * time.Millisecond
}

func (c Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutMS) * time.Millisecond
}
