package schema

import (
	"github.com/LuckyKuang/sub2api-plus/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// QualityInspectionPlan 模型质量巡检计划。
// 单分组 + 单模型：unique(group_id, model)，同一分组可对不同模型各建一个计划。
type QualityInspectionPlan struct {
	ent.Schema
}

func (QualityInspectionPlan) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "quality_inspection_plans"},
	}
}

func (QualityInspectionPlan) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (QualityInspectionPlan) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("group_id"),
		field.String("name").MaxLen(120).NotEmpty(),
		field.String("platform").MaxLen(32).Default(""),
		field.String("model").MaxLen(160).NotEmpty(),
		field.String("endpoint").MaxLen(32).Default("any"),
		field.String("effort").MaxLen(16).Default("medium"),
		field.Bool("enabled").Default(false),
		field.Bool("schedule_enabled").Default(false),
		field.Int("interval_minutes").Default(60),
		field.String("pause_start").MaxLen(5).Default("22:00"),
		field.String("pause_end").MaxLen(5).Default("09:00"),
		field.String("timezone").MaxLen(64).Default("Asia/Shanghai"),
		field.Int("max_accounts").Default(1),
		field.Int("max_saved_rounds").Default(20),
		field.Bool("quarantine_enabled").Default(false),
		field.Int("quarantine_cooldown_minutes").Default(60),
		field.Int("quarantine_fail_rounds").Default(2),
		field.Int64("revision").Default(1),
		field.Int64("created_by").Optional().Nillable(),
	}
}

func (QualityInspectionPlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id", "model").Unique(),
		index.Fields("enabled", "schedule_enabled"),
	}
}
