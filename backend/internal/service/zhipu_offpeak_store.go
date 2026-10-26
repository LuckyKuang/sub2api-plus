package service

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOffPeakLeaseBusy      = errors.New("off-peak operation is owned by another instance")
	ErrOffPeakFenceLost      = errors.New("off-peak operation lease is no longer current")
	ErrOffPeakRecordNotFound = errors.New("off-peak segment does not exist")
)

// ZhipuOffPeakRecord contains admission control data only. Snapshot is AEAD
// ciphertext; no prompt, response, plaintext token or proxy secret is stored.
type ZhipuOffPeakRecord struct {
	ID, Generation, TaskID, TicketID, State        string
	AccountID                                      int64
	KeyID                                          string
	Snapshot                                       []byte
	Position                                       *int
	Retired, Closed, SettlementPending             bool
	Fence                                          int64
	LeaseID                                        string
	LeaseUntil, LastUsed, RetainUntil, NextAttempt time.Time
	Attempts                                       int
	LastError                                      string
}

// ZhipuOffPeakStore is PostgreSQL's authority, including the settlement outbox.
// Implementations must commit intent before network I/O and enforce fencing on
// every result. Redis/local memory cannot grant admission.
type ZhipuOffPeakStore interface {
	Current(context.Context, int64) (ZhipuOffPeakRecord, error)
	Terminate(context.Context, int64, string, string) (ZhipuOffPeakRecord, error)
	Open(context.Context, *Account, ZhipuOffPeakRecord) (ZhipuOffPeakRecord, error)
	Claim(context.Context, string, bool, time.Duration, time.Duration) (ZhipuOffPeakRecord, error)
	Save(context.Context, ZhipuOffPeakRecord) error
	BeginTake(context.Context, ZhipuOffPeakRecord) error
	Admit(context.Context, ZhipuOffPeakRecord, *Account, string, time.Duration) error
	RenewRequest(context.Context, string, string, time.Duration) error
	ReleaseRequest(context.Context, string, string) error
	Due(context.Context, time.Duration, int) ([]string, error)
	RegisterLateTicket(context.Context, ZhipuOffPeakRecord) error
}
