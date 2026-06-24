package search

import "testing"

func TestReciprocalRankFusion(t *testing.T) {
	vec := []string{"a", "b", "c"}    // a best semantically
	lex := []string{"c", "a"}          // c best lexically, a also present
	scores := ReciprocalRankFusion(vec, lex)

	// a appears in both (rank 0 + rank 1) -> highest.
	// c appears in both (rank 2 + rank 0).
	// b appears only in vec (rank 1).
	if scores["a"] <= scores["b"] {
		t.Errorf("a (%v) should beat b (%v)", scores["a"], scores["b"])
	}
	if scores["a"] <= scores["c"] {
		t.Errorf("a (%v) should beat c (%v)", scores["a"], scores["c"])
	}
	if _, ok := scores["b"]; !ok {
		t.Error("b should be present")
	}
}
