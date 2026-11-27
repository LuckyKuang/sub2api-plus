//go:build unit

package admin

import "testing"

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
