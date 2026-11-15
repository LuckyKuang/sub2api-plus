package service

import (
	"context"
	"errors"
	"strings"
)

var errBatchImageSettingsUnavailable = errors.New("batch image settings are unavailable")

// settingKeyBatchImageEnabled stores the administrator batch-image submission
// switch. An absent value keeps the deployment's existing config.yaml or
// environment setting, so upgrading never opens or closes the feature by itself.
const settingKeyBatchImageEnabled = "batch_image_enabled"

// BatchImageAvailability explains why the batch mode is or is not usable. An
// empty Reason means the mode is enabled.
type BatchImageAvailability struct {
	Enabled bool
	Reason  string
}

// BatchImageSettingsService resolves the batch submission switch at request
// time. Batch submissions are infrequent, so the value is read from the setting
// store instead of cached; every instance therefore applies the same state
// immediately after an administrator saves it.
type BatchImageSettingsService struct {
	settingRepo   SettingRepository
	fallback      bool
	workerEnabled bool
}

func NewBatchImageSettingsService(settingRepo SettingRepository, fallback bool) *BatchImageSettingsService {
	return &BatchImageSettingsService{settingRepo: settingRepo, fallback: fallback, workerEnabled: true}
}

// SetWorkerEnabled records deployment readiness independently of the admin switch.
// It is called once during dependency wiring.
func (s *BatchImageSettingsService) SetWorkerEnabled(enabled bool) {
	if s != nil {
		s.workerEnabled = enabled
	}
}

// Enabled reports whether new batch jobs may be submitted. Worker processing,
// settlement, cleanup and downloads are not governed by this switch.
func (s *BatchImageSettingsService) Enabled(ctx context.Context) bool {
	return s.Availability(ctx).Enabled
}

// Availability returns the mode state for the console capability endpoint.
func (s *BatchImageSettingsService) Availability(ctx context.Context) BatchImageAvailability {
	if s == nil {
		return BatchImageAvailability{Reason: "disabled"}
	}
	enabled, ok, err := s.stored(ctx)
	if err != nil {
		return BatchImageAvailability{Reason: "dependency_unavailable"}
	}
	if !ok {
		enabled = s.fallback
	}
	if enabled {
		if !s.workerEnabled {
			return BatchImageAvailability{Enabled: true, Reason: "worker_disabled"}
		}
		return BatchImageAvailability{Enabled: true}
	}
	return BatchImageAvailability{Enabled: false, Reason: "disabled"}
}

// SetEnabled persists the switch and applies it to this instance immediately.
func (s *BatchImageSettingsService) SetEnabled(ctx context.Context, enabled bool) error {
	if s == nil || s.settingRepo == nil {
		return errBatchImageSettingsUnavailable
	}
	return s.settingRepo.Set(ctx, settingKeyBatchImageEnabled, boolSettingValue(enabled))
}

func (s *BatchImageSettingsService) stored(ctx context.Context) (bool, bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, false, errBatchImageSettingsUnavailable
	}
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBatchImageEnabled)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false, nil
	}
	return raw == "true" || raw == "1", true, nil
}

func boolSettingValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
