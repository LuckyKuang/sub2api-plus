package service

import (
	"context"
	"sync"
	"time"
)

type inflightSnapshotLookupKey struct {
	groupID  int64
	hasGroup bool
	platform string
	forced   bool
}

type inflightSnapshotLookupResult struct {
	accounts []Account
	err      error
}

type inflightSnapshotLookup struct {
	mu      sync.Mutex
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	results map[inflightSnapshotLookupKey]inflightSnapshotLookupResult
}

type inflightSnapshotLookupContextKey struct{}

// One estimate can evaluate multiple tiers, efforts and fallback models. Their
// scheduler evidence shares one lazy two-second budget and one result per pool,
// including failed reads. No result survives the estimate that requested it.
func withInflightSnapshotLookup(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(inflightSnapshotLookupContextKey{}).(*inflightSnapshotLookup); ok {
		return ctx, func() {}
	}
	lookup := &inflightSnapshotLookup{results: make(map[inflightSnapshotLookupKey]inflightSnapshotLookupResult)}
	ctx = context.WithValue(ctx, inflightSnapshotLookupContextKey{}, lookup)
	lookup.parent = ctx
	return ctx, func() {
		lookup.mu.Lock()
		defer lookup.mu.Unlock()
		if lookup.cancel != nil {
			lookup.cancel()
		}
	}
}

func (m *inflightSnapshotLookup) contextLocked() context.Context {
	if m.ctx == nil {
		m.ctx, m.cancel = context.WithTimeout(m.parent, 2*time.Second)
	}
	return m.ctx
}

func inflightSnapshotLookupContext(ctx context.Context) context.Context {
	lookup, _ := ctx.Value(inflightSnapshotLookupContextKey{}).(*inflightSnapshotLookup)
	if lookup == nil {
		return ctx
	}
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	return lookup.contextLocked()
}

func listInflightSnapshotAccounts(ctx context.Context, snapshot *SchedulerSnapshotService, groupID *int64, platform string, forced bool) ([]Account, error) {
	lookup, _ := ctx.Value(inflightSnapshotLookupContextKey{}).(*inflightSnapshotLookup)
	if lookup == nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		accounts, _, err := snapshot.ListSchedulableAccounts(lookupCtx, groupID, platform, forced)
		return accounts, err
	}
	key := inflightSnapshotLookupKey{platform: platform, forced: forced, hasGroup: groupID != nil}
	if groupID != nil {
		key.groupID = *groupID
	}
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	if result, ok := lookup.results[key]; ok {
		return result.accounts, result.err
	}
	accounts, _, err := snapshot.ListSchedulableAccounts(lookup.contextLocked(), groupID, platform, forced)
	lookup.results[key] = inflightSnapshotLookupResult{accounts: accounts, err: err}
	return accounts, err
}
