//go:build unit || !integration

package service

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func offPeakPersistenceTestConfig() config.ZhipuOffPeakConfig {
	return config.ZhipuOffPeakConfig{PersistenceEnabled: true, ActiveKeyID: "test-a", SnapshotKeys: `{"test-a":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `"}`, SnapshotRetentionHours: 24}
}

func TestZhipuOffPeakSnapshotEncryptionAndRotation(t *testing.T) {
	d, err := newOffPeakDurable(&offPeakMemoryStore{}, offPeakPersistenceTestConfig())
	require.NoError(t, err)
	account := zhipuOffPeakTestAccount()
	account.CreatedAt = time.Unix(1700000000, 0)
	identity, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(context.Background(), account))
	record := ZhipuOffPeakRecord{ID: "record-a", AccountID: account.ID, Generation: "generation-a", TaskID: "offpeak-task-a", KeyID: "test-a"}
	snapshot := offPeakSnapshot{Account: minimalOffPeakAccount(account), Auth: zhipuOffPeakTestAuth(), Identity: identity}
	record.Snapshot, err = d.encrypt(record, snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(record.Snapshot), "zcode-jwt")
	require.NotContains(t, string(record.Snapshot), "login")
	restored, err := d.decrypt(record)
	require.NoError(t, err)
	require.Equal(t, account.ID, restored.Identity.AccountID)
	require.Equal(t, snapshot.Auth, restored.Auth)
	for _, change := range []string{"record", "account", "generation", "task", "ciphertext"} {
		t.Run(change, func(t *testing.T) {
			other := record
			other.Snapshot = append([]byte(nil), record.Snapshot...)
			switch change {
			case "record":
				other.ID = "record-b"
			case "account":
				other.AccountID++
			case "generation":
				other.Generation = "generation-b"
			case "task":
				other.TaskID = "other-task"
			case "ciphertext":
				other.Snapshot[len(other.Snapshot)-1] ^= 1
			}
			_, err := d.decrypt(other)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "zcode-jwt")
		})
	}
	cfg := offPeakPersistenceTestConfig()
	cfg.ActiveKeyID = "test-b"
	cfg.SnapshotKeys = strings.TrimSuffix(cfg.SnapshotKeys, "}") + `,"test-b":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))) + `"}`
	rotated, err := newOffPeakDurable(&offPeakMemoryStore{}, cfg)
	require.NoError(t, err)
	_, err = rotated.decrypt(record)
	require.NoError(t, err, "rotation retains previous key until old tickets drain")
	delete(rotated.keys, "test-a")
	_, err = rotated.decrypt(record)
	require.Error(t, err, "missing key must preserve pending state, never select a replacement owner")
}

func TestZhipuOffPeakGenerationTracksOwnerNotObservations(t *testing.T) {
	account := zhipuOffPeakTestAccount()
	account.CreatedAt = time.Unix(1700000000, 0)
	identity, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(context.Background(), account))
	auth := zhipuOffPeakTestAuth()
	before := offPeakOwnerGeneration(account, auth, identity)
	account.Name = "cosmetic"
	account.Extra = map[string]any{"observed_at": time.Now().String()}
	auth.PlanKey = "rotated-project-PAT"
	require.Equal(t, before, offPeakOwnerGeneration(account, auth, identity), "same owner PAT rotation and observations must not create a new ticket generation")
	identity.Version = "99.0.0"
	require.NotEqual(t, before, offPeakOwnerGeneration(account, auth, identity))
}

func TestZhipuOffPeakSnapshotKeyIDIsAuthenticated(t *testing.T) {
	cfg := offPeakPersistenceTestConfig()
	// Equal key material under an alias proves that failure is due to AAD's
	// key-ID binding, rather than merely selecting a different AES key.
	cfg.SnapshotKeys = strings.TrimSuffix(cfg.SnapshotKeys, "}") + `,"same-key-alias":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `"}`
	d, err := newOffPeakDurable(&offPeakMemoryStore{}, cfg)
	require.NoError(t, err)
	account := zhipuOffPeakTestAccount()
	identity, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(context.Background(), account))
	record := ZhipuOffPeakRecord{ID: "record-key-binding", AccountID: account.ID, Generation: "generation-key-binding", TaskID: "offpeak-task-key-binding", KeyID: "test-a"}
	snapshot := offPeakSnapshot{Account: minimalOffPeakAccount(account), Auth: zhipuOffPeakTestAuth(), Identity: identity}
	record.Snapshot, err = d.encrypt(record, snapshot)
	require.NoError(t, err)
	_, err = d.decrypt(record)
	require.NoError(t, err)
	record.KeyID = "same-key-alias"
	_, err = d.decrypt(record)
	require.Error(t, err, "ciphertext cannot be relabelled even when both key IDs resolve to the same key bytes")
	require.NotContains(t, err.Error(), "zcode-jwt")
}
