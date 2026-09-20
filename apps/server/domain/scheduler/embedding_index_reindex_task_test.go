package scheduler

import "testing"

func TestEmbeddingIndexTargetsQualified(t *testing.T) {
	want := map[string]string{
		"IDX_graph_objects_embedding_v2_ivfflat":    `"kb"."IDX_graph_objects_embedding_v2_ivfflat"`,
		"idx_graph_relationships_embedding_ivfflat": `"kb"."idx_graph_relationships_embedding_ivfflat"`,
		"idx_chunks_embedding":                      `"kb"."idx_chunks_embedding"`,
		"idx_skills_embedding_ivfflat":              `"kb"."idx_skills_embedding_ivfflat"`,
	}
	if len(embeddingIndexTargets) != len(want) {
		t.Fatalf("expected %d targets, got %d", len(want), len(embeddingIndexTargets))
	}
	for _, target := range embeddingIndexTargets {
		got, ok := want[target.name]
		if !ok {
			t.Errorf("unexpected target %s.%s", target.schema, target.name)
			continue
		}
		if q := target.qualified(); q != got {
			t.Errorf("qualified(%s) = %q, want %q", target.name, q, got)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct{ in, want string }{
		{"kb", `"kb"`},
		{"plain", `"plain"`},
		{`weird"name`, `"weird""name"`},
	}
	for _, tt := range tests {
		if got := quoteIdent(tt.in); got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
