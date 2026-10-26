package repository

import (
	"context"
	"errors"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// The row comparison, proxy lock and durable invalidation are one SQL statement.
// A late probe cannot overwrite a replacement grant or publish stale scheduling
// state, including when the proxy record changes without changing its ID.
func (r *accountRepository) UpdateCNProviderQuotaIfUnchanged(ctx context.Context, expected *service.Account, updates map[string]any) (bool, error) {
	if r == nil || r.sql == nil || expected == nil {
		return false, errors.New("quota snapshot repository is not configured")
	}
	if expected.CredentialGeneration != "" || expected.CNQuotaGeneration != "" {
		return false, errors.New("quota snapshot mutation requires a hydrated credential owner")
	}
	if expected.Platform != service.PlatformKimi && expected.Platform != service.PlatformZhipu && expected.Platform != service.PlatformMiniMax {
		return false, errors.New("unsupported quota snapshot platform")
	}
	return r.UpdateCNControlExtraIfUnchanged(ctx, expected, updates)
}
