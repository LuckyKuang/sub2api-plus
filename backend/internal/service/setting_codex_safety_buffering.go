package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const SettingKeyOpenAICodexSafetyBufferingSuppressed = "openai_codex_safety_buffering_suppressed"

const SettingKeyOpenAIErrorMessagesNormalized = "openai_error_messages_normalized"

// SettingKeyOpenAIErrorMessageOverrides stores only the keys an administrator
// explicitly overrode. A missing value means an empty object.
const SettingKeyOpenAIErrorMessageOverrides = "openai_error_message_overrides"

const codexSafetyBufferingRefreshInterval = time.Second

// Reads on the output path are atomic. The mutex orders database refreshes
// against successful writes so an older refresh cannot overwrite a new save.
type codexSafetyBufferingRuntime struct {
	mu      sync.Mutex
	enabled atomic.Bool
	// publicErrors stores one immutable codexPublicErrorSettings. Mode and
	// overrides are always replaced together so readers cannot observe fields
	// from two revisions.
	publicErrors atomic.Value
	initialized  atomic.Bool
	start        sync.Once
	bus          InvalidationBus
	cancel       context.CancelFunc
	workers      sync.WaitGroup
}

func defaultCodexPublicErrorSettings() codexPublicErrorSettings {
	return codexPublicErrorSettings{
		overrides: map[string]string{},
		revision:  codexPublicErrorRevision(false, nil),
	}
}

func (s *SettingService) CodexSafetyBufferingSuppressed(ctx context.Context) bool {
	if s == nil {
		return false
	}
	if !s.codexSafetyBuffering.initialized.Load() {
		s.refreshCodexSafetyBuffering(ctx)
	}
	return s.codexSafetyBuffering.enabled.Load()
}

func (s *SettingService) OpenAIErrorMessagesNormalized(ctx context.Context) bool {
	if s == nil {
		return false
	}
	return s.codexPublicErrorSettings(ctx).customEnabled
}

func (s *SettingService) refreshCodexSafetyBuffering(ctx context.Context) {
	if s == nil {
		return
	}
	runtime := &s.codexSafetyBuffering
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	// A failed first read uses the zero-value default, while subsequent failures
	// retain the last valid value. The background refresher retries either case.
	defer runtime.initialized.Store(true)
	if s.settingRepo == nil {
		return
	}
	next, ok := s.readCodexPublicErrorSettingsLocked(ctx)
	if !ok {
		return
	}
	values := next.values
	if raw := values[SettingKeyOpenAICodexSafetyBufferingSuppressed]; raw != "" && raw != "true" && raw != "false" {
		slog.Warn("invalid OpenAI output setting", "stage", "settings_refresh", "code", "codex_safety_buffering_setting_invalid", "setting", SettingKeyOpenAICodexSafetyBufferingSuppressed)
	} else {
		runtime.enabled.Store(raw == "true")
	}
	runtime.publicErrors.Store(next.settings)
}

type codexPublicErrorReadResult struct {
	settings codexPublicErrorSettings
	values   map[string]string
}

// readCodexPublicErrorSettingsLocked reads mode and overrides as one snapshot.
// Malformed storage retains the last complete valid snapshot instead of mixing
// a new mode with old overrides.
func (s *SettingService) readCodexPublicErrorSettingsLocked(ctx context.Context) (codexPublicErrorReadResult, bool) {
	if s == nil || s.settingRepo == nil {
		return codexPublicErrorReadResult{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(readCtx, []string{SettingKeyOpenAICodexSafetyBufferingSuppressed, SettingKeyOpenAIErrorMessagesNormalized, SettingKeyOpenAIErrorMessageOverrides})
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("codex safety buffering setting refresh failed", "stage", "settings_refresh", "code", "codex_safety_buffering_setting_read_failed")
		}
		return codexPublicErrorReadResult{}, false
	}
	rawMode := values[SettingKeyOpenAIErrorMessagesNormalized]
	if rawMode != "" && rawMode != "true" && rawMode != "false" {
		slog.Warn("invalid OpenAI output setting", "stage", "settings_refresh", "code", "openai_error_message_setting_invalid", "setting", SettingKeyOpenAIErrorMessagesNormalized)
		return codexPublicErrorReadResult{}, false
	}
	settings, err := codexPublicErrorSettingsFromValues(values)
	if err != nil {
		slog.Warn("invalid OpenAI error message overrides", "stage", "settings_refresh", "code", "openai_error_message_overrides_invalid")
		return codexPublicErrorReadResult{}, false
	}
	return codexPublicErrorReadResult{settings: settings, values: values}, true
}

// StartCodexSafetyBufferingRuntime reuses the host invalidation bus. Periodic
// reconciliation also repairs missed notifications on already-running streams.
// The shared channel refreshes both downstream display settings.
func (s *SettingService) StartCodexSafetyBufferingRuntime(ctx context.Context, bus InvalidationBus) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.codexSafetyBuffering.start.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		workers := 1
		if bus != nil {
			workers++
		}
		s.codexSafetyBuffering.mu.Lock()
		s.codexSafetyBuffering.workers.Add(workers)
		s.codexSafetyBuffering.bus = bus
		s.codexSafetyBuffering.cancel = cancel
		s.codexSafetyBuffering.mu.Unlock()
		s.refreshCodexSafetyBuffering(ctx)
		go func() {
			defer s.codexSafetyBuffering.workers.Done()
			ticker := time.NewTicker(codexSafetyBufferingRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.refreshCodexSafetyBuffering(ctx)
				}
			}
		}()
		if bus != nil {
			go func() {
				defer s.codexSafetyBuffering.workers.Done()
				for ctx.Err() == nil {
					subscribeErr := bus.Subscribe(ctx, SettingKeyOpenAICodexSafetyBufferingSuppressed, func() { s.refreshCodexSafetyBuffering(ctx) })
					if ctx.Err() != nil {
						return
					}
					slog.Warn("codex safety buffering setting subscriber stopped", "stage", "settings_subscribe", "code", "codex_safety_buffering_setting_subscribe_failed", "unexpected_close", subscribeErr == nil)
					timer := time.NewTimer(time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}()
		}
	})
}

func (s *SettingService) StopCodexSafetyBufferingRuntime() {
	if s == nil {
		return
	}
	s.codexSafetyBuffering.mu.Lock()
	cancel := s.codexSafetyBuffering.cancel
	s.codexSafetyBuffering.mu.Unlock()
	if cancel != nil {
		cancel()
		s.codexSafetyBuffering.workers.Wait()
	}
}

func (s *SettingService) persistSystemSettingsWithCodexSafetyBuffering(ctx context.Context, settings *SystemSettings, updates map[string]string) error {
	runtime := &s.codexSafetyBuffering
	runtime.mu.Lock()
	err := s.persistSystemSettingsUpdates(ctx, settings, updates)
	changed := false
	refreshPublicErrors := false
	if err == nil {
		current, _ := runtime.publicErrors.Load().(codexPublicErrorSettings)
		if raw, ok := updates[SettingKeyOpenAICodexSafetyBufferingSuppressed]; ok {
			changed = true
			if raw == "" || raw == "true" || raw == "false" {
				runtime.enabled.Store(raw == "true")
			}
		}
		if raw, ok := updates[SettingKeyOpenAIErrorMessagesNormalized]; ok {
			changed = true
			// The generic form always submits the legacy boolean. Only an actual
			// mode change needs the full re-read; the dedicated UI omits it.
			if (raw == "true") != current.customEnabled {
				refreshPublicErrors = true
			}
		}
		if _, ok := updates[SettingKeyOpenAIErrorMessageOverrides]; ok {
			changed = true
			refreshPublicErrors = true
		}
		if refreshPublicErrors {
			// The generic settings form shares the same runtime snapshot as the
			// dedicated endpoint. Re-read all fields together so a legacy mode
			// write cannot pair with stale overrides.
			if !s.storeCodexPublicErrorSnapshotLocked(ctx) {
				slog.Warn("codex safety buffering runtime refresh after write failed", "stage", "settings_publish", "code", "codex_safety_buffering_setting_refresh_failed")
			}
		}
	}
	bus := runtime.bus
	runtime.mu.Unlock()
	if err == nil && changed && bus != nil {
		publishCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if publishErr := bus.Publish(publishCtx, SettingKeyOpenAICodexSafetyBufferingSuppressed); publishErr != nil {
			slog.Warn("codex safety buffering setting broadcast failed", "stage", "settings_publish", "code", "codex_safety_buffering_setting_publish_failed")
		}
	}
	return err
}
