package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// LiveUsageKey is the existing durable billing key, scoped to its credential owner.
// It is internal metadata, never a client-supplied search or an approximate match.
type LiveUsageKey struct {
	RequestID string `json:"request_id"`
	UserID    int64  `json:"user_id"`
	APIKeyID  int64  `json:"api_key_id"`
}

type LiveUsageReader interface {
	ReadLiveUsage(context.Context, []LiveUsageKey) ([]UsageLog, error)
}

func (s *UsageService) ReadLiveUsage(ctx context.Context, keys []LiveUsageKey) ([]UsageLog, error) {
	reader, ok := s.usageRepo.(LiveUsageReader)
	if !ok {
		return nil, errors.New("live usage reader unavailable")
	}
	return reader.ReadLiveUsage(ctx, keys)
}

type liveUsageBindingKey struct{}
type liveUsageBinding struct {
	service          *LiveRequestService
	id               string
	userID, apiKeyID int64
}

func WithLiveUsageBinding(ctx context.Context, s *LiveRequestService, id string, userID, apiKeyID int64) context.Context {
	return context.WithValue(ctx, liveUsageBindingKey{}, liveUsageBinding{s, id, userID, apiKeyID})
}

func CopyLiveUsageBinding(parent, target context.Context) context.Context {
	if binding, ok := parent.Value(liveUsageBindingKey{}).(liveUsageBinding); ok {
		return context.WithValue(target, liveUsageBindingKey{}, binding)
	}
	return target
}

// Associate only the finalized canonical key. Settlement values are read from
// persisted usage, so failed writes, deduplication and delayed workers cannot
// invent a fee. Multiple money events are deliberately not collapsed into a turn.
func bindLiveUsage(ctx context.Context, usage *UsageLog) {
	if ctx == nil || usage == nil {
		return
	}
	b, ok := ctx.Value(liveUsageBindingKey{}).(liveUsageBinding)
	if !ok || b.service == nil || usage.RequestID == "" || usage.UserID != b.userID || usage.APIKeyID != b.apiKeyID {
		return
	}
	data, err := json.Marshal(LiveUsageKey{usage.RequestID, usage.UserID, usage.APIKeyID})
	if err != nil {
		return
	}
	b.service.enqueue(func(writeCtx context.Context) error {
		return b.service.cache.BindUsage(writeCtx, b.id, data)
	})
}

func (s *LiveRequestService) SetUsageReader(reader LiveUsageReader) { s.usage = reader }

func (s *LiveRequestService) enrich(ctx context.Context, items []LiveRequest) error {
	if s.usage == nil || len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	values, err := s.cache.UsageBindings(ctx, ids)
	if err != nil {
		return err
	}
	bindings := make(map[string]LiveUsageKey, len(items))
	keys := make([]LiveUsageKey, 0, len(items))
	for i, raw := range values {
		var key LiveUsageKey
		if json.Unmarshal([]byte(raw), &key) != nil || key.RequestID == "" || key.UserID != items[i].UserID || key.APIKeyID != items[i].APIKeyID {
			continue
		}
		bindings[items[i].ID] = key
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	usage, err := s.usage.ReadLiveUsage(ctx, keys)
	if err != nil {
		return err
	}
	byKey := make(map[LiveUsageKey]*UsageLog, len(usage))
	for i := range usage {
		u := &usage[i]
		byKey[LiveUsageKey{u.RequestID, u.UserID, u.APIKeyID}] = u
	}
	for i := range items {
		r := &items[i]
		u := byKey[bindings[r.ID]]
		if u == nil {
			continue
		}
		r.ActualCost = &u.ActualCost
		if u.Group != nil {
			r.GroupName = u.Group.Name
		}
		r.InputTokens, r.OutputTokens = &u.InputTokens, &u.OutputTokens
		r.CacheReadTokens, r.CacheCreationTokens = &u.CacheReadTokens, &u.CacheCreationTokens
		r.ImageOutputTokens, r.AudioOutputTokens = &u.ImageOutputTokens, &u.AudioOutputTokens
		r.ImageCount, r.ImageSize = u.ImageCount, u.ImageSize
		r.VideoCount, r.VideoResolution, r.VideoDurationSeconds = u.VideoCount, u.VideoResolution, u.VideoDurationSeconds
		r.BillingMode = u.BillingMode
		r.UserAgent, r.IPAddress = u.UserAgent, u.IPAddress
		r.ReasoningEffort = u.RequestedReasoningEffort
		if r.ReasoningEffort == nil {
			r.ReasoningEffort = u.ReasoningEffort
		}
		r.TimingVersion, r.NativeCompactionV2 = u.TimingVersion, u.NativeCompactionV2
		r.FirstByteMs, r.FirstTokenMs, r.LastTokenMs, r.FirstOutputMs = u.FirstByteMs, u.FirstTokenMs, u.LastTokenMs, u.FirstOutputMs
		if u.FirstOutputKind != nil {
			r.FirstOutputKind = *u.FirstOutputKind
		}
		if u.DurationMs != nil {
			duration := int64(*u.DurationMs)
			r.DurationMs = &duration
		}
		r.IsComplete = u.IsComplete
		if u.RequestedModel != "" {
			r.Model = u.RequestedModel
		}
		r.RequestType = u.RequestType.String()
		if u.RequestType == RequestTypeCyberBlocked {
			r.Status, r.Reason = "rejected", "policy"
		} else if r.Status == "finished" || r.Status == "unknown" {
			switch u.CompletionStatus {
			case UsageCompletionCompleted:
				r.Status = "completed"
			case UsageCompletionClientDisconnected:
				r.Status, r.Reason = "client_disconnected", "client_disconnected"
			case UsageCompletionIncomplete:
				r.Status, r.Reason = "incomplete", "incomplete"
			}
		}
	}
	return nil
}

// One bounded queue is shared by observations and their billing associations.
func (s *LiveRequestService) enqueue(write func(context.Context) error) {
	if s == nil || s.cache == nil {
		return
	}
	select {
	case s.writes <- struct{}{}:
	default:
		s.warn("capacity")
		return
	}
	go func() {
		defer func() { <-s.writes }()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if write(ctx) != nil {
			s.warn("storage")
		}
	}()
}
