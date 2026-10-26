//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type tempUnschedStatusOwnerCache struct {
	state  *TempUnschedState
	getErr error
	setErr error
	writes []*TempUnschedState
}

func (c *tempUnschedStatusOwnerCache) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.state == nil {
		return nil, nil
	}
	copy := *c.state
	return &copy, nil
}

func (c *tempUnschedStatusOwnerCache) SetTempUnsched(_ context.Context, _ int64, state *TempUnschedState) error {
	if state != nil {
		copy := *state
		c.writes = append(c.writes, &copy)
	}
	return c.setErr
}

func (*tempUnschedStatusOwnerCache) DeleteTempUnsched(context.Context, int64) error { return nil }

func TestSevenProviderTempUnschedStatusUsesAuthoritativeSavedState(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, scenario := range []string{"recovered account", "newer plain reason", "newer structured reason", "matching saved state", "deleted account", "account read failure", "cache unavailable"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				now := time.Now().UTC().Truncate(time.Second)
				oldUntil := now.Add(time.Hour)
				oldState := &TempUnschedState{
					UntilUnix: oldUntil.Unix(), TriggeredAtUnix: now.Add(-time.Minute).Unix(),
					StatusCode: 429, MatchedKeyword: "old quota failure", RuleIndex: 1,
					ErrorMessage: "old credential owner cooldown", TriggerCount: 4, TriggerThreshold: 4,
					TriggerWindowMinutes: 5, Scope: "account",
				}
				oldReason, err := json.Marshal(oldState)
				require.NoError(t, err)
				account := &Account{
					ID: 917, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive,
					TempUnschedulableUntil: &oldUntil, TempUnschedulableReason: string(oldReason),
				}
				repo := &rateLimitClearRepoStub{getByIDAccount: account}
				cache := &tempUnschedStatusOwnerCache{state: oldState}
				want := *oldState
				var wantErr error
				wantInactive := false
				switch scenario {
				case "recovered account":
					// Administrative recovery has committed. An older callback may
					// still have repopulated the account-ID-only display cache.
					account.TempUnschedulableUntil = nil
					account.TempUnschedulableReason = ""
					wantInactive = true
				case "newer plain reason", "cache unavailable":
					newUntil := now.Add(3 * time.Hour)
					account.TempUnschedulableUntil = &newUntil
					account.TempUnschedulableReason = "newly committed provider failure"
					want = TempUnschedState{UntilUnix: newUntil.Unix(), ErrorMessage: "newly committed provider failure"}
					if scenario == "cache unavailable" {
						cache.getErr = errors.New("temporary cache read unavailable")
						cache.setErr = errors.New("temporary cache write unavailable")
					}
				case "newer structured reason":
					newUntil := now.Add(2 * time.Hour)
					want = TempUnschedState{
						UntilUnix: newUntil.Unix(), TriggeredAtUnix: now.Unix(), StatusCode: 401,
						ErrorMessage: "new authorization failure", MatchedKeyword: "new rule", RuleIndex: 2,
						TriggerCount: 2, TriggerThreshold: 2, TriggerWindowMinutes: 10, Scope: "account",
					}
					newReason, err := json.Marshal(want)
					require.NoError(t, err)
					account.TempUnschedulableUntil = &newUntil
					account.TempUnschedulableReason = string(newReason)
				case "deleted account":
					wantErr = ErrAccountNotFound
					repo.getByIDAccount = nil
					repo.getByIDErr = wantErr
				case "account read failure":
					wantErr = errors.New("account storage read unavailable")
					repo.getByIDAccount = nil
					repo.getByIDErr = wantErr
				}
				status, statusErr := NewRateLimitService(repo, nil, &config.Config{}, nil, cache).GetTempUnschedStatus(context.Background(), account.ID)
				if wantErr != nil {
					require.ErrorIs(t, statusErr, wantErr, "cached activity cannot authorize status for an unreadable or deleted account")
					require.Nil(t, status)
					require.Empty(t, cache.writes, "an unknown account state cannot be published as a new active cache entry")
				} else {
					require.NoError(t, statusErr, "a cache outage cannot hide a valid authoritative account status")
					if wantInactive {
						require.Nil(t, status, "a recovered saved account must be shown as inactive despite an older cached cooldown")
					} else {
						require.NotNil(t, status)
						require.Equal(t, want, *status, "the displayed deadline and reason must belong to the currently saved account state")
					}
				}
			})
		}
	}
}
