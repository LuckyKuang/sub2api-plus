-- 模型质量巡检：计划、题目快照、轮次、结果、剔除记录。
-- 只新增表与索引，不改动既有表；启动时由迁移执行器自动应用。
-- group_id 不建 FK：分组表由业务侧维护，删除分组时由服务层清理计划，避免级联误删历史证据。

SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS quality_inspection_plans (
    id                          BIGSERIAL PRIMARY KEY,
    group_id                    BIGINT       NOT NULL,
    name                        VARCHAR(120) NOT NULL,
    platform                    VARCHAR(32)  NOT NULL DEFAULT '',
    model                       VARCHAR(160) NOT NULL,
    endpoint                    VARCHAR(32)  NOT NULL DEFAULT 'any',
    effort                      VARCHAR(16)  NOT NULL DEFAULT 'medium',
    enabled                     BOOLEAN      NOT NULL DEFAULT FALSE,
    schedule_enabled            BOOLEAN      NOT NULL DEFAULT FALSE,
    interval_minutes            INT          NOT NULL DEFAULT 60,
    pause_start                 VARCHAR(5)   NOT NULL DEFAULT '22:00',
    pause_end                   VARCHAR(5)   NOT NULL DEFAULT '09:00',
    timezone                    VARCHAR(64)  NOT NULL DEFAULT 'Asia/Shanghai',
    max_accounts                INT          NOT NULL DEFAULT 1,
    max_saved_rounds            INT          NOT NULL DEFAULT 20,
    quarantine_enabled          BOOLEAN      NOT NULL DEFAULT FALSE,
    quarantine_cooldown_minutes INT          NOT NULL DEFAULT 60,
    quarantine_fail_rounds      INT          NOT NULL DEFAULT 2,
    revision                    BIGINT       NOT NULL DEFAULT 1,
    created_by                  BIGINT,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT quality_inspection_plans_group_model_key UNIQUE (group_id, model),
    CONSTRAINT quality_inspection_plans_interval_ck CHECK (interval_minutes BETWEEN 30 AND 1440),
    CONSTRAINT quality_inspection_plans_accounts_ck CHECK (max_accounts BETWEEN 1 AND 5),
    CONSTRAINT quality_inspection_plans_rounds_ck CHECK (max_saved_rounds BETWEEN 1 AND 200),
    CONSTRAINT quality_inspection_plans_cooldown_ck CHECK (quarantine_cooldown_minutes BETWEEN 5 AND 10080),
    CONSTRAINT quality_inspection_plans_failr_ck CHECK (quarantine_fail_rounds BETWEEN 1 AND 5),
    CONSTRAINT quality_inspection_plans_effort_ck CHECK (effort IN ('none','minimal','low','medium','high','xhigh','max'))
);

CREATE INDEX IF NOT EXISTS quality_inspection_plans_enabled_idx
    ON quality_inspection_plans (enabled, schedule_enabled);

CREATE TABLE IF NOT EXISTS quality_inspection_questions (
    id           BIGSERIAL PRIMARY KEY,
    plan_id      BIGINT      NOT NULL REFERENCES quality_inspection_plans(id) ON DELETE CASCADE,
    question_id  VARCHAR(40) NOT NULL,
    title        VARCHAR(80) NOT NULL DEFAULT '',
    kind         VARCHAR(16) NOT NULL DEFAULT 'number',
    prompt       TEXT        NOT NULL DEFAULT '',
    instructions TEXT        NOT NULL DEFAULT '',
    expected     TEXT        NOT NULL DEFAULT '',
    choices      JSONB,
    unit         VARCHAR(16) NOT NULL DEFAULT '',
    rounds       INT         NOT NULL DEFAULT 1,
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    version      VARCHAR(32) NOT NULL DEFAULT '',
    position     INT         NOT NULL DEFAULT 0,
    CONSTRAINT quality_inspection_questions_plan_question_key UNIQUE (plan_id, question_id),
    CONSTRAINT quality_inspection_questions_kind_ck CHECK (kind IN ('number','choice','json','drawing','reference')),
    CONSTRAINT quality_inspection_questions_rounds_ck CHECK (rounds BETWEEN 1 AND 3)
);

CREATE INDEX IF NOT EXISTS quality_inspection_questions_plan_idx
    ON quality_inspection_questions (plan_id, position);

CREATE TABLE IF NOT EXISTS quality_inspection_runs (
    id                      BIGSERIAL PRIMARY KEY,
    plan_id                 BIGINT       NOT NULL REFERENCES quality_inspection_plans(id) ON DELETE CASCADE,
    plan_revision           BIGINT       NOT NULL DEFAULT 0,
    trigger                 VARCHAR(16)  NOT NULL DEFAULT 'manual',
    slot                    BIGINT       NOT NULL DEFAULT 0,
    status                  VARCHAR(32)  NOT NULL DEFAULT 'queued',
    account_id              BIGINT,
    model                   VARCHAR(160) NOT NULL DEFAULT '',
    effort                  VARCHAR(16)  NOT NULL DEFAULT 'medium',
    request_count           INT          NOT NULL DEFAULT 0,
    stats                   JSONB,
    message                 TEXT         NOT NULL DEFAULT '',
    resolved_platform       VARCHAR(32)  NOT NULL DEFAULT '',
    resolved_upstream_model VARCHAR(160) NOT NULL DEFAULT '',
    resolved_endpoint       VARCHAR(32)  NOT NULL DEFAULT '',
    resolved_source         VARCHAR(32)  NOT NULL DEFAULT '',
    started_at              TIMESTAMPTZ,
    finished_at             TIMESTAMPTZ,
    CONSTRAINT quality_inspection_runs_slot_key UNIQUE (plan_id, trigger, slot),
    CONSTRAINT quality_inspection_runs_trigger_ck CHECK (trigger IN ('manual','timer','recheck')),
    CONSTRAINT quality_inspection_runs_status_ck CHECK (status IN
        ('queued','running','completed','completed_with_errors','failed','cancelled'))
);

CREATE INDEX IF NOT EXISTS quality_inspection_runs_plan_time_idx
    ON quality_inspection_runs (plan_id, started_at DESC);

CREATE TABLE IF NOT EXISTS quality_inspection_results (
    id                 BIGSERIAL PRIMARY KEY,
    run_id             BIGINT      NOT NULL REFERENCES quality_inspection_runs(id) ON DELETE CASCADE,
    plan_id            BIGINT      NOT NULL,
    account_id         BIGINT      NOT NULL,
    question_id        VARCHAR(40) NOT NULL,
    kind               VARCHAR(16) NOT NULL DEFAULT '',
    status             VARCHAR(16) NOT NULL DEFAULT 'unknown',
    answer             TEXT        NOT NULL DEFAULT '',
    expected           TEXT        NOT NULL DEFAULT '',
    response           TEXT        NOT NULL DEFAULT '',
    artifact_key       VARCHAR(512) NOT NULL DEFAULT '',
    artifact_bytes     BIGINT      NOT NULL DEFAULT 0,
    artifact_truncated BOOLEAN     NOT NULL DEFAULT FALSE,
    first_token_ms     INT,
    duration_ms        INT,
    failure_class      VARCHAR(16) NOT NULL DEFAULT '',
    error_code         VARCHAR(64) NOT NULL DEFAULT '',
    bank_version       VARCHAR(32) NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT quality_inspection_results_status_ck CHECK (status IN
        ('pass','fail','unknown','error','quota','reference')),
    CONSTRAINT quality_inspection_results_failure_ck CHECK (failure_class IN ('','upstream','budget','local'))
);

CREATE INDEX IF NOT EXISTS quality_inspection_results_plan_time_idx
    ON quality_inspection_results (plan_id, created_at DESC);
CREATE INDEX IF NOT EXISTS quality_inspection_results_run_idx
    ON quality_inspection_results (run_id);
CREATE INDEX IF NOT EXISTS quality_inspection_results_question_idx
    ON quality_inspection_results (plan_id, question_id, created_at DESC);

CREATE TABLE IF NOT EXISTS quality_inspection_quarantines (
    id              BIGSERIAL PRIMARY KEY,
    plan_id         BIGINT      NOT NULL,
    account_id      BIGINT      NOT NULL,
    state           VARCHAR(24) NOT NULL DEFAULT 'cooling',
    generation      INT         NOT NULL DEFAULT 0,
    evidence_run_id BIGINT      NOT NULL DEFAULT 0,
    recheck_run_id  BIGINT      NOT NULL DEFAULT 0,
    fail_rounds     INT         NOT NULL DEFAULT 0,
    due_at          TIMESTAMPTZ,
    attempts        INT         NOT NULL DEFAULT 0,
    error_code      VARCHAR(64) NOT NULL DEFAULT '',
    actor           VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT quality_inspection_quarantines_account_key UNIQUE (account_id),
    CONSTRAINT quality_inspection_quarantines_state_ck CHECK (state IN
        ('cooling','rechecking','disabling','restoring','recovered','blocked','manual'))
);

CREATE INDEX IF NOT EXISTS quality_inspection_quarantines_state_idx
    ON quality_inspection_quarantines (state, due_at);

COMMENT ON TABLE quality_inspection_plans IS '模型质量巡检计划：单分组 + 单模型，unique(group_id, model)。';
COMMENT ON TABLE quality_inspection_questions IS '计划内题目快照；改题干/答案会变更 version，历史按版本隔离。';
COMMENT ON TABLE quality_inspection_runs IS '一次巡检轮次；resolved_* 记录复合分组的解析结果。';
COMMENT ON TABLE quality_inspection_results IS '单题结果；作品正文在对象存储，仅存 artifact_key。';
COMMENT ON TABLE quality_inspection_quarantines IS '账号级剔除记录；schedulable 为账号级开关，故按 account_id 唯一。';
