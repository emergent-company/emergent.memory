package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// Item B: the /query loopback endpoint must apply a caller-supplied graph type
// scope to the internal query agent's run context. Removing the scope
// application in withQueryTypeScope makes these assertions fail.

func TestWithQueryTypeScope_NilScopeLeavesContextUnrestricted(t *testing.T) {
	ctx := withQueryTypeScope(context.Background(), QueryStreamRequest{})
	assert.Nil(t, mcp.TypeScopeFromContext(ctx), "no scope requested => unrestricted, unchanged")
}

func TestWithQueryTypeScope_AppliesObjectAndRelationshipAxes(t *testing.T) {
	ctx := withQueryTypeScope(context.Background(), QueryStreamRequest{
		ObjectTypes:       []string{"Law"},
		RelationshipTypes: []string{"CITES"},
	})

	scope := mcp.TypeScopeFromContext(ctx)
	require.NotNil(t, scope)
	assert.True(t, scope.HasObjectRestriction())
	assert.True(t, scope.AllowsObject("Law"))
	assert.False(t, scope.AllowsObject("Person"))
	assert.True(t, scope.HasRelationshipRestriction())
	assert.True(t, scope.AllowsRelationship("CITES"))
	assert.False(t, scope.AllowsRelationship("OWNS"))
}

func TestWithQueryTypeScope_ObjectOnlyLeavesRelationshipsUnrestricted(t *testing.T) {
	ctx := withQueryTypeScope(context.Background(), QueryStreamRequest{ObjectTypes: []string{"Law"}})

	scope := mcp.TypeScopeFromContext(ctx)
	require.NotNil(t, scope)
	assert.True(t, scope.HasObjectRestriction())
	assert.False(t, scope.HasRelationshipRestriction(), "relationship axis must stay unrestricted")
	assert.True(t, scope.AllowsRelationship("ANY"))
}
