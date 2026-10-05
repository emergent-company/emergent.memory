package main

import (
	"context"
	"net/http"
)

// MergeReadiness reports whether a branch is ready for a similarity-aware merge
// (GET /api/graph/branches/{id}/merge-readiness). Ready is false while any
// object is still missing an embedding.
type MergeReadiness struct {
	Ready             bool `json:"ready"`
	TotalObjects      int  `json:"total_objects"`
	PendingEmbeddings int  `json:"pending_embeddings"`
}

// GetMergeReadiness fetches merge readiness for a branch. The endpoint returns
// bare JSON (no {success,data} envelope).
func (m *MemoryClient) GetMergeReadiness(ctx context.Context, branchID string) (*MergeReadiness, error) {
	var out MergeReadiness
	if err := m.doH(ctx, http.MethodGet, "/api/graph/branches/"+branchID+"/merge-readiness", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BranchMergeResult is the response of a branch merge (POST
// /api/graph/branches/main/merge). Fields are camelCase per the endpoint.
type BranchMergeResult struct {
	TotalObjects       int `json:"total_objects"`
	AddedCount         int `json:"added_count"`
	FastForwardCount   int `json:"fast_forward_count"`
	ConflictCount      int `json:"conflict_count"`
	SimilarCount       int `json:"similar_count"`
	LeftForReviewCount int `json:"left_for_review_count"`
	MergedCount        int `json:"merged_count"`
}

// MergeBranch merges a source branch into main with the given policy (POST
// /api/graph/branches/main/merge). The response is bare JSON. This is a
// mutation, so it is never retried (POST is already non-retryable in doH).
func (m *MemoryClient) MergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error) {
	body := map[string]any{
		"source_branch_id": sourceBranchID,
		"execute":          true,
		"policy":           policy,
	}
	var out BranchMergeResult
	if err := m.doH(ctx, http.MethodPost, "/api/graph/branches/main/merge", body, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
