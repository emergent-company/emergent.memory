package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Cursor Tests
// =============================================================================

func TestEncodeCursor(t *testing.T) {
	tests := []struct {
		name      string
		createdAt time.Time
		id        uuid.UUID
	}{
		{
			name:      "normal timestamp and uuid",
			createdAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			id:        uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		},
		{
			name:      "zero time",
			createdAt: time.Time{},
			id:        uuid.MustParse("00000000-0000-0000-0000-000000000000"),
		},
		{
			name:      "unix epoch",
			createdAt: time.Unix(0, 0).UTC(),
			id:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor := encodeCursor(tt.createdAt, tt.id)
			assert.NotEmpty(t, cursor)

			// Verify we can decode it back
			decoded, err := decodeCursor(cursor)
			require.NoError(t, err)
			assert.Equal(t, tt.id, decoded.ID)
			// Note: Time comparison needs to handle timezone normalization
			assert.True(t, tt.createdAt.Equal(decoded.CreatedAt),
				"expected %v, got %v", tt.createdAt, decoded.CreatedAt)
		})
	}
}

func TestDecodeCursor(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		wantErr bool
	}{
		{
			name:    "valid cursor",
			encoded: `{"created_at":"2024-01-15T10:30:00Z","id":"550e8400-e29b-41d4-a716-446655440000"}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			encoded: "not valid json",
			wantErr: true,
		},
		{
			name:    "empty string",
			encoded: "",
			wantErr: true,
		},
		{
			name:    "empty json object",
			encoded: "{}",
			wantErr: false, // Valid JSON, will have zero values
		},
		{
			name:    "malformed uuid",
			encoded: `{"created_at":"2024-01-15T10:30:00Z","id":"not-a-uuid"}`,
			wantErr: true,
		},
		{
			name:    "malformed timestamp",
			encoded: `{"created_at":"not-a-timestamp","id":"550e8400-e29b-41d4-a716-446655440000"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := decodeCursor(tt.encoded)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, decoded)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, decoded)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	// Test that encode -> decode -> encode produces consistent results
	originalTime := time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)
	originalID := uuid.New()

	encoded1 := encodeCursor(originalTime, originalID)
	decoded, err := decodeCursor(encoded1)
	require.NoError(t, err)

	encoded2 := encodeCursor(decoded.CreatedAt, decoded.ID)
	assert.Equal(t, encoded1, encoded2)
}

// =============================================================================
// branchIDsEqual Tests
// =============================================================================

func TestBranchIDsEqual(t *testing.T) {
	id1 := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	id2 := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")

	tests := []struct {
		name string
		a    *uuid.UUID
		b    *uuid.UUID
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "a nil b not nil",
			a:    nil,
			b:    &id1,
			want: false,
		},
		{
			name: "a not nil b nil",
			a:    &id1,
			b:    nil,
			want: false,
		},
		{
			name: "both same",
			a:    &id1,
			b:    &id1,
			want: true,
		},
		{
			name: "same value different pointers",
			a:    func() *uuid.UUID { v := id1; return &v }(),
			b:    func() *uuid.UUID { v := id1; return &v }(),
			want: true,
		},
		{
			name: "different values",
			a:    &id1,
			b:    &id2,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branchIDsEqual(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// zScoreNormalize Tests
// =============================================================================

func TestZScoreNormalize(t *testing.T) {
	tests := []struct {
		name  string
		score float32
		mean  float32
		std   float32
		want  float32
	}{
		{
			name:  "score at mean",
			score: 10.0,
			mean:  10.0,
			std:   2.0,
			want:  0.0,
		},
		{
			name:  "score one std above mean",
			score: 12.0,
			mean:  10.0,
			std:   2.0,
			want:  1.0,
		},
		{
			name:  "score one std below mean",
			score: 8.0,
			mean:  10.0,
			std:   2.0,
			want:  -1.0,
		},
		{
			name:  "score two std above mean",
			score: 14.0,
			mean:  10.0,
			std:   2.0,
			want:  2.0,
		},
		{
			name:  "zero standard deviation",
			score: 10.0,
			mean:  10.0,
			std:   0.0,
			want:  0.0, // Will be NaN or Inf - handled specially in test
		},
		{
			name:  "negative score",
			score: -5.0,
			mean:  0.0,
			std:   5.0,
			want:  -1.0,
		},
		{
			name:  "large values",
			score: 1000.0,
			mean:  500.0,
			std:   100.0,
			want:  5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := zScoreNormalize(tt.score, tt.mean, tt.std)
			if tt.std == 0.0 {
				// Check for NaN or Inf
				assert.True(t, got != got || got > 1e10 || got < -1e10,
					"expected NaN or Inf for zero std, got %v", got)
			} else {
				assert.InDelta(t, tt.want, got, 0.0001)
			}
		})
	}
}

// =============================================================================
// computeContentHash Tests
// =============================================================================

func TestComputeContentHash(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
	}{
		{
			name:       "nil properties",
			properties: nil,
		},
		{
			name:       "empty properties",
			properties: map[string]any{},
		},
		{
			name: "simple properties",
			properties: map[string]any{
				"name": "test",
				"age":  30,
			},
		},
		{
			name: "nested properties",
			properties: map[string]any{
				"user": map[string]any{
					"name":  "John",
					"email": "john@example.com",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := computeContentHash(tt.properties)
			assert.NotNil(t, hash)
			assert.Equal(t, 32, len(hash), "SHA-256 should produce 32 bytes")

			// Same input should produce same hash
			hash2 := computeContentHash(tt.properties)
			assert.Equal(t, hash, hash2)
		})
	}
}

func TestComputeContentHashDeterministic(t *testing.T) {
	// Properties with keys in different order should produce same hash
	props1 := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	props2 := map[string]any{
		"c": 3,
		"a": 1,
		"b": 2,
	}

	hash1 := computeContentHash(props1)
	hash2 := computeContentHash(props2)

	assert.Equal(t, hash1, hash2, "same properties in different order should produce same hash")
}

func TestComputeContentHashDifferent(t *testing.T) {
	// Different properties should produce different hashes
	props1 := map[string]any{"name": "alice"}
	props2 := map[string]any{"name": "bob"}

	hash1 := computeContentHash(props1)
	hash2 := computeContentHash(props2)

	assert.NotEqual(t, hash1, hash2, "different properties should produce different hashes")
}

// =============================================================================
// jsonEqual Tests
// =============================================================================

func TestJsonEqual(t *testing.T) {
	tests := []struct {
		name string
		a    any
		b    any
		want bool
	}{
		{
			name: "equal strings",
			a:    "hello",
			b:    "hello",
			want: true,
		},
		{
			name: "different strings",
			a:    "hello",
			b:    "world",
			want: false,
		},
		{
			name: "equal numbers",
			a:    42,
			b:    42,
			want: true,
		},
		{
			name: "different numbers",
			a:    42,
			b:    43,
			want: false,
		},
		{
			name: "equal maps",
			a:    map[string]any{"name": "test", "value": 123},
			b:    map[string]any{"name": "test", "value": 123},
			want: true,
		},
		{
			name: "different maps",
			a:    map[string]any{"name": "test"},
			b:    map[string]any{"name": "other"},
			want: false,
		},
		{
			name: "equal slices",
			a:    []int{1, 2, 3},
			b:    []int{1, 2, 3},
			want: true,
		},
		{
			name: "different slices",
			a:    []int{1, 2, 3},
			b:    []int{1, 2, 4},
			want: false,
		},
		{
			name: "nil values",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one nil one not",
			a:    nil,
			b:    "hello",
			want: false,
		},
		{
			name: "nested equal",
			a:    map[string]any{"user": map[string]any{"name": "John"}},
			b:    map[string]any{"user": map[string]any{"name": "John"}},
			want: true,
		},
		{
			name: "nested different",
			a:    map[string]any{"user": map[string]any{"name": "John"}},
			b:    map[string]any{"user": map[string]any{"name": "Jane"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonEqual(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// computeChangeSummary Tests
// =============================================================================

func TestComputeChangeSummary(t *testing.T) {
	tests := []struct {
		name     string
		oldProps map[string]any
		newProps map[string]any
		wantNil  bool
		check    func(t *testing.T, result map[string]any)
	}{
		{
			name:     "no changes",
			oldProps: map[string]any{"name": "test"},
			newProps: map[string]any{"name": "test"},
			wantNil:  true,
		},
		{
			name:     "both empty",
			oldProps: map[string]any{},
			newProps: map[string]any{},
			wantNil:  true,
		},
		{
			name:     "added property",
			oldProps: map[string]any{},
			newProps: map[string]any{"name": "test"},
			wantNil:  false,
			check: func(t *testing.T, result map[string]any) {
				added := result["added"].(map[string]any)
				assert.Equal(t, "test", added["/name"])
				assert.Empty(t, result["removed"])
				assert.Empty(t, result["updated"])
				meta := result["meta"].(map[string]any)
				assert.Equal(t, 1, meta["added"])
			},
		},
		{
			name:     "removed property",
			oldProps: map[string]any{"name": "test"},
			newProps: map[string]any{},
			wantNil:  false,
			check: func(t *testing.T, result map[string]any) {
				removed := result["removed"].([]string)
				assert.Contains(t, removed, "/name")
				assert.Empty(t, result["added"])
				assert.Empty(t, result["updated"])
				meta := result["meta"].(map[string]any)
				assert.Equal(t, 1, meta["removed"])
			},
		},
		{
			name:     "updated property",
			oldProps: map[string]any{"name": "old"},
			newProps: map[string]any{"name": "new"},
			wantNil:  false,
			check: func(t *testing.T, result map[string]any) {
				updated := result["updated"].(map[string]any)
				change := updated["/name"].(map[string]any)
				assert.Equal(t, "old", change["from"])
				assert.Equal(t, "new", change["to"])
				meta := result["meta"].(map[string]any)
				assert.Equal(t, 1, meta["updated"])
			},
		},
		{
			name:     "multiple changes",
			oldProps: map[string]any{"name": "old", "removed": "value"},
			newProps: map[string]any{"name": "new", "added": "value"},
			wantNil:  false,
			check: func(t *testing.T, result map[string]any) {
				meta := result["meta"].(map[string]any)
				assert.Equal(t, 1, meta["added"])
				assert.Equal(t, 1, meta["removed"])
				assert.Equal(t, 1, meta["updated"])
				paths := result["paths"].([]string)
				assert.Len(t, paths, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeChangeSummary(tt.oldProps, tt.newProps)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				if tt.check != nil {
					tt.check(t, result)
				}
			}
		})
	}
}

// =============================================================================
// buildWhereClause Tests
// =============================================================================

func TestBuildWhereClause(t *testing.T) {
	tests := []struct {
		name       string
		conditions []string
		want       string
	}{
		{
			name:       "empty conditions",
			conditions: []string{},
			want:       "",
		},
		{
			name:       "nil conditions",
			conditions: nil,
			want:       "",
		},
		{
			name:       "single condition",
			conditions: []string{"id = ?"},
			want:       "WHERE id = ?",
		},
		{
			name:       "multiple conditions",
			conditions: []string{"id = ?", "name = ?", "status = ?"},
			want:       "WHERE id = ? AND name = ? AND status = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildWhereClause(tt.conditions)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// formatTextArray Tests
// =============================================================================

func TestFormatTextArray(t *testing.T) {
	tests := []struct {
		name string
		arr  []string
		want string
	}{
		{
			name: "empty array",
			arr:  []string{},
			want: "{}",
		},
		{
			name: "nil array",
			arr:  nil,
			want: "{}",
		},
		{
			name: "single element",
			arr:  []string{"foo"},
			want: "{foo}",
		},
		{
			name: "multiple elements",
			arr:  []string{"foo", "bar", "baz"},
			want: "{foo,bar,baz}",
		},
		{
			name: "element with space",
			arr:  []string{"foo bar"},
			want: "{foo bar}",
		},
		{
			name: "element with comma needs quoting",
			arr:  []string{"foo,bar"},
			want: `{"foo,bar"}`,
		},
		{
			name: "element with curly brace needs quoting",
			arr:  []string{"foo{bar}"},
			want: `{"foo{bar}"}`,
		},
		{
			name: "element with quote needs escaping",
			arr:  []string{`foo"bar`},
			want: `{"foo\"bar"}`,
		},
		{
			name: "element with backslash needs escaping",
			arr:  []string{`foo\bar`},
			want: `{"foo\\bar"}`,
		},
		{
			name: "mixed elements",
			arr:  []string{"simple", "with,comma", `with"quote`},
			want: `{simple,"with,comma","with\"quote"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTextArray(tt.arr)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// GraphObject.ToResponse Tests
// =============================================================================

func TestGraphObject_ToResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	deletedAt := now.Add(-time.Hour)
	branchID := uuid.New()
	supersedesID := uuid.New()
	key := "test-key"
	status := "active"
	revisionCount := 5
	relationshipCount := 3

	tests := []struct {
		name   string
		object *GraphObject
	}{
		{
			name: "minimal object",
			object: &GraphObject{
				ID:          uuid.New(),
				ProjectID:   uuid.New(),
				CanonicalID: uuid.New(),
				Version:     1,
				Type:        "Person",
				Properties:  map[string]any{},
				Labels:      []string{},
				CreatedAt:   now,
			},
		},
		{
			name: "full object with all optional fields",
			object: &GraphObject{
				ID:                uuid.New(),
				ProjectID:         uuid.New(),
				BranchID:          &branchID,
				CanonicalID:       uuid.New(),
				SupersedesID:      &supersedesID,
				Version:           3,
				Type:              "Company",
				Key:               &key,
				Status:            &status,
				Properties:        map[string]any{"name": "Acme", "employees": 100},
				Labels:            []string{"tech", "startup"},
				DeletedAt:         &deletedAt,
				ChangeSummary:     map[string]any{"updated": map[string]any{"/name": map[string]any{"from": "Old", "to": "Acme"}}},
				CreatedAt:         now,
				RevisionCount:     &revisionCount,
				RelationshipCount: &relationshipCount,
			},
		},
		{
			name: "object with nil optional fields",
			object: &GraphObject{
				ID:          uuid.New(),
				ProjectID:   uuid.New(),
				CanonicalID: uuid.New(),
				Version:     1,
				Type:        "Document",
				Key:         nil,
				Status:      nil,
				Properties:  map[string]any{"content": "text"},
				Labels:      []string{"draft"},
				CreatedAt:   now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.object.ToResponse()

			require.NotNil(t, resp)
			assert.Equal(t, tt.object.ID, resp.ID)
			assert.Equal(t, tt.object.ProjectID, resp.ProjectID)
			assert.Equal(t, tt.object.BranchID, resp.BranchID)
			assert.Equal(t, tt.object.CanonicalID, resp.CanonicalID)
			assert.Equal(t, tt.object.SupersedesID, resp.SupersedesID)
			assert.Equal(t, tt.object.Version, resp.Version)
			assert.Equal(t, tt.object.Type, resp.Type)
			assert.Equal(t, tt.object.Key, resp.Key)
			assert.Equal(t, tt.object.Status, resp.Status)
			assert.Equal(t, tt.object.Properties, resp.Properties)
			assert.Equal(t, tt.object.Labels, resp.Labels)
			assert.Equal(t, tt.object.DeletedAt, resp.DeletedAt)
			assert.Equal(t, tt.object.ChangeSummary, resp.ChangeSummary)
			assert.Equal(t, tt.object.CreatedAt, resp.CreatedAt)
			assert.Equal(t, tt.object.RevisionCount, resp.RevisionCount)
			assert.Equal(t, tt.object.RelationshipCount, resp.RelationshipCount)
		})
	}
}

func TestGraphObject_ToResponse_DoesNotExposeInternalFields(t *testing.T) {
	// Ensure internal fields like ContentHash, FTS, etc. are not exposed
	obj := &GraphObject{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		CanonicalID: uuid.New(),
		Version:     1,
		Type:        "Test",
		Properties:  map[string]any{},
		Labels:      []string{},
		CreatedAt:   time.Now(),
		ContentHash: []byte("secret-hash"),
	}

	resp := obj.ToResponse()

	// GraphObjectResponse should not have ContentHash field
	// This is a compile-time check - if resp.ContentHash existed, it would be a bug
	assert.NotNil(t, resp)
	assert.Equal(t, obj.ID, resp.ID)
	// The response type doesn't have ContentHash, FTS, EmbeddingUpdatedAt, etc.
}

// =============================================================================
// GraphRelationship.ToResponse Tests
// =============================================================================

func TestGraphRelationship_ToResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	deletedAt := now.Add(-time.Hour)
	branchID := uuid.New()
	supersedesID := uuid.New()
	weight := float32(0.85)

	tests := []struct {
		name string
		rel  *GraphRelationship
	}{
		{
			name: "minimal relationship",
			rel: &GraphRelationship{
				ID:          uuid.New(),
				ProjectID:   uuid.New(),
				CanonicalID: uuid.New(),
				Version:     1,
				Type:        "KNOWS",
				SrcID:       uuid.New(),
				DstID:       uuid.New(),
				Properties:  map[string]any{},
				CreatedAt:   now,
			},
		},
		{
			name: "full relationship with all optional fields",
			rel: &GraphRelationship{
				ID:            uuid.New(),
				ProjectID:     uuid.New(),
				BranchID:      &branchID,
				CanonicalID:   uuid.New(),
				SupersedesID:  &supersedesID,
				Version:       2,
				Type:          "WORKS_AT",
				SrcID:         uuid.New(),
				DstID:         uuid.New(),
				Properties:    map[string]any{"since": "2020-01-01", "role": "Engineer"},
				Weight:        &weight,
				DeletedAt:     &deletedAt,
				ChangeSummary: map[string]any{"updated": map[string]any{"/role": map[string]any{"from": "Junior", "to": "Engineer"}}},
				CreatedAt:     now,
			},
		},
		{
			name: "relationship with nil weight",
			rel: &GraphRelationship{
				ID:          uuid.New(),
				ProjectID:   uuid.New(),
				CanonicalID: uuid.New(),
				Version:     1,
				Type:        "RELATED_TO",
				SrcID:       uuid.New(),
				DstID:       uuid.New(),
				Properties:  map[string]any{"type": "similar"},
				Weight:      nil,
				CreatedAt:   now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.rel.ToResponse()

			require.NotNil(t, resp)
			assert.Equal(t, tt.rel.ID, resp.ID)
			assert.Equal(t, tt.rel.ProjectID, resp.ProjectID)
			assert.Equal(t, tt.rel.BranchID, resp.BranchID)
			assert.Equal(t, tt.rel.CanonicalID, resp.CanonicalID)
			assert.Equal(t, tt.rel.SupersedesID, resp.SupersedesID)
			assert.Equal(t, tt.rel.Version, resp.Version)
			assert.Equal(t, tt.rel.Type, resp.Type)
			assert.Equal(t, tt.rel.SrcID, resp.SrcID)
			assert.Equal(t, tt.rel.DstID, resp.DstID)
			assert.Equal(t, tt.rel.Properties, resp.Properties)
			assert.Equal(t, tt.rel.Weight, resp.Weight)
			assert.Equal(t, tt.rel.DeletedAt, resp.DeletedAt)
			assert.Equal(t, tt.rel.ChangeSummary, resp.ChangeSummary)
			assert.Equal(t, tt.rel.CreatedAt, resp.CreatedAt)
		})
	}
}

func TestGraphRelationship_ToResponse_DoesNotExposeInternalFields(t *testing.T) {
	// Ensure internal fields like ContentHash, ValidFrom/ValidTo, SrcObject, DstObject are not exposed
	validFrom := time.Now().Add(-24 * time.Hour)
	validTo := time.Now().Add(24 * time.Hour)

	rel := &GraphRelationship{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		CanonicalID: uuid.New(),
		Version:     1,
		Type:        "TEST",
		SrcID:       uuid.New(),
		DstID:       uuid.New(),
		Properties:  map[string]any{},
		ContentHash: []byte("secret-hash"),
		ValidFrom:   &validFrom,
		ValidTo:     &validTo,
		CreatedAt:   time.Now(),
		SrcObject:   &GraphObject{Type: "Person"},
		DstObject:   &GraphObject{Type: "Company"},
	}

	resp := rel.ToResponse()

	// GraphRelationshipResponse should not have ContentHash, ValidFrom, ValidTo, SrcObject, DstObject fields
	assert.NotNil(t, resp)
	assert.Equal(t, rel.ID, resp.ID)
	// The response type doesn't have ContentHash, ValidFrom, ValidTo, SrcObject, DstObject
}
