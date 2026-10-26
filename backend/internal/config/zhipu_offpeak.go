package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// ZhipuOffPeakConfig uses a dedicated externally managed keyring. JSON is a
// string so AutomaticEnv can bind it consistently across Viper sources.
type ZhipuOffPeakConfig struct {
	PersistenceEnabled     bool   `mapstructure:"persistence_enabled"`
	ActiveKeyID            string `mapstructure:"active_key_id"`
	SnapshotKeys           string `mapstructure:"snapshot_keys"`
	SnapshotRetentionHours int    `mapstructure:"snapshot_retention_hours"`
}

func (c ZhipuOffPeakConfig) Keyring() (map[string][]byte, error) {
	var encoded map[string]string
	if err := json.Unmarshal([]byte(c.SnapshotKeys), &encoded); err != nil || len(encoded) == 0 {
		return nil, errors.New("zhipu_offpeak.snapshot_keys must be a nonempty JSON keyring")
	}
	keys := make(map[string][]byte, len(encoded))
	for id, value := range encoded {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 64 || strings.ContainsAny(id, "\r\n\x00") {
			return nil, errors.New("zhipu_offpeak snapshot key ID is invalid")
		}
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return nil, errors.New("zhipu_offpeak snapshot keys must be base64-encoded 32-byte keys")
		}
		keys[id] = key
	}
	if _, ok := keys[c.ActiveKeyID]; !ok {
		return nil, errors.New("zhipu_offpeak.active_key_id must select a configured key")
	}
	return keys, nil
}

func (c ZhipuOffPeakConfig) validate() error {
	if !c.PersistenceEnabled {
		return nil
	}
	if c.SnapshotRetentionHours < 1 || c.SnapshotRetentionHours > 720 {
		return errors.New("zhipu_offpeak.snapshot_retention_hours must be between 1 and 720")
	}
	_, err := c.Keyring()
	return err
}
