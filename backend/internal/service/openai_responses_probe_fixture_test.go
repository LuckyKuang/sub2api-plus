//go:build unit || !integration

package service

import (
	"context"
	"reflect"
)

// The capability fixtures model the repository's atomic owner-conditioned
// observation port. Expected verdicts remain literal in the calling tests.
type responsesProbeTestRepo struct {
	stubOpenAIAccountRepo
	updateExtraCalls chan map[string]any
}

func (r *responsesProbeTestRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	current, err := r.GetByID(ctx, expected.ID)
	if err != nil {
		return false, err
	}
	if current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.Credentials, expected.Credentials) || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) || !reflect.DeepEqual(current.Proxy, expected.Proxy) {
		return false, nil
	}
	if r.updateExtraCalls != nil {
		copied := make(map[string]any, len(updates))
		for key, value := range updates {
			copied[key] = value
		}
		r.updateExtraCalls <- copied
	}
	return true, nil
}

func (r *responsesProbeTestRepo) ApplyCredentialStateMutation(ctx context.Context, expected *Account, mutation CredentialStateMutation) (bool, error) {
	return r.UpdateCNControlExtraIfUnchanged(ctx, expected, mutation.Extra)
}
