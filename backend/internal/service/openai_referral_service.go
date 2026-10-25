package service

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

const (
	openAIReferralSnapshotKey = "codex_referral_snapshot"
	openAIReferralConsumer    = "codex_referral_consumer"
	openAIReferralWorkspace   = "codex_referral_workspace"
)

type OpenAIReferralGrant struct {
	GrantType string  `json:"grant_type"`
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

type OpenAIReferralEligibility struct {
	ShouldShow                   bool                  `json:"should_show"`
	RemainingSendCapacity        *int                  `json:"remaining_send_capacity"`
	RemainingRewardCapacity      *int                  `json:"remaining_reward_capacity"`
	RequiresExplicitConfirmation *bool                 `json:"requires_explicit_confirmation"`
	OfferID                      string                `json:"offer_id,omitempty"`
	Title                        string                `json:"title,omitempty"`
	Description                  string                `json:"description,omitempty"`
	Rules                        []string              `json:"rules,omitempty"`
	Grants                       []OpenAIReferralGrant `json:"grants,omitempty"`
	ProgramID                    string                `json:"program_id"`
	AvailableInvites             *int                  `json:"available_invites"`
	FetchedAt                    int64                 `json:"fetched_at"`
	credentialOwner              *Account
}

// referralCapacity follows the desktop's send/reward capacity constraints.
// The desktop additionally caps one batch at five; this API sends one email.
func referralCapacity(e *OpenAIReferralEligibility) *int {
	if !e.ShouldShow {
		zero := 0
		return &zero
	}
	if e.RemainingSendCapacity == nil {
		return nil
	}
	count := max(0, *e.RemainingSendCapacity)
	if len(e.Grants) > 0 || (e.OfferID != "" && e.OfferID != "none") {
		if e.RemainingRewardCapacity == nil {
			return nil
		}
		count = min(count, max(0, *e.RemainingRewardCapacity))
	}
	return &count
}

func (s *OpenAIQuotaService) prepareReferralOperation(ctx context.Context, id int64, sending bool) (*openAIQuotaOperation, error) {
	if s == nil || s.accountRepo == nil || s.referralClient == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "OPENAI_REFERRAL_NOT_CONFIGURED", "referral service is unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_INVALID_ACCOUNT", "referrals require an OpenAI OAuth account")
	}
	if sending && account.IsShadow() {
		return nil, infraerrors.New(http.StatusConflict, "OPENAI_REFERRAL_SHADOW_ACCOUNT", "send invitations from the parent account")
	}
	return s.prepareUpstreamAccountCall(ctx, account)
}

func referralProgram(a *Account) string {
	if strings.EqualFold(a.GetCredential("account_type"), "workspace") {
		return openAIReferralWorkspace
	}
	switch strings.ToLower(strings.TrimSpace(a.GetCredential("plan_type"))) {
	case "team", "business", "self_serve_business_prolite", "self_serve_business_usage_based", "free_workspace", "enterprise", "enterprise_cbp_usage_based", "enterprise_cbp_automation", "edu", "education":
		return openAIReferralWorkspace
	default:
		return openAIReferralConsumer
	}
}

func (s *OpenAIQuotaService) referralCall(operation *openAIQuotaOperation) (OpenAIReferralCall, error) {
	headers, _, err := s.buildCodexQuotaHeaders(operation.ctx, operation)
	if err != nil {
		return OpenAIReferralCall{}, infraerrors.New(http.StatusBadGateway, "OPENAI_REFERRAL_AUTH_ERROR", "failed to authenticate referral request")
	}
	return OpenAIReferralCall{ProxyURL: operation.proxyURL, Headers: headers, ProgramID: referralProgram(operation.owner)}, nil
}

func (s *OpenAIQuotaService) QueryReferralEligibility(ctx context.Context, id int64) (*OpenAIReferralEligibility, error) {
	operation, err := s.prepareReferralOperation(ctx, id, false)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(operation.ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	return s.queryReferralEligibility(callCtx, operation)
}

func (s *OpenAIQuotaService) queryReferralEligibility(ctx context.Context, operation *openAIQuotaOperation) (*OpenAIReferralEligibility, error) {
	call, err := s.referralCall(operation)
	if err != nil {
		return nil, err
	}
	result, err := s.referralClient.QueryEligibility(ctx, call)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_REFERRAL_INVALID_RESPONSE", "invalid invitation eligibility response")
	}
	result.ProgramID = call.ProgramID
	result.AvailableInvites = referralCapacity(result)
	result.FetchedAt = time.Now().Unix()
	result.credentialOwner = snapshotRequestCredentialAccount(operation.owner)
	return result, nil
}

func (s *OpenAIQuotaService) CacheReferralSnapshot(ctx context.Context, id int64, eligibility *OpenAIReferralEligibility) error {
	if eligibility == nil {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_REFERRAL_INVALID_RESPONSE", "invalid invitation eligibility response")
	}
	guarded, ok := s.accountRepo.(OpenAICodexUsageRepository)
	if !ok || eligibility.credentialOwner == nil {
		return infraerrors.New(http.StatusConflict, "OPENAI_REFERRAL_OWNER_CHANGED", "invitation credential owner is unavailable")
	}
	accepted, err := guarded.UpdateOpenAICodexUsageIfUnchanged(ctx, id, eligibility.credentialOwner, map[string]any{openAIReferralSnapshotKey: eligibility})
	if err != nil || !accepted {
		return infraerrors.New(http.StatusConflict, "OPENAI_REFERRAL_OWNER_CHANGED", "invitation credential owner changed before caching").WithCause(err)
	}
	return nil
}

type OpenAIReferralSendRequest struct {
	Email     string `json:"email"`
	ProgramID string `json:"program_id"`
	Confirmed bool   `json:"confirmed"`
}

type OpenAIReferralSendResult struct {
	Email string `json:"email"`
	Sent  bool   `json:"sent"`
}

func (s *OpenAIQuotaService) SendReferralInvite(ctx context.Context, id int64, input OpenAIReferralSendRequest) (*OpenAIReferralSendResult, error) {
	email := strings.TrimSpace(input.Email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_INVALID_EMAIL", "enter one valid email address")
	}
	operation, err := s.prepareReferralOperation(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if input.ProgramID != referralProgram(operation.owner) {
		return nil, infraerrors.New(http.StatusConflict, "OPENAI_REFERRAL_PROGRAM_CHANGED", "refresh invitation eligibility before sending")
	}
	// Recheck the server's current eligibility rather than trusting a UI cache.
	callCtx, cancel := context.WithTimeout(operation.ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	eligibility, err := s.queryReferralEligibility(callCtx, operation)
	if err != nil {
		return nil, err
	}
	if !eligibility.ShouldShow || eligibility.AvailableInvites == nil || *eligibility.AvailableInvites <= 0 {
		return nil, infraerrors.New(http.StatusConflict, "OPENAI_REFERRAL_UNAVAILABLE", "no invitations are currently available")
	}
	if (eligibility.RequiresExplicitConfirmation == nil || *eligibility.RequiresExplicitConfirmation) && !input.Confirmed {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_CONFIRMATION_REQUIRED", "confirm the recipient's consent before sending")
	}
	call, err := s.referralCall(operation)
	if err != nil {
		return nil, err
	}
	if err := s.referralClient.SendInvite(callCtx, call, email); err != nil {
		return nil, err
	}
	return &OpenAIReferralSendResult{Email: email, Sent: true}, nil
}
