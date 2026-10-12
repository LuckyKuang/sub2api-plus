package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type ChannelMonitorV3Service struct {
	repo        ChannelMonitorV3Repository
	settings    channelMonitorRuntimeReader
	now         func() time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	start       sync.Once
	wg          sync.WaitGroup
	mu          sync.Mutex
	unsubscribe func()
	kick        chan struct{}
	cacheMu     sync.Mutex
	cache       map[string]channelMonitorV3CacheEntry
}

type channelMonitorV3CacheEntry struct {
	at       time.Time
	snapshot *ChannelMonitorV3Snapshot
}

func NewChannelMonitorV3Service(repo ChannelMonitorV3Repository, settings channelMonitorRuntimeReader) *ChannelMonitorV3Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &ChannelMonitorV3Service{repo: repo, settings: settings, now: func() time.Time { return time.Now().UTC() }, ctx: ctx, cancel: cancel, kick: make(chan struct{}, 1)}
}

func (s *ChannelMonitorV3Service) Start() {
	s.start.Do(func() {
		if subscriber, ok := s.settings.(channelMonitorRuntimeSubscriber); ok {
			s.mu.Lock()
			s.unsubscribe = subscriber.SubscribeChannelMonitorRuntime(func() {
				select {
				case s.kick <- struct{}{}:
				default:
				}
			})
			s.mu.Unlock()
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				if s.ctx.Err() != nil {
					return
				}
				if s.settings != nil {
					rt := s.settings.GetChannelMonitorRuntime(s.ctx)
					if rt.Enabled && rt.Mode == ChannelMonitorModeV3 {
						ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
						err := s.repo.Refresh(ctx, s.now().UTC().Truncate(time.Minute))
						cancel()
						if err != nil && s.ctx.Err() == nil {
							slog.Warn("service_status_refresh_failed", "stage", "aggregation", "code", "channel_monitor_v3_refresh_failed")
						}
					}
				}
				timer := time.NewTimer(time.Minute)
				select {
				case <-s.ctx.Done():
					timer.Stop()
					return
				case <-s.kick:
					timer.Stop()
				case <-timer.C:
				}
			}
		}()
	})
}

func (s *ChannelMonitorV3Service) Stop() {
	s.cancel()
	s.mu.Lock()
	if s.unsubscribe != nil {
		s.unsubscribe()
		s.unsubscribe = nil
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *ChannelMonitorV3Service) GetConfig(ctx context.Context) (*ChannelMonitorV3Config, error) {
	return s.repo.GetConfig(ctx)
}
func (s *ChannelMonitorV3Service) UpdateConfig(ctx context.Context, cfg ChannelMonitorV3Config) (*ChannelMonitorV3Config, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateConfig(ctx, cfg)
	if err == nil {
		s.cacheMu.Lock()
		s.cache = nil
		s.cacheMu.Unlock()
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
	return updated, err
}

func ChannelMonitorV3Window(value string) (time.Duration, bool) {
	switch value {
	case "", "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func (s *ChannelMonitorV3Service) Snapshot(ctx context.Context, window time.Duration, platform string) (*ChannelMonitorV3Snapshot, error) {
	now := s.now().UTC()
	// A short shared snapshot avoids scanning terminal rows once per viewer.
	// Include the runtime gate in the key so switching extra probes is immediate.
	runtime := ChannelMonitorRuntime{}
	if s.settings != nil {
		runtime = s.settings.GetChannelMonitorRuntime(ctx)
	}
	key := fmt.Sprintf("%d:%s:%v", window, platform, runtime)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if cached, ok := s.cache[key]; ok && now.Before(cached.at.Add(10*time.Second)) {
		return cached.snapshot, nil
	}
	data, err := s.repo.Read(ctx, now, window, platform)
	if err != nil {
		return nil, err
	}
	snapshot := BuildChannelMonitorV3Snapshot(data, data.Config, now, window, platform)
	if s.cache == nil || len(s.cache) >= 64 {
		s.cache = map[string]channelMonitorV3CacheEntry{}
	}
	for key, cached := range s.cache {
		if !now.Before(cached.at.Add(10 * time.Second)) {
			delete(s.cache, key)
		}
	}
	s.cache[key] = channelMonitorV3CacheEntry{at: now, snapshot: snapshot}
	return snapshot, nil
}

func BuildChannelMonitorV3Snapshot(data *ChannelMonitorV3Data, cfg ChannelMonitorV3Config, now time.Time, window time.Duration, filter string) *ChannelMonitorV3Snapshot {
	result := &ChannelMonitorV3Snapshot{ComputedAt: now, DataThrough: data.DataThrough, Platforms: []ChannelMonitorV3Platform{}, Incidents: []ChannelMonitorV3Incident{}, Summary: ChannelMonitorV3Summary{Status: "unknown"}}
	result.CurrentWindowStart, result.CurrentWindowEnd, result.CurrentThrough = now.Add(-5*time.Minute), now, data.CurrentThrough
	result.MonitoringEnabled = len(cfg.EnabledPlatforms()) > 0
	current, totals := map[string]ChannelMonitorV3Fact{}, map[string]ChannelMonitorV3Fact{}
	probeEvidence := map[string]bool{}
	models := map[string]ChannelMonitorV3Fact{}
	observedModels := map[string]bool{}
	catalog := map[string]bool{}
	scopes := map[string]bool{}
	for _, item := range data.Catalog {
		if cfg.PlatformEnabled(item.Platform) && (filter == "" || item.Platform == filter) {
			catalog[item.Platform] = true
			scopes[item.Scope()] = true
		}
	}
	for _, fact := range data.Current {
		if !scopes[fact.Scope("")] {
			continue
		}
		value := current[fact.Platform]
		value.Merge(fact)
		current[fact.Platform] = value
		key := fact.Scope(fact.Model)
		observedModels[key] = true
		value = models[key]
		value.Platform, value.GroupID, value.GroupName, value.Model = fact.Platform, fact.GroupID, fact.GroupName, fact.Model
		value.SourceKind, value.MonitorID = fact.SourceKind, fact.MonitorID
		value.Merge(fact)
		models[key] = value
	}
	for _, fact := range data.KnownModels {
		key := fact.Scope(fact.Model)
		if scopes[fact.Scope("")] && !observedModels[key] {
			// Historical identity and time are useful only when the current window
			// has no observations. Never merge old counters or status into it.
			identity := ChannelMonitorV3Fact{
				Platform: fact.Platform, SourceKind: fact.SourceKind, GroupID: fact.GroupID,
				GroupName: fact.GroupName, MonitorID: fact.MonitorID, Model: fact.Model,
				LastRequest: fact.LastRequest,
			}
			if previous, exists := models[key]; !exists || identity.LastRequest.After(previous.LastRequest) {
				models[key] = identity
			}
		}
	}
	for _, fact := range data.Totals {
		value := totals[fact.Platform]
		value.Merge(fact)
		totals[fact.Platform] = value
		if fact.Source() == "probe" && fact.Success+fact.Failures > 0 {
			probeEvidence[fact.Platform] = true
		}
	}
	through := data.CurrentThrough
	stale := through == nil || through.Before(now.Add(-30*time.Second))
	for name := range catalog {
		fact := current[name]
		status := ChannelMonitorV3Health(fact, cfg)
		if stale || fact.LastRequest.Before(now.Add(-5*time.Minute)) {
			status = "unknown"
		}
		item := ChannelMonitorV3Platform{Platform: name, Status: status, Models: []ChannelMonitorV3Model{}, Timeline: []ChannelMonitorV3Timeline{}}
		total := totals[name]
		n := total.Success + total.Failures
		if n > 0 && (n >= cfg.MinimumSamples || probeEvidence[name]) {
			rate := float64(total.Success) / float64(n)
			item.SuccessRate = &rate
		}
		if n >= cfg.MinimumSamples {
			item.TTFTP50Ms = total.P50()
		}
		last := total.LastRequest
		if fact.LastRequest.After(last) {
			last = fact.LastRequest
		}
		if !last.IsZero() {
			item.LastRequest = &last
		}
		for key, model := range models {
			if model.Platform != name {
				continue
			}
			health := ChannelMonitorV3Health(model, cfg)
			if stale || model.LastRequest.Before(now.Add(-5*time.Minute)) {
				health = "unknown"
			}
			reason := "observed"
			if stale {
				reason = "stale"
			} else if health == "insufficient" {
				reason = "insufficient"
			} else if model.Success+model.Failures == 0 || model.LastRequest.Before(now.Add(-5*time.Minute)) {
				reason = "no_observations"
			}
			var last *time.Time
			if !model.LastRequest.IsZero() {
				value := model.LastRequest
				last = &value
			}
			phase := ""
			if state := data.States[key]; state.Incident != nil {
				phase = state.Incident.Phase
			}
			item.Models = append(item.Models, ChannelMonitorV3Model{SourceKind: model.Source(), MonitorID: model.Monitor(), GroupID: model.GroupID, GroupName: model.GroupName, Model: model.Model, Status: health, LastObserved: last, ObservationReason: reason, IncidentPhase: phase})
			if (item.Status == "normal" || item.Status == "degraded" || item.Status == "recovering") && (health == "outage" || health == "partial") {
				item.Status = "partial"
			}
			if (item.Status == "normal" || item.Status == "recovering") && health == "degraded" {
				item.Status = "degraded"
			}
			if item.Status == "normal" && health == "recovering" {
				item.Status = "recovering"
			}
		}
		item.Status = CombineChannelMonitorV3Scopes(item.Models, item.Status)
		sort.Slice(item.Models, func(i, j int) bool {
			if item.Models[i].SourceKind != item.Models[j].SourceKind {
				return item.Models[i].SourceKind < item.Models[j].SourceKind
			}
			if item.Models[i].MonitorID != nil && item.Models[j].MonitorID != nil && *item.Models[i].MonitorID != *item.Models[j].MonitorID {
				return *item.Models[i].MonitorID < *item.Models[j].MonitorID
			}
			if item.Models[i].GroupID != item.Models[j].GroupID {
				return item.Models[i].GroupID < item.Models[j].GroupID
			}
			return item.Models[i].Model < item.Models[j].Model
		})
		start := now.Truncate(time.Minute).Add(-window)
		bucket := window / 48
		history := map[int64]map[string]ChannelMonitorV3Fact{}
		for _, point := range data.History {
			if point.Platform == name {
				key := point.Bucket.Unix()
				if history[key] == nil {
					history[key] = map[string]ChannelMonitorV3Fact{}
				}
				value := history[key][point.Scope(point.Model)]
				value.SourceKind = point.SourceKind
				value.Merge(point)
				history[key][point.Scope(point.Model)] = value
			}
		}
		for i := 0; i < 48; i++ {
			at := start.Add(time.Duration(i) * bucket)
			histModels := []ChannelMonitorV3Model{}
			pooled := ChannelMonitorV3Fact{}
			for _, fact := range history[at.Unix()] {
				histModels = append(histModels, ChannelMonitorV3Model{Status: ChannelMonitorV3Health(fact, cfg)})
				if fact.SourceKind != "probe" {
					pooled.Merge(fact)
				}
			}
			item.Timeline = append(item.Timeline, ChannelMonitorV3Timeline{At: at, Status: CombineChannelMonitorV3Scopes(histModels, ChannelMonitorV3Health(pooled, cfg))})
		}
		result.Platforms = append(result.Platforms, item)
		switch item.Status {
		case "normal":
			result.Summary.Normal++
		case "recovering":
			result.Summary.Recovering++
		case "unknown", "insufficient":
			result.Summary.Unknown++
		default:
			result.Summary.Affected++
		}
	}
	sort.Slice(result.Platforms, func(i, j int) bool { return result.Platforms[i].Platform < result.Platforms[j].Platform })
	for _, event := range data.Incidents {
		if scopes[event.Scope()] {
			result.Incidents = append(result.Incidents, event)
			if event.ResolvedAt == nil {
				result.Summary.ActiveEvents++
			}
		}
	}
	if result.Summary.Affected > 0 {
		result.Summary.Status = "degraded"
		for _, item := range result.Platforms {
			if item.Status == "partial" {
				result.Summary.Status = "partial"
			}
			if item.Status == "outage" {
				result.Summary.Status = "outage"
				break
			}
		}
	} else if result.Summary.Recovering > 0 {
		result.Summary.Status = "recovering"
	} else if result.Summary.Normal > 0 {
		result.Summary.Status = "normal"
	}
	return result
}

// An outage requires all observed scopes to fail. Healthy traffic never masks
// a failed probe, and a successful probe never masks failing request traffic.
func CombineChannelMonitorV3Scopes(models []ChannelMonitorV3Model, fallback string) string {
	known, outages, affected, degraded, recovering := 0, 0, false, false, false
	for _, m := range models {
		switch m.Status {
		case "outage":
			known++
			outages++
			affected = true
		case "partial":
			known++
			affected = true
		case "degraded":
			known++
			degraded = true
		case "normal":
			known++
		case "recovering":
			known++
			recovering = true
		}
	}
	if known == 0 {
		return fallback
	}
	if outages == known {
		return "outage"
	}
	if affected {
		return "partial"
	}
	if fallback == "outage" || fallback == "partial" {
		return "partial"
	}
	if degraded || fallback == "degraded" {
		return "degraded"
	}
	if recovering {
		return "recovering"
	}
	return "normal"
}
