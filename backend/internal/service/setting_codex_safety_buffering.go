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

const codexSafetyBufferingRefreshInterval = time.Second

// Reads on the output path are atomic. The mutex orders database refreshes
// against successful writes so an older refresh cannot overwrite a new save.
type codexSafetyBufferingRuntime struct {
	mu            sync.Mutex
	enabled       atomic.Bool
	errorMessages atomic.Bool
	initialized   atomic.Bool
	start         sync.Once
	bus           InvalidationBus
	cancel        context.CancelFunc
	workers       sync.WaitGroup
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
	if !s.codexSafetyBuffering.initialized.Load() {
		s.refreshCodexSafetyBuffering(ctx)
	}
	return s.codexSafetyBuffering.errorMessages.Load()
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
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(readCtx, []string{SettingKeyOpenAICodexSafetyBufferingSuppressed, SettingKeyOpenAIErrorMessagesNormalized})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Warn("codex safety buffering setting refresh failed", "stage", "settings_refresh", "code", "codex_safety_buffering_setting_read_failed")
		return
	}
	for key, target := range map[string]*atomic.Bool{
		SettingKeyOpenAICodexSafetyBufferingSuppressed: &runtime.enabled,
		SettingKeyOpenAIErrorMessagesNormalized:        &runtime.errorMessages,
	} {
		raw := values[key]
		if raw != "" && raw != "true" && raw != "false" {
			code := "codex_safety_buffering_setting_invalid"
			if key == SettingKeyOpenAIErrorMessagesNormalized {
				code = "openai_error_message_setting_invalid"
			}
			slog.Warn("invalid OpenAI output setting", "stage", "settings_refresh", "code", code, "setting", key)
			continue
		}
		target.Store(raw == "true")
	}
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
	if err == nil {
		for key, target := range map[string]*atomic.Bool{
			SettingKeyOpenAICodexSafetyBufferingSuppressed: &runtime.enabled,
			SettingKeyOpenAIErrorMessagesNormalized:        &runtime.errorMessages,
		} {
			if value, ok := updates[key]; ok {
				target.Store(value == "true")
				changed = true
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
