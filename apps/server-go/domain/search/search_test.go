package search

import (
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestCalcScoreStats(t *testing.T) {
	tests := []struct {
		name     string
		scores   []float32
		wantMin  float32
		wantMax  float32
		wantMean float32
	}{
		{
			name:     "empty slice",
			scores:   []float32{},
			wantMin:  0,
			wantMax:  0,
			wantMean: 0,
		},
		{
			name:     "single element",
			scores:   []float32{5.0},
			wantMin:  5.0,
			wantMax:  5.0,
			wantMean: 5.0,
		},
		{
			name:     "two elements",
			scores:   []float32{2.0, 8.0},
			wantMin:  2.0,
			wantMax:  8.0,
			wantMean: 5.0,
		},
		{
			name:     "multiple elements",
			scores:   []float32{1.0, 2.0, 3.0, 4.0, 5.0},
			wantMin:  1.0,
			wantMax:  5.0,
			wantMean: 3.0,
		},
		{
			name:     "all same values",
			scores:   []float32{7.0, 7.0, 7.0},
			wantMin:  7.0,
			wantMax:  7.0,
			wantMean: 7.0,
		},
		{
			name:     "negative values",
			scores:   []float32{-5.0, 0.0, 5.0},
			wantMin:  -5.0,
			wantMax:  5.0,
			wantMean: 0.0,
		},
		{
			name:     "zero values",
			scores:   []float32{0.0, 0.0, 0.0},
			wantMin:  0.0,
			wantMax:  0.0,
			wantMean: 0.0,
		},
		{
			name:     "large values",
			scores:   []float32{1000.0, 2000.0, 3000.0},
			wantMin:  1000.0,
			wantMax:  3000.0,
			wantMean: 2000.0,
		},
		{
			name:     "small decimal values",
			scores:   []float32{0.1, 0.2, 0.3},
			wantMin:  0.1,
			wantMax:  0.3,
			wantMean: 0.2,
		},
		{
			name:     "min not first element",
			scores:   []float32{5.0, 3.0, 1.0},
			wantMin:  1.0,
			wantMax:  5.0,
			wantMean: 3.0,
		},
		{
			name:     "descending order",
			scores:   []float32{10.0, 5.0, 2.0, 1.0},
			wantMin:  1.0,
			wantMax:  10.0,
			wantMean: 4.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, gotMax, gotMean := calcScoreStats(tt.scores)
			assert.InDelta(t, tt.wantMin, gotMin, 0.0001)
			assert.InDelta(t, tt.wantMax, gotMax, 0.0001)
			assert.InDelta(t, tt.wantMean, gotMean, 0.0001)
		})
	}
}

// Helper to create a minimal service for testing pure methods
func newTestService() *Service {
	return &Service{
		log: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
}

func strPtr(s string) *string { return &s }

func TestGraphResultToItem(t *testing.T) {
	svc := newTestService()

	explanation := "Matched by name"
	graphResult := &UnifiedSearchGraphResult{
		ObjectID:    "obj-123",
		CanonicalID: "can-456",
		ObjectType:  "Person",
		Key:         "john-doe",
		Fields:      map[string]any{"name": "John Doe"},
		Score:       0.95,
		Rank:        1,
		Relationships: []UnifiedSearchRelationship{
			{ObjectID: "obj-789", Type: "KNOWS", Direction: "out"},
		},
		Explanation: &explanation,
	}

	item := svc.graphResultToItem(graphResult)

	assert.Equal(t, ItemTypeGraph, item.Type)
	assert.Equal(t, "obj-123", item.ID)
	assert.Equal(t, "obj-123", item.ObjectID)
	assert.Equal(t, "can-456", item.CanonicalID)
	assert.Equal(t, "Person", item.ObjectType)
	assert.Equal(t, "john-doe", item.Key)
	assert.Equal(t, float32(0.95), item.Score)
	assert.Equal(t, 1, item.Rank)
	assert.Len(t, item.Relationships, 1)
	assert.Equal(t, &explanation, item.Explanation)
}

func TestTextResultToItem(t *testing.T) {
	svc := newTestService()

	docID := uuid.New()
	chunkID := uuid.New()

	textResult := &TextSearchResult{
		ID:         chunkID,
		DocumentID: docID,
		Score:      0.85,
		Text:       "This is a snippet of text",
		Source:     strPtr("document.pdf"),
		Mode:       strPtr("hybrid"),
	}

	item := svc.textResultToItem(textResult)

	assert.Equal(t, ItemTypeText, item.Type)
	assert.Equal(t, chunkID.String(), item.ID)
	assert.Equal(t, float32(0.85), item.Score)
	assert.Equal(t, "This is a snippet of text", item.Snippet)
	assert.Equal(t, strPtr("document.pdf"), item.Source)
	assert.Equal(t, strPtr("hybrid"), item.Mode)
	assert.NotNil(t, item.DocumentID)
	assert.Equal(t, docID.String(), *item.DocumentID)
}

func TestFuseInterleave(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.8},
		{ObjectID: "g3", Score: 0.7},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.75, Text: "text2"},
	}

	tests := []struct {
		name          string
		limit         int
		expectedOrder []UnifiedSearchItemType
	}{
		{
			name:          "interleave with limit 5",
			limit:         5,
			expectedOrder: []UnifiedSearchItemType{ItemTypeGraph, ItemTypeText, ItemTypeGraph, ItemTypeText, ItemTypeGraph},
		},
		{
			name:          "interleave with limit 3",
			limit:         3,
			expectedOrder: []UnifiedSearchItemType{ItemTypeGraph, ItemTypeText, ItemTypeGraph},
		},
		{
			name:          "interleave with limit 10 (more than available)",
			limit:         10,
			expectedOrder: []UnifiedSearchItemType{ItemTypeGraph, ItemTypeText, ItemTypeGraph, ItemTypeText, ItemTypeGraph},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := svc.fuseInterleave(graphResults, textResults, tt.limit)

			assert.Len(t, results, len(tt.expectedOrder))
			for i, expectedType := range tt.expectedOrder {
				assert.Equal(t, expectedType, results[i].Type, "position %d", i)
			}
		})
	}

	// Test case: limit reached exactly after adding a text result
	// This covers the second break after text result (line 516-517)
	t.Run("limit reached after text result", func(t *testing.T) {
		// Limit=2: adds graph(1), checks limit(1<2), adds text(2), checks limit(2>=2) -> break
		results := svc.fuseInterleave(graphResults, textResults, 2)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	// Test case: limit reached after adding a graph result (odd limit)
	t.Run("limit reached after graph result", func(t *testing.T) {
		// Limit=1: adds graph(1), checks limit(1>=1) -> break
		results := svc.fuseInterleave(graphResults, textResults, 1)
		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
	})
}

func TestFuseGraphFirst(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.8},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.75, Text: "text2"},
	}

	t.Run("graph first with limit exceeding total", func(t *testing.T) {
		results := svc.fuseGraphFirst(graphResults, textResults, 10)

		assert.Len(t, results, 4)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
		assert.Equal(t, ItemTypeText, results[2].Type)
		assert.Equal(t, ItemTypeText, results[3].Type)
	})

	t.Run("graph first with small limit", func(t *testing.T) {
		results := svc.fuseGraphFirst(graphResults, textResults, 3)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
		assert.Equal(t, ItemTypeText, results[2].Type)
	})

	t.Run("limit reached within graph results", func(t *testing.T) {
		// Many graph results, small limit - should break during graph loop
		manyGraphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
			{ObjectID: "g2", Score: 0.8},
			{ObjectID: "g3", Score: 0.7},
			{ObjectID: "g4", Score: 0.6},
		}
		results := svc.fuseGraphFirst(manyGraphResults, textResults, 2)

		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
	})
}

func TestFuseTextFirst(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.8},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.75, Text: "text2"},
	}

	t.Run("text first with limit exceeding total", func(t *testing.T) {
		results := svc.fuseTextFirst(graphResults, textResults, 10)

		assert.Len(t, results, 4)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
		assert.Equal(t, ItemTypeGraph, results[2].Type)
		assert.Equal(t, ItemTypeGraph, results[3].Type)
	})

	t.Run("text first with small limit", func(t *testing.T) {
		results := svc.fuseTextFirst(graphResults, textResults, 3)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
		assert.Equal(t, ItemTypeGraph, results[2].Type)
	})

	t.Run("limit reached within text results", func(t *testing.T) {
		// Many text results, small limit - should break during text loop
		manyTextResults := []*TextSearchResult{
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.9, Text: "text1"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text2"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.7, Text: "text3"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "text4"},
		}
		results := svc.fuseTextFirst(graphResults, manyTextResults, 2)

		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})
}

func TestFuseWeighted(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.5},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "text2"},
	}

	t.Run("equal weights", func(t *testing.T) {
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.5}
		results := svc.fuseWeighted(graphResults, textResults, weights, 10)

		// All 4 results should be included, sorted by weighted score
		assert.Len(t, results, 4)

		// Scores should be weighted (0.5 * original_score)
		// g1: 0.9 * 0.5 = 0.45
		// t1: 0.8 * 0.5 = 0.40
		// t2: 0.6 * 0.5 = 0.30
		// g2: 0.5 * 0.5 = 0.25
		assert.Equal(t, "g1", results[0].ObjectID)
		assert.InDelta(t, 0.45, results[0].Score, 0.01)
	})

	t.Run("graph weighted higher", func(t *testing.T) {
		weights := &UnifiedSearchWeights{GraphWeight: 0.8, TextWeight: 0.2}
		results := svc.fuseWeighted(graphResults, textResults, weights, 10)

		assert.Len(t, results, 4)
		// First result should be g1 with higher weighted score
		assert.Equal(t, "g1", results[0].ObjectID)
	})

	t.Run("nil weights uses defaults", func(t *testing.T) {
		results := svc.fuseWeighted(graphResults, textResults, nil, 10)

		assert.Len(t, results, 4)
		// Should work with default 0.5/0.5 weights
	})

	t.Run("respects limit", func(t *testing.T) {
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.5}
		results := svc.fuseWeighted(graphResults, textResults, weights, 2)

		assert.Len(t, results, 2)
	})
}

func TestFuseRRF(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.5},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "text2"},
	}

	t.Run("basic RRF fusion", func(t *testing.T) {
		results := svc.fuseRRF(graphResults, textResults, 10)

		// All 4 results should be included
		assert.Len(t, results, 4)

		// RRF scores should be based on rank positions
		// Check that scores are positive
		for _, r := range results {
			assert.Greater(t, r.Score, float32(0))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		results := svc.fuseRRF(graphResults, textResults, 2)

		assert.Len(t, results, 2)
	})

	t.Run("overlapping items boost score", func(t *testing.T) {
		// Create a case where the same ID appears in both graph and text results
		overlappingID := uuid.New()

		graphWithOverlap := []*UnifiedSearchGraphResult{
			{ObjectID: overlappingID.String(), Score: 0.9},
			{ObjectID: "unique-graph", Score: 0.5},
		}

		textWithOverlap := []*TextSearchResult{
			{ID: overlappingID, DocumentID: uuid.New(), Score: 0.8, Text: "overlapping text"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "unique text"},
		}

		results := svc.fuseRRF(graphWithOverlap, textWithOverlap, 10)

		// Should have 3 unique items (1 overlapping + 2 unique)
		assert.Len(t, results, 3)

		// The overlapping item should have a boosted score (sum of both RRF scores)
		// and should appear first
		assert.Equal(t, overlappingID.String(), results[0].ID)

		// Its score should be higher than just one RRF score would be
		// RRF score for rank 1 = 1/(60+1) = ~0.0164
		// If boosted, it should be ~2x that
		assert.Greater(t, results[0].Score, float32(0.02))
	})
}

func TestFuseResults_StrategyDispatch(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}

	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
	}

	strategies := []UnifiedSearchFusionStrategy{
		FusionStrategyWeighted,
		FusionStrategyRRF,
		FusionStrategyInterleave,
		FusionStrategyGraphFirst,
		FusionStrategyTextFirst,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			results := svc.fuseResults(graphResults, textResults, strategy, nil, 10)
			assert.NotNil(t, results)
			assert.Len(t, results, 2)
		})
	}

	t.Run("invalid strategy defaults to weighted", func(t *testing.T) {
		results := svc.fuseResults(graphResults, textResults, "invalid", nil, 10)
		assert.NotNil(t, results)
		assert.Len(t, results, 2)
	})
}

func TestFuseInterleave_EmptyInputs(t *testing.T) {
	svc := newTestService()

	t.Run("empty graph results", func(t *testing.T) {
		textResults := []*TextSearchResult{
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
		}
		results := svc.fuseInterleave(nil, textResults, 10)
		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeText, results[0].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
		}
		results := svc.fuseInterleave(graphResults, nil, 10)
		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseInterleave(nil, nil, 10)
		assert.Len(t, results, 0)
	})
}

func TestFuseGraphFirst_EmptyInputs(t *testing.T) {
	svc := newTestService()

	t.Run("empty graph results", func(t *testing.T) {
		textResults := []*TextSearchResult{
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "text2"},
		}
		results := svc.fuseGraphFirst(nil, textResults, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
			{ObjectID: "g2", Score: 0.8},
		}
		results := svc.fuseGraphFirst(graphResults, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseGraphFirst(nil, nil, 10)
		assert.Len(t, results, 0)
	})
}

func TestFuseTextFirst_EmptyInputs(t *testing.T) {
	svc := newTestService()

	t.Run("empty graph results", func(t *testing.T) {
		textResults := []*TextSearchResult{
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
			{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.6, Text: "text2"},
		}
		results := svc.fuseTextFirst(nil, textResults, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
			{ObjectID: "g2", Score: 0.8},
		}
		results := svc.fuseTextFirst(graphResults, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseTextFirst(nil, nil, 10)
		assert.Len(t, results, 0)
	})
}

// =============================================================================
// hasScope Tests
// =============================================================================

func TestHasScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		scope    string
		expected bool
	}{
		{
			name:     "scope exists",
			scopes:   []string{"read", "write", "admin"},
			scope:    "write",
			expected: true,
		},
		{
			name:     "scope does not exist",
			scopes:   []string{"read", "write"},
			scope:    "admin",
			expected: false,
		},
		{
			name:     "empty scopes list",
			scopes:   []string{},
			scope:    "read",
			expected: false,
		},
		{
			name:     "nil scopes list",
			scopes:   nil,
			scope:    "read",
			expected: false,
		},
		{
			name:     "empty scope to find",
			scopes:   []string{"read", "write"},
			scope:    "",
			expected: false,
		},
		{
			name:     "empty scope in list matches empty search",
			scopes:   []string{"read", "", "write"},
			scope:    "",
			expected: true,
		},
		{
			name:     "case sensitive - different case",
			scopes:   []string{"Read", "Write"},
			scope:    "read",
			expected: false,
		},
		{
			name:     "case sensitive - exact case",
			scopes:   []string{"Read", "Write"},
			scope:    "Read",
			expected: true,
		},
		{
			name:     "single scope matches",
			scopes:   []string{"admin"},
			scope:    "admin",
			expected: true,
		},
		{
			name:     "partial match does not count",
			scopes:   []string{"read_all", "write_all"},
			scope:    "read",
			expected: false,
		},
		{
			name:     "scope with special characters",
			scopes:   []string{"api:read", "api:write"},
			scope:    "api:read",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasScope(tt.scopes, tt.scope)
			assert.Equal(t, tt.expected, result)
		})
	}
}
