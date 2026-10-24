//go:build integration

package repository

import (
	"fmt"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func (s *AccountRepoSuite) TestModelNotFoundLateGrantWriteDoesNotBlockReauthorizedOwner() {
	account := mustCreateAccount(s.T(), s.client, &service.Account{
		Name: "model-generation-late-response", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "observed-key"},
	})
	old, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"api_key": "reauthorized-key"}))
	limiter := service.NewRateLimitService(s.repo, nil, &config.Config{}, nil, nil)
	body := []byte(`{"error":{"code":"model_not_found","message":"Model A unavailable"}}`)
	s.Require().True(limiter.HandleUpstreamModelNotFound(s.ctx, old, "model-A", 403, body))
	fresh, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().True(fresh.IsSchedulableForModelWithContext(s.ctx, "model-A"))
	old.Extra = fresh.Extra
	s.Require().False(old.IsSchedulableForModelWithContext(s.ctx, "model-A"))
	s.Require().True(old.IsSchedulableForModelWithContext(s.ctx, "model-B"))
	s.Require().True(limiter.HandleUpstreamModelNotFound(s.ctx, fresh, "model-A", 403, body))
	fresh, err = s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().False(fresh.IsSchedulableForModelWithContext(s.ctx, "model-A"))
	s.Require().True(fresh.IsSchedulableForModelWithContext(s.ctx, "model-B"))
	s.Require().Equal(service.StatusActive, fresh.Status)
	s.Require().True(fresh.Schedulable)
	s.Require().Nil(fresh.TempUnschedulableUntil)
	s.Require().Nil(fresh.RateLimitResetAt)
	s.Require().Len(fresh.Extra["model_rate_limits"].(map[string]any), 2)
}

func (s *AccountRepoSuite) TestModelNotFoundGenerationCleanupPreservesLiveAndUnrelatedScopes() {
	prefix := service.OpenAIModelNotFoundRateLimitScopePrefix
	expired := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	entry := func(reset string) map[string]any {
		return map[string]any{"rate_limit_reset_at": reset, "reason": "preserve-me"}
	}
	expiredKey, liveKey := prefix+"old-generation:model-A", prefix+"current-generation:model-A"
	other429, planGated := "openai:image_generation", "gpt-plan-gated"
	similarPrefix := "openai:modelXnotYfound:legacy:model-A"
	malformedKey := prefix + "malformed:model-A"
	account := mustCreateAccount(s.T(), s.client, &service.Account{
		Name: "model-generation-cleanup", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Extra: map[string]any{"model_rate_limits": map[string]any{
			expiredKey: entry(expired), liveKey: entry(future), other429: entry(expired),
			planGated: entry(expired), similarPrefix: entry(expired), malformedKey: entry("operator-owned-value"),
		}, "unrelated_extra": "preserve-me"},
	})
	newKey := prefix + "new-generation:model-B"
	s.Require().NoError(s.repo.SetModelRateLimit(s.ctx, account.ID, newKey, time.Now().Add(time.Hour), "upstream_model_not_found"))
	fresh, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	limits := fresh.Extra["model_rate_limits"].(map[string]any)
	s.Require().NotContains(limits, expiredKey)
	for _, key := range []string{liveKey, newKey, other429, planGated, similarPrefix, malformedKey} {
		s.Require().Contains(limits, key)
	}
	s.Require().Equal("preserve-me", fresh.Extra["unrelated_extra"])
	for _, key := range []string{liveKey, other429, planGated, similarPrefix, malformedKey} {
		s.Require().Equal("preserve-me", limits[key].(map[string]any)["reason"])
	}
}

func (s *AccountRepoSuite) TestModelNotFoundGenerationConcurrentWritesKeepEveryScope() {
	// Use the real connection pool; a suite transaction would serialize all
	// goroutines on one connection and never exercise row-update contention.
	client := testEntClient(s.T())
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	account := mustCreateAccount(s.T(), client, &service.Account{
		Name: "model-generation-concurrent", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
	})
	const writers = 12
	start := make(chan struct{})
	errors := make(chan error, writers)
	var group sync.WaitGroup
	for i := range writers {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			key := service.OpenAIModelNotFoundRateLimitScopePrefix + fmt.Sprintf("generation-%d:model-%d", index, index)
			errors <- repo.SetModelRateLimit(s.ctx, account.ID, key, time.Now().Add(time.Hour), "upstream_model_not_found")
		}(i)
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		s.Require().NoError(err)
	}
	fresh, err := repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	limits := fresh.Extra["model_rate_limits"].(map[string]any)
	s.Require().Len(limits, writers)
	for i := range writers {
		key := service.OpenAIModelNotFoundRateLimitScopePrefix + fmt.Sprintf("generation-%d:model-%d", i, i)
		s.Require().Contains(limits, key)
	}
}
