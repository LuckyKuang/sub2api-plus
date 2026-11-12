package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnmodels"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/geminicli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/typesafe"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

const (
	GroupBindingSingle = "single"
	GroupBindingAll    = "all"
	GroupBindingCustom = "custom"
)

func NormalizeGroupBindingMode(mode string) string {
	if mode == "" {
		return GroupBindingSingle
	}
	return mode
}

func (k *APIKey) IsAggregate() bool {
	return k != nil && (k.GroupBindingMode == GroupBindingAll || k.GroupBindingMode == GroupBindingCustom)
}

type aggregateAccountReader interface {
	ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error)
}

func aggregateGroupAllowed(user *User, group *Group) bool {
	return user != nil && group != nil && group.IsActive() && group.Platform != PlatformComposite &&
		group.SubscriptionType == SubscriptionTypeStandard && user.CanBindGroup(group.ID, group.IsExclusive)
}

// Validate scope before writes. Membership is deliberately independent of live
// account availability so temporary supplier outages do not rewrite key scope.
func (s *APIKeyService) validateGroupBinding(ctx context.Context, user *User, mode string, ids []int64, groupID *int64, conversion bool, client, endpoint string) error {
	mode = NormalizeGroupBindingMode(mode)
	if mode != GroupBindingSingle && mode != GroupBindingAll && mode != GroupBindingCustom {
		return infraerrors.BadRequest("API_KEY_GROUP_BINDING_INVALID", "Unknown group binding mode")
	}
	if mode == GroupBindingSingle {
		if len(ids) != 0 {
			return infraerrors.BadRequest("API_KEY_GROUP_BINDING_INVALID", "Single keys cannot contain aggregate groups")
		}
		return nil
	}
	if groupID != nil || conversion || client != "" || endpoint != "" {
		return infraerrors.BadRequest("API_KEY_GROUP_BINDING_INVALID", "Aggregate keys use native endpoints and cannot bind a single group or conversion preferences")
	}
	if mode == GroupBindingAll && len(ids) != 0 || mode == GroupBindingCustom && len(ids) == 0 {
		return infraerrors.BadRequest("API_KEY_GROUP_BINDING_INVALID", "All mode stores no members; custom mode requires at least one group")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return infraerrors.BadRequest("API_KEY_GROUP_BINDING_INVALID", "Group IDs must be positive and unique")
		}
		seen[id] = true
		group, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if !aggregateGroupAllowed(user, group) {
			return ErrGroupNotAllowed
		}
	}
	return nil
}

// AggregateScope reads current grants and groups on every request. Cached auth
// snapshots never freeze all-mode membership or revive revoked custom members.
func (s *APIKeyService) AggregateScope(ctx context.Context, key *APIKey) (*APIKey, error) {
	if !key.IsAggregate() {
		return key, nil
	}
	user, err := s.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() {
		return nil, ErrInsufficientPerms
	}
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	out := *key
	out.User = user
	out.Group, out.GroupID = nil, nil
	out.AggregateGroups = nil
	forced, _ := ctx.Value(ctxkey.ForcePlatform).(string)
	for i := range groups {
		group := &groups[i]
		if forced != "" && forced != group.Platform {
			continue
		}
		if aggregateGroupAllowed(user, group) && (key.GroupBindingMode == GroupBindingAll || slices.Contains(key.AggregateGroupIDs, group.ID)) {
			out.AggregateGroups = append(out.AggregateGroups, *group)
		}
	}
	sort.Slice(out.AggregateGroups, func(i, j int) bool {
		a, b := out.AggregateGroups[i], out.AggregateGroups[j]
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		return a.ID < b.ID
	})
	if s.aggregateChannels != nil {
		channels, err := s.aggregateChannels.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		platforms := map[int64]string{}
		for _, group := range out.AggregateGroups {
			platforms[group.ID] = group.Platform
		}
		out.aggregateChannelCache = populateChannelCache(channels, platforms)
	}
	return &out, nil
}

// BindAggregateGroup only inspects persisted capability metadata; it performs
// no account scheduling, probes, billing, RPM acquisition or provider writes.
func (s *APIKeyService) BindAggregateGroup(ctx context.Context, key *APIKey, model, protocol string, groupHint int64) (*APIKey, error) {
	for i := range key.AggregateGroups {
		group := key.AggregateGroups[i]
		if forced, _ := ctx.Value(ctxkey.ForcePlatform).(string); forced != "" && forced != group.Platform {
			continue
		}
		if groupHint != 0 && group.ID != groupHint {
			continue
		}
		if model == "" && groupHint != 0 {
			return aggregateKeyWithGroup(key, group), nil
		}
		if !group.ModelAllowlist.Allows(model) {
			continue
		}
		mapped, restricted := aggregateMappedModel(key, &group, model)
		if restricted {
			continue
		}
		accounts, err := s.aggregateCandidates(ctx, &group)
		if err != nil {
			return nil, err
		}
		for j := range accounts {
			account := &accounts[j]
			if routing := group.GetRoutingAccountIDs(model); len(routing) > 0 && !slices.Contains(routing, account.ID) {
				continue
			}
			if model != "" && !aggregateAccountModelAllowed(account, mapped) {
				continue
			}
			if protocol != "" && !slices.Contains(account.NativeProtocols(account.GetMappedModel(mapped)), protocol) {
				continue
			}
			return aggregateKeyWithGroup(key, group), nil
		}
	}
	return nil, infraerrors.BadRequest("AGGREGATE_GROUP_UNAVAILABLE", "No authorized pay-as-you-go group supports this model and native endpoint")
}

func aggregateKeyWithGroup(key *APIKey, group Group) *APIKey {
	out := *key
	// Resource reads need an authorized group, not a currently schedulable
	// supplier. The existing resource handler still checks the creating owner.
	group.FallbackGroupID, group.FallbackGroupIDOnInvalidRequest = nil, nil
	out.Group, out.GroupID = &group, &group.ID
	return &out
}

func (s *APIKeyService) aggregateCandidates(ctx context.Context, group *Group) ([]Account, error) {
	if s.aggregateAccounts == nil {
		return nil, infraerrors.ServiceUnavailable("AGGREGATE_CAPABILITY_UNAVAILABLE", "Aggregate capability store is unavailable")
	}
	platforms := []string{group.Platform}
	forced, _ := ctx.Value(ctxkey.ForcePlatform).(string)
	if group.Platform == PlatformGemini && forced == "" {
		platforms = append(platforms, PlatformAntigravity)
	}
	accounts, err := s.aggregateAccounts.ListModelAvailabilityCandidates(ctx, &group.ID, platforms, false)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(accounts), func(account Account) bool {
		return account.Platform != group.Platform && (group.Platform != PlatformGemini || forced != "" || account.Platform != PlatformAntigravity || !account.IsMixedSchedulingEnabled())
	}), nil
}

func aggregateAccountModelAllowed(account *Account, model string) bool {
	if !account.IsModelSupported(model) {
		return false
	}
	if len(account.GetModelMapping()) > 0 && !account.IsOpenAIPassthroughEnabled() {
		return true
	}
	if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
		_, ok := snapshot.Models[model]
		return ok
	}
	// Empty mappings are permissive inside a group; cross-group routing needs
	// positive catalog evidence instead of sending every alias to the first pool.
	return slices.Contains(aggregateAccountModels(account), normalizeRequestedModelForLookup(account.Platform, model))
}

func aggregateAccountModels(account *Account) []string {
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		var ids []string
		wildcard := false
		for id := range mapping {
			if !strings.Contains(id, "*") {
				ids = append(ids, id)
			} else {
				wildcard = true
			}
		}
		if wildcard {
			ids = append(ids, aggregateDefaultAccountModels(account)...)
		}
		return ids
	}
	return aggregateDefaultAccountModels(account)
}

func aggregateDefaultAccountModels(account *Account) []string {
	if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
		var ids []string
		for id := range snapshot.Models {
			ids = append(ids, id)
		}
		return ids
	}
	switch account.Platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformAnthropic:
		return claude.DefaultModelIDs()
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformTypeSafe:
		return []string{typesafe.JevLatestModel}
	case PlatformGemini, PlatformAntigravity:
		var ids []string
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	default:
		return cnmodels.DefaultModelIDs(account.Platform)
	}
}

// AggregateNativeModel is a public offer; it never exposes credential/account IDs.
type AggregateNativeModel struct {
	Name                string   `json:"name"`
	Platform            string   `json:"platform"`
	Endpoints           []string `json:"endpoints"`
	ConversionEndpoints []string `json:"conversion_endpoints"`
	GroupIDs            []int64  `json:"group_ids"`
}

func (s *APIKeyService) AggregateNativeModels(ctx context.Context, key *APIKey) ([]AggregateNativeModel, error) {
	paths := map[string]string{APIProtocolResponses: "/v1/responses", APIProtocolChatCompletions: "/v1/chat/completions", APIProtocolAnthropic: "/v1/messages", "gemini": "/v1beta", "systemone": "/v1/systemone"}
	offers := map[string]*AggregateNativeModel{}
	for i := range key.AggregateGroups {
		group := &key.AggregateGroups[i]
		accounts, err := s.aggregateCandidates(ctx, group)
		if err != nil {
			return nil, err
		}
		for j := range accounts {
			account := &accounts[j]
			models := aggregateAccountModels(account)
			if cache := key.aggregateChannelCache; cache != nil {
				if channel := cache.channelByGroupID[group.ID]; channel != nil {
					for model := range channel.ModelMapping[group.Platform] {
						if !strings.Contains(model, "*") {
							models = append(models, model)
						}
					}
				}
			}
			for _, model := range models {
				mapped, restricted := aggregateMappedModel(key, group, model)
				if restricted || !group.ModelAllowlist.Allows(model) || !aggregateAccountModelAllowed(account, mapped) {
					continue
				}
				if routing := group.GetRoutingAccountIDs(model); len(routing) > 0 && !slices.Contains(routing, account.ID) {
					continue
				}
				protocols := account.NativeProtocols(account.GetMappedModel(mapped))
				if len(protocols) == 0 {
					continue
				}
				id := account.Platform + "\x00" + model
				offer := offers[id]
				if offer == nil {
					offer = &AggregateNativeModel{Name: model, Platform: account.Platform, Endpoints: []string{}, ConversionEndpoints: []string{}, GroupIDs: []int64{}}
					offers[id] = offer
				}
				if !slices.Contains(offer.GroupIDs, group.ID) {
					offer.GroupIDs = append(offer.GroupIDs, group.ID)
				}
				for _, protocol := range protocols {
					if path := paths[protocol]; path != "" && !slices.Contains(offer.Endpoints, path) {
						offer.Endpoints = append(offer.Endpoints, path)
					}
				}
			}
		}
	}
	out := make([]AggregateNativeModel, 0, len(offers))
	for _, offer := range offers {
		sort.Strings(offer.Endpoints)
		out = append(out, *offer)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *APIKeyService) AggregateModels(ctx context.Context, key *APIKey, protocol string) ([]string, error) {
	offers, err := s.AggregateNativeModels(ctx, key)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, offer := range offers {
		if protocol == "gemini" && !slices.Contains(offer.Endpoints, "/v1beta") {
			continue
		}
		if protocol == APIProtocolResponses && !slices.Contains(offer.Endpoints, "/v1/responses") {
			continue
		}
		seen[offer.Name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func aggregateMappedModel(key *APIKey, group *Group, model string) (string, bool) {
	cache := key.aggregateChannelCache
	if cache == nil {
		return model, false
	}
	channel := cache.channelByGroupID[group.ID]
	if channel == nil || channel.Status != StatusActive {
		return model, false
	}
	lookup := &channelLookup{cache: cache, channel: channel, platform: group.Platform}
	return resolveMapping(lookup, group.ID, model).MappedModel, checkRestricted(lookup, group.ID, model)
}

type aggregateGroupContextKey struct{}

func WithAggregateGroup(ctx context.Context, groupID int64) context.Context {
	return context.WithValue(ctx, aggregateGroupContextKey{}, groupID)
}

func aggregateGroupFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(aggregateGroupContextKey{}).(int64)
	return id
}

// RefreshAggregateBinding keeps a live connection on its creating group while
// refreshing current grants and prices before each subsequent turn's audit.
func (s *APIKeyService) RefreshAggregateBinding(ctx context.Context, conn *APIKey, model, protocol string) (*APIKey, error) {
	if s == nil || conn == nil || !conn.IsAggregate() || conn.GroupID == nil || conn.Group == nil {
		return nil, ErrGroupNotAllowed
	}
	latest, err := s.GetByKey(ctx, conn.Key)
	if err != nil {
		return nil, err
	}
	if latest.ID != conn.ID || latest.UserID != conn.UserID || !latest.IsAggregate() || !latest.IsActive() || latest.IsExpired() || latest.IsQuotaExhausted() {
		return nil, ErrGroupNotAllowed
	}
	scope, err := s.AggregateScope(ctx, latest)
	if err != nil {
		return nil, err
	}
	bound, err := s.BindAggregateGroup(ctx, scope, model, protocol, *conn.GroupID)
	if err != nil {
		return nil, err
	}
	if bound.Group.Platform != conn.Group.Platform || bound.Group.SubscriptionType != conn.Group.SubscriptionType {
		return nil, ErrGroupNotAllowed
	}
	return bound, nil
}
