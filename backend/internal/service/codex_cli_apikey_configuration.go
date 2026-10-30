package service

import (
	"context"
	"net/http"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

type CodexCLIAPIKeyConfigurationRepository interface {
	UpdateCodexCLIAPIKeyConfigurationIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
}
type CodexCLIAPIKeyConfigurationCommitter interface {
	CommitCodexCLIAPIKeyConfiguration(context.Context, *Account, map[string]any) (*Account, error)
}

func (s *adminServiceImpl) CommitCodexCLIAPIKeyConfiguration(ctx context.Context, expected *Account, credentials map[string]any) (*Account, error) {
	if err := ValidateCodexCLIAccount(expected); err != nil {
		return nil, err
	}
	if expected.Type != AccountTypeAPIKey {
		return nil, codexCLIConfigError("API Key configuration owner is required")
	}
	if err := NormalizeCodexCLIAccountCredentials(expected.Platform, expected.Type, credentials); err != nil {
		return nil, err
	}
	if err := ValidateCodexCLIEnablement(ctx, expected.Credentials, credentials); err != nil {
		return nil, err
	}
	writer, ok := s.accountRepo.(CodexCLIAPIKeyConfigurationRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("CODEX_CLI_PERSISTENCE_UNAVAILABLE", "atomic CLI configuration persistence is unavailable")
	}
	applied, err := writer.UpdateCodexCLIAPIKeyConfigurationIfUnchanged(ctx, expected, credentials)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, infraerrors.New(http.StatusConflict, "CODEX_CLI_OWNER_CHANGED", "account credentials or route changed; reload the CLI configuration")
	}
	return s.accountRepo.GetByID(ctx, expected.ID)
}
