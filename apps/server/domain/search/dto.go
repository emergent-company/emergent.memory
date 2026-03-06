package search

import (
	"github.com/google/uuid"
)

// =============================================================================
// Request DTOs
// =============================================================================

// UnifiedSearchResultType specifies which result types to include
type UnifiedSearchResultType string

const (
	ResultTypeGraph UnifiedSearchResultType = "graph"
	ResultTypeText  UnifiedSearchResultType = "text"
	ResultTypeBoth  UnifiedSearchResultType = "both"
)

// UnifiedSearchFusionStrategy specifies how to combine graph and text results
type UnifiedSearchFusionStrategy string

const (
	FusionStrategyWeighted   UnifiedSearchFusionStrategy = "weighted"
	FusionStrategyRRF        UnifiedSearchFusionStrategy = "rrf"
	FusionStrategyInterleave UnifiedSearchFusionStrategy = "interleave"
	FusionStrategyGraphFirst UnifiedSearchFusionStrategy = "graph_first"
	FusionStrategyTextFirst  UnifiedSearchFusionStrategy = "text_first"
)

// UnifiedSearchWeights configures weights for combining graph and text scores
type UnifiedSearchWeights struct {
	GraphWeight        float32 `json:"graphWeight,omitempty"`
	TextWeight         float32 `json:"textWeight,omitempty"`
	RelationshipWeight float32 `json:"relationshipWeight,omitempty"`
}

// UnifiedSearchRelationshipOptions configures relationship expansion for graph results
type UnifiedSearchRelationshipOptions struct {
	Enabled      bool   `json:"enabled,omitempty"`
	MaxDepth     int    `json:"maxDepth,omitempty"`
	MaxNeighbors int    `json:"maxNeighbors,omitempty"`
	Direction    string `json:"direction,omitempty"` // "in", "out", "both"
}

// UnifiedSearchRequest is the request body for unified search
type UnifiedSearchRequest struct {
	Query               string                            `json:"query" validate:"required,max=800"`
	Limit               int                               `json:"limit,omitempty"`
	ResultTypes         UnifiedSearchResultType           `json:"resultTypes,omitempty"`
	FusionStrategy      UnifiedSearchFusionStrategy       `json:"fusionStrategy,omitempty"`
	Weights             *UnifiedSearchWeights             `json:"weights,omitempty"`
	RelationshipOptions *UnifiedSearchRelationshipOptions `json:"relationshipOptions,omitempty"`
	IncludeDebug        bool                              `json:"includeDebug,omitempty"`
	MaxTokenBudget      int                               `json:"maxTokenBudget,omitempty"`
}

// =============================================================================
// Response DTOs
// =============================================================================

// UnifiedSearchItemType is the type discriminator for search results
type UnifiedSearchItemType string

const (
	ItemTypeGraph        UnifiedSearchItemType = "graph"
	ItemTypeText         UnifiedSearchItemType = "text"
	ItemTypeRelationship UnifiedSearchItemType = "relationship"
)

// UnifiedSearchRelationship contains relationship info for graph results
type UnifiedSearchRelationship struct {
	ObjectID          string         `json:"object_id"`
	Type              string         `json:"type"`
	Direction         string         `json:"direction"` // "in" or "out"
	Properties        map[string]any `json:"properties,omitempty"`
	RelatedObjectType *string        `json:"related_object_type,omitempty"`
	RelatedObjectKey  *string        `json:"related_object_key,omitempty"`
}

// UnifiedSearchGraphResult is a graph search result (knowledge graph object)
type UnifiedSearchGraphResult struct {
	Type            UnifiedSearchItemType       `json:"type"`
	ID              string                      `json:"id"`
	ObjectID        string                      `json:"object_id"`
	CanonicalID     string                      `json:"canonical_id"`
	Score           float32                     `json:"score"`
	Rank            int                         `json:"rank"`
	ObjectType      string                      `json:"object_type"`
	Key             string                      `json:"key"`
	Fields          map[string]any              `json:"fields"`
	LexicalScore    *float32                    `json:"lexical_score,omitempty"`
	VectorScore     *float32                    `json:"vector_score,omitempty"`
	Relationships   []UnifiedSearchRelationship `json:"relationships,omitempty"`
	Explanation     *string                     `json:"explanation,omitempty"`
	TruncatedFields []string                    `json:"truncated_fields,omitempty"`
}

// UnifiedSearchTextResult is a text search result (document chunk)
type UnifiedSearchTextResult struct {
	Type       UnifiedSearchItemType `json:"type"`
	ID         string                `json:"id"`
	Snippet    string                `json:"snippet"`
	Score      float32               `json:"score"`
	Source     *string               `json:"source,omitempty"`
	Mode       *string               `json:"mode,omitempty"`
	DocumentID *string               `json:"document_id,omitempty"`
}

// UnifiedSearchResultItem is the union type for all search results
// We use a struct with all possible fields since Go doesn't have union types
type UnifiedSearchResultItem struct {
	// Common fields
	Type  UnifiedSearchItemType `json:"type"`
	ID    string                `json:"id"`
	Score float32               `json:"score"`

	// Graph-specific fields
	ObjectID        string                      `json:"object_id,omitempty"`
	CanonicalID     string                      `json:"canonical_id,omitempty"`
	Rank            int                         `json:"rank,omitempty"`
	ObjectType      string                      `json:"object_type,omitempty"`
	Key             string                      `json:"key,omitempty"`
	Fields          map[string]any              `json:"fields,omitempty"`
	Relationships   []UnifiedSearchRelationship `json:"relationships,omitempty"`
	Explanation     *string                     `json:"explanation,omitempty"`
	TruncatedFields []string                    `json:"truncated_fields,omitempty"`

	// Text-specific fields
	Snippet    string  `json:"snippet,omitempty"`
	Source     *string `json:"source,omitempty"`
	Mode       *string `json:"mode,omitempty"`
	DocumentID *string `json:"document_id,omitempty"`

	// Relationship-specific fields
	RelationshipType string         `json:"relationship_type,omitempty"`
	TripletText      string         `json:"triplet_text,omitempty"`
	SourceID         string         `json:"source_id,omitempty"`
	TargetID         string         `json:"target_id,omitempty"`
	Properties       map[string]any `json:"properties,omitempty"`
}

// UnifiedSearchExecutionTime contains timing breakdown for the search
type UnifiedSearchExecutionTime struct {
	GraphSearchMs           *int `json:"graphSearchMs,omitempty"`
	TextSearchMs            *int `json:"textSearchMs,omitempty"`
	RelationshipSearchMs    *int `json:"relationshipSearchMs,omitempty"`
	RelationshipExpansionMs *int `json:"relationshipExpansionMs,omitempty"`
	FusionMs                int  `json:"fusionMs"`
	TotalMs                 int  `json:"totalMs"`
}

// UnifiedSearchMetadata contains response metadata
type UnifiedSearchMetadata struct {
	TotalResults            int                         `json:"totalResults"`
	GraphResultCount        int                         `json:"graphResultCount"`
	TextResultCount         int                         `json:"textResultCount"`
	RelationshipResultCount int                         `json:"relationshipResultCount"`
	FusionStrategy          UnifiedSearchFusionStrategy `json:"fusionStrategy"`
	ExecutionTime           UnifiedSearchExecutionTime  `json:"executionTime"`
}

// UnifiedSearchScoreDistribution contains score statistics for debug info
type UnifiedSearchScoreDistribution struct {
	Graph        *ScoreStats `json:"graph,omitempty"`
	Text         *ScoreStats `json:"text,omitempty"`
	Relationship *ScoreStats `json:"relationship,omitempty"`
}

// ScoreStats contains min/max/mean statistics
type ScoreStats struct {
	Min  float32 `json:"min"`
	Max  float32 `json:"max"`
	Mean float32 `json:"mean"`
}

// UnifiedSearchFusionDetails contains fusion debug details
type UnifiedSearchFusionDetails struct {
	Strategy        UnifiedSearchFusionStrategy `json:"strategy"`
	Weights         *UnifiedSearchWeights       `json:"weights,omitempty"`
	PreFusionCounts *PreFusionCounts            `json:"pre_fusion_counts,omitempty"`
	PostFusionCount int                         `json:"post_fusion_count"`
}

// PreFusionCounts contains counts before fusion
type PreFusionCounts struct {
	Graph        int `json:"graph"`
	Text         int `json:"text"`
	Relationship int `json:"relationship"`
}

// UnifiedSearchDebug contains debug information
type UnifiedSearchDebug struct {
	GraphSearch       any                             `json:"graphSearch,omitempty"`
	TextSearch        any                             `json:"textSearch,omitempty"`
	ScoreDistribution *UnifiedSearchScoreDistribution `json:"score_distribution,omitempty"`
	FusionDetails     *UnifiedSearchFusionDetails     `json:"fusion_details,omitempty"`
}

// UnifiedSearchResponse is the response for unified search
type UnifiedSearchResponse struct {
	Results  []UnifiedSearchResultItem `json:"results"`
	Metadata UnifiedSearchMetadata     `json:"metadata"`
	Debug    *UnifiedSearchDebug       `json:"debug,omitempty"`
}

// =============================================================================
// Internal types for search operations
// =============================================================================

// TextSearchResult represents a text search result from the chunks table
type TextSearchResult struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	ChunkIndex int
	Text       string
	Score      float32
	Source     *string
	Mode       *string
}

// GraphSearchResult represents a graph search result with scores
type GraphSearchResult struct {
	ObjectID      uuid.UUID
	CanonicalID   uuid.UUID
	ObjectType    string
	Key           string
	Fields        map[string]any
	Score         float32
	Rank          int
	LexicalScore  *float32
	VectorScore   *float32
	Relationships []UnifiedSearchRelationship
}

// SearchContext contains context information for the search
type SearchContext struct {
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Scopes    []string
}
