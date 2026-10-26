package repository

import "github.com/LuckyKuang/sub2api-plus/internal/service"

// Runtime recovery is a repository-owned value. Account imports and ordinary
// metadata writers cannot choose a generation, including through stale DTOs.
func withoutRuntimeRecoveryGeneration(extra map[string]any) map[string]any {
	clean := copyJSONMap(normalizeJSONMap(extra))
	delete(clean, service.RuntimeRecoveryGenerationExtraKey)
	return clean
}

const newRuntimeRecoveryGenerationSQL = `jsonb_build_object('runtime_recovery_generation', gen_random_uuid()::text)`
