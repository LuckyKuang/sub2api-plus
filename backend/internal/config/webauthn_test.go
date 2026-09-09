package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadWebAuthnDisplayName(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "Sub2API Plus", cfg.WebAuthn.RPDisplayName)

	viper.Set("webauthn.rp_display_name", "My Gateway")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "My Gateway", cfg.WebAuthn.RPDisplayName)
}

func TestWebAuthnDisplayNameDeployExample(t *testing.T) {
	example := viper.New()
	example.SetConfigFile("../../../deploy/config.example.yaml")
	require.NoError(t, example.ReadInConfig())
	require.Equal(t, "Sub2API Plus", example.GetString("webauthn.rp_display_name"))
}

func TestValidateWebAuthnConfig(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "valid production origin",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API",
					RPID:          "sub2api.example.com",
					RPOrigins:     []string{"https://sub2api.example.com"},
				}
			},
		},
		{
			name: "valid localhost development origin",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API Dev",
					RPID:          "localhost",
					RPOrigins:     []string{"http://localhost:5173"},
				}
			},
		},
		{
			name: "missing relying party id",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API",
					RPOrigins:     []string{"https://sub2api.example.com"},
				}
			},
			wantError: "webauthn.rp_id",
		},
		{
			name: "relying party id contains scheme",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API",
					RPID:          "https://sub2api.example.com",
					RPOrigins:     []string{"https://sub2api.example.com"},
				}
			},
			wantError: "domain without scheme",
		},
		{
			name: "non-local insecure origin",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API",
					RPID:          "sub2api.example.com",
					RPOrigins:     []string{"http://sub2api.example.com"},
				}
			},
			wantError: "must use HTTPS",
		},
		{
			name: "origin outside relying party id",
			configure: func(cfg *Config) {
				cfg.WebAuthn = WebAuthnConfig{
					Enabled:       true,
					RPDisplayName: "Sub2API",
					RPID:          "example.com",
					RPOrigins:     []string{"https://example.net"},
				}
			},
			wantError: "not within relying party ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
			cfg, err := Load()
			require.NoError(t, err)
			tt.configure(cfg)

			err = cfg.Validate()
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}
