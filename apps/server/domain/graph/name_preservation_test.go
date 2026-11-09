package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A name-only branch edit shares the content hash (which predates `name`), so
// the merge classifier must compare the title explicitly.
func TestClassifyMergeObjectNameOnlyChange(t *testing.T) {
	sameHash := []byte{1, 2, 3}
	oldName, newName := "Old title", "New title"

	status, conflicts := classifyMergeObject(
		&BranchObjectHead{ContentHash: sameHash, Name: &newName},
		&BranchObjectHead{ContentHash: sameHash, Name: &oldName},
	)
	assert.Equal(t, "fast_forward", status)
	assert.Empty(t, conflicts)

	status, _ = classifyMergeObject(
		&BranchObjectHead{ContentHash: sameHash, Name: &oldName},
		&BranchObjectHead{ContentHash: sameHash, Name: &oldName},
	)
	assert.Equal(t, "unchanged", status)
}

func TestBuildChangesetObjectSetsName(t *testing.T) {
	obj, err := buildChangesetObject(
		uuid.New(), nil, uuid.New(),
		&ChangesetObjectItem{Type: "Task", Properties: map[string]any{"name": "Fix redirect"}},
		nil, ActorSystem, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, obj.Name)
	assert.Equal(t, "Fix redirect", *obj.Name)
}
