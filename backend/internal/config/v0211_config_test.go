//go:build unit || !integration

package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAPIKeyCreateAndInflightReservationDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, APIKeyCreateConfig{MaxActivePerUser: 200, MaxPerUserPerHour: 60}, cfg.APIKeyCreate)
	require.Equal(t, InflightReservationConfig{
		Enabled: true, TTLSeconds: 900, DefaultMaxTokens: 8192,
		MaxOutputTokens: 128000, MaxInputTokens: 200000,
		MaxReservationUSD: 0, FailClosedOnUnpriced: false,
	}, cfg.Billing.InflightReservation)
}

func TestLoadAPIKeyCreateAndInflightReservationEnvironmentOverridesYAML(t *testing.T) {
	resetViperWithJWTSecret(t)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(`api_key_create:
  max_active_per_user: 9
  max_per_user_per_hour: 8
billing:
  inflight_reservation:
    enabled: false
    ttl_seconds: 90
    default_max_tokens: 100
    max_output_tokens: 200
    max_input_tokens: 300
    max_reservation_usd: 1.5
    fail_closed_on_unpriced: false
`), 0o600))
	t.Setenv("CONFIG_FILE", configFile)
	// First confirm these values survive YAML loading rather than reverting to defaults.
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, APIKeyCreateConfig{MaxActivePerUser: 9, MaxPerUserPerHour: 8}, cfg.APIKeyCreate)
	require.Equal(t, InflightReservationConfig{
		Enabled: false, TTLSeconds: 90, DefaultMaxTokens: 100,
		MaxOutputTokens: 200, MaxInputTokens: 300,
		MaxReservationUSD: 1.5, FailClosedOnUnpriced: false,
	}, cfg.Billing.InflightReservation)

	for _, override := range []struct{ key, value string }{
		{"API_KEY_CREATE_MAX_ACTIVE_PER_USER", "25"},
		{"API_KEY_CREATE_MAX_PER_USER_PER_HOUR", "12"},
		{"BILLING_INFLIGHT_RESERVATION_ENABLED", "true"},
		{"BILLING_INFLIGHT_RESERVATION_TTL_SECONDS", "1200"},
		{"BILLING_INFLIGHT_RESERVATION_DEFAULT_MAX_TOKENS", "4096"},
		{"BILLING_INFLIGHT_RESERVATION_MAX_OUTPUT_TOKENS", "64000"},
		{"BILLING_INFLIGHT_RESERVATION_MAX_INPUT_TOKENS", "100000"},
		{"BILLING_INFLIGHT_RESERVATION_MAX_RESERVATION_USD", "2.75"},
		{"BILLING_INFLIGHT_RESERVATION_FAIL_CLOSED_ON_UNPRICED", "true"},
	} {
		t.Setenv(override.key, override.value)
	}
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, APIKeyCreateConfig{MaxActivePerUser: 25, MaxPerUserPerHour: 12}, cfg.APIKeyCreate)
	require.Equal(t, InflightReservationConfig{
		Enabled: true, TTLSeconds: 1200, DefaultMaxTokens: 4096,
		MaxOutputTokens: 64000, MaxInputTokens: 100000,
		MaxReservationUSD: 2.75, FailClosedOnUnpriced: true,
	}, cfg.Billing.InflightReservation)
}

func TestLoadAPIKeyCreateZeroDisablesLimitsAndInflightReservationFalseIsPreserved(t *testing.T) {
	resetViperWithJWTSecret(t)
	for _, key := range []string{
		"API_KEY_CREATE_MAX_ACTIVE_PER_USER",
		"API_KEY_CREATE_MAX_PER_USER_PER_HOUR",
		"BILLING_INFLIGHT_RESERVATION_TTL_SECONDS",
		"BILLING_INFLIGHT_RESERVATION_DEFAULT_MAX_TOKENS",
		"BILLING_INFLIGHT_RESERVATION_MAX_OUTPUT_TOKENS",
		"BILLING_INFLIGHT_RESERVATION_MAX_INPUT_TOKENS",
		"BILLING_INFLIGHT_RESERVATION_MAX_RESERVATION_USD",
	} {
		t.Setenv(key, "0")
	}
	t.Setenv("BILLING_INFLIGHT_RESERVATION_ENABLED", "false")
	t.Setenv("BILLING_INFLIGHT_RESERVATION_FAIL_CLOSED_ON_UNPRICED", "false")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, APIKeyCreateConfig{}, cfg.APIKeyCreate)
	require.Equal(t, InflightReservationConfig{}, cfg.Billing.InflightReservation)
	require.NoError(t, cfg.Validate())
}

func TestValidateAPIKeyCreateAndInflightReservationRejectNegativeValues(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"active keys", func(c *Config) { c.APIKeyCreate.MaxActivePerUser = -1 }, "api_key_create.max_active_per_user"},
		{"hourly creations", func(c *Config) { c.APIKeyCreate.MaxPerUserPerHour = -1 }, "api_key_create.max_per_user_per_hour"},
		{"reservation ttl", func(c *Config) { c.Billing.InflightReservation.TTLSeconds = -1 }, "billing.inflight_reservation"},
		{"default output", func(c *Config) { c.Billing.InflightReservation.DefaultMaxTokens = -1 }, "billing.inflight_reservation"},
		{"max output", func(c *Config) { c.Billing.InflightReservation.MaxOutputTokens = -1 }, "billing.inflight_reservation"},
		{"max input", func(c *Config) { c.Billing.InflightReservation.MaxInputTokens = -1 }, "billing.inflight_reservation"},
		{"max amount", func(c *Config) { c.Billing.InflightReservation.MaxReservationUSD = -0.01 }, "billing.inflight_reservation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			cfg, err := Load()
			require.NoError(t, err)
			tc.change(cfg)
			require.ErrorContains(t, cfg.Validate(), tc.want)
		})
	}
}

func TestValidateInflightReservationRejectsNonFiniteAmountsEvenWhenDisabled(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{"nan", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				resetViperWithJWTSecret(t)
				cfg, err := Load()
				require.NoError(t, err)
				cfg.Billing.InflightReservation.Enabled = enabled
				cfg.Billing.InflightReservation.MaxReservationUSD = tc.value
				require.ErrorContains(t, cfg.Validate(), "billing.inflight_reservation")
			}
		})
	}
}
