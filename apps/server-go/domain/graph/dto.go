package graph

import (
	"time"

	"github.com/google/uuid"
)

// CreateGraphObjectRequest is the request body for creating a graph object.
type CreateGraphObjectRequest struct {
	Type       string         `json:"type" validate:"required,max=64"`
	Key        *string        `json:"key,omitempty" validate:"omitempty,max=128"`
	Status     *string        `json:"status,omitempty" validate:"omitempty,max=64"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty" validate:"omitempty,max=32,dive,max=64"`
	BranchID   *uuid.UUID     `json:"branch_id,omitempty"`
}

// PatchGraphObjectRequest is the request body for patching a graph object.
// Patching creates a new version.
type PatchGraphObjectRequest struct {
	Properties    map[string]any `json:"properties,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	ReplaceLabels bool           `json:"replaceLabels,omitempty"`
	Status        *string        `json:"status,omitempty"`
}

// GraphObjectResponse is the API response for a graph object.
type GraphObjectResponse struct {
	ID            uuid.UUID      `json:"id"`
	OrgID         *string        `json:"org_id,omitempty"`
	ProjectID     uuid.UUID      `json:"project_id"`
	BranchID      *uuid.UUID     `json:"branch_id,omitempty"`
	CanonicalID   uuid.UUID      `json:"canonical_id"`
	SupersedesID  *uuid.UUID     `json:"supersedes_id,omitempty"`
	Version       int            `json:"version"`
	Type          string         `json:"type"`
	Key           *string        `json:"key,omitempty"`
	Status        *string        `json:"status,omitempty"`
	Properties    map[string]any `json:"properties"`
	Labels        []string       `json:"labels"`
	SchemaVersion *string        `json:"schema_version,omitempty"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty"`
	ChangeSummary map[string]any `json:"change_summary,omitempty"`
	ContentHash   *string        `json:"content_hash,omitempty"`
	// External source fields for data sync
	ExternalSource    *string    `json:"external_source,omitempty"`
	ExternalID        *string    `json:"external_id,omitempty"`
	ExternalURL       *string    `json:"external_url,omitempty"`
	ExternalParentID  *string    `json:"external_parent_id,omitempty"`
	SyncedAt          *time.Time `json:"synced_at,omitempty"`
	ExternalUpdatedAt *time.Time `json:"external_updated_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	RevisionCount     *int       `json:"revision_count,omitempty"`
	RelationshipCount *int       `json:"relationship_count,omitempty"`
}

// ToResponse converts a GraphObject entity to API response.
func (o *GraphObject) ToResponse() *GraphObjectResponse {
	// Convert content hash to string if present
	var contentHash *string
	if len(o.ContentHash) > 0 {
		h := string(o.ContentHash)
		contentHash = &h
	}

	return &GraphObjectResponse{
		ID:                o.ID,
		OrgID:             o.OrgID,
		ProjectID:         o.ProjectID,
		BranchID:          o.BranchID,
		CanonicalID:       o.CanonicalID,
		SupersedesID:      o.SupersedesID,
		Version:           o.Version,
		Type:              o.Type,
		Key:               o.Key,
		Status:            o.Status,
		Properties:        o.Properties,
		Labels:            o.Labels,
		SchemaVersion:     o.SchemaVersion,
		DeletedAt:         o.DeletedAt,
		ChangeSummary:     o.ChangeSummary,
		ContentHash:       contentHash,
		ExternalSource:    o.ExternalSource,
		ExternalID:        o.ExternalID,
		ExternalURL:       o.ExternalURL,
		ExternalParentID:  o.ExternalParentID,
		SyncedAt:          o.SyncedAt,
		ExternalUpdatedAt: o.ExternalUpdatedAt,
		CreatedAt:         o.CreatedAt,
		RevisionCount:     o.RevisionCount,
		RelationshipCount: o.RelationshipCount,
	}
}

// AnalyticsObjectItem represents an object in analytics responses.
type AnalyticsObjectItem struct {
	ID              uuid.UUID      `json:"id"`
	CanonicalID     uuid.UUID      `json:"canonical_id"`
	Type            string         `json:"type"`
	Key             *string        `json:"key,omitempty"`
	Properties      map[string]any `json:"properties"`
	Labels          []string       `json:"labels"`
	LastAccessedAt  *time.Time     `json:"last_accessed_at,omitempty"`
	AccessCount     *int64         `json:"access_count,omitempty"`
	DaysSinceAccess *int           `json:"days_since_access,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// MostAccessedResponse is the response for most-accessed analytics.
type MostAccessedResponse struct {
	Items []AnalyticsObjectItem  `json:"items"`
	Total int                    `json:"total"`
	Meta  map[string]interface{} `json:"meta"`
}

// UnusedObjectsResponse is the response for unused objects analytics.
type UnusedObjectsResponse struct {
	Items []AnalyticsObjectItem  `json:"items"`
	Total int                    `json:"total"`
	Meta  map[string]interface{} `json:"meta"`
}

// SearchGraphObjectsRequest contains search/filter parameters.
type SearchGraphObjectsRequest struct {
	Type            *string    `query:"type"`   // NestJS uses single type, not array
	Types           []string   `query:"types"`  // Go also supports array for flexibility
	Label           *string    `query:"label"`  // NestJS uses single label, not array
	Labels          []string   `query:"labels"` // Go also supports array for flexibility
	Status          *string    `query:"status"`
	Key             *string    `query:"key"`
	BranchID        *uuid.UUID `query:"branch_id"`
	IncludeDeleted  bool       `query:"include_deleted"`
	Limit           int        `query:"limit"`
	Cursor          *string    `query:"cursor"`
	Order           *string    `query:"order"`             // "asc" or "desc"
	RelatedToID     *string    `query:"related_to_id"`     // Filter by related object
	IDs             []string   `query:"ids"`               // Comma-separated list
	ExtractionJobID *string    `query:"extraction_job_id"` // Filter by extraction job
}

// SearchGraphObjectsResponse is the paginated search response.
// Uses NestJS-compatible field names: items, next_cursor, total
type SearchGraphObjectsResponse struct {
	Items      []*GraphObjectResponse `json:"items"`
	NextCursor *string                `json:"next_cursor,omitempty"`
	Total      int                    `json:"total"`
}

// CreateGraphRelationshipRequest is the request body for creating a relationship.
type CreateGraphRelationshipRequest struct {
	Type       string         `json:"type" validate:"required,max=64"`
	SrcID      uuid.UUID      `json:"src_id" validate:"required"`
	DstID      uuid.UUID      `json:"dst_id" validate:"required"`
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`
	BranchID   *uuid.UUID     `json:"branch_id,omitempty"`
}

// PatchGraphRelationshipRequest is the request body for patching a relationship.
type PatchGraphRelationshipRequest struct {
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`
}

// GraphRelationshipResponse is the API response for a relationship.
type GraphRelationshipResponse struct {
	ID            uuid.UUID      `json:"id"`
	ProjectID     uuid.UUID      `json:"project_id"`
	BranchID      *uuid.UUID     `json:"branch_id,omitempty"`
	CanonicalID   uuid.UUID      `json:"canonical_id"`
	SupersedesID  *uuid.UUID     `json:"supersedes_id,omitempty"`
	Version       int            `json:"version"`
	Type          string         `json:"type"`
	SrcID         uuid.UUID      `json:"src_id"`
	DstID         uuid.UUID      `json:"dst_id"`
	Properties    map[string]any `json:"properties"`
	Weight        *float32       `json:"weight,omitempty"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty"`
	ChangeSummary map[string]any `json:"change_summary,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ToResponse converts a GraphRelationship entity to API response.
func (r *GraphRelationship) ToResponse() *GraphRelationshipResponse {
	return &GraphRelationshipResponse{
		ID:            r.ID,
		ProjectID:     r.ProjectID,
		BranchID:      r.BranchID,
		CanonicalID:   r.CanonicalID,
		SupersedesID:  r.SupersedesID,
		Version:       r.Version,
		Type:          r.Type,
		SrcID:         r.SrcID,
		DstID:         r.DstID,
		Properties:    r.Properties,
		Weight:        r.Weight,
		DeletedAt:     r.DeletedAt,
		ChangeSummary: r.ChangeSummary,
		CreatedAt:     r.CreatedAt,
	}
}

// GetObjectEdgesResponse is the response for listing edges of an object.
type GetObjectEdgesResponse struct {
	Incoming []*GraphRelationshipResponse `json:"incoming"`
	Outgoing []*GraphRelationshipResponse `json:"outgoing"`
}

// ObjectHistoryResponse is the response for version history.
type ObjectHistoryResponse struct {
	Versions []*GraphObjectResponse `json:"versions"`
}

// BulkUpdateStatusRequest is the request for bulk status updates.
type BulkUpdateStatusRequest struct {
	IDs    []string `json:"ids" validate:"required,min=1,max=100"`
	Status string   `json:"status" validate:"required,max=64"`
}

// BulkUpdateStatusResponse is the response for bulk status updates.
type BulkUpdateStatusResponse struct {
	Success int                      `json:"success"`
	Failed  int                      `json:"failed"`
	Results []BulkUpdateStatusResult `json:"results"`
}

// BulkUpdateStatusResult is the result for a single object in bulk update.
type BulkUpdateStatusResult struct {
	ID      string  `json:"id"`
	Success bool    `json:"success"`
	Error   *string `json:"error,omitempty"`
}

// =============================================================================
// Search DTOs
// =============================================================================

// FTSSearchRequest is the request for full-text search.
type FTSSearchRequest struct {
	Query          string     `query:"q"`
	Types          []string   `query:"types"`
	Labels         []string   `query:"labels"`
	Status         *string    `query:"status"`
	BranchID       *uuid.UUID `query:"branch_id"`
	IncludeDeleted bool       `query:"include_deleted"`
	Limit          int        `query:"limit"`
}

// VectorSearchRequest is the request for vector similarity search.
type VectorSearchRequest struct {
	Vector         []float32  `json:"vector" validate:"required"`
	Types          []string   `json:"types,omitempty"`
	Labels         []string   `json:"labels,omitempty"`
	Status         *string    `json:"status,omitempty"`
	BranchID       *uuid.UUID `json:"branchId,omitempty"`
	IncludeDeleted bool       `json:"includeDeleted,omitempty"`
	MaxDistance    *float32   `json:"maxDistance,omitempty"`
	Limit          int        `json:"limit,omitempty"`
}

// HybridSearchRequest is the request for hybrid (FTS + vector) search.
type HybridSearchRequest struct {
	Query          string     `json:"query" validate:"required"`
	Vector         []float32  `json:"vector,omitempty"`
	Types          []string   `json:"types,omitempty"`
	Labels         []string   `json:"labels,omitempty"`
	Status         *string    `json:"status,omitempty"`
	BranchID       *uuid.UUID `json:"branchId,omitempty"`
	IncludeDeleted bool       `json:"includeDeleted,omitempty"`
	LexicalWeight  *float32   `json:"lexicalWeight,omitempty"`
	VectorWeight   *float32   `json:"vectorWeight,omitempty"`
	Limit          int        `json:"limit,omitempty"`
	IncludeDebug   bool       `json:"includeDebug,omitempty"` // Can also use ?debug=true query param
}

// SearchResultItem represents a single search result with scores.
type SearchResultItem struct {
	Object       *GraphObjectResponse `json:"object"`
	Score        float32              `json:"score"`
	LexicalScore *float32             `json:"lexicalScore,omitempty"`
	VectorScore  *float32             `json:"vectorScore,omitempty"`
	VectorDist   *float32             `json:"vectorDist,omitempty"`
}

// SearchResponse is the response for all search types.
type SearchResponse struct {
	Data    []*SearchResultItem `json:"data"`
	Total   int                 `json:"total"`
	HasMore bool                `json:"hasMore"`
	Meta    *SearchResponseMeta `json:"meta,omitempty"`
}

// SearchResponseMeta contains metadata about the search request.
type SearchResponseMeta struct {
	ElapsedMs    float64             `json:"elapsed_ms"`
	Timing       *SearchTimingDebug  `json:"timing,omitempty"`        // Only when debug=true
	ChannelStats *SearchChannelStats `json:"channel_stats,omitempty"` // Only when debug=true
}

// SearchTimingDebug contains timing breakdown for debug mode.
type SearchTimingDebug struct {
	EmbeddingMs float64 `json:"embedding_ms"` // Time to generate query embedding (if vector needed)
	LexicalMs   float64 `json:"lexical_ms"`   // Time for FTS search
	VectorMs    float64 `json:"vector_ms"`    // Time for vector search
	FusionMs    float64 `json:"fusion_ms"`    // Time for result fusion
	TotalMs     float64 `json:"total_ms"`     // Total elapsed time
}

// SearchChannelStats contains per-channel statistics for debug mode.
type SearchChannelStats struct {
	Lexical *ChannelStat `json:"lexical,omitempty"`
	Vector  *ChannelStat `json:"vector,omitempty"`
}

// ChannelStat represents statistics for a single search channel.
type ChannelStat struct {
	Mean  float64 `json:"mean"`
	Std   float64 `json:"std"`
	Count int     `json:"count"`
}

// =============================================================================
// Search with Neighbors DTOs
// =============================================================================

// SearchWithNeighborsRequest is the request for search with neighbors endpoint.
type SearchWithNeighborsRequest struct {
	Query            string     `json:"query" validate:"required"`
	Limit            int        `json:"limit,omitempty"`
	IncludeNeighbors bool       `json:"includeNeighbors,omitempty"`
	MaxNeighbors     int        `json:"maxNeighbors,omitempty"`
	MaxDistance      *float32   `json:"maxDistance,omitempty"`
	BranchID         *uuid.UUID `json:"branchId,omitempty"`
	Types            []string   `json:"types,omitempty"`
	Labels           []string   `json:"labels,omitempty"`
}

// SearchWithNeighborsResponse is the response for search with neighbors endpoint.
type SearchWithNeighborsResponse struct {
	PrimaryResults []*GraphObjectResponse            `json:"primaryResults"`
	Neighbors      map[string][]*GraphObjectResponse `json:"neighbors,omitempty"`
}

// =============================================================================
// Similar Objects DTOs
// =============================================================================

// SimilarObjectsRequest contains query parameters for similar objects search.
type SimilarObjectsRequest struct {
	Limit       int        `query:"limit"`
	MaxDistance *float32   `query:"maxDistance"`
	MinScore    *float32   `query:"minScore"` // Legacy: alias for maxDistance
	Type        *string    `query:"type"`
	BranchID    *uuid.UUID `query:"branchId"`
	KeyPrefix   *string    `query:"keyPrefix"`
	LabelsAll   []string   `query:"labelsAll"`
	LabelsAny   []string   `query:"labelsAny"`
}

// SimilarObjectResult represents a single similar object with distance.
type SimilarObjectResult struct {
	ID          uuid.UUID  `json:"id"`
	CanonicalID *uuid.UUID `json:"canonical_id,omitempty"`
	Version     *int       `json:"version,omitempty"`
	Distance    float32    `json:"distance"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	BranchID    *uuid.UUID `json:"branch_id,omitempty"`
}

// =============================================================================
// Graph Expand DTOs
// =============================================================================

// GraphExpandRequest is the request for graph expand endpoint.
type GraphExpandRequest struct {
	RootIDs                       []uuid.UUID            `json:"root_ids" validate:"required,max=50"`
	Direction                     string                 `json:"direction,omitempty"` // "out", "in", "both" (default: "both")
	MaxDepth                      int                    `json:"max_depth,omitempty"` // default: 2, max: 8
	MaxNodes                      int                    `json:"max_nodes,omitempty"` // default: 400, max: 5000
	MaxEdges                      int                    `json:"max_edges,omitempty"` // default: 800, max: 15000
	RelationshipTypes             []string               `json:"relationship_types,omitempty"`
	ObjectTypes                   []string               `json:"object_types,omitempty"`
	Labels                        []string               `json:"labels,omitempty"`
	Projection                    *GraphExpandProjection `json:"projection,omitempty"`
	IncludeRelationshipProperties bool                   `json:"include_relationship_properties,omitempty"`
}

// GraphExpandProjection specifies property projection options.
type GraphExpandProjection struct {
	IncludeObjectProperties []string `json:"include_object_properties,omitempty"`
	ExcludeObjectProperties []string `json:"exclude_object_properties,omitempty"`
}

// GraphExpandResponse is the response for graph expand endpoint.
type GraphExpandResponse struct {
	Roots           []uuid.UUID      `json:"roots"`
	Nodes           []*ExpandNode    `json:"nodes"`
	Edges           []*ExpandEdge    `json:"edges"`
	Truncated       bool             `json:"truncated"`
	MaxDepthReached int              `json:"max_depth_reached"`
	TotalNodes      int              `json:"total_nodes"`
	Meta            *GraphExpandMeta `json:"meta"`
}

// ExpandNode represents a node in the expand response.
type ExpandNode struct {
	ID         uuid.UUID      `json:"id"`
	Depth      int            `json:"depth"`
	Type       string         `json:"type"`
	Key        *string        `json:"key,omitempty"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties,omitempty"`
}

// ExpandEdge represents an edge in the expand response.
type ExpandEdge struct {
	ID         uuid.UUID      `json:"id"`
	Type       string         `json:"type"`
	SrcID      uuid.UUID      `json:"src_id"`
	DstID      uuid.UUID      `json:"dst_id"`
	Properties map[string]any `json:"properties,omitempty"`
}

// GraphExpandMeta contains metadata about the expand request.
type GraphExpandMeta struct {
	Requested       GraphExpandRequested `json:"requested"`
	NodeCount       int                  `json:"node_count"`
	EdgeCount       int                  `json:"edge_count"`
	Truncated       bool                 `json:"truncated"`
	MaxDepthReached int                  `json:"max_depth_reached"`
	ElapsedMs       float64              `json:"elapsed_ms"`
	Filters         *GraphExpandFilters  `json:"filters,omitempty"`
}

// GraphExpandRequested contains the original request parameters.
type GraphExpandRequested struct {
	MaxDepth  int    `json:"max_depth"`
	MaxNodes  int    `json:"max_nodes"`
	MaxEdges  int    `json:"max_edges"`
	Direction string `json:"direction"`
}

// GraphExpandFilters contains the filters applied to the expand.
type GraphExpandFilters struct {
	RelationshipTypes             []string               `json:"relationship_types,omitempty"`
	ObjectTypes                   []string               `json:"object_types,omitempty"`
	Labels                        []string               `json:"labels,omitempty"`
	Projection                    *GraphExpandProjection `json:"projection,omitempty"`
	IncludeRelationshipProperties bool                   `json:"include_relationship_properties,omitempty"`
}

// =============================================================================
// Graph Traverse DTOs
// =============================================================================

// TraverseGraphRequest is the request for graph traverse endpoint.
type TraverseGraphRequest struct {
	RootIDs           []uuid.UUID     `json:"root_ids" validate:"required,max=50"`
	Direction         string          `json:"direction,omitempty"` // "out", "in", "both" (default: "both")
	MaxDepth          int             `json:"max_depth,omitempty"` // default: 2, max: 8
	MaxNodes          int             `json:"max_nodes,omitempty"` // default: 200, max: 5000
	MaxEdges          int             `json:"max_edges,omitempty"` // default: 400, max: 10000
	RelationshipTypes []string        `json:"relationship_types,omitempty"`
	ObjectTypes       []string        `json:"object_types,omitempty"`
	Labels            []string        `json:"labels,omitempty"`
	Limit             int             `json:"limit,omitempty"`          // page size, default: 50, max: 200
	PageDirection     string          `json:"page_direction,omitempty"` // "forward" or "backward"
	Cursor            *string         `json:"cursor,omitempty"`
	EdgePhases        []EdgePhase     `json:"edgePhases,omitempty"`
	NodeFilter        *Predicate      `json:"nodeFilter,omitempty"`
	EdgeFilter        *Predicate      `json:"edgeFilter,omitempty"`
	ReturnPaths       bool            `json:"returnPaths,omitempty"`
	MaxPathsPerNode   int             `json:"maxPathsPerNode,omitempty"`
	TemporalFilter    *TemporalFilter `json:"temporalFilter,omitempty"`
	FieldStrategy     string          `json:"fieldStrategy,omitempty"` // "full", "compact", "minimal"
}

// EdgePhase defines a phase in multi-phase traversal.
type EdgePhase struct {
	RelationshipTypes []string `json:"relationshipTypes,omitempty"`
	Direction         string   `json:"direction" validate:"required,oneof=out in both"`
	MaxDepth          int      `json:"maxDepth" validate:"required,min=1,max=8"`
	ObjectTypes       []string `json:"objectTypes,omitempty"`
	Labels            []string `json:"labels,omitempty"`
}

// Predicate defines a filter condition for nodes or edges.
type Predicate struct {
	Path     string `json:"path" validate:"required"` // JSON Pointer path
	Operator string `json:"operator" validate:"required,oneof=equals notEquals contains greaterThan lessThan greaterThanOrEqual lessThanOrEqual in notIn matches exists notExists"`
	Value    any    `json:"value,omitempty"`
}

// TemporalFilter defines point-in-time query parameters.
type TemporalFilter struct {
	AsOf  string `json:"asOf" validate:"required"` // ISO 8601 timestamp
	Field string `json:"field,omitempty"`          // "valid_from", "created_at", "updated_at" (default: "valid_from")
}

// TraverseGraphResponse is the response for graph traverse endpoint.
type TraverseGraphResponse struct {
	Roots               []uuid.UUID     `json:"roots"`
	Nodes               []*TraverseNode `json:"nodes"`
	Edges               []*TraverseEdge `json:"edges"`
	Truncated           bool            `json:"truncated"`
	MaxDepthReached     int             `json:"max_depth_reached"`
	TotalNodes          int             `json:"total_nodes"`
	HasNextPage         bool            `json:"has_next_page"`
	HasPreviousPage     bool            `json:"has_previous_page"`
	NextCursor          *string         `json:"next_cursor,omitempty"`
	PreviousCursor      *string         `json:"previous_cursor,omitempty"`
	ApproxPositionStart int             `json:"approx_position_start"`
	ApproxPositionEnd   int             `json:"approx_position_end"`
	PageDirection       string          `json:"page_direction"`
	QueryTimeMs         *float64        `json:"query_time_ms,omitempty"`
	ResultCount         *int            `json:"result_count,omitempty"`
}

// TraverseNode represents a node in the traverse response.
type TraverseNode struct {
	ID         uuid.UUID  `json:"id"`
	Depth      int        `json:"depth"`
	Type       string     `json:"type"`
	Key        *string    `json:"key,omitempty"`
	Labels     []string   `json:"labels"`
	PhaseIndex *int       `json:"phaseIndex,omitempty"`
	Paths      [][]string `json:"paths,omitempty"`
}

// TraverseEdge represents an edge in the traverse response.
type TraverseEdge struct {
	ID    uuid.UUID `json:"id"`
	Type  string    `json:"type"`
	SrcID uuid.UUID `json:"src_id"`
	DstID uuid.UUID `json:"dst_id"`
}

// =============================================================================
// Branch Merge DTOs
// =============================================================================

// BranchMergeRequest is the request for branch merge endpoint.
type BranchMergeRequest struct {
	SourceBranchID uuid.UUID `json:"sourceBranchId" validate:"required"`
	Execute        bool      `json:"execute,omitempty"`
	Limit          *int      `json:"limit,omitempty"` // Override enumeration limit (testing)
}

// BranchMergeResponse is the response for branch merge endpoint.
type BranchMergeResponse struct {
	TargetBranchID   uuid.UUID                   `json:"targetBranchId"`
	SourceBranchID   uuid.UUID                   `json:"sourceBranchId"`
	DryRun           bool                        `json:"dryRun"`
	TotalObjects     int                         `json:"total_objects"`
	UnchangedCount   int                         `json:"unchanged_count"`
	AddedCount       int                         `json:"added_count"`
	FastForwardCount int                         `json:"fast_forward_count"`
	ConflictCount    int                         `json:"conflict_count"`
	Objects          []*BranchMergeObjectSummary `json:"objects"`
	Truncated        bool                        `json:"truncated,omitempty"`
	HardLimit        *int                        `json:"hard_limit,omitempty"`
	Applied          bool                        `json:"applied,omitempty"`
	AppliedObjects   *int                        `json:"applied_objects,omitempty"`
	// Relationship merge info
	RelationshipsTotal            *int                              `json:"relationships_total,omitempty"`
	RelationshipsUnchangedCount   *int                              `json:"relationships_unchanged_count,omitempty"`
	RelationshipsAddedCount       *int                              `json:"relationships_added_count,omitempty"`
	RelationshipsFastForwardCount *int                              `json:"relationships_fast_forward_count,omitempty"`
	RelationshipsConflictCount    *int                              `json:"relationships_conflict_count,omitempty"`
	Relationships                 []*BranchMergeRelationshipSummary `json:"relationships,omitempty"`
}

// BranchMergeObjectSummary represents merge status for a single object.
type BranchMergeObjectSummary struct {
	CanonicalID  uuid.UUID  `json:"canonical_id"`
	Status       string     `json:"status"` // "unchanged", "added", "fast_forward", "conflict"
	SourceHeadID *uuid.UUID `json:"source_head_id,omitempty"`
	TargetHeadID *uuid.UUID `json:"target_head_id,omitempty"`
	SourcePaths  []string   `json:"source_paths,omitempty"`
	TargetPaths  []string   `json:"target_paths,omitempty"`
	Conflicts    []string   `json:"conflicts,omitempty"`
}

// BranchMergeRelationshipSummary represents merge status for a single relationship.
type BranchMergeRelationshipSummary struct {
	CanonicalID  uuid.UUID  `json:"canonical_id"`
	Status       string     `json:"status"` // "unchanged", "added", "fast_forward", "conflict"
	SourceHeadID *uuid.UUID `json:"source_head_id,omitempty"`
	TargetHeadID *uuid.UUID `json:"target_head_id,omitempty"`
	SourceSrcID  *uuid.UUID `json:"source_src_id,omitempty"`
	SourceDstID  *uuid.UUID `json:"source_dst_id,omitempty"`
	TargetSrcID  *uuid.UUID `json:"target_src_id,omitempty"`
	TargetDstID  *uuid.UUID `json:"target_dst_id,omitempty"`
	SourcePaths  []string   `json:"source_paths,omitempty"`
	TargetPaths  []string   `json:"target_paths,omitempty"`
	Conflicts    []string   `json:"conflicts,omitempty"`
}
