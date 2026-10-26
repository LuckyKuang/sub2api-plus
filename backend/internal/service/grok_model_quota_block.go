package service

import (
	"strings"
	"sync"
	"time"
)

// Process-local per-account model soft-blocks for Grok free-usage that names a
// model (e.g. "used all free usage for model grok-4.5"). Sibling models on the
// same account stay schedulable. Multi-instance: each process learns from its
// own upstream errors.
type grokModelQuotaBlock struct {
	AccountID  int64
	Until      time.Time
	Generation uint64
}

type grokModelQuotaBlockStore struct {
	mu       sync.Mutex
	items    map[string]grokModelQuotaBlock // key: model|credential-and-proxy owner|recovery generation
	sequence uint64
}

var globalGrokModelQuotaBlocks = &grokModelQuotaBlockStore{
	items: make(map[string]grokModelQuotaBlock),
}

const (
	grokModelQuotaBlockDefaultTTL = 2 * time.Hour
	grokModelQuotaBlockMaxTTL     = 6 * time.Hour
	grokModelQuotaBlockMinTTL     = 20 * time.Minute
)

func grokModelQuotaBlockKey(account *Account, model string) string {
	if account == nil || account.ID <= 0 || account.Platform != PlatformGrok {
		return ""
	}
	owner, err := grokRequestOwnerFingerprint(account)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(model)) + grokModelRecoveryOwnerSuffix(account, owner)
}

func grokModelRecoveryOwnerSuffix(account *Account, owner string) string {
	return "|" + owner + "|" + runtimeRecoveryGeneration(account)
}

// markGrokModelQuotaBlock soft-blocks the captured owner for one model.
func markGrokModelQuotaBlock(account *Account, model string, until time.Time) {
	model = strings.TrimSpace(model)
	if model == "" || until.IsZero() {
		return
	}
	now := time.Now()
	if !until.After(now.Add(grokModelQuotaBlockMinTTL)) {
		until = now.Add(grokModelQuotaBlockDefaultTTL)
	}
	if max := now.Add(grokModelQuotaBlockMaxTTL); until.After(max) {
		until = max
	}
	storeGrokModelQuotaBlock(account, model, until, now)
}

const (
	grokModelTransientBlockMinTTL = 500 * time.Millisecond
	grokModelTransientBlockMaxTTL = 5 * time.Minute
)

// markGrokModelTransientBlock soft-blocks a single model for a short capacity
// burst without the free-usage 20m floor (and without unscheduling the account).
func markGrokModelTransientBlock(account *Account, model string, until time.Time) {
	model = strings.TrimSpace(model)
	if model == "" || until.IsZero() {
		return
	}
	now := time.Now()
	if !until.After(now.Add(grokModelTransientBlockMinTTL)) {
		until = now.Add(grokModelTransientBlockMinTTL)
	}
	if max := now.Add(grokModelTransientBlockMaxTTL); until.After(max) {
		until = max
	}
	storeGrokModelQuotaBlock(account, model, until, now)
}

func storeGrokModelQuotaBlock(account *Account, model string, until, now time.Time) {
	key := grokModelQuotaBlockKey(account, model)
	if key == "" {
		return
	}
	globalGrokModelQuotaBlocks.mu.Lock()
	defer globalGrokModelQuotaBlocks.mu.Unlock()
	if cur, ok := globalGrokModelQuotaBlocks.items[key]; ok && cur.Until.After(until) {
		until = cur.Until
	}
	globalGrokModelQuotaBlocks.sequence++
	globalGrokModelQuotaBlocks.items[key] = grokModelQuotaBlock{AccountID: account.ID, Until: until, Generation: globalGrokModelQuotaBlocks.sequence}
	for k, v := range globalGrokModelQuotaBlocks.items {
		if !v.Until.After(now) {
			delete(globalGrokModelQuotaBlocks.items, k)
		}
	}
}

// An administrator resets one account's recoverable local state, including
// historical credential owners. Team-level quota belongs to a different store.
func clearGrokModelQuotaBlocks(accountID int64) {
	globalGrokModelQuotaBlocks.mu.Lock()
	defer globalGrokModelQuotaBlocks.mu.Unlock()
	for key, block := range globalGrokModelQuotaBlocks.items {
		if block.AccountID == accountID {
			delete(globalGrokModelQuotaBlocks.items, key)
		}
	}
}

// isGrokModelQuotaBlocked reports whether this account cannot serve model now.
func isGrokModelQuotaBlocked(account *Account, model string, now time.Time) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	key := grokModelQuotaBlockKey(account, model)
	if key == "" {
		return false
	}
	globalGrokModelQuotaBlocks.mu.Lock()
	defer globalGrokModelQuotaBlocks.mu.Unlock()
	for historyKey, block := range globalGrokModelQuotaBlocks.items {
		if !block.Until.After(now) {
			delete(globalGrokModelQuotaBlocks.items, historyKey)
		}
	}
	cur, ok := globalGrokModelQuotaBlocks.items[key]
	if !ok {
		return false
	}
	if !cur.Until.After(now) {
		delete(globalGrokModelQuotaBlocks.items, key)
		return false
	}
	return true
}

func filterGrokModelQuotaBlockedAccounts(accounts []Account, model string, now time.Time) []Account {
	if len(accounts) == 0 || strings.TrimSpace(model) == "" {
		return accounts
	}
	out := make([]Account, 0, len(accounts))
	for i := range accounts {
		upstreamModel := canonicalOpenAIAccountSchedulingModel(&accounts[i], model)
		if isGrokModelQuotaBlocked(&accounts[i], upstreamModel, now) {
			continue
		}
		out = append(out, accounts[i])
	}
	return out
}

// isGrokModelSpecificFreeUsage is true when free-usage exhaustion is scoped to
// a named model (account may still serve other models).
func isGrokModelSpecificFreeUsage(low, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || low == "" {
		return false
	}
	if strings.Contains(low, "for model") || strings.Contains(low, "模型") {
		return true
	}
	// "used all the included free usage for model grok-4.5"
	if strings.Contains(low, "free usage") && strings.Contains(low, model) {
		return true
	}
	return false
}
