package embeddings

import (
	"reflect"
	"testing"
)

func TestCannedEmbeddingDeterministic(t *testing.T) {
	a := cannedEmbedding("hello world")
	b := cannedEmbedding("hello world")
	c := cannedEmbedding("different text")

	if !reflect.DeepEqual(a, b) {
		t.Fatal("same input produced different vectors")
	}
	if reflect.DeepEqual(a, c) {
		t.Fatal("different inputs produced identical vectors")
	}
	if len(a) != EmbeddingDimension {
		t.Fatalf("vector dimension = %d, want %d", len(a), EmbeddingDimension)
	}
}

func TestCannedEmbeddingDistinctPerInput(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []string{"a", "b", "c", "d"} {
		vec := cannedEmbedding(s)
		// All vectors must differ (collision astronomically unlikely here).
		key := stringOfFloats(vec)
		if seen[key] {
			t.Fatalf("collision for input %q", s)
		}
		seen[key] = true
	}
}

func stringOfFloats(v []float32) string {
	b := make([]byte, len(v))
	for i, f := range v {
		b[i] = byte(f * 255)
	}
	return string(b)
}
