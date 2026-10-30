package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
)

type codexCLIHTTPBridgeRuntime interface {
	Status(context.Context) codexcli.Status
	Bridge(context.Context, codexcli.Status, *http.Request, func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error)
}

type managedCodexCLIHTTPBridgeRuntime struct{}

func isCodexCLIBridgeFailure(err error) bool {
	var bridgeErr *codexcli.BridgeError
	var cliErr *codexcli.TaskError
	return errors.Is(err, ErrCodexCLIBridgeSettingUnavailable) || errors.As(err, &bridgeErr) ||
		(errors.As(err, &cliErr) && strings.HasPrefix(cliErr.Code, "bridge_"))
}

func (managedCodexCLIHTTPBridgeRuntime) Status(ctx context.Context) codexcli.Status {
	return codexcli.DefaultManager().Status(ctx)
}

func (managedCodexCLIHTTPBridgeRuntime) Bridge(ctx context.Context, status codexcli.Status, request *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
	return codexcli.Bridge(ctx, status, request, forward)
}

// Resolve against the acquired credential owner, never an inbound flag or the
// selected shadow. Installation does not opt accounts in. A settings outage
// cannot silently bypass an account's explicitly configured transport.
func (s *OpenAIGatewayService) codexCLIBridgeEnabled(ctx context.Context, account *Account) (bool, error) {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return false, err
	}
	if ValidateCodexCLIAccount(owner) != nil {
		return false, nil
	}
	global, globalErr := s.settingService.GetCodexCLIBridgeEnabled(ctx)
	if globalErr == nil && !global {
		return false, nil
	}
	enabled, err := s.latestCodexCLIAccountBridgeEnabled(ctx, owner)
	if err != nil || !enabled {
		return false, err
	}
	return global, globalErr
}

// Read only the current transport permission. The acquired credential owner,
// tokens, identity and proxy remain the same snapshot for this send or turn.
// Scheduler snapshots can outlive an account toggle, so bypass their cache.
func (s *OpenAIGatewayService) latestCodexCLIAccountBridgeEnabled(ctx context.Context, owner *Account) (bool, error) {
	if s.accountRepo == nil {
		// Internal services constructed without repositories use their snapshot.
		return CodexCLITaskAccountEnabled(owner), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	current, err := s.accountRepo.GetByID(readCtx, owner.ID)
	if err != nil || current == nil || current.ID != owner.ID {
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
	if ValidateCodexCLIAccount(current) != nil {
		return false, nil
	}
	raw, exists := current.Credentials[CodexCLIConfigCredentialKey]
	if !exists {
		return false, nil
	}
	configuration, ok := raw.(map[string]any)
	if !ok {
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
	rawEnabled, exists := configuration["enabled"]
	if !exists {
		return false, nil
	}
	enabled, ok := rawEnabled.(bool)
	if !ok {
		return false, ErrCodexCLIBridgeSettingUnavailable
	}
	return enabled, nil
}

func (s *OpenAIGatewayService) doOpenAICodexCLIBridge(request *http.Request, proxyURL string, owner, selected *Account) (*http.Response, error) {
	runtime := s.codexCLIBridgeRuntime
	if runtime == nil {
		runtime = managedCodexCLIHTTPBridgeRuntime{}
	}
	status := runtime.Status(request.Context())
	if !status.ExecutionAvailable || !status.Installed || !status.SourceVerified || !status.VersionMatch {
		return nil, &codexcli.TaskError{Code: "bridge_runtime_unavailable"}
	}
	return runtime.Bridge(request.Context(), status, request, func(ctx context.Context, trusted *http.Request) (*http.Response, error) {
		// Bridge supplies the same trusted upstream request, not an incoming
		// relay URL/header set. Keep all native/plugin preparation and signing
		// declarations in the existing provider-bound transport.
		return s.doOpenAIPreparedTransport(trusted.WithContext(ctx), proxyURL, owner, selected)
	})
}
