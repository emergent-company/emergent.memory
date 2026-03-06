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
			results := svc.fuseInterleave(graphResults, textResults, nil, tt.limit)

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
		results := svc.fuseInterleave(graphResults, textResults, nil, 2)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	// Test case: limit reached after adding a graph result (odd limit)
	t.Run("limit reached after graph result", func(t *testing.T) {
		// Limit=1: adds graph(1), checks limit(1>=1) -> break
		results := svc.fuseInterleave(graphResults, textResults, nil, 1)
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
		results := svc.fuseGraphFirst(graphResults, textResults, nil, 10)

		assert.Len(t, results, 4)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
		assert.Equal(t, ItemTypeText, results[2].Type)
		assert.Equal(t, ItemTypeText, results[3].Type)
	})

	t.Run("graph first with small limit", func(t *testing.T) {
		results := svc.fuseGraphFirst(graphResults, textResults, nil, 3)

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
		results := svc.fuseGraphFirst(manyGraphResults, textResults, nil, 2)

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
		results := svc.fuseTextFirst(graphResults, textResults, nil, 10)

		assert.Len(t, results, 4)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
		assert.Equal(t, ItemTypeGraph, results[2].Type)
		assert.Equal(t, ItemTypeGraph, results[3].Type)
	})

	t.Run("text first with small limit", func(t *testing.T) {
		results := svc.fuseTextFirst(graphResults, textResults, nil, 3)

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
		results := svc.fuseTextFirst(graphResults, manyTextResults, nil, 2)

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
		results := svc.fuseWeighted(graphResults, textResults, nil, weights, 10)

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
		results := svc.fuseWeighted(graphResults, textResults, nil, weights, 10)

		assert.Len(t, results, 4)
		// First result should be g1 with higher weighted score
		assert.Equal(t, "g1", results[0].ObjectID)
	})

	t.Run("nil weights uses defaults", func(t *testing.T) {
		results := svc.fuseWeighted(graphResults, textResults, nil, nil, 10)

		assert.Len(t, results, 4)
		// Should work with default 0.5/0.5 weights
	})

	t.Run("respects limit", func(t *testing.T) {
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.5}
		results := svc.fuseWeighted(graphResults, textResults, nil, weights, 2)

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
		results := svc.fuseRRF(graphResults, textResults, nil, 10)

		// All 4 results should be included
		assert.Len(t, results, 4)

		// RRF scores should be based on rank positions
		// Check that scores are positive
		for _, r := range results {
			assert.Greater(t, r.Score, float32(0))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		results := svc.fuseRRF(graphResults, textResults, nil, 2)

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

		results := svc.fuseRRF(graphWithOverlap, textWithOverlap, nil, 10)

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
			results := svc.fuseResults(graphResults, textResults, nil, strategy, nil, 10)
			assert.NotNil(t, results)
			assert.Len(t, results, 2)
		})
	}

	t.Run("invalid strategy defaults to weighted", func(t *testing.T) {
		results := svc.fuseResults(graphResults, textResults, nil, "invalid", nil, 10)
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
		results := svc.fuseInterleave(nil, textResults, nil, 10)
		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeText, results[0].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
		}
		results := svc.fuseInterleave(graphResults, nil, nil, 10)
		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseInterleave(nil, nil, nil, 10)
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
		results := svc.fuseGraphFirst(nil, textResults, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
			{ObjectID: "g2", Score: 0.8},
		}
		results := svc.fuseGraphFirst(graphResults, nil, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseGraphFirst(nil, nil, nil, 10)
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
		results := svc.fuseTextFirst(nil, textResults, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
	})

	t.Run("empty text results", func(t *testing.T) {
		graphResults := []*UnifiedSearchGraphResult{
			{ObjectID: "g1", Score: 0.9},
			{ObjectID: "g2", Score: 0.8},
		}
		results := svc.fuseTextFirst(graphResults, nil, nil, 10)
		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeGraph, results[1].Type)
	})

	t.Run("both empty", func(t *testing.T) {
		results := svc.fuseTextFirst(nil, nil, nil, 10)
		assert.Len(t, results, 0)
	})
}

// =============================================================================
// Relationship Results in Fusion Strategies
// =============================================================================

// Helper to create test relationship results
func makeRelationshipResults(count int) []*RelationshipSearchResult {
	results := make([]*RelationshipSearchResult, count)
	for i := 0; i < count; i++ {
		results[i] = &RelationshipSearchResult{
			ID:          uuid.New(),
			SrcID:       uuid.New(),
			DstID:       uuid.New(),
			Type:        "RELATED_TO",
			TripletText: "Entity A related to Entity B",
			Score:       float32(0.9) - float32(i)*0.1,
		}
	}
	return results
}

func TestFuseInterleave_WithRelationships(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
		{ObjectID: "g2", Score: 0.8},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.75, Text: "text2"},
	}
	relResults := makeRelationshipResults(2)

	t.Run("three-way round-robin", func(t *testing.T) {
		results := svc.fuseInterleave(graphResults, textResults, relResults, 10)

		// Expected order: graph, text, rel, graph, text, rel
		assert.Len(t, results, 6)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
		assert.Equal(t, ItemTypeRelationship, results[2].Type)
		assert.Equal(t, ItemTypeGraph, results[3].Type)
		assert.Equal(t, ItemTypeText, results[4].Type)
		assert.Equal(t, ItemTypeRelationship, results[5].Type)
	})

	t.Run("limit truncates mid-round", func(t *testing.T) {
		results := svc.fuseInterleave(graphResults, textResults, relResults, 4)

		assert.Len(t, results, 4)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeText, results[1].Type)
		assert.Equal(t, ItemTypeRelationship, results[2].Type)
		assert.Equal(t, ItemTypeGraph, results[3].Type)
	})

	t.Run("only relationship results", func(t *testing.T) {
		results := svc.fuseInterleave(nil, nil, relResults, 10)

		assert.Len(t, results, 2)
		assert.Equal(t, ItemTypeRelationship, results[0].Type)
		assert.Equal(t, ItemTypeRelationship, results[1].Type)
	})

	t.Run("relationship results with fields populated", func(t *testing.T) {
		results := svc.fuseInterleave(nil, nil, relResults, 1)

		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeRelationship, results[0].Type)
		assert.Equal(t, "RELATED_TO", results[0].RelationshipType)
		assert.Equal(t, "Entity A related to Entity B", results[0].TripletText)
		assert.NotEmpty(t, results[0].SourceID)
		assert.NotEmpty(t, results[0].TargetID)
	})
}

func TestFuseGraphFirst_WithRelationships(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
	}
	relResults := makeRelationshipResults(1)

	t.Run("order is graph then relationship then text", func(t *testing.T) {
		results := svc.fuseGraphFirst(graphResults, textResults, relResults, 10)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeRelationship, results[1].Type)
		assert.Equal(t, ItemTypeText, results[2].Type)
	})

	t.Run("limit reached in relationship section", func(t *testing.T) {
		moreRels := makeRelationshipResults(3)
		results := svc.fuseGraphFirst(graphResults, textResults, moreRels, 3)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeGraph, results[0].Type)
		assert.Equal(t, ItemTypeRelationship, results[1].Type)
		assert.Equal(t, ItemTypeRelationship, results[2].Type)
	})

	t.Run("only relationship results", func(t *testing.T) {
		results := svc.fuseGraphFirst(nil, nil, relResults, 10)

		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeRelationship, results[0].Type)
	})
}

func TestFuseTextFirst_WithRelationships(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.85, Text: "text1"},
	}
	relResults := makeRelationshipResults(1)

	t.Run("order is text then relationship then graph", func(t *testing.T) {
		results := svc.fuseTextFirst(graphResults, textResults, relResults, 10)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeRelationship, results[1].Type)
		assert.Equal(t, ItemTypeGraph, results[2].Type)
	})

	t.Run("limit reached in relationship section", func(t *testing.T) {
		moreRels := makeRelationshipResults(3)
		results := svc.fuseTextFirst(graphResults, textResults, moreRels, 3)

		assert.Len(t, results, 3)
		assert.Equal(t, ItemTypeText, results[0].Type)
		assert.Equal(t, ItemTypeRelationship, results[1].Type)
		assert.Equal(t, ItemTypeRelationship, results[2].Type)
	})

	t.Run("only relationship results", func(t *testing.T) {
		results := svc.fuseTextFirst(nil, nil, relResults, 10)

		assert.Len(t, results, 1)
		assert.Equal(t, ItemTypeRelationship, results[0].Type)
	})
}

func TestFuseWeighted_WithRelationships(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
	}
	relResults := makeRelationshipResults(1) // Score: 0.9

	t.Run("relationships included with default weights", func(t *testing.T) {
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.5}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		assert.Len(t, results, 3)
		// Relationship items should be present
		hasRel := false
		for _, r := range results {
			if r.Type == ItemTypeRelationship {
				hasRel = true
				break
			}
		}
		assert.True(t, hasRel, "weighted fusion should include relationship results")
	})

	t.Run("relationships appear in output with nil weights", func(t *testing.T) {
		results := svc.fuseWeighted(graphResults, textResults, relResults, nil, 10)

		assert.Len(t, results, 3)
		types := map[UnifiedSearchItemType]bool{}
		for _, r := range results {
			types[r.Type] = true
		}
		assert.True(t, types[ItemTypeRelationship])
		assert.True(t, types[ItemTypeGraph])
		assert.True(t, types[ItemTypeText])
	})
}

func TestFuseWeighted_RelationshipWeight(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 1.0},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 1.0, Text: "text1"},
	}
	relResults := []*RelationshipSearchResult{
		{ID: uuid.New(), SrcID: uuid.New(), DstID: uuid.New(), Type: "KNOWS", TripletText: "A knows B", Score: 1.0},
	}

	t.Run("backward compat: omitted RelationshipWeight uses graphWeight", func(t *testing.T) {
		// With equal graph/text weights (0.5/0.5), normalized = 0.5/0.5
		// Relationship should get graphWeight = 0.5
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.5}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		assert.Len(t, results, 3)
		var graphScore, textScore, relScore float32
		for _, r := range results {
			switch r.Type {
			case ItemTypeGraph:
				graphScore = r.Score
			case ItemTypeText:
				textScore = r.Score
			case ItemTypeRelationship:
				relScore = r.Score
			}
		}
		// Graph and text each get 1.0 * 0.5 = 0.5
		assert.InDelta(t, 0.5, graphScore, 0.01)
		assert.InDelta(t, 0.5, textScore, 0.01)
		// Relationship uses graphWeight (0.5) for backward compat
		assert.InDelta(t, 0.5, relScore, 0.01)
	})

	t.Run("backward compat: uneven weights still applies graphWeight to rels", func(t *testing.T) {
		// graphWeight=0.8, textWeight=0.2 → normalized: graph=0.8, text=0.2
		// Relationship should get graphWeight = 0.8
		weights := &UnifiedSearchWeights{GraphWeight: 0.8, TextWeight: 0.2}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		var graphScore, relScore float32
		for _, r := range results {
			switch r.Type {
			case ItemTypeGraph:
				graphScore = r.Score
			case ItemTypeRelationship:
				relScore = r.Score
			}
		}
		assert.InDelta(t, 0.8, graphScore, 0.01)
		assert.InDelta(t, 0.8, relScore, 0.01, "relationship score should equal graph score when RelationshipWeight is omitted")
	})

	t.Run("three-way normalize: explicit RelationshipWeight", func(t *testing.T) {
		// graph=1, text=1, rel=1 → normalized: each = 1/3
		weights := &UnifiedSearchWeights{GraphWeight: 1.0, TextWeight: 1.0, RelationshipWeight: 1.0}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		assert.Len(t, results, 3)
		var graphScore, textScore, relScore float32
		for _, r := range results {
			switch r.Type {
			case ItemTypeGraph:
				graphScore = r.Score
			case ItemTypeText:
				textScore = r.Score
			case ItemTypeRelationship:
				relScore = r.Score
			}
		}
		expected := float32(1.0 / 3.0)
		assert.InDelta(t, expected, graphScore, 0.01)
		assert.InDelta(t, expected, textScore, 0.01)
		assert.InDelta(t, expected, relScore, 0.01)
	})

	t.Run("three-way normalize: uneven weights", func(t *testing.T) {
		// graph=0.5, text=0.3, rel=0.2 → total=1.0
		weights := &UnifiedSearchWeights{GraphWeight: 0.5, TextWeight: 0.3, RelationshipWeight: 0.2}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		assert.Len(t, results, 3)
		var graphScore, textScore, relScore float32
		for _, r := range results {
			switch r.Type {
			case ItemTypeGraph:
				graphScore = r.Score
			case ItemTypeText:
				textScore = r.Score
			case ItemTypeRelationship:
				relScore = r.Score
			}
		}
		assert.InDelta(t, 0.5, graphScore, 0.01)
		assert.InDelta(t, 0.3, textScore, 0.01)
		assert.InDelta(t, 0.2, relScore, 0.01)
	})

	t.Run("three-way normalize: high relationship weight", func(t *testing.T) {
		// graph=0.2, text=0.2, rel=0.6 → total=1.0
		// Relationships should dominate ranking
		weights := &UnifiedSearchWeights{GraphWeight: 0.2, TextWeight: 0.2, RelationshipWeight: 0.6}
		results := svc.fuseWeighted(graphResults, textResults, relResults, weights, 10)

		assert.Len(t, results, 3)
		// The relationship result should be first (highest fused score)
		assert.Equal(t, ItemTypeRelationship, results[0].Type)
		assert.InDelta(t, 0.6, results[0].Score, 0.01)
	})
}

func TestFuseRRF_WithRelationships(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
	}
	relResults := makeRelationshipResults(1)

	t.Run("relationships included in RRF output", func(t *testing.T) {
		results := svc.fuseRRF(graphResults, textResults, relResults, 10)

		assert.Len(t, results, 3)
		types := map[UnifiedSearchItemType]bool{}
		for _, r := range results {
			types[r.Type] = true
		}
		assert.True(t, types[ItemTypeRelationship], "RRF should include relationship results")
		assert.True(t, types[ItemTypeGraph])
		assert.True(t, types[ItemTypeText])
	})

	t.Run("relationship RRF scores are positive", func(t *testing.T) {
		results := svc.fuseRRF(graphResults, textResults, relResults, 10)

		for _, r := range results {
			assert.Greater(t, r.Score, float32(0), "all RRF scores should be positive")
		}
	})
}

func TestFuseResults_WithRelationships_AllStrategies(t *testing.T) {
	svc := newTestService()

	graphResults := []*UnifiedSearchGraphResult{
		{ObjectID: "g1", Score: 0.9},
	}
	textResults := []*TextSearchResult{
		{ID: uuid.New(), DocumentID: uuid.New(), Score: 0.8, Text: "text1"},
	}
	relResults := makeRelationshipResults(1)

	strategies := []UnifiedSearchFusionStrategy{
		FusionStrategyWeighted,
		FusionStrategyRRF,
		FusionStrategyInterleave,
		FusionStrategyGraphFirst,
		FusionStrategyTextFirst,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			results := svc.fuseResults(graphResults, textResults, relResults, strategy, nil, 10)

			assert.Len(t, results, 3, "all 3 result types should appear for strategy %s", strategy)

			types := map[UnifiedSearchItemType]bool{}
			for _, r := range results {
				types[r.Type] = true
			}
			assert.True(t, types[ItemTypeGraph], "strategy %s missing graph results", strategy)
			assert.True(t, types[ItemTypeText], "strategy %s missing text results", strategy)
			assert.True(t, types[ItemTypeRelationship], "strategy %s missing relationship results", strategy)
		})
	}
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
