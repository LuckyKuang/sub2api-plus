//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type offPeakAdminFailureStore struct {
	ZhipuOffPeakStore
	err error
}

func (s offPeakAdminFailureStore) Current(context.Context, int64) (ZhipuOffPeakRecord, error) {
	return ZhipuOffPeakRecord{}, s.err
}
func (s offPeakAdminFailureStore) Terminate(context.Context, int64, string, string) (ZhipuOffPeakRecord, error) {
	return ZhipuOffPeakRecord{}, s.err
}

func TestZhipuOffPeakAdminSegmentRejectsUnsupportedModeAndProfile(t *testing.T) {
	for _, name := range []string{"memory-mode", "nil-account", "invalid-id", "other-platform", "coding-plan", "unsupported-type"} {
		t.Run(name, func(t *testing.T) {
			m := newOffPeakDurableTestManager(t, &zhipuOffPeakStub{}, &offPeakMemoryStore{})
			a := offPeakDurableAccount()
			want := http.StatusBadRequest
			switch name {
			case "memory-mode":
				m.durable = nil
				want = http.StatusNotImplemented
			case "nil-account":
				a = nil
			case "invalid-id":
				a.ID = 0
			case "other-platform":
				a.Platform = PlatformStepFun
			case "coding-plan":
				a.Credentials["plan_kind"] = ZhipuPlanIndividualCodingPlan
			case "unsupported-type":
				a.Type = "upstream"
			}
			segment, err := m.CurrentSegment(context.Background(), a)
			require.Nil(t, segment)
			require.Equal(t, want, infraerrors.Code(err))
			require.Equal(t, want, infraerrors.Code(m.TerminateSegment(context.Background(), a, "record", "generation")))
		})
	}
}

func TestZhipuOffPeakAdminSegmentMapsErrorsWithoutSecretDisclosure(t *testing.T) {
	for _, name := range []string{"not-found", "database-failure", "fence-conflict", "empty-record", "empty-generation"} {
		t.Run(name, func(t *testing.T) {
			m := newOffPeakDurableTestManager(t, &zhipuOffPeakStub{}, &offPeakMemoryStore{})
			failure := errors.New("postgres password=secret-token proxy=http://secret-proxy")
			if name == "not-found" {
				failure = ErrOffPeakRecordNotFound
			}
			if name == "fence-conflict" {
				failure = ErrOffPeakFenceLost
			}
			m.durable.store = offPeakAdminFailureStore{err: failure}
			if name == "not-found" || name == "database-failure" {
				segment, err := m.CurrentSegment(context.Background(), offPeakDurableAccount())
				require.Nil(t, segment)
				want := http.StatusServiceUnavailable
				if name == "not-found" {
					want = http.StatusNotFound
				}
				require.Equal(t, want, infraerrors.Code(err))
				require.NotContains(t, err.Error(), "secret")
			}
			record, generation := "record", "generation"
			want := http.StatusServiceUnavailable
			switch name {
			case "not-found", "fence-conflict":
				want = http.StatusConflict
			case "empty-record":
				record = " "
				want = http.StatusBadRequest
			case "empty-generation":
				generation = " "
				want = http.StatusBadRequest
			}
			err := m.TerminateSegment(context.Background(), offPeakDurableAccount(), record, generation)
			require.Equal(t, want, infraerrors.Code(err))
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestZhipuOffPeakAdminTerminateDrainsAndRetriesOriginalSnapshot(t *testing.T) {
	store := &offPeakMemoryStore{}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "private-remote-ticket", State: zcode.TicketReady}}}
	m := newOffPeakDurableTestManager(t, client, store)
	a := offPeakDurableAccount()
	proxyID := int64(9001)
	a.ProxyID = &proxyID
	a.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "original-proxy", Port: 8080, Status: StatusActive}
	ticket, err := m.Acquire(context.Background(), a, zhipuOffPeakTestAuth(), "http://original-proxy:8080")
	require.NoError(t, err)
	segment, err := m.CurrentSegment(context.Background(), a)
	require.NoError(t, err)
	payload, err := json.Marshal(segment)
	require.NoError(t, err)
	require.JSONEq(t, `{"record_id":"`+store.record.ID+`","generation":"`+store.record.Generation+`","state":"ready"}`, string(payload))
	require.NotContains(t, string(payload), "private-remote-ticket")
	require.NoError(t, m.TerminateSegment(context.Background(), a, segment.RecordID, segment.Generation))
	require.NoError(t, m.TerminateSegment(context.Background(), a, segment.RecordID, segment.Generation))
	require.Empty(t, client.settleAttempts, "accepted only acknowledges durable intent")
	m.sweepDurable(m.persistence())
	require.Empty(t, client.settleAttempts, "already admitted physical requests drain first")
	ticket.Release()
	client.settleEr = errors.New("temporary failure")
	m.sweepDurable(m.persistence())
	require.Equal(t, []string{"private-remote-ticket"}, client.settleAttempts)
	require.True(t, store.record.SettlementPending)
	require.NotEmpty(t, store.record.Snapshot)
	client.settleEr = nil
	m.sweepDurable(m.persistence())
	require.Equal(t, []string{"private-remote-ticket", "private-remote-ticket"}, client.settleAttempts)
	require.Equal(t, []string{"http://original-proxy:8080"}, client.settleProxies)
	require.Equal(t, []zcode.OffPeakAuth{zhipuOffPeakTestAuth()}, client.settleAuths)
	require.True(t, store.record.Closed)
	require.Empty(t, store.record.Snapshot)
	m.sweepDurable(m.persistence())
	require.Len(t, client.settleAttempts, 2)
	require.Equal(t, 1, client.takeCalls)
	require.NoError(t, m.TerminateSegment(context.Background(), a, segment.RecordID, segment.Generation))
}
