// Package graph provides the Graph service client for the Emergent API SDK.
// This includes objects, relationships, graph search, traversal, branch merge,
// and analytics functionality.
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Graph API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new Graph service client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// =============================================================================
// SDK Types — GraphObject
// =============================================================================

// GraphObject represents a graph object response from the API.
type GraphObject struct {
	ID                string         `json:"id"`
	OrgID             *string        `json:"org_id,omitempty"`
	ProjectID         string         `json:"project_id"`
	BranchID          *string        `json:"branch_id,omitempty"`
	CanonicalID       string         `json:"canonical_id"`
	SupersedesID      *string        `json:"supersedes_id,omitempty"`
	Version           int            `json:"version"`
	Type              string         `json:"type"`
	Key               *string        `json:"key,omitempty"`
	Status            *string        `json:"status,omitempty"`
	Properties        map[string]any `json:"properties"`
	Labels            []string       `json:"labels"`
	SchemaVersion     *string        `json:"schema_version,omitempty"`
	DeletedAt         *time.Time     `json:"deleted_at,omitempty"`
	ChangeSummary     map[string]any `json:"change_summary,omitempty"`
	ContentHash       *string        `json:"content_hash,omitempty"`
	ExternalSource    *string        `json:"external_source,omitempty"`
	ExternalID        *string        `json:"external_id,omitempty"`
	ExternalURL       *string        `json:"external_url,omitempty"`
	ExternalParentID  *string        `json:"external_parent_id,omitempty"`
	SyncedAt          *time.Time     `json:"synced_at,omitempty"`
	ExternalUpdatedAt *time.Time     `json:"external_updated_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	RevisionCount     *int           `json:"revision_count,omitempty"`
	RelationshipCount *int           `json:"relationship_count,omitempty"`
}

// =============================================================================
// SDK Types — GraphRelationship
// =============================================================================

// GraphRelationship represents a graph relationship response from the API.
type GraphRelationship struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	BranchID      *string        `json:"branch_id,omitempty"`
	CanonicalID   string         `json:"canonical_id"`
	SupersedesID  *string        `json:"supersedes_id,omitempty"`
	Version       int            `json:"version"`
	Type          string         `json:"type"`
	SrcID         string         `json:"src_id"`
	DstID         string         `json:"dst_id"`
	Properties    map[string]any `json:"properties"`
	Weight        *float32       `json:"weight,omitempty"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty"`
	ChangeSummary map[string]any `json:"change_summary,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	// InverseRelationship is populated when an inverse relationship was auto-created
	// based on the template pack's inverseType declaration.
	InverseRelationship *GraphRelationship `json:"inverse_relationship,omitempty"`
}

// =============================================================================
// SDK Types — Request types
// =============================================================================

// CreateObjectRequest is the request body for creating a graph object.
type CreateObjectRequest struct {
	Type       string         `json:"type"`
	Key        *string        `json:"key,omitempty"`
	Status     *string        `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	BranchID   *string        `json:"branch_id,omitempty"`
}

// UpdateObjectRequest is the request body for patching a graph object.
type UpdateObjectRequest struct {
	Properties    map[string]any `json:"properties,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	ReplaceLabels bool           `json:"replaceLabels,omitempty"`
	Status        *string        `json:"status,omitempty"`
}

// ListObjectsOptions holds query parameters for listing/searching objects.
type ListObjectsOptions struct {
	Type            string
	Types           []string
	Label           string
	Labels          []string
	Status          string
	Key             string
	BranchID        string
	IncludeDeleted  bool
	Limit           int
	Cursor          string
	Order           string // "asc" or "desc"
	RelatedToID     string
	IDs             []string
	ExtractionJobID string
	PropertyFilters []PropertyFilter // JSONB property filters (JSON-encoded in query param)
}

// PropertyFilter defines a filter condition on the JSONB properties column.
type PropertyFilter struct {
	Path  string `json:"path"`            // Property path (dot-notation for nested, e.g. "address.city")
	Op    string `json:"op"`              // Operator: eq, neq, gt, gte, lt, lte, contains, exists, in
	Value any    `json:"value,omitempty"` // Filter value (omitted for "exists" operator)
}

// FTSSearchOptions holds query parameters for full-text search.
type FTSSearchOptions struct {
	Query          string
	Types          []string
	Labels         []string
	Status         string
	BranchID       string
	IncludeDeleted bool
	Limit          int
	Offset         int
}

// VectorSearchRequest is the request body for vector similarity search.
type VectorSearchRequest struct {
	Vector         []float32 `json:"vector"`
	Types          []string  `json:"types,omitempty"`
	Labels         []string  `json:"labels,omitempty"`
	Status         *string   `json:"status,omitempty"`
	BranchID       *string   `json:"branchId,omitempty"`
	IncludeDeleted bool      `json:"includeDeleted,omitempty"`
	MaxDistance    *float32  `json:"maxDistance,omitempty"`
	Limit          int       `json:"limit,omitempty"`
	Offset         int       `json:"offset,omitempty"`
}

// FindSimilarOptions holds query parameters for finding similar objects.
type FindSimilarOptions struct {
	Limit       int
	MaxDistance *float32
	MinScore    *float32
	Type        string
	BranchID    string
	KeyPrefix   string
	LabelsAll   []string
	LabelsAny   []string
}

// BulkUpdateStatusRequest is the request for bulk status updates.
type BulkUpdateStatusRequest struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status"`
}

// HybridSearchRequest is the request for hybrid (FTS + vector) search.
type HybridSearchRequest struct {
	Query          string    `json:"query"`
	Vector         []float32 `json:"vector,omitempty"`
	Types          []string  `json:"types,omitempty"`
	Labels         []string  `json:"labels,omitempty"`
	Status         *string   `json:"status,omitempty"`
	BranchID       *string   `json:"branchId,omitempty"`
	IncludeDeleted bool      `json:"includeDeleted,omitempty"`
	LexicalWeight  *float32  `json:"lexicalWeight,omitempty"`
	VectorWeight   *float32  `json:"vectorWeight,omitempty"`
	Limit          int       `json:"limit,omitempty"`
	Offset         int       `json:"offset,omitempty"`
	IncludeDebug   bool      `json:"includeDebug,omitempty"`
}

// SearchWithNeighborsRequest is the request for search with neighbors.
type SearchWithNeighborsRequest struct {
	Query            string   `json:"query"`
	Limit            int      `json:"limit,omitempty"`
	IncludeNeighbors bool     `json:"includeNeighbors,omitempty"`
	MaxNeighbors     int      `json:"maxNeighbors,omitempty"`
	MaxDistance      *float32 `json:"maxDistance,omitempty"`
	BranchID         *string  `json:"branchId,omitempty"`
	Types            []string `json:"types,omitempty"`
	Labels           []string `json:"labels,omitempty"`
}

// GraphExpandRequest is the request for graph expand.
type GraphExpandRequest struct {
	RootIDs                       []string               `json:"root_ids"`
	Direction                     string                 `json:"direction,omitempty"`
	MaxDepth                      int                    `json:"max_depth,omitempty"`
	MaxNodes                      int                    `json:"max_nodes,omitempty"`
	MaxEdges                      int                    `json:"max_edges,omitempty"`
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

// TraverseGraphRequest is the request for graph traversal.
type TraverseGraphRequest struct {
	RootIDs           []string        `json:"root_ids"`
	Direction         string          `json:"direction,omitempty"`
	MaxDepth          int             `json:"max_depth,omitempty"`
	MaxNodes          int             `json:"max_nodes,omitempty"`
	MaxEdges          int             `json:"max_edges,omitempty"`
	RelationshipTypes []string        `json:"relationship_types,omitempty"`
	ObjectTypes       []string        `json:"object_types,omitempty"`
	Labels            []string        `json:"labels,omitempty"`
	Limit             int             `json:"limit,omitempty"`
	PageDirection     string          `json:"page_direction,omitempty"`
	Cursor            *string         `json:"cursor,omitempty"`
	EdgePhases        []EdgePhase     `json:"edgePhases,omitempty"`
	NodeFilter        *Predicate      `json:"nodeFilter,omitempty"`
	EdgeFilter        *Predicate      `json:"edgeFilter,omitempty"`
	ReturnPaths       bool            `json:"returnPaths,omitempty"`
	MaxPathsPerNode   int             `json:"maxPathsPerNode,omitempty"`
	TemporalFilter    *TemporalFilter `json:"temporalFilter,omitempty"`
	FieldStrategy     string          `json:"fieldStrategy,omitempty"`
}

// EdgePhase defines a phase in multi-phase traversal.
type EdgePhase struct {
	RelationshipTypes []string `json:"relationshipTypes,omitempty"`
	Direction         string   `json:"direction"`
	MaxDepth          int      `json:"maxDepth"`
	ObjectTypes       []string `json:"objectTypes,omitempty"`
	Labels            []string `json:"labels,omitempty"`
}

// Predicate defines a filter condition for nodes or edges.
type Predicate struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value,omitempty"`
}

// TemporalFilter defines point-in-time query parameters.
type TemporalFilter struct {
	AsOf  string `json:"asOf"`
	Field string `json:"field,omitempty"`
}

// BranchMergeRequest is the request for branch merge.
type BranchMergeRequest struct {
	SourceBranchID string `json:"sourceBranchId"`
	Execute        bool   `json:"execute,omitempty"`
	Limit          *int   `json:"limit,omitempty"`
}

// AnalyticsOptions holds query parameters for analytics endpoints.
type AnalyticsOptions struct {
	Limit    int
	Types    []string
	Labels   []string
	BranchID string
	Order    string
}

// UnusedOptions holds query parameters for unused objects endpoint.
type UnusedOptions struct {
	Limit    int
	Types    []string
	Labels   []string
	BranchID string
	DaysIdle int
}

// CreateRelationshipRequest is the request body for creating a relationship.
type CreateRelationshipRequest struct {
	Type       string         `json:"type"`
	SrcID      string         `json:"src_id"`
	DstID      string         `json:"dst_id"`
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`
	BranchID   *string        `json:"branch_id,omitempty"`
}

// UpdateRelationshipRequest is the request body for patching a relationship.
type UpdateRelationshipRequest struct {
	Properties map[string]any `json:"properties,omitempty"`
	Weight     *float32       `json:"weight,omitempty"`
}

// ListRelationshipsOptions holds query parameters for listing relationships.
type ListRelationshipsOptions struct {
	Type           string
	Types          []string
	SrcID          string
	DstID          string
	ObjectID       string
	BranchID       string
	IncludeDeleted bool
	Limit          int
	Cursor         string
}

// =============================================================================
// SDK Types — Response types
// =============================================================================

// SearchObjectsResponse is the paginated response for listing objects.
type SearchObjectsResponse struct {
	Items      []*GraphObject `json:"items"`
	NextCursor *string        `json:"next_cursor,omitempty"`
	Total      int            `json:"total"`
}

// SearchRelationshipsResponse is the paginated response for listing relationships.
type SearchRelationshipsResponse struct {
	Items      []*GraphRelationship `json:"items"`
	NextCursor *string              `json:"next_cursor,omitempty"`
	Total      int                  `json:"total"`
}

// SearchResultItem represents a single search result with scores.
type SearchResultItem struct {
	Object       *GraphObject `json:"object"`
	Score        float32      `json:"score"`
	LexicalScore *float32     `json:"lexicalScore,omitempty"`
	VectorScore  *float32     `json:"vectorScore,omitempty"`
	VectorDist   *float32     `json:"vectorDist,omitempty"`
}

// SearchResponse is the response for FTS, vector, and hybrid search.
type SearchResponse struct {
	Data    []*SearchResultItem `json:"data"`
	Total   int                 `json:"total"`
	HasMore bool                `json:"hasMore"`
	Offset  int                 `json:"offset"`
	Meta    *SearchResponseMeta `json:"meta,omitempty"`
}

// SearchResponseMeta contains metadata about the search request.
type SearchResponseMeta struct {
	ElapsedMs    float64             `json:"elapsed_ms"`
	Timing       *SearchTimingDebug  `json:"timing,omitempty"`
	ChannelStats *SearchChannelStats `json:"channel_stats,omitempty"`
}

// SearchTimingDebug contains timing breakdown for debug mode.
type SearchTimingDebug struct {
	EmbeddingMs float64 `json:"embedding_ms"`
	LexicalMs   float64 `json:"lexical_ms"`
	VectorMs    float64 `json:"vector_ms"`
	FusionMs    float64 `json:"fusion_ms"`
	TotalMs     float64 `json:"total_ms"`
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

// ObjectHistoryResponse is the response for version history.
type ObjectHistoryResponse struct {
	Versions []*GraphObject `json:"versions"`
}

// GetObjectEdgesResponse is the response for listing edges of an object.
type GetObjectEdgesResponse struct {
	Incoming []*GraphRelationship `json:"incoming"`
	Outgoing []*GraphRelationship `json:"outgoing"`
}

// SimilarObjectResult represents a single similar object with distance.
type SimilarObjectResult struct {
	ID          string         `json:"id"`
	CanonicalID *string        `json:"canonical_id,omitempty"`
	Version     *int           `json:"version,omitempty"`
	Distance    float32        `json:"distance"`
	ProjectID   *string        `json:"project_id,omitempty"`
	BranchID    *string        `json:"branch_id,omitempty"`
	Type        string         `json:"type"`
	Key         *string        `json:"key,omitempty"`
	Status      string         `json:"status"`
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	CreatedAt   *string        `json:"created_at,omitempty"`
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

// BulkCreateObjectsRequest is the request for bulk object creation.
type BulkCreateObjectsRequest struct {
	Items []CreateObjectRequest `json:"items"`
}

// BulkCreateObjectsResponse is the response for bulk object creation.
type BulkCreateObjectsResponse struct {
	Success int                      `json:"success"`
	Failed  int                      `json:"failed"`
	Results []BulkCreateObjectResult `json:"results"`
}

// BulkCreateObjectResult is the result for a single object in bulk creation.
type BulkCreateObjectResult struct {
	Index   int          `json:"index"`
	Success bool         `json:"success"`
	Object  *GraphObject `json:"object,omitempty"`
	Error   *string      `json:"error,omitempty"`
}

// BulkCreateRelationshipsRequest is the request for bulk relationship creation.
type BulkCreateRelationshipsRequest struct {
	Items []CreateRelationshipRequest `json:"items"`
}

// BulkCreateRelationshipsResponse is the response for bulk relationship creation.
type BulkCreateRelationshipsResponse struct {
	Success int                            `json:"success"`
	Failed  int                            `json:"failed"`
	Results []BulkCreateRelationshipResult `json:"results"`
}

// BulkCreateRelationshipResult is the result for a single relationship in bulk creation.
type BulkCreateRelationshipResult struct {
	Index        int                `json:"index"`
	Success      bool               `json:"success"`
	Relationship *GraphRelationship `json:"relationship,omitempty"`
	Error        *string            `json:"error,omitempty"`
}

// SearchWithNeighborsResponse is the response for search with neighbors.
type SearchWithNeighborsResponse struct {
	PrimaryResults []*SearchWithNeighborsResultItem `json:"primaryResults"`
	Neighbors      map[string][]*GraphObject        `json:"neighbors,omitempty"`
}

// SearchWithNeighborsResultItem wraps a primary result with its search score.
type SearchWithNeighborsResultItem struct {
	Object *GraphObject `json:"object"`
	Score  float32      `json:"score"`
}

// GraphExpandResponse is the response for graph expand.
type GraphExpandResponse struct {
	Roots           []string         `json:"roots"`
	Nodes           []*ExpandNode    `json:"nodes"`
	Edges           []*ExpandEdge    `json:"edges"`
	Truncated       bool             `json:"truncated"`
	MaxDepthReached int              `json:"max_depth_reached"`
	TotalNodes      int              `json:"total_nodes"`
	Meta            *GraphExpandMeta `json:"meta"`
}

// ExpandNode represents a node in the expand response.
type ExpandNode struct {
	ID         string         `json:"id"`
	Depth      int            `json:"depth"`
	Type       string         `json:"type"`
	Key        *string        `json:"key,omitempty"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties,omitempty"`
}

// ExpandEdge represents an edge in the expand response.
type ExpandEdge struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	SrcID      string         `json:"src_id"`
	DstID      string         `json:"dst_id"`
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

// TraverseGraphResponse is the response for graph traversal.
type TraverseGraphResponse struct {
	Roots               []string        `json:"roots"`
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
	ID         string     `json:"id"`
	Depth      int        `json:"depth"`
	Type       string     `json:"type"`
	Key        *string    `json:"key,omitempty"`
	Labels     []string   `json:"labels"`
	PhaseIndex *int       `json:"phaseIndex,omitempty"`
	Paths      [][]string `json:"paths,omitempty"`
}

// TraverseEdge represents an edge in the traverse response.
type TraverseEdge struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	SrcID string `json:"src_id"`
	DstID string `json:"dst_id"`
}

// BranchMergeResponse is the response for branch merge.
type BranchMergeResponse struct {
	TargetBranchID   string                      `json:"targetBranchId"`
	SourceBranchID   string                      `json:"sourceBranchId"`
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
	CanonicalID  string   `json:"canonical_id"`
	Status       string   `json:"status"`
	SourceHeadID *string  `json:"source_head_id,omitempty"`
	TargetHeadID *string  `json:"target_head_id,omitempty"`
	SourcePaths  []string `json:"source_paths,omitempty"`
	TargetPaths  []string `json:"target_paths,omitempty"`
	Conflicts    []string `json:"conflicts,omitempty"`
}

// BranchMergeRelationshipSummary represents merge status for a single relationship.
type BranchMergeRelationshipSummary struct {
	CanonicalID  string   `json:"canonical_id"`
	Status       string   `json:"status"`
	SourceHeadID *string  `json:"source_head_id,omitempty"`
	TargetHeadID *string  `json:"target_head_id,omitempty"`
	SourceSrcID  *string  `json:"source_src_id,omitempty"`
	SourceDstID  *string  `json:"source_dst_id,omitempty"`
	TargetSrcID  *string  `json:"target_src_id,omitempty"`
	TargetDstID  *string  `json:"target_dst_id,omitempty"`
	SourcePaths  []string `json:"source_paths,omitempty"`
	TargetPaths  []string `json:"target_paths,omitempty"`
	Conflicts    []string `json:"conflicts,omitempty"`
}

// MostAccessedResponse is the response for most-accessed analytics.
type MostAccessedResponse struct {
	Items []AnalyticsObjectItem `json:"items"`
	Total int                   `json:"total"`
	Meta  map[string]any        `json:"meta"`
}

// UnusedObjectsResponse is the response for unused objects analytics.
type UnusedObjectsResponse struct {
	Items []AnalyticsObjectItem `json:"items"`
	Total int                   `json:"total"`
	Meta  map[string]any        `json:"meta"`
}

// AnalyticsObjectItem represents an object in analytics responses.
type AnalyticsObjectItem struct {
	ID              string         `json:"id"`
	CanonicalID     string         `json:"canonical_id"`
	Type            string         `json:"type"`
	Key             *string        `json:"key,omitempty"`
	Properties      map[string]any `json:"properties"`
	Labels          []string       `json:"labels"`
	LastAccessedAt  *time.Time     `json:"last_accessed_at,omitempty"`
	AccessCount     *int64         `json:"access_count,omitempty"`
	DaysSinceAccess *int           `json:"days_since_access,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// RelationshipHistoryResponse is the response for relationship version history.
type RelationshipHistoryResponse struct {
	Versions []*GraphRelationship `json:"versions"`
}

// =============================================================================
// Internal helpers
// =============================================================================

// prepareRequest creates an authenticated HTTP request with org/project headers.
func (c *Client) prepareRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}

	return req, nil
}

// doJSON executes a request, checks for errors, and decodes JSON response.
func (c *Client) doJSON(req *http.Request, result any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return nil
}

// getJSON performs a GET request and decodes the JSON response.
func (c *Client) getJSON(ctx context.Context, reqURL string, result any) error {
	req, err := c.prepareRequest(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, result)
}

// postJSON performs a POST request with JSON body and decodes the response.
func (c *Client) postJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	return c.doJSON(req, result)
}

// patchJSON performs a PATCH request with JSON body and decodes the response.
func (c *Client) patchJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "PATCH", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	return c.doJSON(req, result)
}

// doDelete performs a DELETE request and drains the response body.
func (c *Client) doDelete(ctx context.Context, reqURL string) error {
	req, err := c.prepareRequest(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// =============================================================================
// Object CRUD
// =============================================================================

// CreateObject creates a new graph object.
func (c *Client) CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error) {
	var result GraphObject
	if err := c.postJSON(ctx, c.base+"/api/graph/objects", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetObject retrieves a single graph object by ID.
func (c *Client) GetObject(ctx context.Context, id string) (*GraphObject, error) {
	var result GraphObject
	if err := c.getJSON(ctx, c.base+"/api/graph/objects/"+url.PathEscape(id), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateObject patches a graph object, creating a new version.
func (c *Client) UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error) {
	var result GraphObject
	if err := c.patchJSON(ctx, c.base+"/api/graph/objects/"+url.PathEscape(id), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteObject soft-deletes a graph object.
func (c *Client) DeleteObject(ctx context.Context, id string) error {
	return c.doDelete(ctx, c.base+"/api/graph/objects/"+url.PathEscape(id))
}

// RestoreObject restores a soft-deleted graph object.
func (c *Client) RestoreObject(ctx context.Context, id string) (*GraphObject, error) {
	var result GraphObject
	req, err := c.prepareRequest(ctx, "POST", c.base+"/api/graph/objects/"+url.PathEscape(id)+"/restore", nil)
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetObjectHistory retrieves the version history of a graph object.
func (c *Client) GetObjectHistory(ctx context.Context, id string) (*ObjectHistoryResponse, error) {
	var result ObjectHistoryResponse
	if err := c.getJSON(ctx, c.base+"/api/graph/objects/"+url.PathEscape(id)+"/history", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetObjectEdges retrieves incoming and outgoing relationships for an object.
func (c *Client) GetObjectEdges(ctx context.Context, id string) (*GetObjectEdgesResponse, error) {
	var result GetObjectEdgesResponse
	if err := c.getJSON(ctx, c.base+"/api/graph/objects/"+url.PathEscape(id)+"/edges", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Object Search
// =============================================================================

// ListObjects searches/lists graph objects with optional filters.
func (c *Client) ListObjects(ctx context.Context, opts *ListObjectsOptions) (*SearchObjectsResponse, error) {
	u, err := url.Parse(c.base + "/api/graph/objects/search")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if len(opts.Types) > 0 {
			q.Set("types", strings.Join(opts.Types, ","))
		}
		if opts.Label != "" {
			q.Set("label", opts.Label)
		}
		if len(opts.Labels) > 0 {
			q.Set("labels", strings.Join(opts.Labels, ","))
		}
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.Key != "" {
			q.Set("key", opts.Key)
		}
		if opts.BranchID != "" {
			q.Set("branch_id", opts.BranchID)
		}
		if opts.IncludeDeleted {
			q.Set("include_deleted", "true")
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Cursor != "" {
			q.Set("cursor", opts.Cursor)
		}
		if opts.Order != "" {
			q.Set("order", opts.Order)
		}
		if opts.RelatedToID != "" {
			q.Set("related_to_id", opts.RelatedToID)
		}
		if len(opts.IDs) > 0 {
			q.Set("ids", strings.Join(opts.IDs, ","))
		}
		if opts.ExtractionJobID != "" {
			q.Set("extraction_job_id", opts.ExtractionJobID)
		}
		if len(opts.PropertyFilters) > 0 {
			pfJSON, err := json.Marshal(opts.PropertyFilters)
			if err != nil {
				return nil, fmt.Errorf("marshaling property filters: %w", err)
			}
			q.Set("property_filters", string(pfJSON))
		}
	}
	u.RawQuery = q.Encode()

	var result SearchObjectsResponse
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FTSSearch performs a full-text search on graph objects.
func (c *Client) FTSSearch(ctx context.Context, opts *FTSSearchOptions) (*SearchResponse, error) {
	u, err := url.Parse(c.base + "/api/graph/objects/fts")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Query != "" {
			q.Set("q", opts.Query)
		}
		if len(opts.Types) > 0 {
			q.Set("types", strings.Join(opts.Types, ","))
		}
		if len(opts.Labels) > 0 {
			q.Set("labels", strings.Join(opts.Labels, ","))
		}
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.BranchID != "" {
			q.Set("branch_id", opts.BranchID)
		}
		if opts.IncludeDeleted {
			q.Set("include_deleted", "true")
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
	}
	u.RawQuery = q.Encode()

	var result SearchResponse
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// VectorSearch performs a vector similarity search on graph objects.
func (c *Client) VectorSearch(ctx context.Context, req *VectorSearchRequest) (*SearchResponse, error) {
	var result SearchResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/objects/vector-search", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTagsOptions specifies optional filters for listing tags.
type ListTagsOptions struct {
	Type   string // Filter to tags from objects of this type
	Prefix string // Filter to tags starting with this prefix
	Limit  int    // Maximum number of tags to return (0 = no limit)
}

// ListTags retrieves all tags used across graph objects.
// Pass nil for opts to retrieve all tags without filtering.
func (c *Client) ListTags(ctx context.Context, opts *ListTagsOptions) ([]string, error) {
	u, err := url.Parse(c.base + "/api/graph/objects/tags")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if opts.Prefix != "" {
			q.Set("prefix", opts.Prefix)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		u.RawQuery = q.Encode()
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return result.Tags, nil
}

// FindSimilar finds objects similar to the given object.
func (c *Client) FindSimilar(ctx context.Context, id string, opts *FindSimilarOptions) ([]SimilarObjectResult, error) {
	u, err := url.Parse(c.base + "/api/graph/objects/" + url.PathEscape(id) + "/similar")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.MaxDistance != nil {
			q.Set("maxDistance", fmt.Sprintf("%f", *opts.MaxDistance))
		}
		if opts.MinScore != nil {
			q.Set("minScore", fmt.Sprintf("%f", *opts.MinScore))
		}
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if opts.BranchID != "" {
			q.Set("branchId", opts.BranchID)
		}
		if opts.KeyPrefix != "" {
			q.Set("keyPrefix", opts.KeyPrefix)
		}
		if len(opts.LabelsAll) > 0 {
			q.Set("labelsAll", strings.Join(opts.LabelsAll, ","))
		}
		if len(opts.LabelsAny) > 0 {
			q.Set("labelsAny", strings.Join(opts.LabelsAny, ","))
		}
	}
	u.RawQuery = q.Encode()

	var result []SimilarObjectResult
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// BulkUpdateStatus updates the status of multiple objects at once.
func (c *Client) BulkUpdateStatus(ctx context.Context, req *BulkUpdateStatusRequest) (*BulkUpdateStatusResponse, error) {
	var result BulkUpdateStatusResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/objects/bulk-update-status", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BulkCreateObjects creates multiple graph objects in a single request.
// Each item is processed independently — failures don't roll back other successes.
// Maximum 100 items per request.
func (c *Client) BulkCreateObjects(ctx context.Context, req *BulkCreateObjectsRequest) (*BulkCreateObjectsResponse, error) {
	var result BulkCreateObjectsResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/objects/bulk", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Graph Search
// =============================================================================

// HybridSearch performs a hybrid (FTS + vector) search.
func (c *Client) HybridSearch(ctx context.Context, req *HybridSearchRequest) (*SearchResponse, error) {
	var result SearchResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/search", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchWithNeighbors performs a search and returns results with their neighbors.
func (c *Client) SearchWithNeighbors(ctx context.Context, req *SearchWithNeighborsRequest) (*SearchWithNeighborsResponse, error) {
	var result SearchWithNeighborsResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/search-with-neighbors", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Graph Traversal
// =============================================================================

// ExpandGraph expands the graph from root nodes.
func (c *Client) ExpandGraph(ctx context.Context, req *GraphExpandRequest) (*GraphExpandResponse, error) {
	var result GraphExpandResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/expand", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TraverseGraph performs a paginated graph traversal.
func (c *Client) TraverseGraph(ctx context.Context, req *TraverseGraphRequest) (*TraverseGraphResponse, error) {
	var result TraverseGraphResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/traverse", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Branch
// =============================================================================

// MergeBranch performs or previews a branch merge.
func (c *Client) MergeBranch(ctx context.Context, targetBranchID string, req *BranchMergeRequest) (*BranchMergeResponse, error) {
	var result BranchMergeResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/branches/"+url.PathEscape(targetBranchID)+"/merge", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Analytics
// =============================================================================

// GetMostAccessed retrieves the most-accessed objects.
func (c *Client) GetMostAccessed(ctx context.Context, opts *AnalyticsOptions) (*MostAccessedResponse, error) {
	u, err := url.Parse(c.base + "/api/graph/analytics/most-accessed")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if len(opts.Types) > 0 {
			q.Set("types", strings.Join(opts.Types, ","))
		}
		if len(opts.Labels) > 0 {
			q.Set("labels", strings.Join(opts.Labels, ","))
		}
		if opts.BranchID != "" {
			q.Set("branch_id", opts.BranchID)
		}
		if opts.Order != "" {
			q.Set("order", opts.Order)
		}
	}
	u.RawQuery = q.Encode()

	var result MostAccessedResponse
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetUnused retrieves objects that haven't been accessed recently.
func (c *Client) GetUnused(ctx context.Context, opts *UnusedOptions) (*UnusedObjectsResponse, error) {
	u, err := url.Parse(c.base + "/api/graph/analytics/unused")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if len(opts.Types) > 0 {
			q.Set("types", strings.Join(opts.Types, ","))
		}
		if len(opts.Labels) > 0 {
			q.Set("labels", strings.Join(opts.Labels, ","))
		}
		if opts.BranchID != "" {
			q.Set("branch_id", opts.BranchID)
		}
		if opts.DaysIdle > 0 {
			q.Set("days_idle", fmt.Sprintf("%d", opts.DaysIdle))
		}
	}
	u.RawQuery = q.Encode()

	var result UnusedObjectsResponse
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Relationship CRUD
// =============================================================================

// CreateRelationship creates a new graph relationship.
func (c *Client) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) (*GraphRelationship, error) {
	var result GraphRelationship
	if err := c.postJSON(ctx, c.base+"/api/graph/relationships", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BulkCreateRelationships creates multiple graph relationships in a single request.
// Each item is processed independently — failures don't roll back other successes.
// Maximum 100 items per request.
func (c *Client) BulkCreateRelationships(ctx context.Context, req *BulkCreateRelationshipsRequest) (*BulkCreateRelationshipsResponse, error) {
	var result BulkCreateRelationshipsResponse
	if err := c.postJSON(ctx, c.base+"/api/graph/relationships/bulk", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRelationship retrieves a single graph relationship by ID.
func (c *Client) GetRelationship(ctx context.Context, id string) (*GraphRelationship, error) {
	var result GraphRelationship
	if err := c.getJSON(ctx, c.base+"/api/graph/relationships/"+url.PathEscape(id), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateRelationship patches a graph relationship.
func (c *Client) UpdateRelationship(ctx context.Context, id string, req *UpdateRelationshipRequest) (*GraphRelationship, error) {
	var result GraphRelationship
	if err := c.patchJSON(ctx, c.base+"/api/graph/relationships/"+url.PathEscape(id), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteRelationship soft-deletes a graph relationship.
func (c *Client) DeleteRelationship(ctx context.Context, id string) error {
	return c.doDelete(ctx, c.base+"/api/graph/relationships/"+url.PathEscape(id))
}

// RestoreRelationship restores a soft-deleted graph relationship.
func (c *Client) RestoreRelationship(ctx context.Context, id string) (*GraphRelationship, error) {
	var result GraphRelationship
	req, err := c.prepareRequest(ctx, "POST", c.base+"/api/graph/relationships/"+url.PathEscape(id)+"/restore", nil)
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRelationshipHistory retrieves the version history of a relationship.
func (c *Client) GetRelationshipHistory(ctx context.Context, id string) (*RelationshipHistoryResponse, error) {
	var result RelationshipHistoryResponse
	if err := c.getJSON(ctx, c.base+"/api/graph/relationships/"+url.PathEscape(id)+"/history", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListRelationships searches/lists graph relationships with optional filters.
func (c *Client) ListRelationships(ctx context.Context, opts *ListRelationshipsOptions) (*SearchRelationshipsResponse, error) {
	u, err := url.Parse(c.base + "/api/graph/relationships/search")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if len(opts.Types) > 0 {
			q.Set("types", strings.Join(opts.Types, ","))
		}
		if opts.SrcID != "" {
			q.Set("src_id", opts.SrcID)
		}
		if opts.DstID != "" {
			q.Set("dst_id", opts.DstID)
		}
		if opts.ObjectID != "" {
			q.Set("object_id", opts.ObjectID)
		}
		if opts.BranchID != "" {
			q.Set("branch_id", opts.BranchID)
		}
		if opts.IncludeDeleted {
			q.Set("include_deleted", "true")
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Cursor != "" {
			q.Set("cursor", opts.Cursor)
		}
	}
	u.RawQuery = q.Encode()

	var result SearchRelationshipsResponse
	if err := c.getJSON(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
