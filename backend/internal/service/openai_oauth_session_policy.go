package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// OpenAIOAuthSessionPolicyExtraKey stores the opt-in OAuth session access
// policy in accounts.extra. It is deliberately account scoped: an OAuth
// credential must never share a session namespace with another credential.
const OpenAIOAuthSessionPolicyExtraKey = "openai_oauth_session_policy"

// OpenAIOAuthSessionPolicy controls which API-key groups can use an OpenAI
// OAuth account and share its upstream session namespace.
//
// ScopeVersion is server managed. A policy change rotates it so old sessions
// cannot be resumed by a newly authorized group.
type OpenAIOAuthSessionPolicy struct {
	Enabled         bool    `json:"enabled"`
	AllowedGroupIDs []int64 `json:"allowed_group_ids"`
	ScopeVersion    string  `json:"scope_version"`
}

// OpenAIOAuthSessionAccessError is intentionally safe to expose through the
// gateway. It never embeds account, group, session, or credential details.
type OpenAIOAuthSessionAccessError struct{}

func (e *OpenAIOAuthSessionAccessError) Error() string {
	return "openai oauth session access denied"
}

var ErrOpenAIOAuthSessionAccessDenied = &OpenAIOAuthSessionAccessError{}

func (a *Account) IsOpenAIOAuthSessionPolicyApplicable() bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth
}

// OpenAIOAuthSessionPolicy returns the policy, whether it is configured, and
// whether its serialized form is valid. Invalid configured policies fail
// closed at request time.
func (a *Account) OpenAIOAuthSessionPolicy() (OpenAIOAuthSessionPolicy, bool, bool) {
	if !a.IsOpenAIOAuthSessionPolicyApplicable() || a.Extra == nil {
		return OpenAIOAuthSessionPolicy{}, false, true
	}
	raw, configured := a.Extra[OpenAIOAuthSessionPolicyExtraKey]
	if !configured || raw == nil {
		return OpenAIOAuthSessionPolicy{}, false, true
	}

	payload, err := json.Marshal(raw)
	if err != nil {
		return OpenAIOAuthSessionPolicy{}, true, false
	}
	var policy OpenAIOAuthSessionPolicy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return OpenAIOAuthSessionPolicy{}, true, false
	}
	if !policy.Enabled {
		return OpenAIOAuthSessionPolicy{}, true, true
	}
	if strings.TrimSpace(policy.ScopeVersion) == "" || len(policy.AllowedGroupIDs) == 0 {
		return OpenAIOAuthSessionPolicy{}, true, false
	}
	if !validUniquePositiveGroupIDs(policy.AllowedGroupIDs) {
		return OpenAIOAuthSessionPolicy{}, true, false
	}
	policy.AllowedGroupIDs = normalizedGroupIDs(policy.AllowedGroupIDs)
	return policy, true, true
}

func (a *Account) IsOpenAIOAuthSessionSharingEnabled() bool {
	policy, configured, valid := a.OpenAIOAuthSessionPolicy()
	// A present but malformed policy must not silently restore the legacy
	// API-key-isolated path. Treat it as enabled so every request fails closed.
	return configured && (!valid || policy.Enabled)
}

func (a *Account) IsOpenAIOAuthSessionGroupAllowed(groupID *int64) bool {
	policy, configured, valid := a.OpenAIOAuthSessionPolicy()
	if !configured {
		return true
	}
	if !valid || groupID == nil || *groupID <= 0 {
		return false
	}
	if !policy.Enabled {
		return true
	}
	for _, allowedID := range policy.AllowedGroupIDs {
		if allowedID == *groupID {
			return true
		}
	}
	return false
}

// OpenAIOAuthSessionScopeAccountID uses the credential-owning parent for a
// Spark shadow. Parent and shadow therefore retain one OAuth session domain.
func (a *Account) OpenAIOAuthSessionScopeAccountID() int64 {
	if a == nil {
		return 0
	}
	if a.ParentAccountID != nil && *a.ParentAccountID > 0 {
		return *a.ParentAccountID
	}
	return a.ID
}

func validUniquePositiveGroupIDs(groupIDs []int64) bool {
	if len(groupIDs) == 0 {
		return false
	}
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return false
		}
		if _, exists := seen[groupID]; exists {
			return false
		}
		seen[groupID] = struct{}{}
	}
	return true
}

func normalizedGroupIDs(groupIDs []int64) []int64 {
	result := append([]int64(nil), groupIDs...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sameGroupIDs(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func accountBoundGroupIDs(account *Account) []int64 {
	if account == nil {
		return nil
	}
	if len(account.GroupIDs) > 0 {
		return normalizedGroupIDs(account.GroupIDs)
	}
	groupIDs := make([]int64, 0, len(account.AccountGroups))
	for _, group := range account.AccountGroups {
		groupIDs = append(groupIDs, group.GroupID)
	}
	if len(groupIDs) == 0 {
		return nil
	}
	return normalizedGroupIDs(groupIDs)
}

// normalizeOpenAIOAuthSessionPolicyExtra validates the opt-in policy against
// the exact scheduler group bindings. The group binding and policy must never
// drift: otherwise the scheduler could expose an OAuth account to a group that
// cannot enter its session-sharing domain.
func normalizeOpenAIOAuthSessionPolicyExtra(
	previous *Account,
	platform string,
	accountType string,
	extra map[string]any,
	boundGroupIDs []int64,
) (map[string]any, error) {
	if extra == nil {
		return nil, nil
	}
	normalized := make(map[string]any, len(extra))
	for key, value := range extra {
		normalized[key] = value
	}
	raw, configured := normalized[OpenAIOAuthSessionPolicyExtraKey]
	if !configured || raw == nil {
		return normalized, nil
	}
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
		return nil, errors.New("openai_oauth_session_policy is only supported by OpenAI OAuth accounts")
	}

	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("serialize openai_oauth_session_policy: %w", err)
	}
	var policy OpenAIOAuthSessionPolicy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return nil, errors.New("openai_oauth_session_policy is invalid")
	}
	if !policy.Enabled {
		delete(normalized, OpenAIOAuthSessionPolicyExtraKey)
		return normalized, nil
	}
	if !validUniquePositiveGroupIDs(policy.AllowedGroupIDs) {
		return nil, errors.New("openai_oauth_session_policy.allowed_group_ids must contain unique positive group IDs")
	}
	policy.AllowedGroupIDs = normalizedGroupIDs(policy.AllowedGroupIDs)
	boundGroupIDs = normalizedGroupIDs(boundGroupIDs)
	if !sameGroupIDs(policy.AllowedGroupIDs, boundGroupIDs) {
		return nil, errors.New("openai_oauth_session_policy.allowed_group_ids must exactly match account group bindings")
	}

	previousPolicy, _, previousValid := previous.OpenAIOAuthSessionPolicy()
	if !previousValid || !previousPolicy.Enabled ||
		!sameGroupIDs(previousPolicy.AllowedGroupIDs, policy.AllowedGroupIDs) {
		policy.ScopeVersion = uuid.NewString()
	} else {
		policy.ScopeVersion = previousPolicy.ScopeVersion
	}
	normalized[OpenAIOAuthSessionPolicyExtraKey] = map[string]any{
		"enabled":           true,
		"allowed_group_ids": policy.AllowedGroupIDs,
		"scope_version":     policy.ScopeVersion,
	}
	return normalized, nil
}
