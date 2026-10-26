//go:build unit

package service

import "context"

// Request-token tests retain their original rotation counters and expectations
// while exercising the complete owner-conditional production persistence port.
func (r *tokenRefreshAccountRepo) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, _ string, credentials, _ map[string]any, _ bool) (bool, error) {
	if !testGrokOutcomeOwnersEqual(r.accountsByID[expected.ID], expected) {
		return false, nil
	}
	return r.UpdateGrokOAuthCredentialsIfUnchanged(ctx, expected, credentials)
}

func (r *refreshAPIAccountRepo) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, _ string, credentials, _ map[string]any, _ bool) (bool, error) {
	if !testGrokOutcomeOwnersEqual(activeRefreshAPITestAccount(r.account), expected) {
		return false, nil
	}
	return r.UpdateGrokOAuthCredentialsIfUnchanged(ctx, expected, credentials)
}
