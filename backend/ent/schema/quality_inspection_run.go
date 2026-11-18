package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// QualityInspectionRun 一次巡检轮次（一个计划 × 一个时间槽）。
type QualityInspectionRun struct {
	ent.Schema
}

func (QualityInspectionRun) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "quality_inspection_runs"},
	}
}

func (QualityInspectionRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("plan_id"),
		field.Int64("plan_revision").Default(0),
		field.String("trigger").MaxLen(16).Default("manual"),
		field.Int64("slot").Default(0),
		field.String("status").MaxLen(32).Default("queued"),
		field.Int64("account_id").Optional().Nillable(),
		field.String("model").MaxLen(160).Default(""),
		field.String("effort").MaxLen(16).Default("medium"),
		field.Int("request_count").Default(0),
		field.JSON("stats", map[string]any{}).Optional(),
		field.String("message").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("resolved_platform").MaxLen(32).Default(""),
		field.String("resolved_upstream_model").MaxLen(160).Default(""),
		field.String("resolved_endpoint").MaxLen(32).Default(""),
		field.String("resolved_source").MaxLen(32).Default(""),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (QualityInspectionRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plan_id", "trigger", "slot").Unique(),
		index.Fields("plan_id", "started_at"),
	}
}
