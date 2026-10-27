//go:build unit

package handler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The first accepted WS turn must carry its complete post-audit context into
// reservation and forwarding, even without a group/retail pricing snapshot.
func TestOpenAIResponsesWebSocket_UngroupedFirstTurnKeepsAuditedFastPolicy(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			settingsRepo := &wsUngroupedFastSettingRepo{}
			settings := service.NewSettingService(settingsRepo, &config.Config{})
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			var auditsAtAdmission, policyReadsAtAdmission atomic.Int64
			f := newWSInflightHarnessWithSetup(t, mode, 100, nil, false, settings, func(f *wsInflightHarness, handler *OpenAIGatewayHandler) {
				f.key.Group, f.key.GroupID = nil, nil
				for i := range f.accountRepo.accounts {
					f.accountRepo.accounts[i].AccountGroups = nil
					f.accountRepo.accounts[i].GroupIDs = nil
					// The shared fixture's Luna-only mapping also acts as a model
					// allowlist. This case forwards GPT-5.5 without any alias mapping.
					delete(f.accountRepo.accounts[i].Credentials, "model_mapping")
				}
				handler.securityAuditCoordinator = securityaudit.NewCoordinator(nil, engine)
				f.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) {
					auditsAtAdmission.Store(engine.calls.Load())
					policyReadsAtAdmission.Store(settingsRepo.reads.Load())
					// Simulate an administrator changing the global rule after the
					// audited pricing capture and before the first monetary hold.
					settingsRepo.changed.Store(true)
					return true, nil
				}
			})
			f.send(t, wsInflightPayload("gpt-5.5", 1000, 4000, ""))
			frame := f.frame(t)
			require.Equal(t, int64(1), auditsAtAdmission.Load(), "audit must finish before user admission")
			require.Equal(t, int64(1), policyReadsAtAdmission.Load(), "capture the accepted Fast rules after audit")
			require.Equal(t, "priority", gjson.GetBytes(frame.payload, "service_tier").String(), "the upstream must receive the audited rule despite the later admin edit")
			// GPT-5.5: 1000 input at $5/M + 1000 output at $30/M,
			// multiplied by priority's 2.5. Standard would reserve only $0.035.
			require.Len(t, f.cache.heldAmounts(), 1)
			require.InDelta(t, .0875, f.cache.heldAmounts()[0], 1e-12)
			require.Equal(t, int64(1), settingsRepo.reads.Load(), "reserve/selected owner/forward must reuse the accepted policy")
			f.complete(t, frame, "resp_ungrouped_fast")
			bill := f.bill(t)
			// 2 actual input tokens and 1 output: ($10/M + $30/M) × 2.5.
			require.InDelta(t, .0001, bill.command.BalanceCost, 1e-12)
			require.NoError(t, f.client.Close(coderws.StatusNormalClosure, "done"))
			f.waitDone(t)
			close(bill.release)
			require.Eventually(t, func() bool { return f.cache.count() == 0 }, time.Second, time.Millisecond)
		})
	}
}

type wsUngroupedFastSettingRepo struct {
	reads   atomic.Int64
	changed atomic.Bool
}

func (r *wsUngroupedFastSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyOpenAIFastPolicySettings {
		return "", service.ErrSettingNotFound
	}
	r.reads.Add(1)
	if r.changed.Load() {
		return `{"rules":[]}`, nil
	}
	return `{"rules":[{"service_tier":"missing","action":"force_priority","scope":"all"}]}`, nil
}

func (r *wsUngroupedFastSettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *wsUngroupedFastSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, key := range keys {
		if key == service.SettingKeyOpenAIFastPolicySettings {
			value, err := r.GetValue(ctx, key)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
	}
	return values, nil
}

func (r *wsUngroupedFastSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *wsUngroupedFastSettingRepo) Set(context.Context, string, string) error {
	return errors.New("unexpected settings write in read-only WS fixture")
}

func (r *wsUngroupedFastSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return errors.New("unexpected settings write in read-only WS fixture")
}

func (r *wsUngroupedFastSettingRepo) Delete(context.Context, string) error {
	return errors.New("unexpected settings write in read-only WS fixture")
}

var _ service.SettingRepository = (*wsUngroupedFastSettingRepo)(nil)
