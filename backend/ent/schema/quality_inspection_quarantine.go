package schema

import (
	"github.com/LuckyKuang/sub2api-plus/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// QualityInspectionQuarantine 账号级剔除记录。
// schedulable 是账号级开关，故按 account_id 唯一；plan_id 记录触发它的计划。
type QualityInspectionQuarantine struct {
	ent.Schema
}

func (QualityInspectionQuarantine) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "quality_inspection_quarantines"},
	}
}

func (QualityInspectionQuarantine) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (QualityInspectionQuarantine) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("plan_id"),
		field.Int64("account_id"),
		field.String("state").MaxLen(24).Default("cooling"),
		field.Int("generation").Default(0),
		field.Int64("evidence_run_id").Default(0),
		field.Int64("recheck_run_id").Default(0),
		field.Int("fail_rounds").Default(0),
		field.Time("due_at").Optional().Nillable(),
		field.Int("attempts").Default(0),
		field.String("error_code").MaxLen(64).Default(""),
		field.String("actor").MaxLen(64).Default(""),
	}
}

func (QualityInspectionQuarantine) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("account_id").Unique(),
		index.Fields("state", "due_at"),
	}
}
