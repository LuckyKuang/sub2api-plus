//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type safetyBufferingSettingsRepo struct {
	SettingRepository
	mu                sync.Mutex
	values            map[string]string
	readErr, writeErr error
	beforeRead        func()
}

func (r *safetyBufferingSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	err, before := r.readErr, r.beforeRead
	r.mu.Unlock()
	if before != nil {
		before()
	}
	return result, err
}

func (r *safetyBufferingSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestCodexSafetyBufferingSettingDefaultPersistenceAndFailures(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	s := svc.settingService
	require.False(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	saveSafetyBuffering(t, s, true)
	require.True(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	reloaded := NewSettingService(repo, nil)
	require.True(t, reloaded.CodexSafetyBufferingSuppressed(t.Context()))
	// Neither a failed write nor a failed refresh may reset a valid preference.
	repo.mu.Lock()
	repo.writeErr = errors.New("storage unavailable")
	repo.mu.Unlock()
	err := s.persistSystemSettingsWithCodexSafetyBuffering(t.Context(), &SystemSettings{}, map[string]string{SettingKeyOpenAICodexSafetyBufferingSuppressed: "false"})
	require.Error(t, err)
	require.True(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	repo.mu.Lock()
	repo.readErr = errors.New("storage unavailable")
	repo.writeErr = nil
	repo.mu.Unlock()
	s.refreshCodexSafetyBuffering(t.Context())
	require.True(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	repo.mu.Lock()
	repo.readErr = nil
	repo.values[SettingKeyOpenAICodexSafetyBufferingSuppressed] = "invalid"
	repo.mu.Unlock()
	s.refreshCodexSafetyBuffering(t.Context())
	require.True(t, s.CodexSafetyBufferingSuppressed(t.Context()))
	saveSafetyBuffering(t, s, false)
	require.False(t, s.CodexSafetyBufferingSuppressed(t.Context()))
}

func TestCodexSafetyBufferingOlderRefreshCannotOverwriteSave(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	readStarted, releaseRead := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo.beforeRead = func() { once.Do(func() { close(readStarted); <-releaseRead }) }
	refreshDone := make(chan struct{})
	go func() { svc.settingService.refreshCodexSafetyBuffering(t.Context()); close(refreshDone) }()
	<-readStarted
	saveDone := make(chan error, 1)
	go func() {
		saveDone <- svc.settingService.persistSystemSettingsWithCodexSafetyBuffering(t.Context(), &SystemSettings{}, map[string]string{SettingKeyOpenAICodexSafetyBufferingSuppressed: "true"})
	}()
	close(releaseRead)
	require.NoError(t, <-saveDone)
	<-refreshDone
	require.True(t, svc.settingService.CodexSafetyBufferingSuppressed(t.Context()))
}

type safetyBufferingInvalidationBus struct {
	mu           sync.Mutex
	listeners    []func()
	drop         bool
	publishErr   error
	subscribeErr error
}

func (b *safetyBufferingInvalidationBus) Publish(context.Context, string) error {
	b.mu.Lock()
	listeners := append([]func(){}, b.listeners...)
	drop := b.drop
	err := b.publishErr
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if !drop {
		for _, fn := range listeners {
			fn()
		}
	}
	return nil
}

func (b *safetyBufferingInvalidationBus) Subscribe(ctx context.Context, _ string, onMessage func()) error {
	b.mu.Lock()
	if b.subscribeErr != nil {
		err := b.subscribeErr
		b.mu.Unlock()
		return err
	}
	b.listeners = append(b.listeners, onMessage)
	b.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func TestCodexSafetyBufferingReplicasReconcileMissedNotifications(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	first, second := svc.settingService, NewSettingService(repo, nil)
	bus := &safetyBufferingInvalidationBus{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first.StartCodexSafetyBufferingRuntime(ctx, bus)
	second.StartCodexSafetyBufferingRuntime(ctx, bus)
	t.Cleanup(first.StopCodexSafetyBufferingRuntime)
	t.Cleanup(second.StopCodexSafetyBufferingRuntime)
	require.Eventually(t, func() bool { bus.mu.Lock(); defer bus.mu.Unlock(); return len(bus.listeners) == 2 }, time.Second, time.Millisecond)
	saveSafetyBuffering(t, first, true)
	require.True(t, second.CodexSafetyBufferingSuppressed(ctx), "broadcast updates the running replica")
	bus.mu.Lock()
	bus.drop = true
	bus.mu.Unlock()
	saveSafetyBuffering(t, first, false)
	require.Eventually(t, func() bool { return !second.CodexSafetyBufferingSuppressed(ctx) }, 3*time.Second, 10*time.Millisecond, "periodic reconciliation repairs a lost notification")
}

func TestCodexSafetyBufferingSaveSurvivesBroadcastFailure(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	svc.settingService.StartCodexSafetyBufferingRuntime(t.Context(), &safetyBufferingInvalidationBus{publishErr: errors.New("bus unavailable")})
	defer svc.settingService.StopCodexSafetyBufferingRuntime()
	saveSafetyBuffering(t, svc.settingService, true)
	require.True(t, svc.settingService.CodexSafetyBufferingSuppressed(t.Context()))
	reloaded := NewSettingService(repo, nil)
	require.True(t, reloaded.CodexSafetyBufferingSuppressed(t.Context()), "a failed notification must not roll back or misreport a successful save")
}

func TestCodexSafetyBufferingReplicaPollsWhenSubscriptionFails(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	replica := NewSettingService(repo, nil)
	replica.StartCodexSafetyBufferingRuntime(t.Context(), &safetyBufferingInvalidationBus{subscribeErr: errors.New("subscriber unavailable")})
	defer replica.StopCodexSafetyBufferingRuntime()
	saveSafetyBuffering(t, svc.settingService, true)
	require.Eventually(t, func() bool { return replica.CodexSafetyBufferingSuppressed(t.Context()) }, 3*time.Second, 10*time.Millisecond, "an unavailable subscription must not leave ongoing streams on a stale setting")
}
