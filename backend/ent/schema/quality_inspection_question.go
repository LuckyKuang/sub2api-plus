package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// QualityInspectionQuestion 计划内题目快照。
// 运行时不读题库，只用此快照，保证历史结果可复核。
type QualityInspectionQuestion struct {
	ent.Schema
}

func (QualityInspectionQuestion) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "quality_inspection_questions"},
	}
}

func (QualityInspectionQuestion) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("plan_id"),
		field.String("question_id").MaxLen(40).NotEmpty(),
		field.String("title").MaxLen(80).Default(""),
		field.String("kind").MaxLen(16).Default("number"),
		field.String("prompt").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("instructions").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		// expected 以文本保存：number/choice 为字面量，json 为规范化 JSON 文本。
		field.String("expected").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.JSON("choices", []string{}).Optional(),
		field.String("unit").MaxLen(16).Default(""),
		field.Int("rounds").Default(1),
		field.Bool("enabled").Default(true),
		field.String("version").MaxLen(32).Default(""),
		field.Int("position").Default(0),
	}
}

func (QualityInspectionQuestion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plan_id", "question_id").Unique(),
		index.Fields("plan_id", "position"),
	}
}
