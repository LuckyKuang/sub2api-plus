//go:build unit

package handler

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesWebSocket_LaterTurnUsesFreshForcedFastContext(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			f := newWSInflightHarnessWithSetup(t, mode, 100, wsTurnFastPricingCards(), false, nil, func(f *wsInflightHarness, _ *OpenAIGatewayHandler) {
				f.key.Group.ForceOpenAIFast = true
				f.keyRepo.group.ForceOpenAIFast = true
			})
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			first := f.frame(t)
			require.Equal(t, "priority", gjson.GetBytes(first.payload, "service_tier").String())
			require.Equal(t, []float64{6}, f.cache.heldAmounts())
			f.complete(t, first, "resp_force_first")
			firstBill := f.bill(t)
			require.InDelta(t, .008, firstBill.command.BalanceCost, 1e-12)
			require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.ForceOpenAIFast = false }))
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			second := f.frame(t)
			require.Empty(t, gjson.GetBytes(second.payload, "service_tier").String(), "the new turn must not execute the connection's old force flag")
			require.ElementsMatch(t, []float64{6, 3}, f.cache.heldAmounts())
			// An edit after admission cannot change this turn's execution/price.
			require.NoError(t, f.keyRepo.adminUpdate(f.keyService, func(_ *service.APIKey, group *service.Group) { group.ForceOpenAIFast = true }))
			f.complete(t, second, "resp_force_second")
			secondBill := f.bill(t)
			require.InDelta(t, .004, secondBill.command.BalanceCost, 1e-12)
			finishWSTurnContextBills(t, f, firstBill, secondBill)
		})
	}
}

func TestOpenAIResponsesWebSocket_LaterTurnRecapturesGlobalFastContext(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, ungrouped := range []bool{false, true} {
			name := mode + "/grouped"
			if ungrouped {
				name = mode + "/ungrouped"
			}
			t.Run(name, func(t *testing.T) {
				repo := &wsUngroupedFastSettingRepo{}
				settings := service.NewSettingService(repo, &config.Config{})
				model, firstHold, secondHold, firstCost, secondCost := "gpt-6-luna", 6.0, 3.0, .008, .004
				if ungrouped {
					model, firstHold, secondHold, firstCost, secondCost = "gpt-5.5", .0875, .035, .0001, .00004
				}
				f := newWSInflightHarnessWithSetup(t, mode, 100, wsTurnFastPricingCards(), false, settings, func(f *wsInflightHarness, _ *OpenAIGatewayHandler) {
					if ungrouped {
						f.key.Group, f.key.GroupID = nil, nil
						for i := range f.accountRepo.accounts {
							f.accountRepo.accounts[i].AccountGroups = nil
							f.accountRepo.accounts[i].GroupIDs = nil
							delete(f.accountRepo.accounts[i].Credentials, "model_mapping")
						}
					}
				})
				f.send(t, wsInflightPayload(model, 1000, 4000, ""))
				first := f.frame(t)
				require.Equal(t, "priority", gjson.GetBytes(first.payload, "service_tier").String())
				require.Len(t, f.cache.heldAmounts(), 1)
				require.InDelta(t, firstHold, f.cache.heldAmounts()[0], 1e-12)
				f.complete(t, first, "resp_global_first")
				firstBill := f.bill(t)
				require.InDelta(t, firstCost, firstBill.command.BalanceCost, 1e-12)
				repo.changed.Store(true)
				f.send(t, wsInflightPayload(model, 1000, 4000, ""))
				second := f.frame(t)
				require.Empty(t, gjson.GetBytes(second.payload, "service_tier").String(), "new turn must recapture global settings even without a retail snapshot")
				amounts := f.cache.heldAmounts()
				slices.Sort(amounts)
				require.InDeltaSlice(t, []float64{secondHold, firstHold}, amounts, 1e-12)
				require.Equal(t, int64(2), repo.reads.Load(), "one accepted policy generation per turn")
				f.complete(t, second, "resp_global_second")
				secondBill := f.bill(t)
				require.InDelta(t, secondCost, secondBill.command.BalanceCost, 1e-12)
				finishWSTurnContextBills(t, f, firstBill, secondBill)
			})
		}
	}
}

func TestOpenAIResponsesWebSocket_LaterTurnUsesFrozenChannelMappingForWireAndBill(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			price := func(value float64) *float64 { return &value }
			repo := &openAIWSUsageHandlerChannelRepoStub{
				channels: []service.Channel{{
					ID: 7701, Name: "turn-mapping", Status: service.StatusActive,
					GroupIDs: []int64{wsGroupPricingGroupID}, BillingModelSource: service.BillingModelSourceChannelMapped,
					ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"gpt-6-luna": "gpt-6-sol"}},
					ModelPricing: []service.ChannelModelPricing{
						{Platform: service.PlatformOpenAI, Models: []string{"gpt-6-sol"}, BillingMode: service.BillingModeToken, InputPrice: price(.001), OutputPrice: price(.002)},
						{Platform: service.PlatformOpenAI, Models: []string{"gpt-5.6-sol"}, BillingMode: service.BillingModeToken, InputPrice: price(.003), OutputPrice: price(.004)},
					},
				}},
				groupPlatforms: map[int64]string{wsGroupPricingGroupID: service.PlatformOpenAI},
			}
			channels := service.NewChannelService(repo, nil, nil, nil, nil)
			var userAdmissions atomic.Int32
			f := newWSInflightHarnessWithSetup(t, mode, 100, nil, false, nil, func(f *wsInflightHarness, h *OpenAIGatewayHandler) {
				delete(f.accountRepo.accounts[0].Credentials, "model_mapping")
				f.slots.acquireUserSlotFn = func(ctx context.Context, _ int64, _ int, _ string) (bool, error) {
					if userAdmissions.Add(1) == 2 {
						// Before the fix, wire mapping ran before this admission,
						// while the fresh billing snapshot ran after it.
						channel, err := repo.GetByID(ctx, 7701)
						if err != nil {
							return false, err
						}
						channel.ModelMapping[service.PlatformOpenAI]["gpt-6-luna"] = "gpt-5.6-sol"
						if err := repo.Update(ctx, channel); err != nil {
							return false, err
						}
						channels.InvalidateCache()
					}
					return true, nil
				}
				logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
				h.gatewayService = service.NewOpenAIGatewayService(f.accountRepo, logs, f.ledger, nil, nil, nil, nil, h.cfg, nil,
					service.NewConcurrencyService(f.slots), service.NewBillingService(h.cfg, nil), service.NewRateLimitService(f.accountRepo, nil, h.cfg, nil, nil), f.billing,
					&compositeWSHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, channels, nil, nil, nil, nil)
			})
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			first := f.frame(t)
			require.Equal(t, "gpt-6-sol", gjson.GetBytes(first.payload, "model").String())
			require.Equal(t, []float64{3}, f.cache.heldAmounts())
			f.complete(t, first, "resp_mapping_first")
			firstBill := f.bill(t)
			require.InDelta(t, .004, firstBill.command.BalanceCost, 1e-12)
			f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
			second := f.frame(t)
			require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(second.payload, "model").String(), "wire must use the new turn's channel generation")
			require.ElementsMatch(t, []float64{3, 7}, f.cache.heldAmounts())
			updateWSTurnChannelMapping(t, repo, channels, "gpt-6-sol")
			f.complete(t, second, "resp_mapping_second")
			secondBill := f.bill(t)
			require.InDelta(t, .010, secondBill.command.BalanceCost, 1e-12, "settlement must retain the channel mapping admitted for the sent frame")
			finishWSTurnContextBills(t, f, firstBill, secondBill)
		})
	}
}

func TestOpenAIResponsesWebSocket_LaterTurnUngroupedFailoverRetainsAcceptedFastContext(t *testing.T) {
	// The HTTP bridge can retry a later turn before output. Its new handler
	// attempt must copy the failed turn's global rules without a retail snapshot.
	repo := &wsUngroupedFastSettingRepo{}
	f := newWSInflightHarnessWithSetup(t, service.OpenAIWSIngressModeHTTPBridge, 100, nil, true,
		service.NewSettingService(repo, &config.Config{}), func(f *wsInflightHarness, _ *OpenAIGatewayHandler) {
			f.key.Group, f.key.GroupID = nil, nil
			for i := range f.accountRepo.accounts {
				f.accountRepo.accounts[i].AccountGroups = nil
				f.accountRepo.accounts[i].GroupIDs = nil
				delete(f.accountRepo.accounts[i].Credentials, "model_mapping")
			}
		})
	f.send(t, wsInflightPayload("gpt-5.5", 1000, 4000, ""))
	first := f.frame(t)
	require.Equal(t, "priority", gjson.GetBytes(first.payload, "service_tier").String())
	f.complete(t, first, "resp_ungrouped_retry_first")
	firstBill := f.bill(t)
	close(firstBill.release)
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
	repo.changed.Store(true)
	f.send(t, wsInflightPayload("gpt-5.5", 1000, 4000, ""))
	failed := f.frame(t)
	require.Empty(t, gjson.GetBytes(failed.payload, "service_tier").String())
	require.Len(t, f.cache.heldAmounts(), 1)
	require.InDelta(t, .035, f.cache.heldAmounts()[0], 1e-12)
	// A third policy generation must not enter this accepted turn's retry.
	repo.changed.Store(false)
	failed.reply <- `{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"The usage limit has been reached"}}`
	retried := f.frame(t)
	require.Empty(t, gjson.GetBytes(retried.payload, "service_tier").String(), "retry must retain turn two's empty rules instead of the connection's initial force rule")
	require.Len(t, f.cache.heldAmounts(), 1)
	require.InDelta(t, .035, f.cache.heldAmounts()[0], 1e-12)
	require.Equal(t, int64(2), repo.reads.Load(), "retry cannot recapture current administrator settings")
	f.complete(t, retried, "resp_ungrouped_retry_recovered")
	retryBill := f.bill(t)
	require.InDelta(t, .00004, retryBill.command.BalanceCost, 1e-12)
	finishWSTurnContextBills(t, f, retryBill)
	require.Len(t, f.cache.createdIDs(), 3, "the failed attempt must release its hold before the retry takes a new owner")
	require.Empty(t, f.ledger.bills, "a failed attempt with no usage cannot create a bill")
}

func TestOpenAIResponsesWebSocket_LaterTurnPreparationRejectionReleasesOwners(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, failure := range []string{"mapping", "fast_policy"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				repo := &wsTurnDenySettingRepo{}
				f := newWSInflightHarnessWithSetup(t, mode, 100, wsTurnFastPricingCards(), false, service.NewSettingService(repo, &config.Config{}), nil)
				f.send(t, wsInflightPayload("gpt-6-luna", 1000, 4000, ""))
				first := f.frame(t)
				f.complete(t, first, "resp_reject_first")
				firstBill := f.bill(t)
				model, metadata := "gpt-6-luna", `,"service_tier":"priority"`
				if failure == "mapping" {
					model, metadata = "gpt-unsupported-switch", ""
				} else {
					repo.deny.Store(true)
				}
				f.send(t, wsInflightPayload(model, 1000, 4000, metadata))
				readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancelRead()
				for {
					_, _, err := f.client.Read(readCtx)
					if err != nil {
						require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
						break
					}
				}
				f.waitDone(t)
				select {
				case frame := <-f.frames:
					t.Fatalf("rejected second turn reached provider: %s", frame.payload)
				default:
				}
				require.Equal(t, []float64{3}, f.cache.heldAmounts(), "only the first turn's independent pending settlement may remain")
				require.Equal(t, int32(2), atomic.LoadInt32(&f.slots.releaseUserCalled))
				accountReleases := int32(1)
				if failure == "fast_policy" {
					accountReleases = 2
				}
				require.Equal(t, accountReleases, atomic.LoadInt32(&f.slots.releaseAccountCalled))
				close(firstBill.release)
				require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
			})
		}
	}
}

func updateWSTurnChannelMapping(t *testing.T, repo *openAIWSUsageHandlerChannelRepoStub, channels *service.ChannelService, mapped string) {
	t.Helper()
	channel, err := repo.GetByID(context.Background(), 7701)
	require.NoError(t, err)
	channel.ModelMapping[service.PlatformOpenAI]["gpt-6-luna"] = mapped
	require.NoError(t, repo.Update(context.Background(), channel))
	channels.InvalidateCache()
}

func finishWSTurnContextBills(t *testing.T, f *wsInflightHarness, bills ...wsInflightBill) {
	t.Helper()
	require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
	f.waitDone(t)
	for _, bill := range bills {
		close(bill.release)
	}
	require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
}

type wsTurnDenySettingRepo struct {
	wsUngroupedFastSettingRepo
	deny atomic.Bool
}

func (r *wsTurnDenySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyOpenAIFastPolicySettings {
		return "", service.ErrSettingNotFound
	}
	if r.deny.Load() {
		return `{"rules":[{"service_tier":"priority","action":"block","scope":"all"}]}`, nil
	}
	return `{"rules":[]}`, nil
}

// Keep these cases about Fast only. The shared fixture also configures an
// effort=high multiplier, whose conservative admission bound is independent.
func wsTurnFastPricingCards() []service.ChannelModelPricing {
	cards := wsInflightPublicCards()
	for i := range cards {
		cards[i].ReasoningEffortMultipliers = nil
	}
	return cards
}
