package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

// QualityInspectionHandler 模型质量巡检的管理端接口（仅管理员）。
type QualityInspectionHandler struct {
	plans     *service.QualityInspectionPlanService
	runner    *service.QualityInspectionRunner
	repo      service.QualityInspectionRepository
	artifacts *service.QualityArtifactStore
	settings  *service.SettingService
	signKey   []byte
}

func NewQualityInspectionHandler(plans *service.QualityInspectionPlanService, runner *service.QualityInspectionRunner,
	repo service.QualityInspectionRepository, artifacts *service.QualityArtifactStore,
	settings *service.SettingService, jwtSecret string) *QualityInspectionHandler {
	key := []byte(jwtSecret)
	if len(key) == 0 {
		// 未配置 JWT 密钥时退化为进程内随机密钥：链接仅在本次进程内有效，
		// 重启后旧链接失效，不会出现可预测签名。
		fallback := make([]byte, 32)
		_, _ = rand.Read(fallback)
		key = fallback
	}
	return &QualityInspectionHandler{
		plans: plans, runner: runner, repo: repo, artifacts: artifacts, settings: settings,
		signKey: key,
	}
}

// ---------- 视图模型 ----------

type qualityQuestionView struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Kind         string   `json:"kind"`
	Prompt       string   `json:"prompt"`
	Instructions string   `json:"instructions"`
	Expected     string   `json:"expected"`
	Choices      []string `json:"choices"`
	Unit         string   `json:"unit"`
	Rounds       int      `json:"rounds"`
	Enabled      bool     `json:"enabled"`
	Version      string   `json:"version"`
}

type qualityPlanView struct {
	ID                        int64                 `json:"id"`
	GroupID                   int64                 `json:"group_id"`
	GroupName                 string                `json:"group_name"`
	Name                      string                `json:"name"`
	Platform                  string                `json:"platform"`
	Model                     string                `json:"model"`
	Endpoint                  string                `json:"endpoint"`
	Effort                    string                `json:"effort"`
	Enabled                   bool                  `json:"enabled"`
	ScheduleEnabled           bool                  `json:"schedule_enabled"`
	IntervalMinutes           int                   `json:"interval_minutes"`
	PauseStart                string                `json:"pause_start"`
	PauseEnd                  string                `json:"pause_end"`
	Timezone                  string                `json:"timezone"`
	MaxAccounts               int                   `json:"max_accounts"`
	MaxSavedRounds            int                   `json:"max_saved_rounds"`
	QuarantineEnabled         bool                  `json:"quarantine_enabled"`
	QuarantineCooldownMinutes int                   `json:"quarantine_cooldown_minutes"`
	QuarantineFailRounds      int                   `json:"quarantine_fail_rounds"`
	Revision                  int64                 `json:"revision"`
	Questions                 []qualityQuestionView `json:"questions"`
	EstimatedRequests         int                   `json:"estimated_requests"`
	UpdatedAt                 time.Time             `json:"updated_at"`
}

type qualityPlanRequest struct {
	GroupID                   int64                 `json:"group_id"`
	Name                      string                `json:"name"`
	Model                     string                `json:"model"`
	Endpoint                  string                `json:"endpoint"`
	Effort                    string                `json:"effort"`
	Enabled                   bool                  `json:"enabled"`
	ScheduleEnabled           bool                  `json:"schedule_enabled"`
	IntervalMinutes           int                   `json:"interval_minutes"`
	PauseStart                string                `json:"pause_start"`
	PauseEnd                  string                `json:"pause_end"`
	Timezone                  string                `json:"timezone"`
	MaxAccounts               int                   `json:"max_accounts"`
	MaxSavedRounds            int                   `json:"max_saved_rounds"`
	QuarantineEnabled         bool                  `json:"quarantine_enabled"`
	QuarantineCooldownMinutes int                   `json:"quarantine_cooldown_minutes"`
	QuarantineFailRounds      int                   `json:"quarantine_fail_rounds"`
	Revision                  int64                 `json:"revision"`
	Questions                 []qualityQuestionView `json:"questions"`
}

func (r qualityPlanRequest) params() service.QualityPlanParams {
	questions := make([]service.QualityQuestion, 0, len(r.Questions))
	for _, question := range r.Questions {
		questions = append(questions, service.QualityQuestion{
			ID: question.ID, Title: question.Title, Kind: question.Kind, Prompt: question.Prompt,
			Instructions: question.Instructions, Expected: question.Expected, Choices: question.Choices,
			Unit: question.Unit, Rounds: question.Rounds, Enabled: question.Enabled,
		})
	}
	return service.QualityPlanParams{
		GroupID: r.GroupID, Name: r.Name, Model: r.Model, Endpoint: r.Endpoint, Effort: r.Effort,
		Enabled: r.Enabled, ScheduleEnabled: r.ScheduleEnabled, IntervalMinutes: r.IntervalMinutes,
		PauseStart: r.PauseStart, PauseEnd: r.PauseEnd, Timezone: r.Timezone,
		MaxAccounts: r.MaxAccounts, MaxSavedRounds: r.MaxSavedRounds,
		QuarantineEnabled: r.QuarantineEnabled, QuarantineCooldownMinutes: r.QuarantineCooldownMinutes,
		QuarantineFailRounds: r.QuarantineFailRounds, Questions: questions,
	}
}

func qualityQuestionsToView(items []service.QualityQuestion) []qualityQuestionView {
	questions := make([]qualityQuestionView, 0, len(items))
	for _, question := range items {
		questions = append(questions, qualityQuestionView{
			ID: question.ID, Title: question.Title, Kind: question.Kind, Prompt: question.Prompt,
			Instructions: question.Instructions, Expected: question.Expected, Choices: question.Choices,
			Unit: question.Unit, Rounds: question.Rounds, Enabled: question.Enabled, Version: question.Version,
		})
	}
	return questions
}

func qualityPlanToView(plan *service.QualityPlan, estimated int) qualityPlanView {
	return qualityPlanView{
		ID: plan.ID, GroupID: plan.GroupID, GroupName: plan.GroupName, Name: plan.Name,
		Platform: plan.Platform, Model: plan.Model, Endpoint: plan.Endpoint, Effort: plan.Effort,
		Enabled: plan.Enabled, ScheduleEnabled: plan.ScheduleEnabled, IntervalMinutes: plan.IntervalMinutes,
		PauseStart: plan.PauseStart, PauseEnd: plan.PauseEnd, Timezone: plan.Timezone,
		MaxAccounts: plan.MaxAccounts, MaxSavedRounds: plan.MaxSavedRounds,
		QuarantineEnabled: plan.QuarantineEnabled, QuarantineCooldownMinutes: plan.QuarantineCooldownMinutes,
		QuarantineFailRounds: plan.QuarantineFailRounds, Revision: plan.Revision,
		Questions: qualityQuestionsToView(plan.Questions), EstimatedRequests: estimated, UpdatedAt: plan.UpdatedAt,
	}
}

type qualityRunView struct {
	ID                    int64          `json:"id"`
	PlanID                int64          `json:"plan_id"`
	Trigger               string         `json:"trigger"`
	Status                string         `json:"status"`
	Model                 string         `json:"model"`
	Effort                string         `json:"effort"`
	RequestCount          int            `json:"request_count"`
	Message               string         `json:"message"`
	Stats                 map[string]any `json:"stats"`
	ResolvedPlatform      string         `json:"resolved_platform"`
	ResolvedUpstreamModel string         `json:"resolved_upstream_model"`
	ResolvedEndpoint      string         `json:"resolved_endpoint"`
	ResolvedSource        string         `json:"resolved_source"`
	StartedAt             *time.Time     `json:"started_at"`
	FinishedAt            *time.Time     `json:"finished_at"`
}

type qualityResultView struct {
	ID                int64     `json:"id"`
	RunID             int64     `json:"run_id"`
	AccountID         int64     `json:"account_id"`
	QuestionID        string    `json:"question_id"`
	Kind              string    `json:"kind"`
	Status            string    `json:"status"`
	Answer            string    `json:"answer"`
	Expected          string    `json:"expected"`
	Response          string    `json:"response"`
	HasArtifact       bool      `json:"has_artifact"`
	ArtifactTruncated bool      `json:"artifact_truncated"`
	FirstTokenMs      *int      `json:"first_token_ms"`
	DurationMs        *int      `json:"duration_ms"`
	FailureClass      string    `json:"failure_class"`
	ErrorCode         string    `json:"error_code"`
	BankVersion       string    `json:"bank_version"`
	CreatedAt         time.Time `json:"created_at"`
}

func qualityResultsToView(results []*service.QualityResult) []qualityResultView {
	out := make([]qualityResultView, 0, len(results))
	for _, result := range results {
		out = append(out, qualityResultView{
			ID: result.ID, RunID: result.RunID, AccountID: result.AccountID,
			QuestionID: result.QuestionID, Kind: result.Kind, Status: result.Status,
			Answer: result.Answer, Expected: result.Expected, Response: result.Response,
			HasArtifact: result.ArtifactKey != "", ArtifactTruncated: result.ArtifactTruncated,
			FirstTokenMs: result.FirstTokenMs, DurationMs: result.DurationMs,
			FailureClass: result.FailureClass, ErrorCode: result.ErrorCode,
			BankVersion: result.BankVersion, CreatedAt: result.CreatedAt,
		})
	}
	return out
}

// ---------- 计划 ----------

func (h *QualityInspectionHandler) List(c *gin.Context) {
	plans, err := h.plans.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	views := make([]qualityPlanView, 0, len(plans))
	for _, plan := range plans {
		views = append(views, qualityPlanToView(plan, h.plans.Estimate(plan)))
	}
	settings, _ := h.settings.GetAllSettings(c.Request.Context())
	response.Success(c, gin.H{"plans": views, "settings": qualitySettingsView(settings), "questions": qualityQuestionsToView(service.BuiltinQualityQuestions())})
}

func (h *QualityInspectionHandler) Create(c *gin.Context) {
	var req qualityPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求体格式错误")
		return
	}
	plan, err := h.plans.Create(c.Request.Context(), req.params(), h.planSettings(c), currentAdminID(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, qualityPlanToView(plan, h.plans.Estimate(plan)))
}

func (h *QualityInspectionHandler) Update(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	var req qualityPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求体格式错误")
		return
	}
	plan, err := h.plans.Update(c.Request.Context(), id, req.params(), h.planSettings(c), req.Revision)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, qualityPlanToView(plan, h.plans.Estimate(plan)))
}

func (h *QualityInspectionHandler) Delete(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.plans.Delete(c.Request.Context(), id); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// Estimate 只做请求数预估，便于编辑页实时提示费用。
func (h *QualityInspectionHandler) Estimate(c *gin.Context) {
	var req qualityPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求体格式错误")
		return
	}
	params := req.params()
	accounts := params.MaxAccounts
	if accounts < 1 {
		accounts = 1
	}
	total := 0
	for _, question := range params.Questions {
		if question.Enabled {
			rounds := question.Rounds
			if rounds < 1 {
				rounds = 1
			}
			total += rounds
		}
	}
	response.Success(c, gin.H{"estimated_requests": total * accounts, "accounts": accounts})
}

// ---------- 轮次 ----------

func (h *QualityInspectionHandler) Run(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	var body struct {
		Confirmed bool `json:"confirmed"`
	}
	_ = c.ShouldBindJSON(&body)
	if !body.Confirmed {
		response.BadRequest(c, "请先确认本轮会产生的请求数")
		return
	}
	run, err := h.runner.RunManual(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, qualityRunToView(run))
}

func (h *QualityInspectionHandler) StopRun(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "runId")
	if !ok {
		return
	}
	run, err := h.runner.StopRun(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, qualityRunToView(run))
}

func (h *QualityInspectionHandler) ListRuns(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	runs, err := h.repo.ListRuns(c.Request.Context(), id, limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	views := make([]qualityRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, qualityRunToView(run))
	}
	response.Success(c, gin.H{"runs": views})
}

func (h *QualityInspectionHandler) RunDetail(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "runId")
	if !ok {
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	results, err := h.repo.ListResultsByRun(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"run": qualityRunToView(run), "results": qualityResultsToView(results)})
}

// ---------- 结果与作品 ----------

func (h *QualityInspectionHandler) Result(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.repo.GetResult(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, qualityResultsToView([]*service.QualityResult{result})[0])
}

// ArtifactURL 由管理员会话换取 10 分钟签名链接（iframe 无法携带 Authorization 头）。
func (h *QualityInspectionHandler) ArtifactURL(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.repo.GetResult(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	if result.ArtifactKey == "" {
		response.Error(c, http.StatusNotFound, "该结果没有作品")
		return
	}
	expires := time.Now().Add(10 * time.Minute).Unix()
	token := h.signArtifact(id, expires)
	response.Success(c, gin.H{
		"url": fmt.Sprintf("/api/v1/admin/quality-inspections/results/%d/artifact?exp=%d&token=%s", id, expires, token),
	})
}

func (h *QualityInspectionHandler) Artifact(c *gin.Context) {
	id, ok := parseQualityIDParam(c, "id")
	if !ok {
		return
	}
	expires, _ := strconv.ParseInt(c.Query("exp"), 10, 64)
	if expires <= 0 || time.Now().Unix() > expires {
		response.Error(c, http.StatusForbidden, "作品链接已过期，请刷新记录")
		return
	}
	if !hmac.Equal([]byte(c.Query("token")), []byte(h.signArtifact(id, expires))) {
		response.Error(c, http.StatusForbidden, "作品链接无效")
		return
	}
	result, err := h.repo.GetResult(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	if result.Kind != service.QualityKindDrawing || result.ArtifactKey == "" {
		response.Error(c, http.StatusNotFound, "作品不存在")
		return
	}
	data, err := h.artifacts.Open(c.Request.Context(), result.ArtifactKey)
	if err != nil {
		response.Error(c, http.StatusNotFound, "作品不存在或已清理")
		return
	}
	c.Header("Content-Security-Policy",
		"sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; "+
			"style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; "+
			"form-action 'none'; base-uri 'none'; frame-ancestors 'self'")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

// Compare 返回 2–4 条记录的元数据，作品由前端逐个换取签名链接。
func (h *QualityInspectionHandler) Compare(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("ids"))
	if raw == "" {
		response.BadRequest(c, "请选择要对比的记录")
		return
	}
	parts := strings.Split(raw, ",")
	if len(parts) < 2 || len(parts) > 4 {
		response.BadRequest(c, "对比记录需为 2–4 条")
		return
	}
	views := make([]qualityResultView, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			response.BadRequest(c, "记录 ID 无效")
			return
		}
		result, err := h.repo.GetResult(c.Request.Context(), id)
		if err != nil {
			h.fail(c, err)
			return
		}
		views = append(views, qualityResultsToView([]*service.QualityResult{result})[0])
	}
	response.Success(c, gin.H{"results": views})
}

func (h *QualityInspectionHandler) Questions(c *gin.Context) {
	response.Success(c, gin.H{"questions": service.BuiltinQualityQuestions()})
}

// ---------- 剔除 ----------

func (h *QualityInspectionHandler) Quarantines(c *gin.Context) {
	items, err := h.repo.ListQuarantines(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"quarantines": items})
}

func (h *QualityInspectionHandler) Handover(c *gin.Context) {
	accountID, ok := parseQualityIDParam(c, "accountId")
	if !ok {
		return
	}
	if err := h.repo.SetQuarantineState(c.Request.Context(), accountID, "manual", currentAdminIDString(c)); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": accountID, "state": "manual", "scheduling_changed": false})
}

// ---------- helpers ----------

func (h *QualityInspectionHandler) planSettings(c *gin.Context) service.QualityPlanSettings {
	settings, _ := h.settings.GetAllSettings(c.Request.Context())
	if settings == nil {
		return service.QualityPlanSettings{MaxRequestsPerRound: 30, DefaultStabilityRounds: 5}
	}
	return service.QualityPlanSettings{
		QuarantineEnabled:      settings.QualityInspectionQuarantineEnabled,
		MaxRequestsPerRound:    settings.QualityInspectionMaxRequestsPerRound,
		DefaultStabilityRounds: settings.QualityInspectionStabilityRounds,
	}
}

func qualitySettingsView(settings *service.SystemSettings) gin.H {
	if settings == nil {
		return gin.H{"enabled": false, "quarantine_enabled": false,
			"quarantine_cooldown_minutes": 60, "max_requests_per_round": 30, "stability_rounds": 5}
	}
	return gin.H{
		"enabled":                     settings.ModelQualityInspectionEnabled,
		"quarantine_enabled":          settings.QualityInspectionQuarantineEnabled,
		"quarantine_cooldown_minutes": settings.QualityInspectionQuarantineCooldownMinutes,
		"max_requests_per_round":      settings.QualityInspectionMaxRequestsPerRound,
		"stability_rounds":            settings.QualityInspectionStabilityRounds,
	}
}

func qualityRunToView(run *service.QualityRun) qualityRunView {
	return qualityRunView{
		ID: run.ID, PlanID: run.PlanID, Trigger: run.Trigger, Status: run.Status,
		Model: run.Model, Effort: run.Effort, RequestCount: run.RequestCount, Message: run.Message,
		Stats: run.Stats, ResolvedPlatform: run.ResolvedPlatform,
		ResolvedUpstreamModel: run.ResolvedUpstreamModel, ResolvedEndpoint: run.ResolvedEndpoint,
		ResolvedSource: run.ResolvedSource, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func (h *QualityInspectionHandler) signArtifact(resultID, expires int64) string {
	mac := hmac.New(sha256.New, h.signKey)
	fmt.Fprintf(mac, "%d:%d", resultID, expires)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *QualityInspectionHandler) fail(c *gin.Context, err error) {
	var validation *service.QualityValidationError
	switch {
	case errors.As(err, &validation):
		response.BadRequest(c, validation.Message)
	case errors.Is(err, service.ErrQualityPlanNotFound), errors.Is(err, service.ErrQualityRunNotFound),
		errors.Is(err, service.ErrQualityResultNotFound):
		response.Error(c, http.StatusNotFound, "记录不存在")
	case errors.Is(err, service.ErrQualityPlanConflict):
		response.Error(c, http.StatusConflict, "计划冲突：该分组已有相同模型的计划，或配置已被他人修改")
	case errors.Is(err, service.ErrQualityStorageUnavailable):
		response.Error(c, http.StatusBadRequest, "对象存储不可用：请先在系统设置中配置异步生图对象存储")
	case errors.Is(err, service.ErrQualityCompositeUnresolved):
		response.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrQualityInspectionDisabled):
		response.Error(c, http.StatusForbidden, "模型质量巡检未开启")
	default:
		response.Error(c, http.StatusInternalServerError, "操作失败，请查看服务日志")
	}
}

func parseQualityIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "ID 无效")
		return 0, false
	}
	return id, true
}

func currentAdminID(c *gin.Context) int64 {
	value, ok := c.Get("user_id")
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func currentAdminIDString(c *gin.Context) string {
	if id := currentAdminID(c); id > 0 {
		return strconv.FormatInt(id, 10)
	}
	return "admin"
}
