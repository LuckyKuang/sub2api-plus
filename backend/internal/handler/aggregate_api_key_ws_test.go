//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type aggregateWSKeys struct{ *wsGroupPricingAPIKeyRepoStub }

func (r aggregateWSKeys) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	out, err := r.wsGroupPricingAPIKeyRepoStub.GetByKeyForAuth(ctx, key)
	if out != nil {
		out.Group, out.GroupID = nil, nil
	}
	return out, err
}
func (aggregateWSKeys) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type aggregateWSGroups struct {
	service.GroupRepository
	repo *wsGroupPricingAPIKeyRepoStub
}

func (r aggregateWSGroups) ListActive(context.Context) ([]service.Group, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	return []service.Group{r.repo.group}, nil
}

type aggregateWSUsers struct {
	service.UserRepository
	repo *wsGroupPricingAPIKeyRepoStub
}

func (r aggregateWSUsers) GetByID(context.Context, int64) (*service.User, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	out := *r.repo.apiKey.User
	return &out, nil
}

func TestAggregateWebSocketRefreshesPriceAndRevokesNextTurn(t *testing.T) {
	for _, mode := range []string{service.GroupBindingAll, service.GroupBindingCustom} {
		for _, ingress := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
			for _, change := range []string{"price", "grant", "member", "subscription"} {
				t.Run(mode+"/"+ingress+"/"+change, func(t *testing.T) {
					repo := newWSGroupPricingAPIKeyRepoStub(3)
					repo.group.SubscriptionType = service.SubscriptionTypeStandard
					repo.apiKey.GroupBindingMode = mode
					repo.apiKey.GroupID = nil
					repo.apiKey.AggregateGroupIDs = []int64{wsGroupPricingGroupID}
					repo.apiKey.User.AllowedGroups = []int64{wsGroupPricingGroupID}
					repo.apiKey.User.RestrictPublicGroups = true
					accounts := aggregateAuditAccounts{accounts: []service.Account{{ID: 1901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}}}
					keys := service.ProvideAPIKeyService(accounts, nil, aggregateWSKeys{repo}, aggregateWSUsers{repo: repo}, aggregateWSGroups{repo: repo}, nil, nil, nil, &config.Config{}, nil, nil)
					got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
						firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond,
						ingressMode: ingress, apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
						afterFirstUpstreamRequest: func(*service.ChannelService) error {
							repo.mu.Lock()
							defer repo.mu.Unlock()
							switch change {
							case "price":
								repo.group.RateMultiplier = .3
							case "grant":
								repo.apiKey.User.AllowedGroups = nil
							case "member":
								repo.apiKey.GroupBindingMode = service.GroupBindingCustom
								repo.apiKey.AggregateGroupIDs = []int64{999}
							case "subscription":
								repo.group.SubscriptionType = service.SubscriptionTypeSubscription
							}
							return nil
						},
						secondTurnCloseExpected: change != "price", closeReason: "aggregate group is no longer available",
					})
					if change == "price" {
						require.Len(t, got.logs, 2)
						require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
						require.InDelta(t, .3, got.logs[1].RateMultiplier, 1e-12)
						require.Greater(t, got.logs[0].ActualCost, 0.0)
						require.InDelta(t, got.logs[0].ActualCost/10, got.logs[1].ActualCost, 1e-15)
					}
				})
			}
		}
	}
}
