package service

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// SettingKeyCodexCLIBridgeEnabled is deliberately opt-in. Installing the
// reviewed executable must never change the transport of existing accounts.
const SettingKeyCodexCLIBridgeEnabled = "openai_codex_cli_bridge_enabled"

var ErrCodexCLIBridgeSettingUnavailable = errors.New("codex CLI bridge setting is unavailable")

// GetCodexCLIBridgeEnabled reads the current permission without a process-local
// cache, so a successful disable applies to the next admission on every node.
// A missing setting is disabled; storage failures and corrupt values are errors
// and must not be interpreted as permission to use another transport.
func (s *SettingService) GetCodexCLIBridgeEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Protocol resolvers also call this with a background context. Bound those
	// reads while preserving any earlier caller deadline or cancellation.
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := s.settingRepo.GetValue(dbCtx, SettingKeyCodexCLIBridgeEnabled)
	if errors.Is(err, ErrSettingNotFound) {
		return false, nil
	}
	if err != nil {
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
}

// SetCodexCLIBridgeEnabled persists an administrator's explicit choice. The
// caller must verify the managed runtime before enabling; disabling needs no
// executable and preserves all per-account configuration and credentials.
func (s *SettingService) SetCodexCLIBridgeEnabled(ctx context.Context, enabled bool) error {
	if s == nil || s.settingRepo == nil {
		return ErrCodexCLIBridgeSettingUnavailable
	}
	if err := s.settingRepo.Set(ctx, SettingKeyCodexCLIBridgeEnabled, strconv.FormatBool(enabled)); err != nil {
		return ErrCodexCLIBridgeSettingUnavailable
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
