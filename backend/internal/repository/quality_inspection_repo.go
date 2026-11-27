package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/qualityinspectionplan"
	"github.com/LuckyKuang/sub2api-plus/ent/qualityinspectionquarantine"
	"github.com/LuckyKuang/sub2api-plus/ent/qualityinspectionquestion"
	"github.com/LuckyKuang/sub2api-plus/ent/qualityinspectionresult"
	"github.com/LuckyKuang/sub2api-plus/ent/qualityinspectionrun"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
)

// qualityInspectionRepository 实现 service.QualityInspectionRepository。
// 计划/题目/轮次/结果/剔除均在 ent 内完成；PruneRuns 需要成批删除并返回作品键，
// 用原生 SQL 一次性完成，避免逐行查询。
type qualityInspectionRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewQualityInspectionRepository(client *dbent.Client, db *sql.DB) service.QualityInspectionRepository {
	return &qualityInspectionRepository{client: client, db: db}
}

func (r *qualityInspectionRepository) CreatePlan(ctx context.Context, plan *service.QualityPlan) (*service.QualityPlan, error) {
	client := clientFromContext(ctx, r.client)
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	builder := tx.QualityInspectionPlan.Create().
		SetGroupID(plan.GroupID).
		SetName(plan.Name).
		SetPlatform(plan.Platform).
		SetModel(plan.Model).
		SetEndpoint(defaultQualityString(plan.Endpoint, "any")).
		SetEffort(defaultQualityString(plan.Effort, "medium")).
		SetEnabled(plan.Enabled).
		SetScheduleEnabled(plan.ScheduleEnabled).
		SetIntervalMinutes(plan.IntervalMinutes).
		SetPauseStart(defaultQualityString(plan.PauseStart, "22:00")).
		SetPauseEnd(defaultQualityString(plan.PauseEnd, "09:00")).
		SetTimezone(defaultQualityString(plan.Timezone, "Asia/Shanghai")).
		SetMaxAccounts(plan.MaxAccounts).
		SetMaxSavedRounds(plan.MaxSavedRounds).
		SetQuarantineEnabled(plan.QuarantineEnabled).
		SetQuarantineCooldownMinutes(plan.QuarantineCooldownMinutes).
		SetQuarantineFailRounds(plan.QuarantineFailRounds).
		SetRevision(1)
	if plan.CreatedBy > 0 {
		builder.SetCreatedBy(plan.CreatedBy)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, mapQualityConstraint(err)
	}
	if err := insertQualityQuestions(ctx, tx, created.ID, plan.Questions); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPlan(ctx, created.ID)
}

func (r *qualityInspectionRepository) UpdatePlan(ctx context.Context, plan *service.QualityPlan, expectedRevision int64) (*service.QualityPlan, error) {
	client := clientFromContext(ctx, r.client)
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	update := tx.QualityInspectionPlan.Update().
		Where(qualityinspectionplan.IDEQ(plan.ID), qualityinspectionplan.RevisionEQ(expectedRevision)).
		SetGroupID(plan.GroupID).
		SetName(plan.Name).
		SetPlatform(plan.Platform).
		SetModel(plan.Model).
		SetEndpoint(defaultQualityString(plan.Endpoint, "any")).
		SetEffort(defaultQualityString(plan.Effort, "medium")).
		SetEnabled(plan.Enabled).
		SetScheduleEnabled(plan.ScheduleEnabled).
		SetIntervalMinutes(plan.IntervalMinutes).
		SetPauseStart(defaultQualityString(plan.PauseStart, "22:00")).
		SetPauseEnd(defaultQualityString(plan.PauseEnd, "09:00")).
		SetTimezone(defaultQualityString(plan.Timezone, "Asia/Shanghai")).
		SetMaxAccounts(plan.MaxAccounts).
		SetMaxSavedRounds(plan.MaxSavedRounds).
		SetQuarantineEnabled(plan.QuarantineEnabled).
		SetQuarantineCooldownMinutes(plan.QuarantineCooldownMinutes).
		SetQuarantineFailRounds(plan.QuarantineFailRounds).
		SetRevision(expectedRevision + 1)
	affected, err := update.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, mapQualityConstraint(err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return nil, service.ErrQualityPlanConflict
	}
	if _, err := tx.QualityInspectionQuestion.Delete().
		Where(qualityinspectionquestion.PlanIDEQ(plan.ID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := insertQualityQuestions(ctx, tx, plan.ID, plan.Questions); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPlan(ctx, plan.ID)
}

func (r *qualityInspectionRepository) DeletePlan(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	return client.QualityInspectionPlan.DeleteOneID(id).Exec(ctx)
}

func (r *qualityInspectionRepository) GetPlan(ctx context.Context, id int64) (*service.QualityPlan, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.QualityInspectionPlan.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrQualityPlanNotFound
		}
		return nil, err
	}
	questions, err := r.questionsFor(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	plan := mapQualityPlan(row)
	plan.Questions = questions[id]
	return plan, nil
}

func (r *qualityInspectionRepository) ListPlans(ctx context.Context) ([]*service.QualityPlan, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QualityInspectionPlan.Query().
		Order(dbent.Desc(qualityinspectionplan.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	questions, err := r.questionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*service.QualityPlan, 0, len(rows))
	for _, row := range rows {
		plan := mapQualityPlan(row)
		plan.Questions = questions[row.ID]
		out = append(out, plan)
	}
	return out, nil
}

func (r *qualityInspectionRepository) ListSchedulablePlans(ctx context.Context) ([]*service.QualityPlan, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QualityInspectionPlan.Query().
		Where(qualityinspectionplan.EnabledEQ(true), qualityinspectionplan.ScheduleEnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	questions, err := r.questionsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*service.QualityPlan, 0, len(rows))
	for _, row := range rows {
		plan := mapQualityPlan(row)
		plan.Questions = questions[row.ID]
		out = append(out, plan)
	}
	return out, nil
}

func (r *qualityInspectionRepository) ReplaceQuestions(ctx context.Context, planID int64, questions []service.QualityQuestion) error {
	client := clientFromContext(ctx, r.client)
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.QualityInspectionQuestion.Delete().
		Where(qualityinspectionquestion.PlanIDEQ(planID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertQualityQuestions(ctx, tx, planID, questions); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *qualityInspectionRepository) CreateRun(ctx context.Context, run *service.QualityRun) (*service.QualityRun, error) {
	client := clientFromContext(ctx, r.client)
	builder := client.QualityInspectionRun.Create().
		SetPlanID(run.PlanID).
		SetPlanRevision(run.PlanRevision).
		SetTrigger(defaultQualityString(run.Trigger, service.QualityTriggerManual)).
		SetSlot(run.Slot).
		SetStatus(defaultQualityString(run.Status, service.QualityRunQueued)).
		SetModel(run.Model).
		SetEffort(defaultQualityString(run.Effort, "medium"))
	if run.AccountID > 0 {
		builder.SetAccountID(run.AccountID)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return nil, mapQualityConstraint(err)
	}
	return mapQualityRun(created), nil
}

func (r *qualityInspectionRepository) UpdateRun(ctx context.Context, run *service.QualityRun) error {
	client := clientFromContext(ctx, r.client)
	update := client.QualityInspectionRun.UpdateOneID(run.ID).
		SetStatus(run.Status).
		SetModel(run.Model).
		SetEffort(run.Effort).
		SetRequestCount(run.RequestCount).
		SetMessage(run.Message).
		SetResolvedPlatform(run.ResolvedPlatform).
		SetResolvedUpstreamModel(run.ResolvedUpstreamModel).
		SetResolvedEndpoint(run.ResolvedEndpoint).
		SetResolvedSource(run.ResolvedSource)
	if run.Stats != nil {
		update.SetStats(run.Stats)
	}
	if run.AccountID > 0 {
		update.SetAccountID(run.AccountID)
	}
	if run.StartedAt != nil {
		update.SetStartedAt(*run.StartedAt)
	}
	if run.FinishedAt != nil {
		update.SetFinishedAt(*run.FinishedAt)
	}
	return update.Exec(ctx)
}

func (r *qualityInspectionRepository) GetRun(ctx context.Context, id int64) (*service.QualityRun, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.QualityInspectionRun.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrQualityRunNotFound
		}
		return nil, err
	}
	return mapQualityRun(row), nil
}

func (r *qualityInspectionRepository) ListRuns(ctx context.Context, planID int64, limit int) ([]*service.QualityRun, error) {
	client := clientFromContext(ctx, r.client)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := client.QualityInspectionRun.Query().
		Where(qualityinspectionrun.PlanIDEQ(planID)).
		Order(dbent.Desc(qualityinspectionrun.FieldID)).
		Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*service.QualityRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapQualityRun(row))
	}
	return out, nil
}

func (r *qualityInspectionRepository) InsertResult(ctx context.Context, result *service.QualityResult) (*service.QualityResult, error) {
	client := clientFromContext(ctx, r.client)
	builder := client.QualityInspectionResult.Create().
		SetRunID(result.RunID).
		SetPlanID(result.PlanID).
		SetAccountID(result.AccountID).
		SetQuestionID(result.QuestionID).
		SetKind(result.Kind).
		SetStatus(result.Status).
		SetAnswer(result.Answer).
		SetExpected(result.Expected).
		SetResponse(result.Response).
		SetArtifactKey(result.ArtifactKey).
		SetArtifactBytes(result.ArtifactBytes).
		SetArtifactTruncated(result.ArtifactTruncated).
		SetFailureClass(result.FailureClass).
		SetErrorCode(result.ErrorCode).
		SetBankVersion(result.BankVersion)
	if result.FirstTokenMs != nil {
		builder.SetFirstTokenMs(*result.FirstTokenMs)
	}
	if result.DurationMs != nil {
		builder.SetDurationMs(*result.DurationMs)
	}
	if !result.CreatedAt.IsZero() {
		builder.SetCreatedAt(result.CreatedAt)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	return mapQualityResult(created), nil
}

func (r *qualityInspectionRepository) UpdateResultArtifact(ctx context.Context, id int64, key string, size int64, truncated bool) error {
	client := clientFromContext(ctx, r.client)
	return client.QualityInspectionResult.UpdateOneID(id).
		SetArtifactKey(key).
		SetArtifactBytes(size).
		SetArtifactTruncated(truncated).
		Exec(ctx)
}

func (r *qualityInspectionRepository) ListResultsByRun(ctx context.Context, runID int64) ([]*service.QualityResult, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QualityInspectionResult.Query().
		Where(qualityinspectionresult.RunIDEQ(runID)).
		Order(dbent.Asc(qualityinspectionresult.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*service.QualityResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapQualityResult(row))
	}
	return out, nil
}

func (r *qualityInspectionRepository) GetResult(ctx context.Context, id int64) (*service.QualityResult, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.QualityInspectionResult.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrQualityResultNotFound
		}
		return nil, err
	}
	return mapQualityResult(row), nil
}

// PruneRuns 保留最近 keep 个轮次，删除更早的轮次并返回其作品对象键。
func (r *qualityInspectionRepository) PruneRuns(ctx context.Context, planID int64, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	if r.db == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM quality_inspection_runs
		WHERE plan_id = $1 AND status NOT IN ('queued','running')
		ORDER BY id DESC OFFSET $2`, planID, keep)
	if err != nil {
		return nil, err
	}
	var runIDs []int64
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		runIDs = append(runIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(runIDs) == 0 {
		return nil, nil
	}

	keyRows, err := r.db.QueryContext(ctx,
		`SELECT artifact_key FROM quality_inspection_results WHERE run_id = ANY($1) AND artifact_key <> ''`, pq.Array(runIDs))
	if err != nil {
		return nil, err
	}
	var keys []string
	for keyRows.Next() {
		var key string
		if scanErr := keyRows.Scan(&key); scanErr != nil {
			_ = keyRows.Close()
			return nil, scanErr
		}
		keys = append(keys, key)
	}
	_ = keyRows.Close()

	client := clientFromContext(ctx, r.client)
	if _, err := client.QualityInspectionRun.Delete().
		Where(qualityinspectionrun.IDIn(runIDs...)).Exec(ctx); err != nil {
		return nil, err
	}
	return keys, nil
}

func (r *qualityInspectionRepository) ListQuarantines(ctx context.Context) ([]*service.QualityQuarantine, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QualityInspectionQuarantine.Query().
		Order(dbent.Desc(qualityinspectionquarantine.FieldUpdatedAt)).Limit(200).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*service.QualityQuarantine, 0, len(rows))
	for _, row := range rows {
		item := &service.QualityQuarantine{
			ID: row.ID, PlanID: row.PlanID, AccountID: row.AccountID, State: row.State,
			Generation: row.Generation, EvidenceRunID: row.EvidenceRunID, RecheckRunID: row.RecheckRunID,
			FailRounds: row.FailRounds, Attempts: row.Attempts, ErrorCode: row.ErrorCode,
			Actor: row.Actor, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.DueAt != nil {
			item.DueAt = row.DueAt
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *qualityInspectionRepository) SetQuarantineState(ctx context.Context, accountID int64, state, actor string) error {
	client := clientFromContext(ctx, r.client)
	return client.QualityInspectionQuarantine.Update().
		Where(qualityinspectionquarantine.AccountIDEQ(accountID)).
		SetState(state).SetActor(actor).Exec(ctx)
}

// ---------- helpers ----------

func insertQualityQuestions(ctx context.Context, tx *dbent.Tx, planID int64, questions []service.QualityQuestion) error {
	for index, question := range questions {
		builder := tx.QualityInspectionQuestion.Create().
			SetPlanID(planID).
			SetQuestionID(question.ID).
			SetTitle(question.Title).
			SetKind(question.Kind).
			SetPrompt(question.Prompt).
			SetInstructions(question.Instructions).
			SetExpected(question.Expected).
			SetUnit(question.Unit).
			SetRounds(question.Rounds).
			SetEnabled(question.Enabled).
			SetVersion(question.Version).
			SetPosition(index)
		if len(question.Choices) > 0 {
			builder.SetChoices(question.Choices)
		}
		if _, err := builder.Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *qualityInspectionRepository) questionsFor(ctx context.Context, planIDs []int64) (map[int64][]service.QualityQuestion, error) {
	out := make(map[int64][]service.QualityQuestion, len(planIDs))
	if len(planIDs) == 0 {
		return out, nil
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QualityInspectionQuestion.Query().
		Where(qualityinspectionquestion.PlanIDIn(planIDs...)).
		Order(dbent.Asc(qualityinspectionquestion.FieldPosition)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PlanID] = append(out[row.PlanID], service.QualityQuestion{
			ID: row.QuestionID, Title: row.Title, Kind: row.Kind, Prompt: row.Prompt,
			Instructions: row.Instructions, Expected: row.Expected, Choices: row.Choices,
			Unit: row.Unit, Rounds: row.Rounds, Enabled: row.Enabled, Version: row.Version,
			Position: row.Position,
		})
	}
	return out, nil
}

func mapQualityPlan(row *dbent.QualityInspectionPlan) *service.QualityPlan {
	plan := &service.QualityPlan{
		ID: row.ID, GroupID: row.GroupID, Name: row.Name, Platform: row.Platform,
		Model: row.Model, Endpoint: row.Endpoint, Effort: row.Effort,
		Enabled: row.Enabled, ScheduleEnabled: row.ScheduleEnabled,
		IntervalMinutes: row.IntervalMinutes, PauseStart: row.PauseStart, PauseEnd: row.PauseEnd,
		Timezone: row.Timezone, MaxAccounts: row.MaxAccounts, MaxSavedRounds: row.MaxSavedRounds,
		QuarantineEnabled: row.QuarantineEnabled, QuarantineCooldownMinutes: row.QuarantineCooldownMinutes,
		QuarantineFailRounds: row.QuarantineFailRounds, Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.CreatedBy != nil {
		plan.CreatedBy = *row.CreatedBy
	}
	return plan
}

func mapQualityRun(row *dbent.QualityInspectionRun) *service.QualityRun {
	run := &service.QualityRun{
		ID: row.ID, PlanID: row.PlanID, PlanRevision: row.PlanRevision, Trigger: row.Trigger,
		Slot: row.Slot, Status: row.Status, Model: row.Model, Effort: row.Effort,
		RequestCount: row.RequestCount, Message: row.Message, Stats: row.Stats,
		ResolvedPlatform: row.ResolvedPlatform, ResolvedUpstreamModel: row.ResolvedUpstreamModel,
		ResolvedEndpoint: row.ResolvedEndpoint, ResolvedSource: row.ResolvedSource,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
	}
	if row.AccountID != nil {
		run.AccountID = *row.AccountID
	}
	return run
}

func mapQualityResult(row *dbent.QualityInspectionResult) *service.QualityResult {
	result := &service.QualityResult{
		ID: row.ID, RunID: row.RunID, PlanID: row.PlanID, AccountID: row.AccountID,
		QuestionID: row.QuestionID, Kind: row.Kind, Status: row.Status, Answer: row.Answer,
		Expected: row.Expected, Response: row.Response, ArtifactKey: row.ArtifactKey,
		ArtifactBytes: row.ArtifactBytes, ArtifactTruncated: row.ArtifactTruncated,
		FailureClass: row.FailureClass, ErrorCode: row.ErrorCode, BankVersion: row.BankVersion,
		CreatedAt: row.CreatedAt,
	}
	if row.FirstTokenMs != nil {
		result.FirstTokenMs = row.FirstTokenMs
	}
	if row.DurationMs != nil {
		result.DurationMs = row.DurationMs
	}
	return result
}

func mapQualityConstraint(err error) error {
	var constraint *dbent.ConstraintError
	if errors.As(err, &constraint) {
		return service.ErrQualityPlanConflict
	}
	return err
}

func defaultQualityString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
