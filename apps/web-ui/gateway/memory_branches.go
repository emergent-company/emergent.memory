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
	// Objects is the per-object breakdown the server returns alongside the
	// counts. It carries the canonical id, merge status/resolution, and — for
	// the objects a merge does not apply — the conflicting keys and/or the
	// similar target, so the UI can name which objects were left for review
	// instead of showing only a bare count (#242).
	Objects []BranchMergeObject `json:"objects"`
}

// BranchMergeObject is one entry of a merge response's per-object breakdown.
// Resolution is "skipped" (a conflict preserved on the target) or "suggested"
// (a similarity match flagged for review) for the objects a "partial" merge
// leaves on the source branch; SimilarTargetName names the matched object when
// the entry is a similarity match.
type BranchMergeObject struct {
	CanonicalID       string   `json:"canonical_id"`
	Status            string   `json:"status"`
	Resolution        string   `json:"resolution"`
	Conflicts         []string `json:"conflicts"`
	SimilarityScore   float32  `json:"similarity_score"`
	SimilarTargetID   string   `json:"similar_target_id"`
	SimilarTargetName string   `json:"similar_target_name"`
}

// LeftForReviewObjects returns the per-object entries a merge left on the
// source branch for human decision: conflicts preserved on the target
// ("skipped") and similarity matches flagged for review ("suggested"). Nil-safe;
// returns nil when the breakdown is absent.
func (r *BranchMergeResult) LeftForReviewObjects() []BranchMergeObject {
	if r == nil {
		return nil
	}
	var out []BranchMergeObject
	for _, o := range r.Objects {
		switch o.Resolution {
		case "skipped", "suggested":
			out = append(out, o)
		}
	}
	return out
}

// MergeBranch merges a source branch into main with the given policy (POST
// /api/graph/branches/main/merge). The response is bare JSON. This is a
// mutation, so it is never retried (POST is already non-retryable in doH).
func (m *MemoryClient) MergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error) {
	return m.postMerge(ctx, sourceBranchID, policy, true)
}

// PreviewMergeBranch previews a merge without executing it (POST
// /api/graph/branches/main/merge with execute:false). The response shape is
// identical to MergeBranch: the same counts, but nothing is written. It backs
// the "Review merge" modal so the user sees the split (added / merged /
// left-for-review) before confirming.
func (m *MemoryClient) PreviewMergeBranch(ctx context.Context, sourceBranchID, policy string) (*BranchMergeResult, error) {
	return m.postMerge(ctx, sourceBranchID, policy, false)
}

func (m *MemoryClient) postMerge(ctx context.Context, sourceBranchID, policy string, execute bool) (*BranchMergeResult, error) {
	body := map[string]any{
		"source_branch_id": sourceBranchID,
		"execute":          execute,
		"policy":           policy,
	}
	var out BranchMergeResult
	if err := m.doH(ctx, http.MethodPost, "/api/graph/branches/main/merge", body, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
