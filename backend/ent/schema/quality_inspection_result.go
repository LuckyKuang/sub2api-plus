package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// QualityInspectionResult 单题结果。作品正文在对象存储，只存 artifact_key。
type QualityInspectionResult struct {
	ent.Schema
}

func (QualityInspectionResult) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "quality_inspection_results"},
	}
}

func (QualityInspectionResult) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("plan_id"),
		field.Int64("account_id"),
		field.String("question_id").MaxLen(40).NotEmpty(),
		field.String("kind").MaxLen(16).Default(""),
		field.String("status").MaxLen(16).Default("unknown"),
		field.String("answer").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("expected").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("response").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("artifact_key").MaxLen(512).Default(""),
		field.Int64("artifact_bytes").Default(0),
		field.Bool("artifact_truncated").Default(false),
		field.Int("first_token_ms").Optional().Nillable(),
		field.Int("duration_ms").Optional().Nillable(),
		field.String("failure_class").MaxLen(16).Default(""),
		field.String("error_code").MaxLen(64).Default(""),
		field.String("bank_version").MaxLen(32).Default(""),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (QualityInspectionResult) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plan_id", "created_at"),
		index.Fields("run_id"),
		index.Fields("plan_id", "question_id", "created_at"),
	}
}
