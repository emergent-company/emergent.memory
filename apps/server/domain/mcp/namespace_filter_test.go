package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/search"
)

// makeGraphResult is a helper that builds a graph-type UnifiedSearchResultItem.
func makeGraphResult(objectType string) search.UnifiedSearchResultItem {
	return search.UnifiedSearchResultItem{
		Type:       search.ItemTypeGraph,
		ObjectType: objectType,
		Score:      1.0,
	}
}

// =============================================================================
// mapUnifiedToSearchResponse — allowedTypes filtering
// =============================================================================

func TestMapUnifiedToSearchResponse_NoFilters(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeGraphResult("Task"),
		},
	}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, nil)
	require.NotNil(t, out)
	assert.Len(t, out.Data, 2)
}

func TestMapUnifiedToSearchResponse_AllowedTypesFilter(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeGraphResult("Task"),
			makeGraphResult("MCPSecret"),
		},
	}
	allowed := map[string]bool{"Note": true, "Task": true}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, allowed, nil)
	require.Len(t, out.Data, 2)
	for _, item := range out.Data {
		assert.NotEqual(t, "MCPSecret", item.Object.Type)
	}
}

func TestMapUnifiedToSearchResponse_BlockedTypesFilter(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeGraphResult("MCPSecret"),
			makeGraphResult("MCPProxyConfig"),
		},
	}
	blocked := map[string]bool{"MCPSecret": true, "MCPProxyConfig": true}
	// nil allowedTypes = no allowlist restriction
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, blocked)
	require.Len(t, out.Data, 1)
	assert.Equal(t, "Note", out.Data[0].Object.Type)
}

func TestMapUnifiedToSearchResponse_BlockedTypesOverrideAllowedNil(t *testing.T) {
	// Simulates the gap scenario: allowedTypes is nil (no non-system types registered)
	// but blockedTypes is set. System types must still be excluded.
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("MCPSecret"),
			makeGraphResult("DianeNodeConfig"),
		},
	}
	blocked := map[string]bool{"MCPSecret": true, "DianeNodeConfig": true}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, blocked)
	assert.Empty(t, out.Data)
}

func TestMapUnifiedToSearchResponse_AllowedAndBlockedCombined(t *testing.T) {
	// allowedTypes = {Note, MCPSecret} (shouldn't happen in practice, but tests precedence)
	// blockedTypes = {MCPSecret}
	// MCPSecret should be blocked even if in allowed list
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeGraphResult("MCPSecret"),
		},
	}
	allowed := map[string]bool{"Note": true, "MCPSecret": true}
	blocked := map[string]bool{"MCPSecret": true}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, allowed, blocked)
	require.Len(t, out.Data, 1)
	assert.Equal(t, "Note", out.Data[0].Object.Type)
}

func TestMapUnifiedToSearchResponse_TypesListFilter(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeGraphResult("Task"),
			makeGraphResult("Contact"),
		},
	}
	// types list restricts to only Note and Task
	out := svc.mapUnifiedToSearchResponse(res, []string{"Note", "Task"}, nil, nil, nil)
	require.Len(t, out.Data, 2)
}

func TestMapUnifiedToSearchResponse_EmptyResults(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{Results: nil}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, nil)
	assert.NotNil(t, out)
	assert.Empty(t, out.Data)
}

// =============================================================================
// mapUnifiedToSearchResponse — ItemTypeText (chunk) filtering
// =============================================================================

// makeTextResult is a helper that builds a text-chunk UnifiedSearchResultItem
// mirroring the shape produced by the chunk leg (no ObjectType, snippet/source/
// document_id carried).
func makeTextResult(id string) search.UnifiedSearchResultItem {
	docID := "fed94373-0000-0000-0000-000000000000"
	src := "fed94373-0000-0000-0000-000000000000"
	return search.UnifiedSearchResultItem{
		Type:       search.ItemTypeText,
		ID:         id,
		Snippet:    "a snippet",
		Source:     &src,
		DocumentID: &docID,
		Score:      0.9,
	}
}

func TestMapUnifiedToSearchResponse_TypesDropChunks(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("LegalParagraph"),
			makeTextResult("00000000-0000-0000-0000-000000000000"),
		},
	}
	// An explicit types list that omits the text_chunk pseudo-type suppresses
	// chunks; the graph entity that IS in the list is kept.
	out := svc.mapUnifiedToSearchResponse(res, []string{"LegalParagraph"}, nil, nil, nil)
	require.Len(t, out.Data, 1)
	assert.Equal(t, "LegalParagraph", out.Data[0].Object.Type)
}

func TestMapUnifiedToSearchResponse_AllowedTypesKeepChunks(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeTextResult("00000000-0000-0000-0000-000000000000"),
		},
	}
	// The default-namespace allowlist is non-nil but never contains "text_chunk";
	// chunks carry no namespace and are not schema types, so the allowlist must
	// not gate them. include_chunks=true must still return the chunk.
	allowed := map[string]bool{"Note": true}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, allowed, nil)
	require.Len(t, out.Data, 2)
}

func TestMapUnifiedToSearchResponse_BlockedTypesKeepChunks(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeTextResult("00000000-0000-0000-0000-000000000000"),
		},
	}
	// Even a blocklist containing "text_chunk" must not gate chunks: the
	// blocklist only applies to schema types, and chunks are governed solely by
	// include_chunks.
	blocked := map[string]bool{"text_chunk": true}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, blocked)
	require.Len(t, out.Data, 2)
}

func TestMapUnifiedToSearchResponse_NoFilterKeepsChunks(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			makeGraphResult("Note"),
			makeTextResult("00000000-0000-0000-0000-000000000000"),
		},
	}
	// No filters: chunks are kept, subject to the caller having opted in via
	// include_chunks at the request level.
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, nil)
	require.Len(t, out.Data, 2)
}

func TestMapUnifiedToSearchResponse_LabelsFilterKeepsChunks(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			{Type: search.ItemTypeGraph, ObjectType: "Note", Score: 1.0, Labels: []string{"keep"}},
			makeTextResult("00000000-0000-0000-0000-000000000000"),
		},
	}
	// Chunks carry no labels, so the entity label filter must not drop them;
	// inclusion is governed by include_chunks alone.
	out := svc.mapUnifiedToSearchResponse(res, nil, []string{"keep"}, nil, nil)
	require.Len(t, out.Data, 2)
}

// =============================================================================
// mapUnifiedToSearchResponse — labels threading + labels filter
// =============================================================================

func TestMapUnifiedToSearchResponse_LabelsThreaded(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			{Type: search.ItemTypeGraph, ObjectType: "Note", Score: 1.0, Labels: []string{"probe-label-xyz"}},
		},
	}
	out := svc.mapUnifiedToSearchResponse(res, nil, nil, nil, nil)
	require.Len(t, out.Data, 1)
	require.NotNil(t, out.Data[0].Object)
	assert.Equal(t, []string{"probe-label-xyz"}, out.Data[0].Object.Labels)
}

func TestMapUnifiedToSearchResponse_LabelsFilterMatches(t *testing.T) {
	svc := &Service{}
	res := &search.UnifiedSearchResponse{
		Results: []search.UnifiedSearchResultItem{
			{Type: search.ItemTypeGraph, ObjectType: "Note", Score: 1.0, Labels: []string{"probe-label-xyz"}},
			{Type: search.ItemTypeGraph, ObjectType: "Task", Score: 1.0, Labels: []string{"other-label"}},
		},
	}
	out := svc.mapUnifiedToSearchResponse(res, nil, []string{"probe-label-xyz"}, nil, nil)
	require.Len(t, out.Data, 1)
	assert.Equal(t, "Note", out.Data[0].Object.Type)
}

// =============================================================================
// getSystemNamespaceTypes — namespaceFilter logic (no DB)
// =============================================================================

func TestGetSystemNamespaceTypes_ReturnsNilForNonDefaultFilter(t *testing.T) {
	svc := &Service{}
	// When namespaceFilter is "all", no blocklist should be returned
	result, err := svc.getSystemNamespaceTypes(context.Background(), "any-project", "all")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetSystemNamespaceTypes_ReturnsNilForSpecificNamespace(t *testing.T) {
	svc := &Service{}
	// When a specific namespace is requested, no blocklist needed
	result, err := svc.getSystemNamespaceTypes(context.Background(), "any-project", "system")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetSystemNamespaceTypes_InvalidProjectIDReturnsError(t *testing.T) {
	svc := &Service{}
	// namespaceFilter="" triggers the DB path; invalid project UUID should fail
	_, err := svc.getSystemNamespaceTypes(context.Background(), "not-a-uuid", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid project_id")
}
