package branches

import (
	"time"
)

// CreateBranchRequest is the request DTO for creating a branch
type CreateBranchRequest struct {
	ProjectID      *string `json:"project_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
	ParentBranchID *string `json:"parent_branch_id"`
	// Origin is who or what created the branch: extraction | agent | manual |
	// system. Empty defaults to manual. Server-side creators set it; it is not
	// mutable afterwards.
	Origin string `json:"origin,omitempty"`
	// OriginID is the creator's id (extraction job, agent run or user).
	OriginID *string `json:"origin_id,omitempty"`
	// OriginLabel is the creator rendered for humans (document filename, agent
	// name, user name).
	OriginLabel *string `json:"origin_label,omitempty"`
}

// UpdateBranchRequest is the request DTO for updating a branch. Origin is
// deliberately absent: a branch must not be able to change why it exists (an
// agent could otherwise re-label itself out of a filter).
type UpdateBranchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description,omitempty"`
}

// BranchResponse is the response DTO for a branch
type BranchResponse struct {
	ID             string  `json:"id"`
	ProjectID      *string `json:"project_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
	ParentBranchID *string `json:"parent_branch_id"`
	CreatedAt      string  `json:"created_at"`
	// MergedAt is set once the branch has been merged into main; nil (omitted)
	// while the branch is still open.
	MergedAt *string `json:"merged_at,omitempty"`
	// Origin is who or what created the branch (extraction | agent | manual |
	// system), OriginID its id and OriginLabel the human-readable source.
	Origin      string  `json:"origin"`
	OriginID    *string `json:"origin_id,omitempty"`
	OriginLabel *string `json:"origin_label,omitempty"`
}

// ToResponse converts a Branch entity to a BranchResponse
func ToResponse(b *Branch) *BranchResponse {
	origin := b.Origin
	if origin == "" {
		origin = OriginManual
	}
	resp := &BranchResponse{
		ID:             b.ID,
		ProjectID:      b.ProjectID,
		Name:           b.Name,
		Description:    b.Description,
		ParentBranchID: b.ParentBranchID,
		CreatedAt:      b.CreatedAt.Format(time.RFC3339Nano),
		Origin:         origin,
		OriginID:       b.OriginID,
		OriginLabel:    b.OriginLabel,
	}
	if b.MergedAt != nil {
		mergedAt := b.MergedAt.Format(time.RFC3339Nano)
		resp.MergedAt = &mergedAt
	}
	return resp
}

// ToResponseList converts a slice of Branch entities to BranchResponses
func ToResponseList(branches []*Branch) []*BranchResponse {
	result := make([]*BranchResponse, len(branches))
	for i, b := range branches {
		result[i] = ToResponse(b)
	}
	return result
}
