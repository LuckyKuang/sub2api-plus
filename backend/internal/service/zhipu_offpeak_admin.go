package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

// ZhipuOffPeakSegment contains only opaque gateway-owned control handles.
type ZhipuOffPeakSegment struct {
	RecordID   string `json:"record_id"`
	Generation string `json:"generation"`
	State      string `json:"state"`
}

func (m *ZhipuOffPeakTicketManager) segmentPersistence(account *Account) (*zhipuOffPeakDurable, error) {
	if account == nil || account.ID <= 0 || account.Platform != PlatformZhipu || account.GetCredential("plan_kind") != ZhipuPlanOffPeak ||
		(account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey) {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", "off-peak segment requires a ZCode off-peak account")
	}
	if m == nil {
		return nil, offPeakDurableRequired()
	}
	d := m.persistence()
	if d == nil {
		return nil, offPeakDurableRequired()
	}
	return d, nil
}

func offPeakDurableRequired() error {
	return infraerrors.New(http.StatusNotImplemented, "ZHIPU_OFFPEAK_DURABLE_REQUIRED", "off-peak segment management requires durable persistence")
}

func (m *ZhipuOffPeakTicketManager) CurrentSegment(ctx context.Context, account *Account) (*ZhipuOffPeakSegment, error) {
	d, err := m.segmentPersistence(account)
	if err != nil {
		return nil, err
	}
	r, err := d.store.Current(ctx, account.ID)
	if errors.Is(err, ErrOffPeakRecordNotFound) {
		return nil, infraerrors.New(http.StatusNotFound, "ZHIPU_OFFPEAK_SEGMENT_NOT_FOUND", "off-peak segment does not exist")
	}
	if err != nil {
		return nil, offPeakStoreFailure()
	}
	return &ZhipuOffPeakSegment{RecordID: r.ID, Generation: r.Generation, State: r.State}, nil
}

func (m *ZhipuOffPeakTicketManager) TerminateSegment(ctx context.Context, account *Account, id, generation string) error {
	d, err := m.segmentPersistence(account)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(generation) == "" || len(id) > 128 || len(generation) > 128 {
		return infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_SEGMENT_INVALID", "record_id and generation are required")
	}
	_, err = d.store.Terminate(ctx, account.ID, id, generation)
	if errors.Is(err, ErrOffPeakFenceLost) || errors.Is(err, ErrOffPeakRecordNotFound) {
		return infraerrors.New(http.StatusConflict, "ZHIPU_OFFPEAK_SEGMENT_CONFLICT", "off-peak segment does not match the inspected account and generation")
	}
	if err != nil {
		return offPeakStoreFailure()
	}
	return nil
}
