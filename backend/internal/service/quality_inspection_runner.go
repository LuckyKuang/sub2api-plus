package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// qualityRunnerTick 调度器扫描间隔。
const qualityRunnerTick = 30 * time.Second

// QualityInspectionRunner 定时巡检调度与单轮执行。
//
// 单 goroutine 扫描 + 计划级 inFlight 去重：巡检是"整轮"语义，不需要 per-plan
// ticker 的精度，30 秒扫描足以覆盖最小 30 分钟的间隔。
type QualityInspectionRunner struct {
	repo      QualityInspectionRepository
	plans     *QualityInspectionPlanService
	accounts  QualityAccountLister
	probe     QualityProber
	artifacts QualityArtifactWriter
	composite *QualityCompositeResolver
	settings  QualitySwitchReader

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	inFlight map[int64]bool
	started  bool
}

func NewQualityInspectionRunner(repo QualityInspectionRepository, plans *QualityInspectionPlanService,
	accounts QualityAccountLister, probe QualityProber, artifacts QualityArtifactWriter,
	composite *QualityCompositeResolver, settings QualitySwitchReader) *QualityInspectionRunner {
	return &QualityInspectionRunner{
		repo: repo, plans: plans, accounts: accounts, probe: probe,
		artifacts: artifacts, composite: composite, settings: settings,
		inFlight: make(map[int64]bool),
	}
}

// Start 启动调度循环。总开关关闭时循环仍在，但每轮直接返回，不产生任何请求。
func (r *QualityInspectionRunner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(qualityRunnerTick)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				r.tick(r.ctx)
			}
		}
	}()
}

// Stop 优雅停止调度循环。
func (r *QualityInspectionRunner) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.started = false
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

func (r *QualityInspectionRunner) enabled(ctx context.Context) bool {
	if r.settings == nil {
		return false
	}
	return r.settings.ModelQualityInspectionEnabled(ctx)
}

func (r *QualityInspectionRunner) tick(ctx context.Context) {
	if !r.enabled(ctx) {
		return
	}
	plans, err := r.repo.ListSchedulablePlans(ctx)
	if err != nil {
		slog.Warn("quality_inspection.list_plans_failed", "error", err)
		return
	}
	now := time.Now()
	for _, plan := range plans {
		if r.paused(plan, now) {
			continue
		}
		slot := now.Unix() / int64(qualityMaxInt(plan.IntervalMinutes, 1)*60)
		run := &QualityRun{
			PlanID: plan.ID, PlanRevision: plan.Revision, Trigger: QualityTriggerTimer,
			Slot: slot, Status: QualityRunQueued, Model: plan.Model, Effort: plan.Effort,
		}
		created, createErr := r.repo.CreateRun(ctx, run)
		if createErr != nil {
			// 唯一键冲突表示该时间槽已排队或已执行，属于正常去重。
			continue
		}
		go r.execute(created.ID)
	}
}

// manualTimeSlot 为手动运行生成时间槽，避免与定时槽冲突。
func manualTimeSlot(now time.Time) int64 { return now.UnixNano() / int64(time.Millisecond) }

// RunManual 创建并异步执行一轮手动巡检。
func (r *QualityInspectionRunner) RunManual(ctx context.Context, planID int64) (*QualityRun, error) {
	plan, err := r.repo.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if !plan.Enabled {
		return nil, qualityInvalid("计划未启用，不能手动运行")
	}
	if !r.enabled(ctx) {
		return nil, ErrQualityInspectionDisabled
	}
	run := &QualityRun{
		PlanID: plan.ID, PlanRevision: plan.Revision, Trigger: QualityTriggerManual,
		Slot: manualTimeSlot(time.Now()), Status: QualityRunQueued,
		Model: plan.Model, Effort: plan.Effort,
	}
	created, err := r.repo.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}
	go r.execute(created.ID)
	return created, nil
}

// StopRun 将排队中的轮次标记为取消；运行中的轮次不再发起新请求。
func (r *QualityInspectionRunner) StopRun(ctx context.Context, runID int64) (*QualityRun, error) {
	run, err := r.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == QualityRunQueued || run.Status == QualityRunRunning {
		run.Status = QualityRunCancelled
		finished := time.Now()
		run.FinishedAt = &finished
		run.Message = "已取消；已发出的请求会自然结束"
		if err := r.repo.UpdateRun(ctx, run); err != nil {
			return nil, err
		}
	}
	return run, nil
}

func (r *QualityInspectionRunner) execute(runID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	run, err := r.repo.GetRun(ctx, runID)
	if err != nil || run == nil {
		return
	}
	r.mu.Lock()
	if r.inFlight[run.PlanID] {
		r.mu.Unlock()
		// 上一轮仍在执行：把本次排队标记为取消，避免长期挂在 queued。
		r.finish(ctx, run, QualityRunCancelled, "上一轮仍在运行，本次未执行", nil)
		return
	}
	r.inFlight[run.PlanID] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.inFlight, run.PlanID)
		r.mu.Unlock()
	}()

	plan, err := r.repo.GetPlan(ctx, run.PlanID)
	if err != nil || plan == nil {
		r.finish(ctx, run, QualityRunFailed, "计划不存在", nil)
		return
	}

	started := time.Now()
	run.Status = QualityRunRunning
	run.StartedAt = &started
	_ = r.repo.UpdateRun(ctx, run)

	upstreamModel := plan.Model
	run.ResolvedPlatform = plan.Platform
	run.ResolvedUpstreamModel = plan.Model
	run.ResolvedEndpoint = plan.Endpoint
	run.ResolvedSource = CompositeRouteSourceDetector
	if plan.Platform == PlatformComposite {
		decision, resolveErr := r.composite.Resolve(ctx, plan.GroupID, plan.Model, plan.Endpoint)
		if resolveErr != nil {
			r.finish(ctx, run, QualityRunWithError, resolveErr.Error(), nil)
			return
		}
		run.ResolvedPlatform = decision.TargetPlatform
		run.ResolvedUpstreamModel = decision.UpstreamModel
		run.ResolvedEndpoint = decision.Endpoint
		run.ResolvedSource = decision.Source
		upstreamModel = decision.UpstreamModel
	}

	candidates := r.candidates(ctx, plan, run.ResolvedPlatform)
	if len(candidates) == 0 {
		r.finish(ctx, run, QualityRunWithError, "当前分组没有可巡检的账号", nil)
		return
	}
	if len(candidates) > qualityMaxInt(plan.MaxAccounts, 1) {
		candidates = candidates[:qualityMaxInt(plan.MaxAccounts, 1)]
	}

	counts := make(map[string]int)
	statuses := make([]string, 0, 8)
	requestCount := 0

	for _, account := range candidates {
		for _, question := range plan.Questions {
			if !question.Enabled {
				continue
			}
			for round := 0; round < qualityMaxInt(question.Rounds, 1); round++ {
				if current, getErr := r.repo.GetRun(ctx, run.ID); getErr == nil && current.Status == QualityRunCancelled {
					run.Status = QualityRunCancelled
					run.RequestCount = requestCount
					r.finish(ctx, run, QualityRunCancelled, "已取消", nil)
					return
				}
				outcome, probeErr := r.probe.Run(ctx, account.ID, upstreamModel, BuildQualityPrompt(question))
				requestCount++
				result := &QualityResult{
					RunID: run.ID, PlanID: plan.ID, AccountID: account.ID,
					QuestionID: question.ID, Kind: question.Kind,
					Expected: question.Expected, BankVersion: question.Version,
					CreatedAt: time.Now(), FirstTokenMs: outcome.FirstTokenMs, DurationMs: outcome.DurationMs,
				}
				switch {
				case probeErr != nil:
					result.Status = QualityStatusError
					result.FailureClass = QualityFailureLocal
					result.ErrorCode = "PROBE_UNAVAILABLE"
					result.Response = probeErr.Error()
				case outcome.Status == QualityStatusError || outcome.Status == QualityStatusQuota:
					result.Status = outcome.Status
					result.FailureClass = outcome.FailureClass
					result.ErrorCode = outcome.ErrorCode
					result.Response = truncateQualityText(outcome.Text, 12000)
				default:
					status, answer := GradeQualityQuestion(question, outcome.Text)
					result.Status = status
					result.Answer = answer
					result.Response = truncateQualityText(outcome.Text, 100000)
				}

				stored, insertErr := r.repo.InsertResult(ctx, result)
				if insertErr != nil {
					slog.Warn("quality_inspection.insert_result_failed", "error", insertErr)
					continue
				}
				if stored != nil && result.Kind == QualityKindDrawing && result.Status == QualityStatusPass {
					r.saveArtifact(ctx, plan, run, stored)
				}
				counts[result.Status]++
				statuses = append(statuses, result.Status)
			}
		}
	}

	verdict := AggregateQualityVerdict(statuses)
	stats := make(map[string]any, len(counts)+2)
	for key, value := range counts {
		stats[key] = value
	}
	stats["verdict"] = verdict
	stats["accounts"] = len(candidates)

	run.RequestCount = requestCount
	r.finish(ctx, run, QualityRunCompleted, "", stats)
	r.prune(ctx, plan)
}

func (r *QualityInspectionRunner) saveArtifact(ctx context.Context, plan *QualityPlan, run *QualityRun, result *QualityResult) {
	if r.artifacts == nil || !r.artifacts.Available() {
		return
	}
	key, size, truncated, err := r.artifacts.Save(ctx, plan.ID, run.ID, result.ID, result.Response)
	if err != nil {
		slog.Warn("quality_inspection.artifact_save_failed", "error", err, "result_id", result.ID)
		return
	}
	if err := r.repo.UpdateResultArtifact(ctx, result.ID, key, size, truncated); err != nil {
		slog.Warn("quality_inspection.artifact_key_update_failed", "error", err, "result_id", result.ID)
	}
}

func (r *QualityInspectionRunner) finish(ctx context.Context, run *QualityRun, status, message string, stats map[string]any) {
	finished := time.Now()
	run.Status = status
	run.FinishedAt = &finished
	if message != "" {
		run.Message = message
	}
	if stats != nil {
		run.Stats = stats
	}
	if err := r.repo.UpdateRun(ctx, run); err != nil {
		slog.Warn("quality_inspection.update_run_failed", "error", err, "run_id", run.ID)
	}
}

// candidates 选出分组内可巡检账号：启用、可调度、未过期、无临时限额。
func (r *QualityInspectionRunner) candidates(ctx context.Context, plan *QualityPlan, platform string) []Account {
	all, err := r.accounts.ListByGroup(ctx, plan.GroupID)
	if err != nil {
		return nil
	}
	return FilterQualityAccounts(all, platform, time.Now(), qualityMaxInt(plan.MaxAccounts, 1))
}

// FilterQualityAccounts 是账号筛选的纯函数，便于单测覆盖各条排除规则。
func FilterQualityAccounts(all []Account, platform string, now time.Time, limit int) []Account {
	out := make([]Account, 0, len(all))
	for _, account := range all {
		if platform != "" && account.Platform != platform {
			continue
		}
		if account.Status != StatusActive || !account.Schedulable {
			continue
		}
		if account.ExpiresAt != nil && !account.ExpiresAt.After(now) {
			continue
		}
		if futureTime(account.RateLimitResetAt, now) || futureTime(account.OverloadUntil, now) ||
			futureTime(account.TempUnschedulableUntil, now) {
			continue
		}
		out = append(out, account)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func futureTime(value *time.Time, now time.Time) bool { return value != nil && value.After(now) }

// paused 判断是否处于夜间暂停窗口。
func (r *QualityInspectionRunner) paused(plan *QualityPlan, now time.Time) bool {
	location, err := time.LoadLocation(plan.Timezone)
	if err != nil {
		location = time.UTC
	}
	local := now.In(location)
	start := parseClockMinutes(plan.PauseStart)
	end := parseClockMinutes(plan.PauseEnd)
	if start < 0 || end < 0 || start == end {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func parseClockMinutes(value string) int {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return -1
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil {
		return -1
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return -1
	}
	return hour*60 + minute
}

func (r *QualityInspectionRunner) prune(ctx context.Context, plan *QualityPlan) {
	keys, err := r.repo.PruneRuns(ctx, plan.ID, plan.MaxSavedRounds)
	if err != nil {
		slog.Warn("quality_inspection.prune_failed", "error", err)
		return
	}
	for _, key := range keys {
		if err := r.artifacts.Delete(ctx, key); err != nil && !errors.Is(err, errQualityDeleteUnsupported) {
			slog.Warn("quality_inspection.artifact_delete_failed", "error", err)
		}
	}
}

func truncateQualityText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func qualityMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
