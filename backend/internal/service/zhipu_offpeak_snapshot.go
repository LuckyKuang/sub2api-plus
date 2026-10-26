package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

type offPeakSnapshot struct {
	Account  *Account                  `json:"account"`
	Auth     zcode.OffPeakAuth         `json:"auth"`
	Identity outboundidentity.Identity `json:"identity"`
	ProxyURL string                    `json:"proxy_url"`
}

type zhipuOffPeakDurable struct {
	store         ZhipuOffPeakStore
	activeKeyID   string
	keys          map[string]cipher.AEAD
	retention     time.Duration
	renewInterval time.Duration
}

func newOffPeakDurable(store ZhipuOffPeakStore, cfg config.ZhipuOffPeakConfig) (*zhipuOffPeakDurable, error) {
	if store == nil {
		return nil, errors.New("off-peak persistence requires PostgreSQL")
	}
	keys, err := cfg.Keyring()
	if err != nil {
		return nil, err
	}
	if cfg.SnapshotRetentionHours < 1 || cfg.SnapshotRetentionHours > 720 {
		return nil, errors.New("invalid off-peak snapshot retention")
	}
	d := &zhipuOffPeakDurable{store: store, activeKeyID: cfg.ActiveKeyID, keys: map[string]cipher.AEAD{}, retention: time.Duration(cfg.SnapshotRetentionHours) * time.Hour, renewInterval: offPeakRequestRenew}
	for id, key := range keys {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		d.keys[id] = aead
	}
	return d, nil
}

func offPeakSnapshotAAD(r ZhipuOffPeakRecord) []byte {
	return []byte(fmt.Sprintf("sub2api/zhipu-offpeak/v1/%s/%d/%s/%s/%s", r.ID, r.AccountID, r.Generation, r.TaskID, r.KeyID))
}

func offPeakOwnerGeneration(account *Account, auth zcode.OffPeakAuth, identity outboundidentity.Identity) string {
	if usesZhipuProjectToken(account) {
		auth.PlanKey = ""
	}
	// Resolver provenance and metadata are not wire declarations. Source
	// precedence remains immutable; an identical selected fingerprint may reuse
	// its ticket even when the same value now comes from another valid source.
	identity.Source, identity.Language, identity.Timezone = "", "", ""
	data, _ := json.Marshal([]any{projectTokenScope(account), account.CreatedAt.UTC().Format(time.RFC3339Nano), auth, identity})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func minimalOffPeakAccount(account *Account) *Account {
	copy := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type, Status: account.Status, CreatedAt: account.CreatedAt, Credentials: map[string]any{}, ProxyID: account.ProxyID}
	for _, key := range []string{domesticOAuthLoginIDCredential, "plan_kind", "oauth_provider", "access_token", "oauth_access_token", "zcode_jwt_token", "api_key", "off_peak_plan_key", "zhipu_project_kind", "zhipu_project_key_name", "zhipu_project_key_id", "zhipu_organization", "zhipu_project"} {
		if v, ok := account.Credentials[key]; ok {
			copy.Credentials[key] = v
		}
	}
	if account.Proxy != nil {
		p := *account.Proxy
		copy.Proxy = &p
	}
	return copy
}

func (d *zhipuOffPeakDurable) encrypt(record ZhipuOffPeakRecord, snapshot offPeakSnapshot) ([]byte, error) {
	plaintext, err := json.Marshal(snapshot)
	if err != nil {
		return nil, errors.New("off-peak snapshot encoding failed")
	}
	aead := d.keys[record.KeyID]
	if aead == nil {
		return nil, errors.New("off-peak snapshot key unavailable")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, offPeakSnapshotAAD(record)), nil
}

func (d *zhipuOffPeakDurable) decrypt(record ZhipuOffPeakRecord) (offPeakSnapshot, error) {
	var snapshot offPeakSnapshot
	aead := d.keys[record.KeyID]
	if aead == nil {
		return snapshot, errors.New("off-peak snapshot key unavailable")
	}
	if len(record.Snapshot) < aead.NonceSize()+aead.Overhead() {
		return snapshot, errors.New("off-peak snapshot authentication failed")
	}
	plaintext, err := aead.Open(nil, record.Snapshot[:aead.NonceSize()], record.Snapshot[aead.NonceSize():], offPeakSnapshotAAD(record))
	if err != nil {
		return snapshot, errors.New("off-peak snapshot authentication failed")
	}
	if err = json.Unmarshal(plaintext, &snapshot); err != nil || snapshot.Account == nil || snapshot.Account.ID != record.AccountID {
		return offPeakSnapshot{}, errors.New("off-peak snapshot is invalid")
	}
	proxy, err := snapshot.Account.StrictProxyURL()
	if err != nil || proxy != snapshot.ProxyURL {
		return offPeakSnapshot{}, errors.New("off-peak snapshot route is invalid")
	}
	snapshot.Identity.AccountID = record.AccountID
	return snapshot, nil
}
