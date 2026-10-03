package service

import (
	"context"
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
	data, err := s.repo.Read(ctx, now, window, platform)
	if err != nil {
		return nil, err
	}
	return BuildChannelMonitorV3Snapshot(data, data.Config, now, window, platform), nil
}

func BuildChannelMonitorV3Snapshot(data *ChannelMonitorV3Data, cfg ChannelMonitorV3Config, now time.Time, window time.Duration, filter string) *ChannelMonitorV3Snapshot {
	result := &ChannelMonitorV3Snapshot{ComputedAt: now, DataThrough: data.DataThrough, Platforms: []ChannelMonitorV3Platform{}, Incidents: []ChannelMonitorV3Incident{}, Summary: ChannelMonitorV3Summary{Status: "unknown"}}
	result.MonitoringEnabled = len(cfg.EnabledPlatforms()) > 0
	current, totals := map[string]ChannelMonitorV3Fact{}, map[string]ChannelMonitorV3Fact{}
	models := map[string]ChannelMonitorV3Fact{}
	catalog := map[string]bool{}
	scopes := map[string]bool{}
	for _, item := range data.Catalog {
		if cfg.PlatformEnabled(item.Platform) && (filter == "" || item.Platform == filter) {
			catalog[item.Platform] = true
			scopes[ChannelMonitorV3Scope(item.Platform, item.GroupID, "")] = true
		}
	}
	for _, fact := range data.KnownModels {
		if scopes[ChannelMonitorV3Scope(fact.Platform, fact.GroupID, "")] {
			models[ChannelMonitorV3Scope(fact.Platform, fact.GroupID, fact.Model)] = fact
		}
	}
	for _, fact := range data.Current {
		if !scopes[ChannelMonitorV3Scope(fact.Platform, fact.GroupID, "")] {
			continue
		}
		value := current[fact.Platform]
		value.Merge(fact)
		current[fact.Platform] = value
		key := ChannelMonitorV3Scope(fact.Platform, fact.GroupID, fact.Model)
		value = models[key]
		value.Platform, value.GroupID, value.GroupName, value.Model = fact.Platform, fact.GroupID, fact.GroupName, fact.Model
		value.Merge(fact)
		models[key] = value
	}
	for _, fact := range data.Totals {
		value := totals[fact.Platform]
		value.Merge(fact)
		totals[fact.Platform] = value
	}
	stale := data.DataThrough == nil || data.DataThrough.Before(now.Add(-3*time.Minute))
	for name := range catalog {
		fact := current[name]
		status := ChannelMonitorV3Health(fact, cfg)
		if stale || fact.LastRequest.Before(now.Add(-5*time.Minute)) {
			status = "unknown"
		}
		item := ChannelMonitorV3Platform{Platform: name, Status: status, Models: []ChannelMonitorV3Model{}, Timeline: []ChannelMonitorV3Timeline{}}
		total := totals[name]
		if n := total.Success + total.Failures; n >= cfg.MinimumSamples {
			rate := float64(total.Success) / float64(n)
			item.SuccessRate = &rate
			item.TTFTP50Ms = total.P50()
		}
		last := total.LastRequest
		if fact.LastRequest.After(last) {
			last = fact.LastRequest
		}
		if !last.IsZero() {
			item.LastRequest = &last
		}
		uncertainActive := false
		for key, model := range models {
			if model.Platform != name {
				continue
			}
			health := ChannelMonitorV3Health(model, cfg)
			if stale || model.LastRequest.Before(now.Add(-5*time.Minute)) {
				health = "unknown"
			}
			if state := data.States[key]; health == "normal" && state.Incident != nil {
				// Old failures aging out of the rolling window (or changed
				// thresholds) are not fresh evidence of recovery.
				if state.Incident.Phase == "recovering" {
					health = "recovering"
				} else {
					health = "unknown"
				}
			}
			if state := data.States[key]; state.Incident != nil && (health == "unknown" || health == "insufficient") {
				uncertainActive = true
			}
			item.Models = append(item.Models, ChannelMonitorV3Model{GroupID: model.GroupID, GroupName: model.GroupName, Model: model.Model, Status: health})
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
		if uncertainActive && (item.Status == "normal" || item.Status == "recovering") {
			item.Status = "unknown"
		}
		// Some scopes can be healthy while the pooled rate is high. A platform
		// outage requires every observed eligible scope to be in outage.
		if item.Status == "outage" {
			for _, model := range item.Models {
				if model.Status != "outage" {
					item.Status = "partial"
					break
				}
			}
		}
		sort.Slice(item.Models, func(i, j int) bool {
			if item.Models[i].GroupID != item.Models[j].GroupID {
				return item.Models[i].GroupID < item.Models[j].GroupID
			}
			return item.Models[i].Model < item.Models[j].Model
		})
		start := now.Truncate(time.Minute).Add(-window)
		bucket := window / 48
		history := map[int64]ChannelMonitorV3Fact{}
		for _, point := range data.History {
			if point.Platform == name {
				key := point.Bucket.Unix()
				value := history[key]
				value.Merge(point)
				history[key] = value
			}
		}
		for i := 0; i < 48; i++ {
			at := start.Add(time.Duration(i) * bucket)
			item.Timeline = append(item.Timeline, ChannelMonitorV3Timeline{At: at, Status: ChannelMonitorV3Health(history[at.Unix()], cfg)})
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
		if scopes[ChannelMonitorV3Scope(event.Platform, event.GroupID, "")] {
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
