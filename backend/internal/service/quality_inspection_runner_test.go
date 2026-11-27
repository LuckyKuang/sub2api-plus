//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// docs/MODEL_QUALITY_INSPECTION.md requires a current request budget at run time,
// disabled plans/switches to issue no requests, and storage for drawing questions.
func TestQualityRunnerExecute_CurrentRuntimeConstraints(t *testing.T) {
	for _, test := range []struct {
		name         string
		change       func(*runnerFixture, *QualityPlan, *QualityRun)
		wantStatus   string
		wantRequests int
	}{
		{"lowered budget", func(f *runnerFixture, _ *QualityPlan, _ *QualityRun) {
			f.settings.settings.QualityInspectionMaxRequestsPerRound = 1
		}, QualityRunWithError, 0},
		{"exact budget", func(f *runnerFixture, _ *QualityPlan, _ *QualityRun) {
			f.settings.settings.QualityInspectionMaxRequestsPerRound = 2
		}, QualityRunCompleted, 2},
		{"disabled switch", func(f *runnerFixture, _ *QualityPlan, _ *QualityRun) {
			f.settings.settings.ModelQualityInspectionEnabled = false
		}, QualityRunWithError, 0},
		{"unreadable settings", func(f *runnerFixture, _ *QualityPlan, _ *QualityRun) {
			f.settings.err = errors.New("settings unavailable")
		}, QualityRunWithError, 0},
		{"disabled plan", func(f *runnerFixture, p *QualityPlan, _ *QualityRun) {
			f.repo.plans[p.ID].Enabled = false
		}, QualityRunWithError, 0},
		{"disabled drawing storage", func(f *runnerFixture, p *QualityPlan, _ *QualityRun) {
			f.repo.questions[p.ID] = []QualityQuestion{{ID: "pelican", Kind: QualityKindDrawing, Enabled: true, Rounds: 1}}
			f.artifacts.available = false
		}, QualityRunWithError, 0},
		{"standard answers without storage", func(f *runnerFixture, _ *QualityPlan, _ *QualityRun) {
			f.artifacts.available = false
		}, QualityRunCompleted, 2},
		{"cancelled queued run", func(f *runnerFixture, _ *QualityPlan, run *QualityRun) {
			_, err := f.runner.StopRun(context.Background(), run.ID)
			if err != nil {
				t.Fatalf("stop: %v", err)
			}
		}, QualityRunCancelled, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRunnerFixture(t, true)
			question := numberQuestion()
			question.Rounds = 2
			plan := f.seedPlan(t, []QualityQuestion{question}, nil)
			run := f.seedRun(t, plan.ID, QualityTriggerManual)
			test.change(f, plan, run)
			f.runner.execute(run.ID)
			stored, err := f.repo.GetRun(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != test.wantStatus || stored.RequestCount != test.wantRequests || f.prober.calls != test.wantRequests {
				t.Errorf("status=%s requests=%d probes=%d, want %s/%d: %s", stored.Status, stored.RequestCount, f.prober.calls, test.wantStatus, test.wantRequests, stored.Message)
			}
			results, _ := f.repo.ListResultsByRun(context.Background(), run.ID)
			if len(results) != test.wantRequests || len(f.artifacts.saved) != 0 {
				t.Errorf("unexpected side effects: results=%d artifact writes=%d", len(results), len(f.artifacts.saved))
			}
		})
	}
}

func TestQualityRunnerRunManual_RejectsCurrentBudgetBeforeEnqueue(t *testing.T) {
	f := newRunnerFixture(t, true)
	q := numberQuestion()
	q.Rounds = 2
	p := f.seedPlan(t, []QualityQuestion{q}, func(p *QualityPlanParams) { p.MaxAccounts = 2 })
	f.settings.settings.QualityInspectionMaxRequestsPerRound = 3
	f.repo.createRunErr = errors.New("must not enqueue an over-budget run")
	_, err := f.runner.RunManual(context.Background(), p.ID)
	var invalid *QualityValidationError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "4") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("expected budget validation for 4 requests / limit 3, got %v", err)
	}
	if len(f.repo.runs) != 0 || f.prober.calls != 0 {
		t.Fatal("rejection must precede queue/probe side effects")
	}
}

func TestQualityRunnerTick_RejectsCurrentBudgetBeforeEnqueue(t *testing.T) {
	f := newRunnerFixture(t, true)
	q := numberQuestion()
	q.Rounds = 2
	f.seedPlan(t, []QualityQuestion{q}, func(p *QualityPlanParams) {
		p.ScheduleEnabled = true
		p.PauseStart, p.PauseEnd = "00:00", "00:00"
	})
	f.settings.settings.QualityInspectionMaxRequestsPerRound = 1
	f.repo.createRunErr = errors.New("must not enqueue an over-budget run")
	f.runner.tick(context.Background())
	if f.repo.createRunCalls != 0 || f.prober.calls != 0 {
		t.Fatalf("over-budget timer attempted enqueue/probe: %d/%d", f.repo.createRunCalls, f.prober.calls)
	}
}

type runnerFixture struct {
	repo      *fakeQualityRepo
	plans     *QualityInspectionPlanService
	runner    *QualityInspectionRunner
	prober    *fakeProber
	artifacts *fakeArtifacts
	accounts  *fakeAccountLister
	settings  *fakeQualitySettings
}

func newRunnerFixture(t *testing.T, enabled bool) *runnerFixture {
	t.Helper()
	repo := newFakeQualityRepo()
	groups := &fakeGroupLookup{groups: map[int64]*Group{
		1: {ID: 1, Name: "Plus", Platform: PlatformOpenAI},
	}}
	artifacts := &fakeArtifacts{available: true}
	plans := NewQualityInspectionPlanService(repo, groups, artifacts, NewQualityCompositeResolver(nil))
	prober := &fakeProber{}
	accounts := &fakeAccountLister{accounts: []Account{{
		ID: 11, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
	}}}
	settings := &fakeQualitySettings{settings: &SystemSettings{ModelQualityInspectionEnabled: enabled}}
	runner := NewQualityInspectionRunner(repo, plans, accounts, prober, artifacts, NewQualityCompositeResolver(nil), settings)
	return &runnerFixture{repo: repo, plans: plans, runner: runner, prober: prober,
		artifacts: artifacts, accounts: accounts, settings: settings}
}

func (f *runnerFixture) seedPlan(t *testing.T, questions []QualityQuestion, mutate func(*QualityPlanParams)) *QualityPlan {
	t.Helper()
	params := basePlanParams()
	params.Questions = questions
	if mutate != nil {
		mutate(&params)
	}
	plan, err := f.plans.Create(context.Background(), params, defaultQualitySettings(), 1)
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return plan
}

func (f *runnerFixture) seedRun(t *testing.T, planID int64, trigger string) *QualityRun {
	t.Helper()
	run, err := f.repo.CreateRun(context.Background(), &QualityRun{
		PlanID: planID, PlanRevision: 1, Trigger: trigger, Slot: time.Now().UnixNano(), Status: QualityRunQueued,
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return run
}

func numberQuestion() QualityQuestion {
	return QualityQuestion{ID: "numeric", Kind: QualityKindNumber, Expected: "323", Enabled: true, Rounds: 1}
}

func TestQualityRunnerExecute_Pass(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	fixture.prober.outcomes = []QualityProbeOutcome{{Status: QualityStatusPass, Text: "最终答案：323"}}

	fixture.runner.execute(run.ID)

	stored, err := fixture.repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.Status != QualityRunCompleted {
		t.Fatalf("status = %s, want completed (%s)", stored.Status, stored.Message)
	}
	if stored.RequestCount != 1 {
		t.Fatalf("request_count = %d, want 1", stored.RequestCount)
	}
	results, _ := fixture.repo.ListResultsByRun(context.Background(), run.ID)
	if len(results) != 1 || results[0].Status != QualityStatusPass || results[0].Answer != "323" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if stored.Stats["verdict"] != QualityStatusPass {
		t.Fatalf("verdict = %v", stored.Stats["verdict"])
	}
}

func TestQualityRunnerExecute_Fail(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	fixture.prober.outcomes = []QualityProbeOutcome{{Status: QualityStatusPass, Text: "最终答案：322"}}

	fixture.runner.execute(run.ID)

	results, _ := fixture.repo.ListResultsByRun(context.Background(), run.ID)
	if len(results) != 1 || results[0].Status != QualityStatusFail {
		t.Fatalf("expected fail, got %+v", results)
	}
	stored, _ := fixture.repo.GetRun(context.Background(), run.ID)
	if stored.Stats["verdict"] != QualityStatusFail {
		t.Fatalf("verdict = %v", stored.Stats["verdict"])
	}
}

func TestQualityRunnerExecute_QuotaIsNotFailure(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	fixture.prober.outcomes = []QualityProbeOutcome{{
		Status: QualityStatusQuota, ErrorCode: "HTTP_429", FailureClass: QualityFailureUpstream,
	}}

	fixture.runner.execute(run.ID)

	results, _ := fixture.repo.ListResultsByRun(context.Background(), run.ID)
	if len(results) != 1 || results[0].Status != QualityStatusQuota {
		t.Fatalf("quota must stay quota, got %+v", results)
	}
	if results[0].FailureClass != QualityFailureUpstream {
		t.Fatalf("failure class = %s", results[0].FailureClass)
	}
}

func TestQualityRunnerExecute_NoCandidates(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	fixture.accounts.accounts = nil
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)

	fixture.runner.execute(run.ID)

	stored, _ := fixture.repo.GetRun(context.Background(), run.ID)
	if stored.Status != QualityRunWithError {
		t.Fatalf("status = %s, want %s", stored.Status, QualityRunWithError)
	}
	if stored.RequestCount != 0 {
		t.Fatalf("request_count = %d, want 0", stored.RequestCount)
	}
	if len(fixture.prober.prompts) != 0 {
		t.Fatal("no probe should run without candidates")
	}
}

func TestQualityRunnerExecute_DrawingSavesArtifact(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{{
		ID: "pelican", Kind: QualityKindDrawing, Enabled: true, Rounds: 1,
	}}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	fixture.prober.outcomes = []QualityProbeOutcome{{Status: QualityStatusPass, Text: "<svg></svg>"}}

	fixture.runner.execute(run.ID)

	if len(fixture.artifacts.saved) != 1 {
		t.Fatalf("artifact saved %d times, want 1", len(fixture.artifacts.saved))
	}
	results, _ := fixture.repo.ListResultsByRun(context.Background(), run.ID)
	if len(results) != 1 || results[0].ArtifactKey == "" {
		t.Fatalf("artifact key not recorded: %+v", results)
	}
}

func TestQualityRunnerExecute_ProbeUnavailable(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	fixture.prober.errs = []error{errTestProbeUnavailable{}}

	fixture.runner.execute(run.ID)

	results, _ := fixture.repo.ListResultsByRun(context.Background(), run.ID)
	if len(results) != 1 || results[0].Status != QualityStatusError || results[0].ErrorCode != "PROBE_UNAVAILABLE" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

type errTestProbeUnavailable struct{}

func (errTestProbeUnavailable) Error() string { return "probe unavailable" }

func TestQualityRunnerTick_RespectsMasterSwitch(t *testing.T) {
	fixture := newRunnerFixture(t, false)
	fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, func(p *QualityPlanParams) {
		p.ScheduleEnabled = true
		p.PauseStart, p.PauseEnd = "00:00", "00:00"
	})
	fixture.runner.tick(context.Background())
	if len(fixture.repo.runs) != 0 {
		t.Fatalf("disabled switch must not create runs, got %d", len(fixture.repo.runs))
	}
}

func TestQualityRunnerTick_DeduplicatesSlot(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	fixture.accounts.accounts = nil
	fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, func(p *QualityPlanParams) {
		p.ScheduleEnabled = true
		p.PauseStart, p.PauseEnd = "00:00", "00:00"
	})
	fixture.runner.tick(context.Background())
	fixture.runner.tick(context.Background())
	if len(fixture.repo.runs) != 1 {
		t.Fatalf("same slot must queue once, got %d runs", len(fixture.repo.runs))
	}
}

func TestQualityRunnerPaused_Window(t *testing.T) {
	runner := &QualityInspectionRunner{}
	overnight := &QualityPlan{PauseStart: "22:00", PauseEnd: "09:00", Timezone: "UTC"}
	if !runner.paused(overnight, time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)) {
		t.Fatal("23:30 must be inside 22:00-09:00")
	}
	if !runner.paused(overnight, time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)) {
		t.Fatal("03:00 must be inside 22:00-09:00")
	}
	if runner.paused(overnight, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("12:00 must be outside 22:00-09:00")
	}
	daytime := &QualityPlan{PauseStart: "09:00", PauseEnd: "12:00", Timezone: "UTC"}
	if !runner.paused(daytime, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatal("10:00 must be inside 09:00-12:00")
	}
	same := &QualityPlan{PauseStart: "09:00", PauseEnd: "09:00", Timezone: "UTC"}
	if runner.paused(same, time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("equal start/end must disable pause")
	}
	invalid := &QualityPlan{PauseStart: "oops", PauseEnd: "09:00", Timezone: "UTC"}
	if runner.paused(invalid, time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("invalid window must not pause")
	}
}

func TestQualityRunnerStopRun_RunningBecomesCancelled(t *testing.T) {
	fixture := newRunnerFixture(t, true)
	plan := fixture.seedPlan(t, []QualityQuestion{numberQuestion()}, nil)
	run := fixture.seedRun(t, plan.ID, QualityTriggerManual)
	started := time.Now()
	run.Status = QualityRunRunning
	run.StartedAt = &started
	if err := fixture.repo.UpdateRun(context.Background(), run); err != nil {
		t.Fatalf("update: %v", err)
	}
	stopped, err := fixture.runner.StopRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopped.Status != QualityRunCancelled {
		t.Fatalf("status = %s, want cancelled", stopped.Status)
	}
}

func TestFilterQualityAccounts(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false},
		{ID: 3, Platform: PlatformOpenAI, Status: "disabled", Schedulable: true},
		{ID: 4, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, ExpiresAt: &past},
		{ID: 5, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, RateLimitResetAt: &future},
		{ID: 6, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true},
		{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, ExpiresAt: &future},
	}
	got := FilterQualityAccounts(accounts, PlatformOpenAI, now, 10)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 7 {
		t.Fatalf("unexpected candidates: %+v", got)
	}
	limited := FilterQualityAccounts(accounts, PlatformOpenAI, now, 1)
	if len(limited) != 1 || limited[0].ID != 1 {
		t.Fatalf("limit failed: %+v", limited)
	}
	all := FilterQualityAccounts(accounts, "", now, 0)
	if len(all) != 3 {
		t.Fatalf("platform-agnostic filter = %d, want 3", len(all))
	}
}
