package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	qualityQuestionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	qualityKindValues        = map[string]bool{
		QualityKindNumber: true, QualityKindChoice: true, QualityKindJSON: true,
		QualityKindDrawing: true, QualityKindReference: true,
	}
)

// QualityValidationError 表示调用方输入不合法（HTTP 400）。
type QualityValidationError struct{ Message string }

func (e *QualityValidationError) Error() string { return e.Message }

func qualityInvalid(format string, args ...any) error {
	return &QualityValidationError{Message: fmt.Sprintf(format, args...)}
}

// ErrQualityPlanConflict 计划唯一键冲突或乐观锁冲突（HTTP 409）。
var ErrQualityPlanConflict = errors.New("quality inspection plan conflict")

var qualityEffortValues = map[string]bool{
	"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

// QualityPlanSettings 来自系统设置的全局约束，由调用方注入，避免服务直接读设置。
type QualityPlanSettings struct {
	QuarantineEnabled      bool
	MaxRequestsPerRound    int
	DefaultStabilityRounds int
}

// QualityPlanParams 计划保存入参。
type QualityPlanParams struct {
	GroupID                   int64
	Name                      string
	Model                     string
	Endpoint                  string
	Effort                    string
	Enabled                   bool
	ScheduleEnabled           bool
	IntervalMinutes           int
	PauseStart                string
	PauseEnd                  string
	Timezone                  string
	MaxAccounts               int
	MaxSavedRounds            int
	QuarantineEnabled         bool
	QuarantineCooldownMinutes int
	QuarantineFailRounds      int
	Questions                 []QualityQuestion
}

// QualityInspectionPlanService 计划的校验与编排。
type QualityInspectionPlanService struct {
	repo      QualityInspectionRepository
	groups    QualityGroupLookup
	artifacts QualityArtifactWriter
	composite *QualityCompositeResolver
}

func NewQualityInspectionPlanService(repo QualityInspectionRepository, groups QualityGroupLookup,
	artifacts QualityArtifactWriter, composite *QualityCompositeResolver) *QualityInspectionPlanService {
	return &QualityInspectionPlanService{repo: repo, groups: groups, artifacts: artifacts, composite: composite}
}

func (s *QualityInspectionPlanService) List(ctx context.Context) ([]*QualityPlan, error) {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	s.fillGroupNames(ctx, plans)
	return plans, nil
}

func (s *QualityInspectionPlanService) Get(ctx context.Context, id int64) (*QualityPlan, error) {
	plan, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	s.fillGroupNames(ctx, []*QualityPlan{plan})
	return plan, nil
}

func (s *QualityInspectionPlanService) Create(ctx context.Context, params QualityPlanParams, settings QualityPlanSettings, actorID int64) (*QualityPlan, error) {
	plan, err := s.validate(ctx, params, settings, nil)
	if err != nil {
		return nil, err
	}
	plan.CreatedBy = actorID
	created, err := s.repo.CreatePlan(ctx, plan)
	if err != nil {
		if errors.Is(err, ErrQualityPlanConflict) {
			return nil, fmt.Errorf("%w: 该分组已有相同模型的巡检计划", ErrQualityPlanConflict)
		}
		return nil, err
	}
	s.fillGroupNames(ctx, []*QualityPlan{created})
	return created, nil
}

func (s *QualityInspectionPlanService) Update(ctx context.Context, id int64, params QualityPlanParams, settings QualityPlanSettings, expectedRevision int64) (*QualityPlan, error) {
	current, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	plan, err := s.validate(ctx, params, settings, current)
	if err != nil {
		return nil, err
	}
	plan.ID = id
	plan.CreatedBy = current.CreatedBy
	updated, err := s.repo.UpdatePlan(ctx, plan, expectedRevision)
	if err != nil {
		if errors.Is(err, ErrQualityPlanConflict) {
			return nil, err
		}
		return nil, err
	}
	s.fillGroupNames(ctx, []*QualityPlan{updated})
	return updated, nil
}

func (s *QualityInspectionPlanService) Delete(ctx context.Context, id int64) error {
	return s.repo.DeletePlan(ctx, id)
}

// Estimate 返回单轮预估请求数：账号数 × 启用题目请求次数之和。
func (s *QualityInspectionPlanService) Estimate(plan *QualityPlan) int {
	if plan == nil {
		return 0
	}
	total := 0
	for _, question := range plan.Questions {
		if question.Enabled {
			total += question.Rounds
		}
	}
	accounts := plan.MaxAccounts
	if accounts < 1 {
		accounts = 1
	}
	return total * accounts
}

func (s *QualityInspectionPlanService) validate(ctx context.Context, params QualityPlanParams, settings QualityPlanSettings, current *QualityPlan) (*QualityPlan, error) {
	if params.GroupID <= 0 {
		return nil, qualityInvalid("请选择分组")
	}
	group, err := s.groups.GetByID(ctx, params.GroupID)
	if err != nil || group == nil {
		return nil, qualityInvalid("分组不存在")
	}
	model := strings.TrimSpace(params.Model)
	if model == "" || utf8.RuneCountInString(model) > 160 {
		return nil, qualityInvalid("模型不能为空且不超过 160 个字符")
	}
	effort := strings.TrimSpace(params.Effort)
	if effort == "" {
		effort = "medium"
	}
	if !qualityEffortValues[effort] {
		return nil, qualityInvalid("思考强度无效")
	}
	endpoint := strings.TrimSpace(params.Endpoint)
	if endpoint == "" {
		endpoint = CompositeRouteEndpointAny
	}
	platform := strings.TrimSpace(group.Platform)
	if platform == "" {
		return nil, qualityInvalid("分组平台信息不完整")
	}
	// 复合分组必须能把该模型解析到具体平台，否则保存即失败。
	if platform == PlatformComposite {
		decision, resolveErr := s.composite.Resolve(ctx, group.ID, model, endpoint)
		if resolveErr != nil {
			return nil, resolveErr
		}
		platform = decision.TargetPlatform
	}

	interval := defaultInt(params.IntervalMinutes, 60)
	if interval < 30 || interval > 1440 {
		return nil, qualityInvalid("巡检间隔应为 30–1440 分钟")
	}
	maxAccounts := defaultInt(params.MaxAccounts, 1)
	if maxAccounts < 1 || maxAccounts > 5 {
		return nil, qualityInvalid("每轮账号数应为 1–5")
	}
	maxRounds := defaultInt(params.MaxSavedRounds, 20)
	if maxRounds < 1 || maxRounds > 200 {
		return nil, qualityInvalid("轮次保留上限应为 1–200")
	}
	cooldown := defaultInt(params.QuarantineCooldownMinutes, 60)
	if cooldown < 5 || cooldown > 10080 {
		return nil, qualityInvalid("剔除冷却时长应为 5–10080 分钟")
	}
	failRounds := defaultInt(params.QuarantineFailRounds, 2)
	if failRounds < 1 || failRounds > 5 {
		return nil, qualityInvalid("剔除门槛应为连续 1–5 轮")
	}
	if params.QuarantineEnabled && !settings.QuarantineEnabled {
		return nil, qualityInvalid("请先在系统设置中开启自动剔除能力")
	}

	questions, err := s.normalizeQuestions(params.Questions)
	if err != nil {
		return nil, err
	}
	if params.QuarantineEnabled {
		gradable := false
		for _, question := range questions {
			if question.Enabled && isGradableKind(question.Kind) {
				gradable = true
				break
			}
		}
		if !gradable {
			return nil, qualityInvalid("自动剔除需要至少一道启用的数值、选择或 JSON 题")
		}
	}
	if hasDrawing(questions) && (s.artifacts == nil || !s.artifacts.Available()) {
		return nil, ErrQualityStorageUnavailable
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = group.Name + " · " + model
	}
	if utf8.RuneCountInString(name) > 120 {
		return nil, qualityInvalid("计划名称不超过 120 个字符")
	}

	plan := &QualityPlan{
		GroupID: params.GroupID, GroupName: group.Name, Name: name, Platform: platform,
		Model: model, Endpoint: endpoint, Effort: effort,
		Enabled: params.Enabled, ScheduleEnabled: params.ScheduleEnabled,
		IntervalMinutes: interval, PauseStart: defaultString(params.PauseStart, "22:00"),
		PauseEnd: defaultString(params.PauseEnd, "09:00"), Timezone: defaultString(params.Timezone, "Asia/Shanghai"),
		MaxAccounts: maxAccounts, MaxSavedRounds: maxRounds,
		QuarantineEnabled: params.QuarantineEnabled, QuarantineCooldownMinutes: cooldown,
		QuarantineFailRounds: failRounds, Questions: questions,
	}
	if current != nil {
		plan.Revision = current.Revision
	}
	if limit := settings.MaxRequestsPerRound; limit > 0 && s.Estimate(plan) > limit {
		return nil, qualityInvalid("单轮预估请求数 %d 超过上限 %d，请减少账号数或题目次数", s.Estimate(plan), limit)
	}
	return plan, nil
}

// normalizeQuestions 补齐默认值、校验字段并重算版本。
func (s *QualityInspectionPlanService) normalizeQuestions(input []QualityQuestion) ([]QualityQuestion, error) {
	if len(input) == 0 {
		input = BuiltinQualityQuestions()
	}
	if len(input) > 30 {
		return nil, qualityInvalid("题目数量不能超过 30 道")
	}
	seen := make(map[string]bool, len(input))
	out := make([]QualityQuestion, 0, len(input))
	for index, raw := range input {
		question := raw
		question.ID = strings.TrimSpace(question.ID)
		if question.ID == "" || !qualityQuestionIDPattern.MatchString(question.ID) {
			return nil, qualityInvalid("题目 ID 无效：%s", raw.ID)
		}
		if seen[question.ID] {
			return nil, qualityInvalid("题目 ID 重复：%s", question.ID)
		}
		seen[question.ID] = true

		if builtin, ok := BuiltinQualityQuestion(question.ID); ok {
			if strings.TrimSpace(question.Title) == "" {
				question.Title = builtin.Title
			}
			if strings.TrimSpace(question.Kind) == "" {
				question.Kind = builtin.Kind
			}
			if strings.TrimSpace(question.Prompt) == "" {
				question.Prompt = builtin.Prompt
			}
			if strings.TrimSpace(question.Instructions) == "" {
				question.Instructions = builtin.Instructions
			}
			if strings.TrimSpace(question.Expected) == "" {
				question.Expected = builtin.Expected
			}
			if len(question.Choices) == 0 {
				question.Choices = builtin.Choices
			}
			if strings.TrimSpace(question.Unit) == "" {
				question.Unit = builtin.Unit
			}
		}
		if !qualityKindValues[question.Kind] {
			return nil, qualityInvalid("题目 %s 的判题类型无效", question.ID)
		}
		if strings.TrimSpace(question.Prompt) == "" {
			return nil, qualityInvalid("题目 %s 缺少题干", question.ID)
		}
		question.Rounds = defaultInt(question.Rounds, 1)
		if question.Rounds < 1 || question.Rounds > 3 {
			return nil, qualityInvalid("题目 %s 的请求次数应为 1–3", question.ID)
		}
		question.Position = index
		question.Version = QuestionVersion(question)
		switch question.Kind {
		case QualityKindNumber, QualityKindChoice, QualityKindJSON:
			if strings.TrimSpace(question.Expected) == "" {
				return nil, qualityInvalid("题目 %s 需要标准答案", question.ID)
			}
		}
		if question.Kind == QualityKindChoice {
			if len(question.Choices) == 0 {
				return nil, qualityInvalid("选择题 %s 需要选项", question.ID)
			}
			if !containsString(question.Choices, question.Expected) {
				return nil, qualityInvalid("题目 %s 的标准答案必须属于选项", question.ID)
			}
		}
		out = append(out, question)
	}
	return out, nil
}

func (s *QualityInspectionPlanService) fillGroupNames(ctx context.Context, plans []*QualityPlan) {
	for _, plan := range plans {
		if plan == nil || plan.GroupID <= 0 {
			continue
		}
		if group, err := s.groups.GetByID(ctx, plan.GroupID); err == nil && group != nil {
			plan.GroupName = group.Name
		}
	}
}

func isGradableKind(kind string) bool {
	return kind == QualityKindNumber || kind == QualityKindChoice || kind == QualityKindJSON
}

func hasDrawing(questions []QualityQuestion) bool {
	for _, question := range questions {
		if question.Enabled && question.Kind == QualityKindDrawing {
			return true
		}
	}
	return false
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// parseIntWithRange 解析设置值并夹在合法区间；非法值回退默认值。
func parseIntWithRange(raw string, fallback, low, high int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return clampInt(value, low, high)
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
