package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
)

// GrokRequestOutcome binds quota and scheduling observations to the complete
// credential, proxy and scheduling owner that produced the upstream request.
// Nil scheduling fields retain the current state.
type GrokRequestOutcome struct {
	Extra                   map[string]any
	ModelRateLimits         map[string]any
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string
	RateLimitResetAt        *time.Time
	ClearRateLimit          bool
}

type GrokRequestOutcomeRepository interface {
	ApplyGrokRequestOutcomeIfUnchanged(context.Context, *Account, GrokRequestOutcome) (GrokRequestOutcomeResult, error)
}

type GrokRequestOutcomeResult struct {
	Applied            bool
	RecoveryGeneration string
}

type grokRequestOutcomeKey struct{}

type grokRequestOutcomeCollector struct {
	owner   *Account
	outcome GrokRequestOutcome
	effects []func(*Account)
	dirty   bool
}

func captureGrokRequestOwner(ctx context.Context, account *Account) (context.Context, *Account, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	owner, err := snapshotCNControlAccount(account)
	if err != nil {
		return ctx, nil, "", err
	}
	owner.Extra, err = cloneAccountJSONMap(account.Extra)
	if err != nil {
		return ctx, nil, "", err
	}
	proxy, err := owner.StrictProxyURL()
	if err != nil {
		return ctx, nil, "", err
	}
	return context.WithValue(ctx, cnControlRequestOwnerKey{}, owner), owner, proxy, nil
}

func collectGrokRequestOutcome(ctx context.Context, repo AccountRepository, account *Account, outcome GrokRequestOutcome, effect func(*Account)) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if collector, ok := ctx.Value(grokRequestOutcomeKey{}).(*grokRequestOutcomeCollector); ok {
		collector.dirty = true
		if len(outcome.Extra) > 0 {
			if collector.outcome.Extra == nil {
				collector.outcome.Extra = make(map[string]any)
			}
			for key, value := range outcome.Extra {
				collector.outcome.Extra[key] = value
			}
		}
		if len(outcome.ModelRateLimits) > 0 {
			if collector.outcome.ModelRateLimits == nil {
				collector.outcome.ModelRateLimits = make(map[string]any)
			}
			for key, value := range outcome.ModelRateLimits {
				collector.outcome.ModelRateLimits[key] = value
			}
		}
		if outcome.TempUnschedulableUntil != nil && (collector.outcome.TempUnschedulableUntil == nil || collector.outcome.TempUnschedulableUntil.Before(*outcome.TempUnschedulableUntil)) {
			collector.outcome.TempUnschedulableUntil = outcome.TempUnschedulableUntil
			collector.outcome.TempUnschedulableReason = outcome.TempUnschedulableReason
		}
		if outcome.RateLimitResetAt != nil && (collector.outcome.RateLimitResetAt == nil || collector.outcome.RateLimitResetAt.Before(*outcome.RateLimitResetAt)) {
			collector.outcome.RateLimitResetAt = outcome.RateLimitResetAt
		}
		collector.outcome.ClearRateLimit = collector.outcome.ClearRateLimit || outcome.ClearRateLimit
		if effect != nil {
			collector.effects = append(collector.effects, effect)
		}
		return true
	}
	return commitGrokRequestOutcome(ctx, repo, cnControlRequestOwner(ctx, account), outcome, []func(*Account){effect})
}

func commitGrokRequestOutcome(ctx context.Context, repo AccountRepository, owner *Account, outcome GrokRequestOutcome, effects []func(*Account)) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if dbent.TxFromContext(ctx) != nil {
		if owner != nil {
			slog.Warn("grok_request_outcome_not_saved", "account_id", owner.ID, "reason", "caller_transaction_requires_commit_hook")
		}
		return false
	}
	writer, ok := repo.(GrokRequestOutcomeRepository)
	if !ok || owner == nil {
		return false
	}
	effectOwner, err := CaptureOAuthCredentialOwner(owner)
	if err != nil {
		return false
	}
	if effectOwner.Extra == nil {
		effectOwner.Extra = make(map[string]any)
	}
	publication, releasePublication := acquireGrokRuntimePublication(owner.ID)
	defer releasePublication()
	publication.mu.Lock()
	epoch := publication.epoch
	publication.mu.Unlock()
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	result, err := writer.ApplyGrokRequestOutcomeIfUnchanged(stateCtx, owner, outcome)
	if err != nil {
		slog.Warn("grok_request_outcome_not_saved", "account_id", owner.ID, "reason", "storage_failure")
	}
	if err != nil || !result.Applied {
		return false
	}
	effectOwner.Extra[RuntimeRecoveryGenerationExtraKey] = result.RecoveryGeneration
	publication.mu.Lock()
	defer publication.mu.Unlock()
	if publication.epoch != epoch && result.RecoveryGeneration == "" {
		return true
	}
	for _, effect := range effects {
		if effect != nil {
			effect(effectOwner)
		}
	}
	return true
}

// Runtime ownership excludes the quota/scheduler fields changed by the outcome
// itself and the proxy's unrelated latency/name metadata.
func grokRequestOwnerFingerprint(account *Account) (string, error) {
	owner, err := cnControlOwnerFingerprint(account)
	if err != nil {
		return "", err
	}
	var status, expires string
	if account.ProxyID != nil && account.Proxy != nil {
		status = account.Proxy.Status
		if account.Proxy.ExpiresAt != nil {
			expires = account.Proxy.ExpiresAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
		}
	}
	payload, err := json.Marshal([]string{owner, status, expires})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

type grokRuntimeOwnerKey struct {
	accountID          int64
	owner              string
	recoveryGeneration string
}

func openAIAccountRuntimeOwnerKey(account *Account) any {
	if account == nil {
		return int64(0)
	}
	if account != nil && account.IsGrok() {
		fingerprint := credentialStateGeneration(account)
		if fingerprint == "" {
			return grokRuntimeOwnerKey{accountID: account.ID, owner: "invalid-owner", recoveryGeneration: runtimeRecoveryGeneration(account)}
		}
		return grokRuntimeOwnerKey{accountID: account.ID, owner: fingerprint, recoveryGeneration: runtimeRecoveryGeneration(account)}
	}
	return openAIAccountRuntimeBlockKey(account)
}
