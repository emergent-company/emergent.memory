package branches

import (
	"time"
)

// CreateBranchRequest is the request DTO for creating a branch
type CreateBranchRequest struct {
	ProjectID      *string `json:"project_id"`
	Name           string  `json:"name"`
	ParentBranchID *string `json:"parent_branch_id"`
}

// UpdateBranchRequest is the request DTO for updating a branch
type UpdateBranchRequest struct {
	Name *string `json:"name"`
}

// BranchResponse is the response DTO for a branch
type BranchResponse struct {
	ID             string  `json:"id"`
	ProjectID      *string `json:"project_id"`
	Name           string  `json:"name"`
	ParentBranchID *string `json:"parent_branch_id"`
	CreatedAt      string  `json:"created_at"`
}

// ToResponse converts a Branch entity to a BranchResponse
func ToResponse(b *Branch) *BranchResponse {
	return &BranchResponse{
		ID:             b.ID,
		ProjectID:      b.ProjectID,
		Name:           b.Name,
		ParentBranchID: b.ParentBranchID,
		CreatedAt:      b.CreatedAt.Format(time.RFC3339Nano),
	}
}

// ToResponseList converts a slice of Branch entities to BranchResponses
func ToResponseList(branches []*Branch) []*BranchResponse {
	result := make([]*BranchResponse, len(branches))
	for i, b := range branches {
		result[i] = ToResponse(b)
	}
	return result
}
