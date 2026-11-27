//go:build unit

package admin

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func TestQualityArtifactSignature_ScopedAndStable(t *testing.T) {
	handler := NewQualityInspectionHandler(nil, nil, nil, nil, nil, "test-secret")
	first := handler.signArtifact(1, 100)
	if first == "" {
		t.Fatal("signature must not be empty")
	}
	if first != handler.signArtifact(1, 100) {
		t.Fatal("signature must be deterministic for the same input")
	}
	if first == handler.signArtifact(1, 101) {
		t.Fatal("signature must bind expiry")
	}
	if first == handler.signArtifact(2, 100) {
		t.Fatal("signature must bind result id")
	}
	other := NewQualityInspectionHandler(nil, nil, nil, nil, nil, "another-secret")
	if other.signArtifact(1, 100) == first {
		t.Fatal("different secrets must not produce the same signature")
	}
}

func TestQualityArtifactSignature_EmptySecretFallsBack(t *testing.T) {
	handler := NewQualityInspectionHandler(nil, nil, nil, nil, nil, "")
	first := handler.signArtifact(7, 200)
	if first == "" {
		t.Fatal("fallback key must still sign")
	}
	if first != handler.signArtifact(7, 200) {
		t.Fatal("fallback signing must stay deterministic within one process")
	}
}

func TestQualityQuestionsToView_SerializesSnakeCaseBuiltinQuestions(t *testing.T) {
	views := qualityQuestionsToView(service.BuiltinQualityQuestions())
	if len(views) == 0 {
		t.Fatal("builtin questions must not be empty")
	}
	raw, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("marshal question views: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal question views: %v", err)
	}
	for i, item := range decoded {
		for _, key := range []string{"id", "title", "kind", "prompt", "rounds", "enabled"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("question[%d] missing snake_case key %q: %v", i, key, item)
			}
		}
		if title, _ := item["title"].(string); title == "" {
			t.Fatalf("question[%d] title must not be empty", i)
		}
		if prompt, _ := item["prompt"].(string); prompt == "" {
			t.Fatalf("question[%d] prompt must not be empty", i)
		}
	}
}
