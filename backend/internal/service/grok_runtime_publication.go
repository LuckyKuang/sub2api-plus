package service

import "sync"

type grokRuntimePublicationState struct {
	mu    sync.Mutex
	epoch uint64
	refs  int
}

var grokRuntimePublications = struct {
	sync.Mutex
	accounts map[int64]*grokRuntimePublicationState
}{accounts: make(map[int64]*grokRuntimePublicationState)}

// References exist only while an observation or reset is in flight. Retaining
// the state until the last reference leaves preserves reset ordering without
// accumulating permanent per-account tombstones.
func acquireGrokRuntimePublication(accountID int64) (*grokRuntimePublicationState, func()) {
	grokRuntimePublications.Lock()
	state := grokRuntimePublications.accounts[accountID]
	if state == nil {
		state = &grokRuntimePublicationState{}
		grokRuntimePublications.accounts[accountID] = state
	}
	state.refs++
	grokRuntimePublications.Unlock()
	return state, func() {
		grokRuntimePublications.Lock()
		defer grokRuntimePublications.Unlock()
		state.refs--
		if state.refs == 0 {
			delete(grokRuntimePublications.accounts, accountID)
		}
	}
}
