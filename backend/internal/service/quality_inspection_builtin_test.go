//go:build unit

package service

import (
	"strings"
	"testing"
)

func TestBuiltinQualityQuestions_Shape(t *testing.T) {
	questions := BuiltinQualityQuestions()
	if len(questions) != 6 {
		t.Fatalf("builtin questions = %d, want 6", len(questions))
	}
	seen := map[string]bool{}
	for _, question := range questions {
		if !qualityQuestionIDPattern.MatchString(question.ID) {
			t.Fatalf("invalid builtin id %q", question.ID)
		}
		if seen[question.ID] {
			t.Fatalf("duplicate builtin id %q", question.ID)
		}
		seen[question.ID] = true
		if !qualityKindValues[question.Kind] {
			t.Fatalf("builtin %s has invalid kind %q", question.ID, question.Kind)
		}
		if strings.TrimSpace(question.Prompt) == "" {
			t.Fatalf("builtin %s missing prompt", question.ID)
		}
		if question.Rounds != 1 {
			t.Fatalf("builtin %s rounds = %d, want 1", question.ID, question.Rounds)
		}
		if !question.Enabled {
			t.Fatalf("builtin %s should be enabled by default", question.ID)
		}
		switch question.Kind {
		case QualityKindNumber, QualityKindChoice, QualityKindJSON:
			if strings.TrimSpace(question.Expected) == "" {
				t.Fatalf("gradable builtin %s needs expected answer", question.ID)
			}
		}
		if question.Kind == QualityKindChoice && !containsString(question.Choices, question.Expected) {
			t.Fatalf("choice builtin %s expected must be one of choices", question.ID)
		}
	}
	for _, id := range []string{"candy", "logic-choice", "numeric", "json-contract", "fingerprint", "pelican"} {
		if !seen[id] {
			t.Fatalf("missing builtin %q", id)
		}
	}
}

func TestBuiltinQualityQuestion_Lookup(t *testing.T) {
	question, ok := BuiltinQualityQuestion("numeric")
	if !ok || question.Expected != "323" {
		t.Fatalf("numeric lookup failed: %+v ok=%v", question, ok)
	}
	if _, ok := BuiltinQualityQuestion("nope"); ok {
		t.Fatal("unknown id must not resolve")
	}
}

func TestBuildQualityPrompt_MergesInstructions(t *testing.T) {
	question := QualityQuestion{Prompt: "题目", Instructions: "指令"}
	if got := BuildQualityPrompt(question); got != "指令\n\n题目" {
		t.Fatalf("prompt = %q", got)
	}
	if got := BuildQualityPrompt(QualityQuestion{Prompt: " 题目 "}); got != "题目" {
		t.Fatalf("prompt without instructions = %q", got)
	}
}

func TestParseClockMinutes(t *testing.T) {
	cases := map[string]int{"00:00": 0, "09:30": 570, "22:00": 1320, "23:59": 1439, "24:00": -1, "9:00x": -1, "": -1}
	for input, want := range cases {
		if got := parseClockMinutes(input); got != want {
			t.Fatalf("parseClockMinutes(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestQualityHelpers(t *testing.T) {
	if got := qualityMaxInt(3, 5); got != 5 {
		t.Fatalf("qualityMaxInt = %d", got)
	}
	if got := qualityMaxInt(7, 5); got != 7 {
		t.Fatalf("qualityMaxInt = %d", got)
	}
	if got := truncateQualityText("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncateQualityText("ab", 3); got != "ab" {
		t.Fatalf("truncate short = %q", got)
	}
	if got := parseIntWithRange("120", 60, 5, 100); got != 100 {
		t.Fatalf("clamp high = %d", got)
	}
	if got := parseIntWithRange("1", 60, 5, 100); got != 5 {
		t.Fatalf("clamp low = %d", got)
	}
	if got := parseIntWithRange("abc", 60, 5, 100); got != 60 {
		t.Fatalf("fallback = %d", got)
	}
}

func TestQuestionVersion_StableAcrossCalls(t *testing.T) {
	question, _ := BuiltinQualityQuestion("candy")
	first := QuestionVersion(question)
	second := QuestionVersion(question)
	if first == "" || first != second {
		t.Fatalf("version unstable: %q vs %q", first, second)
	}
}
