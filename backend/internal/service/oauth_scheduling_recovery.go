package service

import "strings"

// PrepareOAuthSchedulingRecovery captures only the failure generations seen
// before the credential commit. Its closure cannot clear a later failure or
// another credential owner's model limits.
func (s *OpenAIGatewayService) PrepareOAuthSchedulingRecovery(account *Account) func() {
	if s == nil || account == nil {
		return nil
	}
	accountID := account.ID
	runtime := s.peekOpenAIAccountRuntimeBlock(account)
	models := make(map[string]grokModelQuotaBlock)
	if account.IsGrok() {
		owner, err := grokRequestOwnerFingerprint(account)
		if err == nil {
			globalGrokModelQuotaBlocks.mu.Lock()
			for key, block := range globalGrokModelQuotaBlocks.items {
				if block.AccountID == accountID && strings.HasSuffix(key, grokModelRecoveryOwnerSuffix(account, owner)) {
					models[key] = block
				}
			}
			globalGrokModelQuotaBlocks.mu.Unlock()
		}
	}
	return func() {
		s.clearOpenAIAccountRuntimeBlockIfUnchanged(accountID, runtime)
		globalGrokModelQuotaBlocks.mu.Lock()
		defer globalGrokModelQuotaBlocks.mu.Unlock()
		for key, expected := range models {
			if current, ok := globalGrokModelQuotaBlocks.items[key]; ok && current.Generation == expected.Generation && current.Until.Equal(expected.Until) {
				delete(globalGrokModelQuotaBlocks.items, key)
			}
		}
	}
}
