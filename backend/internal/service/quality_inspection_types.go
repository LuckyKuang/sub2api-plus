package service

import (
	"context"
	"errors"
	"time"
)

// 模型质量巡检的状态常量。
const (
	QualityStatusPass      = "pass"
	QualityStatusFail      = "fail"
	QualityStatusUnknown   = "unknown"
	QualityStatusError     = "error"
	QualityStatusQuota     = "quota"
	QualityStatusReference = "reference"

	QualityRunQueued    = "queued"
	QualityRunRunning   = "running"
	QualityRunCompleted = "completed"
	QualityRunWithError = "completed_with_errors"
	QualityRunFailed    = "failed"
	QualityRunCancelled = "cancelled"

	QualityTriggerManual  = "manual"
	QualityTriggerTimer   = "timer"
	QualityTriggerRecheck = "recheck"

	QualityKindNumber    = "number"
	QualityKindChoice    = "choice"
	QualityKindJSON      = "json"
	QualityKindDrawing   = "drawing"
	QualityKindReference = "reference"

	QualityFailureUpstream = "upstream"
	QualityFailureBudget   = "budget"
	QualityFailureLocal    = "local"
)

var (
	// ErrQualityInspectionDisabled 由路由层在总开关关闭时返回。
	ErrQualityInspectionDisabled = errors.New("model quality inspection is disabled")
	// ErrQualityPlanNotFound 计划不存在。
	ErrQualityPlanNotFound = errors.New("quality inspection plan not found")
	// ErrQualityRunNotFound 轮次不存在。
	ErrQualityRunNotFound = errors.New("quality inspection run not found")
	// ErrQualityResultNotFound 结果不存在。
	ErrQualityResultNotFound = errors.New("quality inspection result not found")
	// ErrQualityStorageUnavailable 对象存储不可用（含 drawing 题的计划禁止启用）。
	ErrQualityStorageUnavailable = errors.New("object storage is unavailable; configure async image storage first")
	// ErrQualityCompositeUnresolved 复合分组无法把模型解析到具体平台。
	ErrQualityCompositeUnresolved = errors.New("composite group cannot resolve this model to a concrete platform")
)

// QualityQuestion 计划内题目快照。运行期只读快照，不回查题库。
type QualityQuestion struct {
	ID           string
	Title        string
	Kind         string
	Prompt       string
	Instructions string
	Expected     string
	Choices      []string
	Unit         string
	Rounds       int
	Enabled      bool
	Version      string
	Position     int
}

// QualityPlan 巡检计划。单分组 + 单模型。
type QualityPlan struct {
	ID                        int64
	GroupID                   int64
	GroupName                 string
	Name                      string
	Platform                  string
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
	Revision                  int64
	CreatedBy                 int64
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	Questions                 []QualityQuestion
}

// QualityRun 一次巡检轮次。
type QualityRun struct {
	ID                    int64
	PlanID                int64
	PlanRevision          int64
	Trigger               string
	Slot                  int64
	Status                string
	AccountID             int64
	Model                 string
	Effort                string
	RequestCount          int
	Message               string
	Stats                 map[string]any
	ResolvedPlatform      string
	ResolvedUpstreamModel string
	ResolvedEndpoint      string
	ResolvedSource        string
	StartedAt             *time.Time
	FinishedAt            *time.Time
}

// QualityResult 单题结果。作品正文在对象存储，只存键。
type QualityResult struct {
	ID                int64
	RunID             int64
	PlanID            int64
	AccountID         int64
	QuestionID        string
	Kind              string
	Status            string
	Answer            string
	Expected          string
	Response          string
	ArtifactKey       string
	ArtifactBytes     int64
	ArtifactTruncated bool
	FirstTokenMs      *int
	DurationMs        *int
	FailureClass      string
	ErrorCode         string
	BankVersion       string
	CreatedAt         time.Time
}

// QualityQuarantine 账号级剔除记录（P2 执行；P0 仅读取与人工接管）。
type QualityQuarantine struct {
	ID            int64
	PlanID        int64
	AccountID     int64
	State         string
	Generation    int
	EvidenceRunID int64
	RecheckRunID  int64
	FailRounds    int
	DueAt         *time.Time
	Attempts      int
	ErrorCode     string
	Actor         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// 巡检依赖的窄接口：便于单测注入替身，也避免服务层直接耦合具体实现。
type (
	// QualityGroupLookup 只需要按 ID 取分组。
	QualityGroupLookup interface {
		GetByID(ctx context.Context, id int64) (*Group, error)
	}
	// QualityAccountLister 只需要按分组列账号。
	QualityAccountLister interface {
		ListByGroup(ctx context.Context, groupID int64) ([]Account, error)
	}
	// QualitySettingsSource 读取功能开关与每轮运行约束。
	QualitySettingsSource interface {
		GetAllSettings(ctx context.Context) (*SystemSettings, error)
		ModelQualityInspectionEnabled(ctx context.Context) bool
	}
	// QualityProber 一次账号探测。
	QualityProber interface {
		Run(ctx context.Context, accountID int64, model, prompt string) (QualityProbeOutcome, error)
	}
	// QualityArtifactWriter 作品对象读写。
	QualityArtifactWriter interface {
		Available() bool
		Save(ctx context.Context, planID, runID, resultID int64, html string) (string, int64, bool, error)
		Open(ctx context.Context, key string) ([]byte, error)
		Delete(ctx context.Context, key string) error
	}
)

// QualityInspectionBundle 把巡检的构造结果聚合一次，减少 wire 的暴露面。
type QualityInspectionBundle struct {
	Repo      QualityInspectionRepository
	Plans     *QualityInspectionPlanService
	Runner    *QualityInspectionRunner
	Artifacts *QualityArtifactStore
	Composite *QualityCompositeResolver
}

// QualityInspectionRepository 巡检持久化端口。
type QualityInspectionRepository interface {
	CreatePlan(ctx context.Context, plan *QualityPlan) (*QualityPlan, error)
	UpdatePlan(ctx context.Context, plan *QualityPlan, expectedRevision int64) (*QualityPlan, error)
	DeletePlan(ctx context.Context, id int64) error
	GetPlan(ctx context.Context, id int64) (*QualityPlan, error)
	ListPlans(ctx context.Context) ([]*QualityPlan, error)
	ListSchedulablePlans(ctx context.Context) ([]*QualityPlan, error)
	ReplaceQuestions(ctx context.Context, planID int64, questions []QualityQuestion) error

	CreateRun(ctx context.Context, run *QualityRun) (*QualityRun, error)
	UpdateRun(ctx context.Context, run *QualityRun) error
	GetRun(ctx context.Context, id int64) (*QualityRun, error)
	ListRuns(ctx context.Context, planID int64, limit int) ([]*QualityRun, error)

	InsertResult(ctx context.Context, result *QualityResult) (*QualityResult, error)
	UpdateResultArtifact(ctx context.Context, id int64, key string, size int64, truncated bool) error
	ListResultsByRun(ctx context.Context, runID int64) ([]*QualityResult, error)
	GetResult(ctx context.Context, id int64) (*QualityResult, error)

	// PruneRuns 保留最近 keep 个轮次，返回被删除轮次的作品对象键（供调用方清理存储）。
	PruneRuns(ctx context.Context, planID int64, keep int) ([]string, error)

	ListQuarantines(ctx context.Context) ([]*QualityQuarantine, error)
	SetQuarantineState(ctx context.Context, accountID int64, state, actor string) error
}
