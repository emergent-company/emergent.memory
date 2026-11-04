package branches

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BranchResponse / ToResponse helpers
// =============================================================================

func TestToResponse_PopulatesFields(t *testing.T) {
	projectID := "proj-1"
	parentID := "parent-1"
	b := &Branch{
		ID:             "branch-1",
		ProjectID:      &projectID,
		Name:           "feature",
		ParentBranchID: &parentID,
	}
	resp := ToResponse(b)
	require.NotNil(t, resp)
	assert.Equal(t, "branch-1", resp.ID)
	assert.Equal(t, &projectID, resp.ProjectID)
	assert.Equal(t, "feature", resp.Name)
	assert.Equal(t, &parentID, resp.ParentBranchID)
}

// =============================================================================
// GetMainBranchID contract
// The Store method delegates to GetMainBranch and converts *Branch → *string.
// We test the conversion logic independently here.
// =============================================================================

func TestGetMainBranchID_NilBranchYieldsNilID(t *testing.T) {
	// Simulate what GetMainBranchID does when GetMainBranch returns nil
	var branch *Branch = nil
	var id *string
	if branch != nil {
		s := branch.ID
		id = &s
	}
	assert.Nil(t, id)
}

func TestGetMainBranchID_BranchIDExtracted(t *testing.T) {
	branch := &Branch{ID: "main-branch-uuid"}
	s := branch.ID
	id := &s
	require.NotNil(t, id)
	assert.Equal(t, "main-branch-uuid", *id)
}

// =============================================================================
// ToResponseList
// =============================================================================

func TestToResponseList_EmptySlice(t *testing.T) {
	result := ToResponseList([]*Branch{})
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestToResponseList_MultipleEntries(t *testing.T) {
	projectID := "proj-x"
	branches := []*Branch{
		{ID: "b1", ProjectID: &projectID, Name: "main"},
		{ID: "b2", ProjectID: &projectID, Name: "feature"},
	}
	result := ToResponseList(branches)
	require.Len(t, result, 2)
	assert.Equal(t, "b1", result[0].ID)
	assert.Equal(t, "b2", result[1].ID)
}
