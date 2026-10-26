//go:build unit || !integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
)

// These fakes implement the same owner-conditional business port as the SQL
// repository. Original recovery and quota assertions remain unchanged.
func quotaPersistenceOwnerMatches(actual, expected *Account) bool {
	if actual == nil || expected == nil || actual.ID != expected.ID || actual.Platform != expected.Platform || actual.Type != expected.Type || !reflect.DeepEqual(actual.ProxyID, expected.ProxyID) {
		return false
	}
	actualCredentials, actualErr := json.Marshal(actual.Credentials)
	expectedCredentials, expectedErr := json.Marshal(expected.Credentials)
	actualRoute, actualRouteErr := actual.StrictProxyURL()
	expectedRoute, expectedRouteErr := expected.StrictProxyURL()
	return actualErr == nil && expectedErr == nil && actualRouteErr == nil && expectedRouteErr == nil && bytes.Equal(actualCredentials, expectedCredentials) && actualRoute == expectedRoute
}

func (r *stubQuotaAccountRepo) UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	if !quotaPersistenceOwnerMatches(r.accounts[expected.ID], expected) {
		return false, nil
	}
	return true, r.UpdateCredentials(ctx, expected.ID, credentials)
}
