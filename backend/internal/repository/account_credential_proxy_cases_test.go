//go:build unit || integration

package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type credentialProxyMutationCase struct {
	name  string
	apply func(context.Context, *accountRepository, *service.Account) (bool, error)
}

func credentialProxyMutationCases() []credentialProxyMutationCase {
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	snapshot := func(owner *service.Account) service.GrokCredentialMutationSnapshot {
		credentials, _ := json.Marshal(owner.Credentials)
		return service.GrokCredentialMutationSnapshot{Owner: owner, CredentialsJSON: string(credentials), ProxyID: owner.ProxyID, Proxy: owner.Proxy}
	}
	return []credentialProxyMutationCase{
		{name: "generic-error", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetCredentialErrorIfUnchanged(ctx, owner, "revoked")
		}},
		{name: "generic-cooldown", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetCredentialTempUnschedulableIfUnchanged(ctx, owner, until, "rejected")
		}},
		{name: "generic-refresh", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, owner, map[string]any{"access_token": "rotated"})
		}},
		{name: "grok-request-error", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetGrokCredentialErrorIfMatch(ctx, owner.ID, snapshot(owner), "revoked")
		}},
		{name: "grok-request-cooldown", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetGrokCredentialTempUnschedulableIfMatch(ctx, owner.ID, snapshot(owner), until, "rejected")
		}},
		{name: "grok-missing-refresh", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetGrokOAuthErrorIfCredentialsUnchanged(ctx, owner, "missing refresh token")
		}},
		{name: "grok-refresh-success", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.UpdateGrokOAuthCredentialsIfUnchanged(ctx, owner, map[string]any{"access_token": "rotated"})
		}},
		{name: "grok-refresh-error", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetGrokOAuthRefreshErrorIfCredentialsUnchanged(ctx, owner, "revoked")
		}},
		{name: "grok-refresh-cooldown", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.SetGrokOAuthRefreshTempUnschedulableIfCredentialsUnchanged(ctx, owner, until, "rejected")
		}},
		{name: "quota-state", apply: func(ctx context.Context, repo *accountRepository, owner *service.Account) (bool, error) {
			return repo.ApplyCredentialStateMutation(ctx, owner, service.CredentialStateMutation{Extra: map[string]any{"old_observation": true}, TempUnschedulableUntil: &until, TempUnschedulableReason: "old response"})
		}},
	}
}
