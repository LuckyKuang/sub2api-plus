package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

// fakeQualityRepo 是 QualityInspectionRepository 的内存实现，供计划/执行器单测使用。
type fakeQualityRepo struct {
	mu sync.Mutex

	plans     map[int64]*QualityPlan
	questions map[int64][]QualityQuestion
	runs      map[int64]*QualityRun
	results   map[int64]*QualityResult
	runKeys   map[string]bool

	nextPlanID   int64
	nextRunID    int64
	nextResultID int64

	createRunErr  error
	pruneKeys     []string
	artifactKeys  map[int64]string
	artifactSizes map[int64]int64
	quarantines   []*QualityQuarantine
	states        map[int64]string
}

func newFakeQualityRepo() *fakeQualityRepo {
	return &fakeQualityRepo{
		plans:         map[int64]*QualityPlan{},
		questions:     map[int64][]QualityQuestion{},
		runs:          map[int64]*QualityRun{},
		results:       map[int64]*QualityResult{},
		runKeys:       map[string]bool{},
		artifactKeys:  map[int64]string{},
		artifactSizes: map[int64]int64{},
		states:        map[int64]string{},
	}
}

func (f *fakeQualityRepo) CreatePlan(_ context.Context, plan *QualityPlan) (*QualityPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.plans {
		if existing.GroupID == plan.GroupID && existing.Model == plan.Model {
			return nil, ErrQualityPlanConflict
		}
	}
	f.nextPlanID++
	stored := *plan
	stored.ID = f.nextPlanID
	stored.Revision = 1
	f.plans[stored.ID] = &stored
	f.questions[stored.ID] = append([]QualityQuestion{}, plan.Questions...)
	result := stored
	result.Questions = append([]QualityQuestion{}, plan.Questions...)
	return &result, nil
}

func (f *fakeQualityRepo) UpdatePlan(_ context.Context, plan *QualityPlan, expectedRevision int64) (*QualityPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.plans[plan.ID]
	if !ok {
		return nil, ErrQualityPlanNotFound
	}
	if existing.Revision != expectedRevision {
		return nil, ErrQualityPlanConflict
	}
	stored := *plan
	stored.Revision = expectedRevision + 1
	stored.CreatedAt = existing.CreatedAt
	f.plans[plan.ID] = &stored
	f.questions[plan.ID] = append([]QualityQuestion{}, plan.Questions...)
	result := stored
	result.Questions = append([]QualityQuestion{}, plan.Questions...)
	return &result, nil
}

func (f *fakeQualityRepo) DeletePlan(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.plans, id)
	delete(f.questions, id)
	return nil
}

func (f *fakeQualityRepo) GetPlan(_ context.Context, id int64) (*QualityPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, ok := f.plans[id]
	if !ok {
		return nil, ErrQualityPlanNotFound
	}
	copied := *plan
	copied.Questions = append([]QualityQuestion{}, f.questions[id]...)
	return &copied, nil
}

func (f *fakeQualityRepo) ListPlans(_ context.Context) ([]*QualityPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*QualityPlan, 0, len(f.plans))
	for id, plan := range f.plans {
		copied := *plan
		copied.Questions = append([]QualityQuestion{}, f.questions[id]...)
		out = append(out, &copied)
	}
	return out, nil
}

func (f *fakeQualityRepo) ListSchedulablePlans(ctx context.Context) ([]*QualityPlan, error) {
	all, err := f.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*QualityPlan, 0, len(all))
	for _, plan := range all {
		if plan.Enabled && plan.ScheduleEnabled {
			out = append(out, plan)
		}
	}
	return out, nil
}

func (f *fakeQualityRepo) ReplaceQuestions(_ context.Context, planID int64, questions []QualityQuestion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questions[planID] = append([]QualityQuestion{}, questions...)
	return nil
}

func (f *fakeQualityRepo) CreateRun(_ context.Context, run *QualityRun) (*QualityRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createRunErr != nil {
		return nil, f.createRunErr
	}
	key := qualityRunDedupeKey(run)
	if f.runKeys[key] {
		return nil, ErrQualityPlanConflict
	}
	f.runKeys[key] = true
	f.nextRunID++
	stored := *run
	stored.ID = f.nextRunID
	stored.Status = QualityRunQueued
	f.runs[stored.ID] = &stored
	result := stored
	return &result, nil
}

func (f *fakeQualityRepo) UpdateRun(_ context.Context, run *QualityRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[run.ID]; !ok {
		return ErrQualityRunNotFound
	}
	stored := *run
	f.runs[run.ID] = &stored
	return nil
}

func (f *fakeQualityRepo) GetRun(_ context.Context, id int64) (*QualityRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return nil, ErrQualityRunNotFound
	}
	copied := *run
	return &copied, nil
}

func (f *fakeQualityRepo) ListRuns(_ context.Context, planID int64, limit int) ([]*QualityRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*QualityRun, 0, len(f.runs))
	for _, run := range f.runs {
		if run.PlanID == planID {
			copied := *run
			out = append(out, &copied)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeQualityRepo) InsertResult(_ context.Context, result *QualityResult) (*QualityResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextResultID++
	stored := *result
	stored.ID = f.nextResultID
	f.results[stored.ID] = &stored
	copied := stored
	return &copied, nil
}

func (f *fakeQualityRepo) UpdateResultArtifact(_ context.Context, id int64, key string, size int64, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if result, ok := f.results[id]; ok {
		result.ArtifactKey = key
		result.ArtifactBytes = size
	}
	f.artifactKeys[id] = key
	f.artifactSizes[id] = size
	return nil
}

func (f *fakeQualityRepo) ListResultsByRun(_ context.Context, runID int64) ([]*QualityResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*QualityResult, 0, len(f.results))
	for _, result := range f.results {
		if result.RunID == runID {
			copied := *result
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeQualityRepo) GetResult(_ context.Context, id int64) (*QualityResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result, ok := f.results[id]
	if !ok {
		return nil, ErrQualityResultNotFound
	}
	copied := *result
	return &copied, nil
}

func (f *fakeQualityRepo) PruneRuns(_ context.Context, _ int64, _ int) ([]string, error) {
	return f.pruneKeys, nil
}

func (f *fakeQualityRepo) ListQuarantines(_ context.Context) ([]*QualityQuarantine, error) {
	return f.quarantines, nil
}

func (f *fakeQualityRepo) SetQuarantineState(_ context.Context, accountID int64, state, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[accountID] = state
	return nil
}

func qualityRunDedupeKey(run *QualityRun) string {
	return run.Trigger + ":" + itoa(run.PlanID) + ":" + itoa(run.Slot)
}

func itoa(value int64) string {
	// 避免引入 strconv 只为一个内部测试键。
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buf := make([]byte, 0, 20)
	for value > 0 {
		buf = append([]byte{byte('0' + value%10)}, buf...)
		value /= 10
	}
	if negative {
		return "-" + string(buf)
	}
	return string(buf)
}

// fakeGroupLookup 只实现按 ID 取分组。
type fakeGroupLookup struct {
	groups map[int64]*Group
	err    error
}

func (f *fakeGroupLookup) GetByID(_ context.Context, id int64) (*Group, error) {
	if f.err != nil {
		return nil, f.err
	}
	group, ok := f.groups[id]
	if !ok {
		return nil, errors.New("group not found")
	}
	return group, nil
}

// fakeAccountLister 只实现按分组列账号。
type fakeAccountLister struct {
	accounts []Account
	err      error
}

func (f *fakeAccountLister) ListByGroup(_ context.Context, _ int64) ([]Account, error) {
	return f.accounts, f.err
}

// fakeQualitySettings 只实现读取系统设置。
type fakeQualitySettings struct {
	settings *SystemSettings
	err      error
}

func (f *fakeQualitySettings) GetAllSettings(_ context.Context) (*SystemSettings, error) {
	return f.settings, f.err
}

func (f *fakeQualitySettings) ModelQualityInspectionEnabled(_ context.Context) bool {
	return f.err == nil && f.settings != nil && f.settings.ModelQualityInspectionEnabled
}

// fakeProber 按预设脚本返回探测结果。
type fakeProber struct {
	mu       sync.Mutex
	outcomes []QualityProbeOutcome
	errs     []error
	calls    int
	prompts  []string
}

func (f *fakeProber) Run(_ context.Context, _ int64, _ string, prompt string) (QualityProbeOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.calls
	f.calls++
	f.prompts = append(f.prompts, prompt)
	var outcome QualityProbeOutcome
	if index < len(f.outcomes) {
		outcome = f.outcomes[index]
	}
	var err error
	if index < len(f.errs) {
		err = f.errs[index]
	}
	return outcome, err
}

// fakeArtifacts 记录写入的对象，并支持模拟不可用/删除失败。
type fakeArtifacts struct {
	available bool
	saved     []string
	deleted   []string
	saveErr   error
	deleteErr error
	openData  []byte
}

func (f *fakeArtifacts) Available() bool { return f.available }

func (f *fakeArtifacts) Save(_ context.Context, planID, runID, resultID int64, html string) (string, int64, bool, error) {
	if f.saveErr != nil {
		return "", 0, false, f.saveErr
	}
	key := "quality/" + itoa(planID) + "/" + itoa(runID) + "-" + itoa(resultID) + ".html"
	f.saved = append(f.saved, key)
	return key, int64(len(html)), false, nil
}

func (f *fakeArtifacts) Open(_ context.Context, _ string) ([]byte, error) {
	return f.openData, nil
}

func (f *fakeArtifacts) Delete(_ context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, key)
	return nil
}

// fakeCompositeRepo 提供复合路由列表。
type fakeCompositeRepo struct {
	routes []CompositeModelRoute
	err    error
}

func (f *fakeCompositeRepo) ListByGroup(_ context.Context, _ int64, _ bool) ([]CompositeModelRoute, error) {
	return f.routes, f.err
}

func (f *fakeCompositeRepo) Create(_ context.Context, _ *CompositeModelRoute) error { return nil }
func (f *fakeCompositeRepo) Update(_ context.Context, _ *CompositeModelRoute) error { return nil }
func (f *fakeCompositeRepo) Delete(_ context.Context, _ int64) error                { return nil }
func (f *fakeCompositeRepo) DeleteByGroup(_ context.Context, _ int64) error         { return nil }

var _ QualityInspectionRepository = (*fakeQualityRepo)(nil)
var _ QualityGroupLookup = (*fakeGroupLookup)(nil)
var _ QualityAccountLister = (*fakeAccountLister)(nil)
var _ QualitySettingsSource = (*fakeQualitySettings)(nil)
var _ QualitySwitchReader = (*fakeQualitySettings)(nil)
var _ QualityProber = (*fakeProber)(nil)
var _ QualityArtifactWriter = (*fakeArtifacts)(nil)
var _ CompositeModelRouteRepository = (*fakeCompositeRepo)(nil)

var _ = time.Now
