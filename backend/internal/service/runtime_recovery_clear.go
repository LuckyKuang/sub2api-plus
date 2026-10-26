package service

import "time"

// RuntimeRecoveryGenerationClearer prepares local cleanup before a durable
// reset starts. Only the failure observations captured then may be retired.
// The actual committed account selects durable-generation or legacy behavior.
type RuntimeRecoveryGenerationClearer interface {
	PrepareRuntimeRecoveryGenerationClear(accountID int64) func(committed *Account)
}

func (s *OpenAIGatewayService) PrepareRuntimeRecoveryGenerationClear(accountID int64) func(*Account) {
	if s == nil || accountID <= 0 {
		return nil
	}
	var runtime []openAIAccountRuntimeBlockSnapshot
	mu := s.openAIAccountRuntimeBlockLock(accountID)
	mu.Lock()
	s.openaiAccountRuntimeBlockUntil.Range(func(key, value any) bool {
		matches := key == any(accountID)
		if owner, ok := key.(grokRuntimeOwnerKey); ok {
			matches = owner.accountID == accountID
		}
		until, ok := value.(time.Time)
		if !matches || !ok || !until.After(time.Now()) {
			return true
		}
		value, _ = s.openaiAccountRuntimeBlockGeneration.Load(key)
		generation, _ := value.(uint64)
		runtime = append(runtime, openAIAccountRuntimeBlockSnapshot{key: key, until: until, generation: generation, blocked: true})
		return true
	})
	mu.Unlock()
	models := make(map[string]grokModelQuotaBlock)
	globalGrokModelQuotaBlocks.mu.Lock()
	for key, block := range globalGrokModelQuotaBlocks.items {
		if block.AccountID == accountID {
			models[key] = block
		}
	}
	globalGrokModelQuotaBlocks.mu.Unlock()
	return func(committed *Account) {
		if runtimeRecoveryGeneration(committed) == "" {
			s.ClearAccountSchedulingBlock(accountID)
			return
		}
		publication, releasePublication := acquireGrokRuntimePublication(accountID)
		defer releasePublication()
		publication.mu.Lock()
		defer publication.mu.Unlock()
		publication.epoch++
		for _, snapshot := range runtime {
			s.clearOpenAIAccountRuntimeBlockIfUnchanged(accountID, snapshot)
		}
		globalGrokModelQuotaBlocks.mu.Lock()
		defer globalGrokModelQuotaBlocks.mu.Unlock()
		for key, expected := range models {
			if current, ok := globalGrokModelQuotaBlocks.items[key]; ok && current.Generation == expected.Generation && current.Until.Equal(expected.Until) {
				delete(globalGrokModelQuotaBlocks.items, key)
			}
		}
	}
}
