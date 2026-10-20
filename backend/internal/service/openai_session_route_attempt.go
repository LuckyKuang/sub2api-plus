package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type openAISessionRouteAttemptKey struct{}
type openAISessionRouteSelectionKey struct{}
type openAISessionRecoveryKey struct{}

type openAISessionRecovery struct {
	accountID int64
	retry     bool
}

// Protocol readers use the request context for route commits. Attach only the
// selected attempt and identity scope, preserving the original cancellation.
// Forward-only cancellation must never poison the handler's next recovery.
func bindOpenAISessionRouteRequestContext(ctx context.Context, c *gin.Context) func() {
	a := openAIRouteAttempt(ctx)
	if a == nil || c == nil || c.Request == nil {
		return func() {}
	}
	original := c.Request.Context()
	bound := context.WithValue(original, openAISessionRouteAttemptKey{}, a)
	c.Request = c.Request.WithContext(carryOutboundIdentityScope(bound, ctx))
	return func() { c.Request = c.Request.WithContext(original) }
}

// WithOpenAISessionRecovery pins a recoverable retry to its original account.
// Only an exhausted account-scoped recovery may authorize a different owner.
func WithOpenAISessionRecovery(ctx context.Context, accountID int64, retry bool) context.Context {
	if retry {
		if previous := openAIRouteAttempt(ctx); previous != nil {
			previous.mu.Lock()
			if previous.accountID == accountID && previous.closed {
				next := &openAISessionRouteAttempt{store: previous.store, route: previous.route, accountID: accountID, reason: previous.reason, logCtx: ctx}
				ctx = context.WithValue(ctx, openAISessionRouteAttemptKey{}, next)
			}
			previous.mu.Unlock()
		}
	}
	return context.WithValue(ctx, openAISessionRecoveryKey{}, openAISessionRecovery{accountID: accountID, retry: retry})
}

func openAISessionRouteForwardError(ctx context.Context, err error) error {
	if a := openAIRouteAttempt(ctx); a != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.renewErr != nil {
			return a.renewErr
		}
		var failure *UpstreamFailoverError
		if a.apiKey && errors.As(err, &failure) && failure.StatusCode == http.StatusTooManyRequests &&
			failure.ShouldRetryNextAccount() && failure.ShouldReportAccountScheduleFailure() {
			disposition, _ := classifyOpenAIOAuth429(failure.ResponseHeaders, failure.ResponseBody)
			if disposition == openAIOAuth429Transient {
				failure.RetryableOnSameAccount = true
				failure.SameAccountRetryDelay = openAIOAuth429RetryDelay
				if reset := parseRetryAfterResetTime(failure.ResponseHeaders, time.Now()); reset != nil {
					delay := time.Until(*reset)
					if delay > openAIOAuth429MaxRetryDelay {
						// A long provider backoff returns a retryable response to
						// the client; it cannot authorize scanning other owners.
						failure.RetryableOnSameAccount = false
						failure.NextAccountAction = NextAccountStop
					} else if delay > failure.SameAccountRetryDelay {
						failure.SameAccountRetryDelay = delay
					}
				}
			}
		}
	}
	return err
}

func IsOpenAISessionRouteError(err error) bool {
	return errors.Is(err, ErrOpenAISessionRouteStoreUnavailable) || errors.Is(err, ErrOpenAISessionRouteLeaseLost) || errors.Is(err, ErrOpenAISessionRouteNotAccepted)
}

func openAISessionTakeoverPending(ctx context.Context) bool {
	if a := openAIRouteAttempt(ctx); a != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		return !a.committed && a.route.OwnerAccountID != a.accountID
	}
	return false
}

// One attempt retains the generation observed at selection. Neither admission
// nor a late response may read a newer generation to acquire overwrite rights.
type openAISessionRouteAttempt struct {
	mu             sync.Mutex
	store          OpenAISessionRouteStore
	route          OpenAISessionRoute
	accountID      int64
	apiKey         bool
	reason         string
	leaseID        string
	committed      bool
	closed         bool
	renewErr       error
	forwardStarted bool
	accepted       chan struct{}
	leaseContext   context.Context
	leaseCancel    context.CancelCauseFunc
	logCtx         context.Context
}

func openAIRouteAttempt(ctx context.Context) *openAISessionRouteAttempt {
	if ctx == nil || openAISessionRouteDisabled(ctx) {
		return nil
	}
	a, _ := ctx.Value(openAISessionRouteAttemptKey{}).(*openAISessionRouteAttempt)
	return a
}

func (a *openAISessionRouteAttempt) current(ctx context.Context) (OpenAISessionRoute, error) {
	route, err := a.store.Get(ctx, a.route.AuthzDomain, a.route.SessionDigest)
	if errors.Is(err, ErrOpenAISessionRouteNotFound) {
		return route, ErrOpenAISessionRouteLeaseLost
	}
	if err != nil && !IsOpenAISessionRouteError(err) {
		return route, fmt.Errorf("%w: read", ErrOpenAISessionRouteStoreUnavailable)
	}
	return route, err
}

func (a *openAISessionRouteAttempt) admit(ctx context.Context, accountID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if (a.closed && !a.committed) || a.accountID != accountID {
		return ErrOpenAISessionRouteLeaseLost
	}
	if a.committed {
		return nil
	}
	if a.leaseID != "" {
		current, err := a.current(ctx)
		if err != nil {
			return err
		}
		if current.Generation != a.route.Generation || current.LeaseID != a.leaseID || !openAISessionRouteLeaseActive(current, time.Now()) {
			return ErrOpenAISessionRouteLeaseLost
		}
		return nil
	}
	if a.route.OwnerAccountID == accountID {
		current, err := a.current(ctx)
		if err != nil {
			return err
		}
		if current.Generation != a.route.Generation || current.OwnerAccountID != accountID || openAISessionRouteLeaseActive(current, time.Now()) {
			return ErrOpenAISessionRouteLeaseLost
		}
		return nil
	}
	a.leaseID = openAISessionRouteLeaseID()
	_, err := a.store.BeginMigration(ctx, a.route.AuthzDomain, a.route.SessionDigest, a.route.Generation, accountID, a.leaseID, a.reason, time.Minute)
	if err != nil {
		a.leaseID = ""
		return err
	}
	a.log(ctx, "leased", a.reason)
	return nil
}

func (a *openAISessionRouteAttempt) commit(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed && !a.committed {
		return ErrOpenAISessionRouteLeaseLost
	}
	if a.renewErr != nil {
		return a.renewErr
	}
	if a.committed {
		return nil
	}
	if a.route.OwnerAccountID == a.accountID {
		current, err := a.current(ctx)
		if err != nil {
			return err
		}
		if current.OwnerAccountID != a.accountID || current.Generation != a.route.Generation || openAISessionRouteLeaseActive(current, time.Now()) {
			return ErrOpenAISessionRouteLeaseLost
		}
		a.committed = true
		if a.accepted != nil {
			close(a.accepted)
		}
		return nil
	}
	if a.leaseID == "" {
		return ErrOpenAISessionRouteLeaseLost
	}
	route, err := a.store.CommitOwner(ctx, a.route.AuthzDomain, a.route.SessionDigest, a.leaseID, a.accountID)
	if err != nil {
		return err
	}
	a.committed = true
	a.log(ctx, "committed", a.reason)
	a.route = route
	if a.accepted != nil {
		close(a.accepted)
	}
	return nil
}

// ValidateOpenAISessionRouteOwner fences every new turn on a retained socket.
// Releasing the previous turn's concurrency slot does not authorize a stale
// connection to write after another request has moved the logical session.
func ValidateOpenAISessionRouteOwner(ctx context.Context) error {
	a := openAIRouteAttempt(ctx)
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.current(ctx)
	if err != nil {
		return err
	}
	if !a.committed {
		if current.Generation != a.route.Generation || (a.leaseID != "" && (current.LeaseID != a.leaseID || !openAISessionRouteLeaseActive(current, time.Now()))) {
			return ErrOpenAISessionRouteLeaseLost
		}
		return nil
	}
	if current.OwnerAccountID != a.accountID || current.Generation != a.route.Generation || openAISessionRouteLeaseActive(current, time.Now()) {
		return ErrOpenAISessionRouteLeaseLost
	}
	return nil
}

func (a *openAISessionRouteAttempt) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	if !a.committed && a.route.OwnerAccountID != a.accountID && a.leaseCancel != nil {
		a.leaseCancel(ErrOpenAISessionRouteLeaseLost)
	}
	if a.committed || a.leaseID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.store.AbandonMigration(ctx, a.route.AuthzDomain, a.route.SessionDigest, a.leaseID); err != nil {
		a.log(a.logCtx, "abandon_failed", "route_store_unavailable")
	}
}

// Call with the attempt locked; only safe IDs and stable reasons enter logs.
func (a *openAISessionRouteAttempt) log(ctx context.Context, stage, reason string) {
	logger.FromContext(ctx).Info("openai_session_migration", zap.String("stage", stage),
		zap.Int64("owner_account_id", a.route.OwnerAccountID), zap.Int64("candidate_account_id", a.accountID),
		zap.Int64("generation", a.route.Generation), zap.String("reason", reason))
}

// commitOpenAISessionTakeover is called only after the existing protocol parser
// verifies semantic output or a successful terminal, before exposing it.
func commitOpenAISessionTakeover(ctx context.Context) error {
	if a := openAIRouteAttempt(ctx); a != nil {
		return a.commit(ctx)
	}
	return nil
}

func commitOpenAISessionTakeoverEvent(ctx context.Context, payload []byte) error {
	if openAIRouteAttempt(ctx) == nil {
		return nil
	}
	kind := gjson.GetBytes(payload, "type").String()
	if kind == "error" || kind == "response.failed" || kind == "response.incomplete" {
		return nil
	}
	status := gjson.GetBytes(payload, "response.status").String()
	if status == "failed" || status == "incomplete" || status == "cancelled" {
		return nil
	}
	success := (kind == "response.completed" || kind == "response.done") && (status == "" || status == "completed") &&
		(gjson.GetBytes(payload, "response.usage.input_tokens").Int() > 0 || gjson.GetBytes(payload, "response.usage.output_tokens").Int() > 0)
	if apicompat.ObserveResponsesOutput(payload).MeaningfulOutput || success {
		return commitOpenAISessionTakeover(ctx)
	}
	return nil
}

// Chat Completions may finish without text and send usage in a separate final
// chunk. Both a successful finish and real usage are required in that case.
type openAIChatTakeoverAcceptance struct {
	finished bool
}

func (a *openAIChatTakeoverAcceptance) observe(chunk *apicompat.ChatCompletionsChunk, usage OpenAIUsage) bool {
	if apicompat.ObserveChatChunkOutput(chunk).MeaningfulOutput {
		return true
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "stop", "length", "tool_calls", "function_call":
				a.finished = true
			}
		}
	}
	return a.finished && (usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadInputTokens > 0)
}

// Buffered Responses bodies must prove a result too. A transport 200, an
// error object or an empty terminal cannot promote a candidate account.
func commitOpenAISessionTakeoverJSON(ctx context.Context, payload []byte) error {
	a := openAIRouteAttempt(ctx)
	if a == nil {
		return nil
	}
	response := gjson.ParseBytes(payload)
	if nested := response.Get("response"); nested.IsObject() {
		response = nested
	}
	status := response.Get("status").String()
	chatAccepted := false
	for _, choice := range response.Get("choices").Array() {
		message := choice.Get("message")
		semantic := message.Get("content").String() != "" || message.Get("refusal").String() != "" ||
			message.Get("reasoning_content").String() != "" || len(message.Get("tool_calls").Array()) > 0 || message.Get("function_call.name").String() != ""
		finish := choice.Get("finish_reason").String()
		success := finish == "stop" || finish == "length" || finish == "tool_calls" || finish == "function_call"
		chatAccepted = chatAccepted || semantic || (success && (response.Get("usage.prompt_tokens").Int() > 0 || response.Get("usage.completion_tokens").Int() > 0))
	}
	valid := gjson.ValidBytes(payload) && !response.Get("error").Exists() &&
		status != "failed" && status != "incomplete" && status != "cancelled" &&
		(apicompat.ObserveResponsesOutput([]byte(`{"type":"response.completed","response":`+response.Raw+`}`)).MeaningfulOutput || chatAccepted ||
			(status == "completed" && (response.Get("usage.input_tokens").Int() > 0 || response.Get("usage.output_tokens").Int() > 0)))
	if valid {
		return a.commit(ctx)
	}
	a.mu.Lock()
	pending := !a.committed && a.route.OwnerAccountID != a.accountID
	a.mu.Unlock()
	if pending {
		return ErrOpenAISessionRouteNotAccepted
	}
	return nil
}

// Admission is rechecked at the forwarding boundary. Lease renewal stops at
// the first accepted output, cancellation or the existing first-output budget.
func beginOpenAISessionRouteForward(ctx context.Context, account *Account, timeout time.Duration) (context.Context, func(), error) {
	a := openAIRouteAttempt(ctx)
	if a == nil {
		return ctx, func() {}, nil
	}
	if err := a.admit(ctx, account.ID); err != nil {
		return ctx, nil, err
	}
	a.mu.Lock()
	if a.forwardStarted {
		a.mu.Unlock()
		return ctx, func() {}, nil
	}
	a.forwardStarted = true
	a.apiKey = account.IsOpenAIApiKey()
	pending := !a.committed && a.route.OwnerAccountID != a.accountID
	a.accepted = make(chan struct{})
	leaseCtx, cancelLease := context.WithCancelCause(context.Background())
	a.leaseContext = leaseCtx
	a.leaseCancel = cancelLease
	a.mu.Unlock()
	if !pending {
		return ctx, func() { a.close(); cancelLease(context.Canceled) }, nil
	}
	if timeout <= 0 {
		timeout = time.Minute
	}
	forwardCtx, cancelForward := context.WithCancelCause(ctx)
	renewCtx, cancel := context.WithTimeout(ctx, timeout)
	done := make(chan struct{})
	fail := func(err error) {
		a.mu.Lock()
		if !a.committed && !a.closed {
			a.renewErr = err
			cancelLease(err)
			cancelForward(err)
		}
		a.mu.Unlock()
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-a.accepted:
				return
			case <-renewCtx.Done():
				if errors.Is(renewCtx.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
					fail(ErrOpenAISessionRouteLeaseLost)
				}
				return
			case <-ticker.C:
				a.mu.Lock()
				if a.committed || a.closed {
					a.mu.Unlock()
					return
				}
				checkCtx, release := context.WithTimeout(renewCtx, 3*time.Second)
				_, err := a.store.BeginMigration(checkCtx, a.route.AuthzDomain, a.route.SessionDigest, a.route.Generation, a.accountID, a.leaseID, a.reason, time.Minute)
				release()
				a.mu.Unlock()
				if err != nil {
					fail(err)
					return
				}
			}
		}
	}()
	return forwardCtx, func() { cancel(); <-done; a.close(); cancelLease(context.Canceled); cancelForward(context.Canceled) }, nil
}

// Detached usage-draining requests still obey lease cancellation. Client
// disconnect draining after acceptance retains its existing behavior.
func openAISessionRouteDetachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	a := openAIRouteAttempt(ctx)
	if a == nil {
		return base, func() {}
	}
	a.mu.Lock()
	leaseCtx := a.leaseContext
	a.mu.Unlock()
	if leaseCtx == nil {
		return base, func() {}
	}
	bound, cancel := context.WithCancelCause(base)
	context.AfterFunc(leaseCtx, func() { cancel(context.Cause(leaseCtx)) })
	// Existing builders release immediately after attaching this context to
	// their request. Preserve that contract: lifecycle completion/loss cancels
	// the bound context, while release must not cancel a not-yet-sent request.
	return bound, func() {}
}

func validateOpenAISessionContinuation(ctx context.Context, payload []byte) error {
	a := openAIRouteAttempt(ctx)
	if a == nil || !openAISessionTakeoverPending(ctx) {
		return nil
	}
	request := openAIRequestPayloadView(payload)
	payload = []byte(request.Raw)
	coverage := AnalyzeToolCallOutputContextCoverageBytes(payload)
	opaque := false
	input := request.Get("input")
	observeReference := func(item gjson.Result) {
		if item.Get("type").String() == "item_reference" {
			opaque = true
		}
	}
	if input.IsObject() {
		observeReference(input)
	} else {
		for _, item := range input.Array() {
			observeReference(item)
		}
	}
	if request.Get("previous_response_id").String() == "" && !opaque &&
		(!coverage.HasFunctionCallOutput || coverage.ContextCoversAllCallIDs) && openAISessionMessageToolHistoryComplete(request) {
		return nil
	}
	return &UpstreamFailoverError{StatusCode: 400, ClientStatusCode: 400, Stage: GatewayFailureStageInference, Scope: GatewayFailureScopeRequest,
		NextAccountAction: NextAccountStop, Reason: OpenAIHTTPContinuationUnsupportedReason, ClientMessage: "This continuation cannot be reconstructed on another account; resend complete conversation and tool-call history without previous_response_id or item references."}
}

// Chat and Messages adapters must prove tool dependencies before conversion:
// provider normalization cannot supply missing history for a different owner.
func openAISessionMessageToolHistoryComplete(request gjson.Result) bool {
	calls := make(map[string]struct{})
	var outputs []string
	for _, message := range request.Get("messages").Array() {
		role := message.Get("role").String()
		if role == "assistant" {
			for _, call := range message.Get("tool_calls").Array() {
				if id := strings.TrimSpace(call.Get("id").String()); id != "" {
					calls["call:"+id] = struct{}{}
				}
			}
			if name := strings.TrimSpace(message.Get("function_call.name").String()); name != "" {
				calls["function:"+name] = struct{}{}
			}
		}
		switch role {
		case "tool":
			outputs = append(outputs, "call:"+strings.TrimSpace(message.Get("tool_call_id").String()))
		case "function":
			outputs = append(outputs, "function:"+strings.TrimSpace(message.Get("name").String()))
		}
		for _, block := range message.Get("content").Array() {
			switch block.Get("type").String() {
			case "tool_use":
				if id := strings.TrimSpace(block.Get("id").String()); role == "assistant" && id != "" {
					calls["call:"+id] = struct{}{}
				}
			case "tool_result":
				outputs = append(outputs, "call:"+strings.TrimSpace(block.Get("tool_use_id").String()))
			}
		}
	}
	for _, output := range outputs {
		if _, ok := calls[output]; !ok {
			return false
		}
	}
	return true
}

// A route write during selection must never implicitly migrate its owner.
func openAISessionRouteSelectionDeferred(ctx context.Context) bool {
	if openAISessionRouteDisabled(ctx) {
		return false
	}
	deferred, _ := ctx.Value(openAISessionRouteSelectionKey{}).(bool)
	return deferred
}

func (s *OpenAIGatewayService) selectAccountWithCanonicalRoute(ctx context.Context, req OpenAIAccountScheduleRequest, selectCandidate func(context.Context, string, map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	if !openAISessionRouteApplies(req) || openAISessionRouteDisabled(ctx) {
		ctx = WithoutOpenAISessionRoute(ctx)
		selection, decision, err := selectCandidate(ctx, req.SessionHash, req.ExcludedIDs)
		if selection != nil {
			selection.sessionRouteDisabled = true
		}
		return selection, decision, err
	}
	store := s.openAISessionRouteStoreForRequest(ctx)
	if store == nil || req.SessionHash == "" {
		return selectCandidate(ctx, req.SessionHash, req.ExcludedIDs)
	}
	// Affinity cannot bypass the request's public-model authorization merely
	// because this owner has an allowed mapped upstream model.
	if s.checkChannelPricingRestriction(ctx, req.GroupID, req.RequestedModel) {
		return nil, OpenAIAccountScheduleDecision{}, noAvailableOpenAIChannelPricingError(req.RequestedModel)
	}
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIGroupPrivacyRequirement(ctx, req.GroupID)
	if req.RequiredImageCapability == "" {
		ctx = s.withOpenAIProfitControlGate(ctx, req.GroupID)
	}
	ctx = context.WithValue(ctx, openAISessionRouteSelectionKey{}, true)
	domain, digest := openAISessionRouteDomain(derefGroupID(req.GroupID)), OpenAISessionRouteDigest(req.SessionHash)
	route, err := store.Get(ctx, domain, digest)
	if err != nil && !errors.Is(err, ErrOpenAISessionRouteNotFound) {
		return nil, OpenAIAccountScheduleDecision{}, fmt.Errorf("%w: read", ErrOpenAISessionRouteStoreUnavailable)
	}
	exists := err == nil
	if !exists {
		if previous := openAIRouteAttempt(ctx); previous != nil {
			previous.mu.Lock()
			lost := previous.route.AuthzDomain == domain && previous.route.SessionDigest == digest
			previous.mu.Unlock()
			if lost {
				return nil, OpenAIAccountScheduleDecision{}, ErrOpenAISessionRouteLeaseLost
			}
		}
	}
	if exists && openAISessionRouteLeaseActive(route, time.Now()) {
		return nil, OpenAIAccountScheduleDecision{}, ErrOpenAISessionRouteLeaseLost
	}
	excluded := cloneExcludedAccountIDs(req.ExcludedIDs)
	if excluded == nil {
		excluded = make(map[int64]struct{})
	}
	var selection *AccountSelectionResult
	var decision OpenAIAccountScheduleDecision
	reason := "owner_unavailable"
	if exists {
		recovery, _ := ctx.Value(openAISessionRecoveryKey{}).(openAISessionRecovery)
		if recovery.retry {
			if recovery.accountID != route.OwnerAccountID {
				previous := openAIRouteAttempt(ctx)
				if previous == nil {
					return nil, decision, ErrOpenAISessionRouteLeaseLost
				}
				previous.mu.Lock()
				valid := previous.accountID == recovery.accountID && previous.route.Generation == route.Generation && previous.route.OwnerAccountID == route.OwnerAccountID
				previous.mu.Unlock()
				if !valid {
					return nil, decision, ErrOpenAISessionRouteLeaseLost
				}
			}
			retryReq := req
			retryReq.StickyAccountID, retryReq.DisableStickyEscape = recovery.accountID, true
			retryReq.RequirePrivacySet = s.openAIGroupRequiresPrivacySet(ctx, req.GroupID)
			canonical := &defaultOpenAIAccountScheduler{service: s, stats: s.openaiAccountStats}
			selection, _, err = canonical.selectBySessionHash(ctx, retryReq)
			if err != nil {
				return nil, decision, err
			}
			if selection == nil {
				return nil, decision, ErrNoAvailableAccounts
			}
			decision.Layer, decision.StickySessionHit = openAIAccountScheduleLayerSessionSticky, true
		} else {
			owner, ownerErr := s.getSchedulableAccount(ctx, route.OwnerAccountID)
			if ownerErr != nil && !errors.Is(ownerErr, ErrAccountNotFound) {
				return nil, decision, fmt.Errorf("%w: owner lookup", ErrOpenAISessionRouteStoreUnavailable)
			}
			if owner != nil && owner.IsSchedulable() {
				if vetoed, _ := openAIProfitControlVetoReason(ctx, owner); vetoed {
					return nil, decision, ErrNoAvailableAccounts
				}
			}
			if _, skip := excluded[route.OwnerAccountID]; !skip {
				ownerReq := req
				ownerReq.StickyAccountID = route.OwnerAccountID
				ownerReq.DisableStickyEscape = true
				ownerReq.RequirePrivacySet = s.openAIGroupRequiresPrivacySet(ctx, req.GroupID)
				canonical := &defaultOpenAIAccountScheduler{service: s, stats: s.openaiAccountStats}
				selection, _, err = canonical.selectBySessionHash(ctx, ownerReq)
				if err != nil {
					return nil, decision, err
				}
				if selection != nil {
					decision.Layer = openAIAccountScheduleLayerSessionSticky
					decision.StickySessionHit = true
				}
			} else {
				if owner != nil && owner.IsSchedulable() && recovery.accountID != route.OwnerAccountID {
					return nil, decision, ErrNoAvailableAccounts
				}
				reason = "owner_recovery_exhausted"
			}
		}
		excluded[route.OwnerAccountID] = struct{}{}
	}
	if selection == nil {
		// Candidate selection cannot consult stale response/session bindings or
		// eagerly write a new owner. Request-local profile denials must be
		// filtered before CreateIfAbsent makes a candidate authoritative.
		var clientPolicyErr error
		for {
			selection, decision, err = selectCandidate(ctx, "", excluded)
			if err != nil || selection == nil || selection.Account == nil {
				if clientPolicyErr != nil && (err == nil || errors.Is(err, ErrNoAvailableAccounts)) {
					return nil, decision, clientPolicyErr
				}
				return selection, decision, err
			}
			if err := s.checkOpenAIClientRestriction(ctx, selection.Account); err != nil {
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				if _, alreadyExcluded := excluded[selection.Account.ID]; alreadyExcluded {
					return nil, decision, err
				}
				excluded[selection.Account.ID] = struct{}{}
				clientPolicyErr = err
				continue
			}
			break
		}
	} else if err := s.checkOpenAIClientRestriction(ctx, selection.Account); err != nil {
		// An established owner cannot be bypassed by a request-local denial.
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return nil, decision, err
	}
	if !exists {
		route, err = store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: selection.Account.ID})
		if err != nil {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			return nil, decision, err
		}
		if route.OwnerAccountID != selection.Account.ID {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			// The concurrent winner is authoritative. Recheck it once; never
			// turn this losing creation into permission to migrate the winner.
			if openAISessionRouteLeaseActive(route, time.Now()) {
				return nil, decision, ErrOpenAISessionRouteLeaseLost
			}
			ownerReq := req
			ownerReq.StickyAccountID, ownerReq.DisableStickyEscape = route.OwnerAccountID, true
			canonical := &defaultOpenAIAccountScheduler{service: s, stats: s.openaiAccountStats}
			selection, _, err = canonical.selectBySessionHash(ctx, ownerReq)
			if err != nil {
				return nil, decision, err
			}
			if selection == nil {
				return nil, decision, ErrOpenAISessionRouteLeaseLost
			}
			if err := s.checkOpenAIClientRestriction(ctx, selection.Account); err != nil {
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				return nil, decision, err
			}
		}
	}
	// Canonical and concurrent-winner lookups bypass candidate selection, but
	// still obey shared-response authorization before any provider writes.
	if strings.TrimSpace(req.PreviousResponseID) != "" && (!req.PreviousResponseCanMove || decision.StickyPreviousHit) {
		if err := s.validateOpenAISharedPreviousResponseAccountSelection(ctx, req.GroupID, req.PreviousResponseID, selection.Account); err != nil {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			return nil, decision, err
		}
	}
	decision.SelectedAccountID, decision.SelectedAccountType = selection.Account.ID, selection.Account.Type
	a := &openAISessionRouteAttempt{store: store, route: route, accountID: selection.Account.ID, reason: reason, logCtx: ctx}
	selection.routeAttempt = a
	release := selection.ReleaseFunc
	selection.ReleaseFunc = func() {
		a.close()
		if release != nil {
			release()
		}
	}
	return selection, decision, nil
}
