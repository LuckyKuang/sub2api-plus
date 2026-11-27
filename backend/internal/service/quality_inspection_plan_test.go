//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
)

func newPlanTestService(t *testing.T, storageAvailable bool) (*QualityInspectionPlanService, *fakeQualityRepo) {
	t.Helper()
	repo := newFakeQualityRepo()
	groups := &fakeGroupLookup{groups: map[int64]*Group{
		1: {ID: 1, Name: "Plus", Platform: PlatformOpenAI},
		2: {ID: 2, Name: "Composite", Platform: PlatformComposite},
	}}
	artifacts := &fakeArtifacts{available: storageAvailable}
	composite := NewQualityCompositeResolver(nil)
	return NewQualityInspectionPlanService(repo, groups, artifacts, composite), repo
}

func basePlanParams() QualityPlanParams {
	return QualityPlanParams{
		GroupID: 1, Model: "gpt-5.1-codex", Effort: "medium",
		Enabled: true, ScheduleEnabled: false, IntervalMinutes: 60,
		MaxAccounts: 1, MaxSavedRounds: 20,
	}
}

func TestQualityPlanValidate_ConfiguredButDisabledStorage(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	svc.artifacts = NewQualityArtifactStore(func() (*ImageResultUploader, bool) {
		return NewImageResultUploader(&stubImageStorage{}, "images", true, 0, nil), false
	})
	params := basePlanParams()
	params.Questions = []QualityQuestion{{ID: "pelican", Kind: QualityKindDrawing, Enabled: true, Rounds: 1}}
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); !errors.Is(err, ErrQualityStorageUnavailable) {
		t.Fatalf("drawing plan with disabled storage: %v", err)
	}
	params.Questions = []QualityQuestion{numberQuestion()}
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); err != nil {
		t.Fatalf("standard-answer plan must not require storage: %v", err)
	}
}

func defaultQualitySettings() QualityPlanSettings {
	return QualityPlanSettings{QuarantineEnabled: false, MaxRequestsPerRound: 30, DefaultStabilityRounds: 5}
}

func TestQualityPlanCreate_DefaultsToBuiltinQuestions(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	plan, err := svc.Create(context.Background(), basePlanParams(), defaultQualitySettings(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(plan.Questions) != 6 {
		t.Fatalf("expected 6 builtin questions, got %d", len(plan.Questions))
	}
	if plan.Platform != PlatformOpenAI || plan.GroupName != "Plus" {
		t.Fatalf("platform/group not derived: %+v", plan)
	}
	if plan.Revision != 1 {
		t.Fatalf("revision = %d, want 1", plan.Revision)
	}
	for _, question := range plan.Questions {
		if question.Version == "" {
			t.Fatalf("question %s missing version", question.ID)
		}
	}
	if got := svc.Estimate(plan); got != 6 {
		t.Fatalf("estimate = %d, want 6", got)
	}
}

func TestQualityPlanValidate_RejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*QualityPlanParams)
	}{
		{"missing group", func(p *QualityPlanParams) { p.GroupID = 0 }},
		{"unknown group", func(p *QualityPlanParams) { p.GroupID = 99 }},
		{"empty model", func(p *QualityPlanParams) { p.Model = "  " }},
		{"bad effort", func(p *QualityPlanParams) { p.Effort = "turbo" }},
		{"interval too small", func(p *QualityPlanParams) { p.IntervalMinutes = 10 }},
		{"interval too large", func(p *QualityPlanParams) { p.IntervalMinutes = 2000 }},
		{"accounts too many", func(p *QualityPlanParams) { p.MaxAccounts = 9 }},
		{"rounds retention too large", func(p *QualityPlanParams) { p.MaxSavedRounds = 500 }},
		{"cooldown too small", func(p *QualityPlanParams) { p.QuarantineCooldownMinutes = 1 }},
		{"fail rounds too large", func(p *QualityPlanParams) { p.QuarantineFailRounds = 9 }},
		{"quarantine gate off", func(p *QualityPlanParams) { p.QuarantineEnabled = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newPlanTestService(t, true)
			params := basePlanParams()
			tc.mutate(&params)
			if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); err == nil {
				t.Fatal("expected validation error")
			} else {
				var validation *QualityValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("error should be validation error, got %T: %v", err, err)
				}
			}
		})
	}
}

func TestQualityPlanValidate_QuarantineNeedsGradableQuestion(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	settings := defaultQualitySettings()
	settings.QuarantineEnabled = true
	params := basePlanParams()
	params.QuarantineEnabled = true
	params.Questions = []QualityQuestion{{ID: "pelican", Kind: QualityKindDrawing, Enabled: true, Rounds: 1}}

	if _, err := svc.Create(context.Background(), params, settings, 1); err == nil {
		t.Fatal("drawing-only plan must not enable quarantine")
	}

	params.Questions = append(params.Questions, QualityQuestion{
		ID: "numeric", Kind: QualityKindNumber, Expected: "323", Enabled: true, Rounds: 1,
	})
	if _, err := svc.Create(context.Background(), params, settings, 1); err != nil {
		t.Fatalf("gradable question should allow quarantine: %v", err)
	}
}

func TestQualityPlanValidate_DrawingRequiresStorage(t *testing.T) {
	svc, _ := newPlanTestService(t, false)
	params := basePlanParams()
	params.Questions = []QualityQuestion{{ID: "pelican", Kind: QualityKindDrawing, Enabled: true, Rounds: 1}}
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); !errors.Is(err, ErrQualityStorageUnavailable) {
		t.Fatalf("expected storage error, got %v", err)
	}

	params.Questions = []QualityQuestion{{ID: "numeric", Kind: QualityKindNumber, Expected: "323", Enabled: true, Rounds: 1}}
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); err != nil {
		t.Fatalf("answer-only plan must work without storage: %v", err)
	}
}

func TestQualityPlanValidate_RequestBudget(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	params := basePlanParams()
	params.MaxAccounts = 2
	settings := defaultQualitySettings()
	settings.MaxRequestsPerRound = 5
	if _, err := svc.Create(context.Background(), params, settings, 1); err == nil {
		t.Fatal("2 accounts x 6 default questions should exceed the budget of 5")
	}
}

func TestQualityPlanEstimate(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	plan := &QualityPlan{
		MaxAccounts: 2,
		Questions: []QualityQuestion{
			{Enabled: true, Rounds: 2}, {Enabled: true, Rounds: 1},
			{Enabled: true, Rounds: 3}, {Enabled: false, Rounds: 3},
		},
	}
	if got := svc.Estimate(plan); got != 12 {
		t.Fatalf("estimate = %d, want 12", got)
	}
	if got := svc.Estimate(nil); got != 0 {
		t.Fatalf("nil plan estimate = %d, want 0", got)
	}
}

func TestQualityPlanQuestions_Validation(t *testing.T) {
	cases := []struct {
		name      string
		questions []QualityQuestion
	}{
		{"invalid id", []QualityQuestion{{ID: "Bad ID", Kind: QualityKindNumber, Expected: "1", Enabled: true}}},
		{"bad kind", []QualityQuestion{{ID: "custom", Kind: "essay", Prompt: "x", Enabled: true}}},
		{"missing expected", []QualityQuestion{{ID: "custom", Kind: QualityKindNumber, Prompt: "x", Enabled: true}}},
		{"rounds too high", []QualityQuestion{{ID: "custom", Kind: QualityKindNumber, Expected: "1", Prompt: "x", Enabled: true, Rounds: 4}}},
		{"choice expected not in choices", []QualityQuestion{{ID: "custom", Kind: QualityKindChoice, Expected: "4", Choices: []string{"1", "2"}, Prompt: "x", Enabled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newPlanTestService(t, true)
			params := basePlanParams()
			params.Questions = tc.questions
			if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestQualityPlanQuestions_DuplicateRejected(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	params := basePlanParams()
	params.Questions = []QualityQuestion{
		{ID: "numeric", Kind: QualityKindNumber, Expected: "323", Enabled: true},
		{ID: "numeric", Kind: QualityKindNumber, Expected: "323", Enabled: true},
	}
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); err == nil {
		t.Fatal("duplicate question ids must be rejected")
	}
}

func TestQualityPlanCreate_DuplicateGroupModelConflict(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	if _, err := svc.Create(context.Background(), basePlanParams(), defaultQualitySettings(), 1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := svc.Create(context.Background(), basePlanParams(), defaultQualitySettings(), 1)
	if !errors.Is(err, ErrQualityPlanConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestQualityPlanUpdate_RevisionConflict(t *testing.T) {
	svc, repo := newPlanTestService(t, true)
	plan, err := svc.Create(context.Background(), basePlanParams(), defaultQualitySettings(), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	params := basePlanParams()
	params.Name = "renamed"
	updated, err := svc.Update(context.Background(), plan.ID, params, defaultQualitySettings(), plan.Revision)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Revision != plan.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, plan.Revision+1)
	}
	if _, err := svc.Update(context.Background(), plan.ID, params, defaultQualitySettings(), plan.Revision); !errors.Is(err, ErrQualityPlanConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	if len(repo.plans[plan.ID].Questions) != 6 {
		t.Fatalf("questions should be replaced, got %d", len(repo.plans[plan.ID].Questions))
	}
}

func TestQualityPlanComposite_RequiresResolvableModel(t *testing.T) {
	svc, _ := newPlanTestService(t, true)
	params := basePlanParams()
	params.GroupID = 2
	params.Model = "totally-unknown-model-xyz"
	if _, err := svc.Create(context.Background(), params, defaultQualitySettings(), 1); !errors.Is(err, ErrQualityCompositeUnresolved) {
		t.Fatalf("expected composite unresolved, got %v", err)
	}
}
