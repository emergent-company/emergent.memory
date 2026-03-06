package graph

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGraphObjectResponse_MarshalJSON verifies that both old (id, canonical_id)
// and new (version_id, entity_id) field names appear in the JSON output.
func TestGraphObjectResponse_MarshalJSON(t *testing.T) {
	objID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	projID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	resp := GraphObjectResponse{
		ID:          objID,
		ProjectID:   projID,
		CanonicalID: canID,
		Version:     2,
		Type:        "Person",
		Properties:  map[string]any{"name": "Alice"},
		Labels:      []string{"test"},
		CreatedAt:   now,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	// Old names present
	assert.Contains(t, raw, "id", "old field 'id' should be present")
	assert.Contains(t, raw, "canonical_id", "old field 'canonical_id' should be present")

	// New names present
	assert.Contains(t, raw, "version_id", "new field 'version_id' should be present")
	assert.Contains(t, raw, "entity_id", "new field 'entity_id' should be present")

	// Values match
	var idStr, canStr, verStr, entStr string
	json.Unmarshal(raw["id"], &idStr)
	json.Unmarshal(raw["canonical_id"], &canStr)
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, objID.String(), idStr)
	assert.Equal(t, canID.String(), canStr)
	assert.Equal(t, objID.String(), verStr, "version_id should equal id")
	assert.Equal(t, canID.String(), entStr, "entity_id should equal canonical_id")
}

// TestGraphRelationshipResponse_MarshalJSON verifies dual field names for relationships.
func TestGraphRelationshipResponse_MarshalJSON(t *testing.T) {
	relID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	canID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	projID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	srcID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	dstID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	resp := GraphRelationshipResponse{
		ID:          relID,
		ProjectID:   projID,
		CanonicalID: canID,
		Version:     1,
		Type:        "WORKS_FOR",
		SrcID:       srcID,
		DstID:       dstID,
		Properties:  map[string]any{},
		CreatedAt:   now,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	// Old names
	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "canonical_id")
	// New names
	assert.Contains(t, raw, "version_id")
	assert.Contains(t, raw, "entity_id")

	var verStr, entStr string
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, relID.String(), verStr)
	assert.Equal(t, canID.String(), entStr)
}

// TestGraphRelationshipResponse_MarshalJSON_WithInverse verifies that nested
// inverse relationships also get the dual field names.
func TestGraphRelationshipResponse_MarshalJSON_WithInverse(t *testing.T) {
	relID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	invID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	canID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	invCanID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	projID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	srcID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	dstID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	resp := GraphRelationshipResponse{
		ID:          relID,
		ProjectID:   projID,
		CanonicalID: canID,
		Version:     1,
		Type:        "WORKS_FOR",
		SrcID:       srcID,
		DstID:       dstID,
		Properties:  map[string]any{},
		CreatedAt:   now,
		InverseRelationship: &GraphRelationshipResponse{
			ID:          invID,
			ProjectID:   projID,
			CanonicalID: invCanID,
			Version:     1,
			Type:        "EMPLOYS",
			SrcID:       dstID,
			DstID:       srcID,
			Properties:  map[string]any{},
			CreatedAt:   now,
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Parse the nested inverse_relationship
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "inverse_relationship")

	var invRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["inverse_relationship"], &invRaw))

	assert.Contains(t, invRaw, "version_id", "inverse should also have version_id")
	assert.Contains(t, invRaw, "entity_id", "inverse should also have entity_id")

	var invVerStr string
	json.Unmarshal(invRaw["version_id"], &invVerStr)
	assert.Equal(t, invID.String(), invVerStr)
}

// TestExpandNode_MarshalJSON verifies dual field names for expand nodes.
func TestExpandNode_MarshalJSON(t *testing.T) {
	nodeID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	node := ExpandNode{
		ID:          nodeID,
		CanonicalID: canID,
		Depth:       1,
		Type:        "Person",
		Labels:      []string{"test"},
	}

	data, err := json.Marshal(node)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "canonical_id")
	assert.Contains(t, raw, "version_id")
	assert.Contains(t, raw, "entity_id")

	// Value assertions
	var idStr, canStr, verStr, entStr string
	json.Unmarshal(raw["id"], &idStr)
	json.Unmarshal(raw["canonical_id"], &canStr)
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, nodeID.String(), idStr)
	assert.Equal(t, canID.String(), canStr)
	assert.Equal(t, nodeID.String(), verStr, "version_id should equal id")
	assert.Equal(t, canID.String(), entStr, "entity_id should equal canonical_id")
}

// TestTraverseNode_MarshalJSON verifies dual field names for traverse nodes.
func TestTraverseNode_MarshalJSON(t *testing.T) {
	nodeID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	node := TraverseNode{
		ID:          nodeID,
		CanonicalID: canID,
		Depth:       2,
		Type:        "Document",
		Labels:      []string{},
	}

	data, err := json.Marshal(node)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "canonical_id")
	assert.Contains(t, raw, "version_id")
	assert.Contains(t, raw, "entity_id")

	// Value assertions
	var idStr, canStr, verStr, entStr string
	json.Unmarshal(raw["id"], &idStr)
	json.Unmarshal(raw["canonical_id"], &canStr)
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, nodeID.String(), idStr)
	assert.Equal(t, canID.String(), canStr)
	assert.Equal(t, nodeID.String(), verStr, "version_id should equal id")
	assert.Equal(t, canID.String(), entStr, "entity_id should equal canonical_id")
}

// TestAnalyticsObjectItem_MarshalJSON verifies dual field names for analytics items.
func TestAnalyticsObjectItem_MarshalJSON(t *testing.T) {
	objID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	item := AnalyticsObjectItem{
		ID:          objID,
		CanonicalID: canID,
		Type:        "Person",
		Properties:  map[string]any{"name": "Test"},
		Labels:      []string{},
		CreatedAt:   time.Now(),
	}

	data, err := json.Marshal(item)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "canonical_id")
	assert.Contains(t, raw, "version_id")
	assert.Contains(t, raw, "entity_id")

	// Value assertions
	var idStr, canStr, verStr, entStr string
	json.Unmarshal(raw["id"], &idStr)
	json.Unmarshal(raw["canonical_id"], &canStr)
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, objID.String(), idStr)
	assert.Equal(t, canID.String(), canStr)
	assert.Equal(t, objID.String(), verStr, "version_id should equal id")
	assert.Equal(t, canID.String(), entStr, "entity_id should equal canonical_id")
}

// TestSimilarObjectResult_MarshalJSON verifies dual field names for similar object results.
func TestSimilarObjectResult_MarshalJSON(t *testing.T) {
	objID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	result := SimilarObjectResult{
		ID:          objID,
		CanonicalID: &canID,
		Type:        "Person",
		Status:      "active",
		Distance:    0.5,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "canonical_id")
	assert.Contains(t, raw, "version_id")
	assert.Contains(t, raw, "entity_id")

	// Value assertions
	var idStr, canStr, verStr, entStr string
	json.Unmarshal(raw["id"], &idStr)
	json.Unmarshal(raw["canonical_id"], &canStr)
	json.Unmarshal(raw["version_id"], &verStr)
	json.Unmarshal(raw["entity_id"], &entStr)

	assert.Equal(t, objID.String(), idStr)
	assert.Equal(t, canID.String(), canStr)
	assert.Equal(t, objID.String(), verStr, "version_id should equal id")
	assert.Equal(t, canID.String(), entStr, "entity_id should equal canonical_id")
}
