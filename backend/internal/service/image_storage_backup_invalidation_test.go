//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type inheritedImageTargetStorage struct {
	target config.ImageStorageConfig
}

type imageStorageInvalidationBus struct {
	mu         sync.Mutex
	listeners  map[string][]func()
	publishErr error
}

func (b *imageStorageInvalidationBus) Publish(_ context.Context, channel string) error {
	b.mu.Lock()
	listeners := append([]func(){}, b.listeners[channel]...)
	b.mu.Unlock()
	if b.publishErr != nil {
		return b.publishErr
	}
	for _, listener := range listeners {
		listener()
	}
	return nil
}

func (b *imageStorageInvalidationBus) Subscribe(ctx context.Context, channel string, listener func()) error {
	b.mu.Lock()
	if b.listeners == nil {
		b.listeners = make(map[string][]func())
	}
	b.listeners[channel] = append(b.listeners[channel], listener)
	b.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (s *inheritedImageTargetStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	return s.target.Endpoint + "/" + s.target.Bucket + "/" + key, nil
}

// ASYNC_IMAGE_TASKS.md requires inherited backup changes to invalidate image
// clients across instances. Warm both clients, save through the real backup
// service, then upload again: each must use the newly saved target/credentials.
func TestImageStorageBackupChangesInvalidateLocalAndReplicaTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	bus := &imageStorageInvalidationBus{}
	first.SetInvalidationBus(bus)
	replicaBackup := NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}, reversibleEncryptor{}, nil, nil)
	factory := func(_ context.Context, cfg *config.ImageStorageConfig) (ImageStorage, error) {
		return &inheritedImageTargetStorage{target: *cfg}, nil
	}
	first.factory = factory
	replica := NewImageStorageSettingService(repo, reversibleEncryptor{}, replicaBackup, factory, config.ImageStorageConfig{})
	replica.SetInvalidationBus(bus)
	replica.StartInvalidationSubscriber(ctx)
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.listeners[imageStorageConfigInvalidationChannel]) == 1
	}, time.Second, time.Millisecond)

	oldTarget := BackupS3Config{Endpoint: "https://old.example", Region: "old-region", Bucket: "old-images", AccessKeyID: "old-ak", SecretAccessKey: "old-sk"}
	_, err := first.backup.UpdateS3Config(ctx, oldTarget)
	require.NoError(t, err)
	_, err = first.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true, Prefix: "images/"})
	require.NoError(t, err)
	result := json.RawMessage(`{"data":[{"b64_json":"aW1hZ2U="}]}`)
	assertTarget := func(svc *ImageStorageSettingService, endpoint, bucket, region, accessKey, secret string) {
		t.Helper()
		uploader, enabled := svc.resolve()
		require.True(t, enabled)
		rewritten, err := uploader.Rewrite(ctx, "task", result)
		require.NoError(t, err)
		require.JSONEq(t, `{"data":[{"url":"`+endpoint+`/`+bucket+`/images/task-0.png"}]}`, string(rewritten))
		storage := uploader.storage.(*inheritedImageTargetStorage)
		require.Equal(t, region, storage.target.Region)
		require.Equal(t, accessKey, storage.target.AccessKeyID)
		require.Equal(t, secret, storage.target.SecretAccessKey)
	}
	for _, svc := range []*ImageStorageSettingService{first, replica} {
		assertTarget(svc, "https://old.example", "old-images", "old-region", "old-ak", "old-sk")
	}

	_, err = first.backup.UpdateS3Config(ctx, BackupS3Config{Endpoint: "https://new.example", Region: "new-region", Bucket: "new-images", AccessKeyID: "new-ak", SecretAccessKey: "new-sk"})
	require.NoError(t, err)
	for _, svc := range []*ImageStorageSettingService{first, replica} {
		assertTarget(svc, "https://new.example", "new-images", "new-region", "new-ak", "new-sk")
	}
}

// A failed backup save must leave the accepted effective target in place, even
// when no cross-instance invalidation transport is available.
func TestImageStorageBackupChangesInvalidateLocallyWithoutBus(t *testing.T) {
	ctx := context.Background()
	svc, _, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	_, err := svc.backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "old", AccessKeyID: "ak", SecretAccessKey: "sk"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true})
	require.NoError(t, err)
	_, enabled := svc.resolve()
	require.True(t, enabled)

	svc.backup.encryptionKeyConfigured = false
	_, err = svc.backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "rejected", AccessKeyID: "ak", SecretAccessKey: "sk"})
	require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)
	svc.resolve()
	require.Len(t, *built, 1)

	svc.backup.encryptionKeyConfigured = true
	svc.SetInvalidationBus(&imageStorageInvalidationBus{publishErr: errors.New("Redis unavailable")})
	_, err = svc.backup.UpdateS3Config(ctx, BackupS3Config{Bucket: "accepted", AccessKeyID: "ak", SecretAccessKey: "next-sk"})
	require.NoError(t, err)
	_, enabled = svc.resolve()
	require.True(t, enabled)
	require.Len(t, *built, 2)
	require.Equal(t, "accepted", (*built)[1].Bucket)
	require.Equal(t, "next-sk", (*built)[1].SecretAccessKey)
}

// ASYNC_IMAGE_TASKS.md bounds missed-notification reconciliation to 30 seconds.
// A disconnected replica must eventually stop admitting work after disablement.
func TestImageStorageInvalidationMissedNotificationReconcilesDisabledMode(t *testing.T) {
	ctx := context.Background()
	first, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	settings := baseStorageSettings()
	settings.SecretAccessKey = "sk"
	_, err := first.Update(ctx, settings)
	require.NoError(t, err)
	replica := NewImageStorageSettingService(repo, reversibleEncryptor{}, first.backup, first.factory, config.ImageStorageConfig{})
	_, enabled := replica.resolve()
	require.True(t, enabled)

	first.SetInvalidationBus(&imageStorageInvalidationBus{publishErr: errors.New("Redis unavailable")})
	settings.Enabled = false
	_, err = first.Update(ctx, settings)
	require.NoError(t, err)
	_, enabled = first.resolve()
	require.False(t, enabled)
	_, enabled = replica.resolve()
	require.True(t, enabled, "the replica has not received an invalidation yet")
	replica.mu.Lock()
	replica.resolvedAt = time.Now().Add(-31 * time.Second)
	replica.mu.Unlock()
	_, enabled = replica.resolve()
	require.False(t, enabled, "a lost notification must not preserve submission permission beyond 30 seconds")
}

// Expiring an enabled snapshot must never extend its permission when the
// setting database is unavailable; a later successful read restores service.
func TestImageStorageInvalidationReconciliationReadFailureFailsClosed(t *testing.T) {
	svc, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	settings := baseStorageSettings()
	settings.SecretAccessKey = "sk"
	_, err := svc.Update(context.Background(), settings)
	require.NoError(t, err)
	_, enabled := svc.resolve()
	require.True(t, enabled)
	failing := &failingImageSettingRepo{SettingRepository: repo, readErr: errors.New("database unavailable")}
	svc.settingRepo = failing
	svc.mu.Lock()
	svc.resolvedAt = time.Now().Add(-31 * time.Second)
	svc.mu.Unlock()
	require.Equal(t, ImageStorageAvailability{Reason: "dependency_unavailable"}, svc.Availability())
	_, enabled = svc.resolve()
	require.False(t, enabled)
	failing.readErr = nil
	_, enabled = svc.resolve()
	require.True(t, enabled)
}
