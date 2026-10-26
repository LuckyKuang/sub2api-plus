package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const CredentialUnavailableClientMessage = "No healthy upstream account credential is currently available"

func usesHTTP401CredentialFence(account *Account) bool {
	if account == nil {
		return false
	}
	switch account.Platform {
	case PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu:
		return true
	default:
		return false
	}
}

// HTTP authentication failures carry the grant selected before transport.
// Quarantine and its process-local notification must both wait for the exact
// credential CAS; a late 401 cannot disable a concurrent reauthorization.
func (s *RateLimitService) handleHTTP401CredentialError(ctx context.Context, selected, owner *Account, message string) {
	if owner == nil {
		return
	}
	if !usesHTTP401CredentialFence(owner) {
		s.handleAuthError(ctx, owner, message)
		return
	}
	if selected != nil && (selected.IsShadow() || requestCredentialOwnerBound(ctx, selected)) {
		frozen, ok := requestCredentialOwner(ctx, selected)
		if !ok || frozen.ID != owner.ID {
			slog.Warn("http_401_credential_snapshot_unavailable", "account_id", owner.ID)
			return
		}
		owner = frozen
	}
	repo, ok := s.accountRepo.(credentialFailureRepository)
	if !ok {
		slog.Warn("http_401_credential_quarantine_unavailable", "account_id", owner.ID)
		return
	}
	applied, err := repo.SetCredentialErrorIfUnchanged(ctx, owner, message)
	if err != nil {
		slog.Warn("http_401_credential_quarantine_failed", "account_id", owner.ID, "error", err)
		return
	}
	if !applied {
		slog.Info("http_401_credential_quarantine_superseded", "account_id", owner.ID)
		return
	}
	if owner.Type == AccountTypeOAuth && s.tokenCacheInvalidator != nil {
		if err := s.tokenCacheInvalidator.InvalidateToken(ctx, owner); err != nil {
			slog.Warn("oauth_401_invalidate_cache_failed", "account_id", owner.ID, "error", err)
		}
	}
	s.notifyAccountSchedulingBlocked(owner, time.Time{}, "auth_error")
	slog.Warn("account_disabled_auth_error", "account_id", owner.ID, "reason", "http_401")
}

// CredentialFailureError preserves the distinction between an unusable account
// grant and a shared authentication dependency. It never exposes its cause to
// clients. Providers may attach the exact failed refresh snapshot for fencing.
type CredentialFailureError struct {
	Scope     GatewayFailureScope
	Reason    GatewayFailureReason
	Permanent bool
	Cause     error
	account   *Account
}

func (e *CredentialFailureError) Error() string {
	if e.Cause != nil {
		return string(e.Reason) + ": " + e.Cause.Error()
	}
	return string(e.Reason)
}
func (e *CredentialFailureError) Unwrap() error { return e.Cause }

func NewAccountCredentialFailure(reason GatewayFailureReason, cause error) error {
	return &CredentialFailureError{Scope: GatewayFailureScopeAccount, Reason: reason, Permanent: true, Cause: cause}
}

func NewProviderCredentialFailure(reason GatewayFailureReason, cause error) error {
	return &CredentialFailureError{Scope: GatewayFailureScopeProvider, Reason: reason, Cause: cause}
}

// WithCredentialFailureAccount records the credentials actually rejected by a
// refresh operation, rather than an older request-selection snapshot.
func WithCredentialFailureAccount(err error, account *Account) error {
	var failure *CredentialFailureError
	if !errors.As(err, &failure) || account == nil {
		return err
	}
	copy := *failure
	copy.account = snapshotRequestCredentialAccount(account)
	return &copy
}

type credentialFailureRepository interface {
	SetCredentialErrorIfUnchanged(context.Context, *Account, string) (bool, error)
}

type credentialCooldownRepository interface {
	SetCredentialTempUnschedulableIfUnchanged(context.Context, *Account, time.Time, string) (bool, error)
}

func (s *RateLimitService) handleHTTP401CredentialCooldown(ctx context.Context, selected, owner *Account, until time.Time, message string) {
	if owner == nil {
		return
	}
	if selected != nil && (selected.IsShadow() || requestCredentialOwnerBound(ctx, selected)) {
		frozen, ok := requestCredentialOwner(ctx, selected)
		if !ok || frozen.ID != owner.ID {
			slog.Warn("http_401_credential_snapshot_unavailable", "account_id", owner.ID)
			return
		}
		owner = frozen
	}
	repo, ok := s.accountRepo.(credentialCooldownRepository)
	if !ok {
		slog.Warn("http_401_credential_cooldown_unavailable", "account_id", owner.ID)
		return
	}
	applied, err := repo.SetCredentialTempUnschedulableIfUnchanged(ctx, owner, until, message)
	if err != nil {
		slog.Warn("http_401_credential_cooldown_failed", "account_id", owner.ID, "error", err)
		return
	}
	if !applied {
		slog.Info("http_401_credential_cooldown_superseded", "account_id", owner.ID)
		return
	}
	if s.tokenCacheInvalidator != nil {
		if err := s.tokenCacheInvalidator.InvalidateToken(ctx, owner); err != nil {
			slog.Warn("oauth_401_invalidate_cache_failed", "account_id", owner.ID, "error", err)
		}
	}
	s.notifyAccountSchedulingBlocked(owner, until, "oauth_401")
}

// accountCredentialFailure converts a pre-send credential failure into the same
// explicit failover decision consumed by HTTP and WebSocket request handlers.
// Permanent quarantine uses a single conditional write, including token
// version, so a concurrent refresh/reauthorization cannot be disabled.
func (s *OpenAIGatewayService) accountCredentialFailure(ctx context.Context, c *gin.Context, account *Account, err error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err == nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	var existing *UpstreamFailoverError
	if errors.As(err, &existing) {
		return err
	}
	var failure *CredentialFailureError
	if !errors.As(err, &failure) {
		return err
	}
	scope, reason := failure.Scope, failure.Reason
	action := NextAccountStop
	if scope == GatewayFailureScopeAccount {
		action = NextAccountRetry
	}
	if failure.Permanent && account != nil && s != nil && s.accountRepo != nil {
		failed := account
		if failure.account != nil {
			failed = failure.account
		}
		repo, ok := s.accountRepo.(credentialFailureRepository)
		if !ok {
			scope, reason, action = GatewayFailureScopeProvider, "credential_state_unavailable", NextAccountStop
		} else {
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			applied, mutationErr := repo.SetCredentialErrorIfUnchanged(writeCtx, failed, string(reason))
			cancel()
			if mutationErr != nil {
				scope, reason, action = GatewayFailureScopeProvider, "credential_state_unavailable", NextAccountStop
			} else if applied {
				s.BlockAccountScheduling(failed, time.Time{}, string(reason))
			}
		}
	}
	failover := &UpstreamFailoverError{
		StatusCode: http.StatusServiceUnavailable, Stage: GatewayFailureStageAccountAuth,
		Scope: scope, Reason: reason, NextAccountAction: action,
		ClientStatusCode: http.StatusServiceUnavailable, ClientMessage: CredentialUnavailableClientMessage,
	}
	if c != nil && account != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			Kind: "credential_error", Scope: string(scope), Reason: string(reason), Message: CredentialUnavailableClientMessage,
		})
	}
	return failover
}

func classifyCredentialAcquisitionFailure(account *Account, err error) error {
	if err == nil || account == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var typed *CredentialFailureError
	var failover *UpstreamFailoverError
	if errors.As(err, &typed) || errors.As(err, &failover) {
		return err
	}
	message := strings.ToLower(err.Error())
	if strings.HasPrefix(message, "unsupported account type:") {
		return err
	}
	if strings.Contains(message, "invalid_grant") || strings.Contains(message, "token_revoked") ||
		strings.Contains(message, "refresh_token is missing") || strings.Contains(message, "not found in credentials") ||
		strings.Contains(message, "access_token not found") || strings.Contains(message, "missing api_key") {
		return NewAccountCredentialFailure("account_credential_invalid", err)
	}
	return NewProviderCredentialFailure("credential_provider_unavailable", err)
}
