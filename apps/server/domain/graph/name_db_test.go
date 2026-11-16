package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// A name is resolved on Create and stored in its own column, so every read that
// feeds a write path must select it. GetHeadByCanonicalID is what Patch reads,
// and a name it cannot see looks like a rename on the very next no-op write
// (#530 regression: a no-op `name` patch created a version).
func TestCreateAndGetByIDCarryName(t *testing.T) {
	ctx, svc, projectID, _, _ := setupWorkConfigEnqueueTest(t, nil)
	key := "name-col-1"
	name := "Original"
	created, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "NormalType",
		Key:        &key,
		Name:       &name,
		Properties: map[string]any{"version": "1"},
	}, nil)
	require.NoError(t, err)

	require.NotNil(t, created.Name, "Create must report the explicit name")
	require.Equal(t, "Original", *created.Name)

	got, err := svc.GetByID(ctx, projectID, created.ID, true)
	require.NoError(t, err)
	require.NotNil(t, got.Name, "GetByID reads the detail columns; a column missing there reads as nil")
	require.Equal(t, "Original", *got.Name)
}

// TestCreateIgnoresPropertiesNameForTitle pins the #824 contract on the graph
// write path: properties["name"] and properties["title"] are ordinary
// properties, never a title. A create without an explicit name stores a NULL
// name, so the object renders as `type · #ref`.
func TestCreateIgnoresPropertiesNameForTitle(t *testing.T) {
	ctx, svc, projectID, _, _ := setupWorkConfigEnqueueTest(t, nil)
	key := "name-col-2"
	created, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "NormalType",
		Key:  &key,
		Properties: map[string]any{
			"name":    "Not a title",
			"title":   "Also not a title",
			"version": "1",
		},
	}, nil)
	require.NoError(t, err)

	require.Nil(t, created.Name, "properties.name/title must not populate the name column")
	require.Equal(t, "Not a title", created.Properties["name"], "the property itself is stored untouched")
}
