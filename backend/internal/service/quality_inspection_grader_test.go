//go:build unit

package service

import "testing"

func TestGradeQualityQuestion_Number(t *testing.T) {
	candy, _ := BuiltinQualityQuestion("candy")
	cases := []struct {
		name     string
		response string
		want     string
	}{
		{"explicit marker", "推理略。\n最终答案：21", QualityStatusPass},
		{"with unit", "最终答案：21 个", QualityStatusPass},
		{"wrong", "最终答案：22", QualityStatusFail},
		{"no marker multi line", "我认为答案是 21\n但不确定", QualityStatusUnknown},
		{"bare single line", "21", QualityStatusPass},
		{"fenced reasoning then marker", "```\n21\n```\n最终答案：21", QualityStatusPass},
		{"marker with explanation", "最终答案：21，最坏情况已证明", QualityStatusPass},
		{"marker with competing number", "最终答案：21，因为最坏情况是 5 只", QualityStatusUnknown},
		{"boxed", "\\boxed{21}", QualityStatusPass},
		{"negated", "最终答案不是 21", QualityStatusUnknown},
		{"conditional", "如果取 21 个就可以\n最终答案：", QualityStatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := GradeQualityQuestion(candy, tc.response); got != tc.want {
				t.Fatalf("grade = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGradeQualityQuestion_Choice(t *testing.T) {
	logic, _ := BuiltinQualityQuestion("logic-choice")
	cases := []struct {
		response string
		want     string
	}{
		{"最终答案：3", QualityStatusPass},
		{"最终答案：2", QualityStatusFail},
		{"最终答案：4", QualityStatusUnknown},
		{"3", QualityStatusPass},
		{"最终答案：三个", QualityStatusUnknown},
	}
	for _, tc := range cases {
		if got, _ := GradeQualityQuestion(logic, tc.response); got != tc.want {
			t.Fatalf("%q grade = %s, want %s", tc.response, got, tc.want)
		}
	}
}

func TestGradeQualityQuestion_JSON(t *testing.T) {
	question, _ := BuiltinQualityQuestion("json-contract")
	cases := []struct {
		response string
		want     string
	}{
		{`{"answer":42,"unit":"items"}`, QualityStatusPass},
		{"```json\n{\"answer\":42,\"unit\":\"items\"}\n```", QualityStatusUnknown},
		{`{"answer":42,"unit":"items","extra":1}`, QualityStatusFail},
		{`{"answer":"42","unit":"items"}`, QualityStatusFail},
		{`{"answer":42}`, QualityStatusFail},
	}
	for _, tc := range cases {
		if got, _ := GradeQualityQuestion(question, tc.response); got != tc.want {
			t.Fatalf("%q grade = %s, want %s", tc.response, got, tc.want)
		}
	}
}

func TestGradeQualityQuestion_Drawing(t *testing.T) {
	question, _ := BuiltinQualityQuestion("pelican")
	if got, _ := GradeQualityQuestion(question, "说明\n<html><body><svg></svg></body></html>"); got != QualityStatusPass {
		t.Fatalf("complete svg should pass, got %s", got)
	}
	if got, _ := GradeQualityQuestion(question, "<html><body>没有图形</body></html>"); got != QualityStatusFail {
		t.Fatalf("missing svg should fail, got %s", got)
	}
}

func TestGradeQualityQuestion_Reference(t *testing.T) {
	question, _ := BuiltinQualityQuestion("fingerprint")
	if got, _ := GradeQualityQuestion(question, "任意回答"); got != QualityStatusReference {
		t.Fatalf("reference should never be graded, got %s", got)
	}
}

func TestAggregateQualityVerdict(t *testing.T) {
	cases := []struct {
		statuses []string
		want     string
	}{
		{[]string{QualityStatusPass, QualityStatusPass}, QualityStatusPass},
		{[]string{QualityStatusPass, QualityStatusFail}, QualityStatusFail},
		{[]string{QualityStatusFail, QualityStatusError}, QualityStatusError},
		{[]string{QualityStatusFail, QualityStatusQuota}, QualityStatusQuota},
		{[]string{QualityStatusPass, QualityStatusUnknown}, QualityStatusUnknown},
		{[]string{QualityStatusPass, QualityStatusReference}, QualityStatusReference},
		{nil, QualityStatusError},
	}
	for _, tc := range cases {
		if got := AggregateQualityVerdict(tc.statuses); got != tc.want {
			t.Fatalf("aggregate %v = %s, want %s", tc.statuses, got, tc.want)
		}
	}
}

func TestQuestionVersionChangesWithContent(t *testing.T) {
	base, _ := BuiltinQualityQuestion("numeric")
	version := QuestionVersion(base)
	changed := base
	changed.Expected = "324"
	if QuestionVersion(changed) == version {
		t.Fatal("changing expected must change version")
	}
	rounds := base
	rounds.Rounds = 2
	if QuestionVersion(rounds) == version {
		t.Fatal("changing rounds must change version")
	}
	if QuestionVersion(base) != version {
		t.Fatal("version must be stable for identical content")
	}
}
