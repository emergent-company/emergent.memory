package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTypeScope_NilWhenUnrestricted(t *testing.T) {
	assert.Nil(t, NewTypeScope(nil, nil))
	assert.Nil(t, NewTypeScope([]string{}, []string{}))
	assert.Nil(t, NewTypeScope([]string{"", "  "}, nil))
}

func TestNewTypeScope_NonNilAndNormalized(t *testing.T) {
	s := NewTypeScope([]string{" Person ", "Person", "", "Org"}, []string{"references"})
	require.NotNil(t, s)
	// Trim, drop blanks, de-dupe, sort.
	assert.Equal(t, []string{"Org", "Person"}, s.ObjectTypes())
	assert.Equal(t, []string{"references"}, s.RelationshipTypes())
	assert.True(t, s.HasObjectRestriction())
	assert.True(t, s.HasRelationshipRestriction())

	// Object-only restriction leaves relationships unrestricted.
	objOnly := NewTypeScope([]string{"Person"}, nil)
	require.NotNil(t, objOnly)
	assert.True(t, objOnly.HasObjectRestriction())
	assert.False(t, objOnly.HasRelationshipRestriction())
	assert.Nil(t, objOnly.RelationshipTypes())

	// Relationship-only restriction leaves objects unrestricted.
	relOnly := NewTypeScope(nil, []string{"references"})
	require.NotNil(t, relOnly)
	assert.False(t, relOnly.HasObjectRestriction())
	assert.True(t, relOnly.HasRelationshipRestriction())
	assert.Nil(t, relOnly.ObjectTypes())
}

func TestTypeScope_AllowsObject(t *testing.T) {
	var nilScope *TypeScope
	assert.True(t, nilScope.AllowsObject("Person"))
	assert.True(t, nilScope.AllowsObject(""))

	// Object axis unrestricted => everything allowed.
	relOnly := NewTypeScope(nil, []string{"references"})
	assert.True(t, relOnly.AllowsObject("Person"))

	restricted := NewTypeScope([]string{"Person", "Org"}, nil)
	assert.True(t, restricted.AllowsObject("Person"))
	assert.True(t, restricted.AllowsObject("Org"))
	assert.False(t, restricted.AllowsObject("Dog"))
	assert.False(t, restricted.AllowsObject(""))
}

func TestTypeScope_AllowsRelationship(t *testing.T) {
	var nilScope *TypeScope
	assert.True(t, nilScope.AllowsRelationship("references"))
	assert.True(t, nilScope.AllowsRelationship(""))

	objOnly := NewTypeScope([]string{"Person"}, nil)
	assert.True(t, objOnly.AllowsRelationship("references"))

	restricted := NewTypeScope(nil, []string{"references", "contains"})
	assert.True(t, restricted.AllowsRelationship("references"))
	assert.True(t, restricted.AllowsRelationship("contains"))
	assert.False(t, restricted.AllowsRelationship("works_at"))
	assert.False(t, restricted.AllowsRelationship(""))
}

func TestTypeScope_FilterRequestedObjectTypes(t *testing.T) {
	var nilScope *TypeScope

	// nil/unrestricted => passthrough unchanged.
	got, denied := nilScope.FilterRequestedObjectTypes([]string{"Person", "Org"})
	assert.False(t, denied)
	assert.Equal(t, []string{"Person", "Org"}, got)

	// Empty requested => narrowed to the (sorted) allowlist.
	restricted := NewTypeScope([]string{"Person", "Org"}, nil)
	got, denied = restricted.FilterRequestedObjectTypes(nil)
	assert.False(t, denied)
	assert.Equal(t, []string{"Org", "Person"}, got)

	// Subset intersection preserves order, drops excluded, de-dupes.
	got, denied = restricted.FilterRequestedObjectTypes([]string{"Dog", "Person", "Org", "Person"})
	assert.False(t, denied)
	assert.Equal(t, []string{"Person", "Org"}, got)

	// All-excluded => denied.
	got, denied = restricted.FilterRequestedObjectTypes([]string{"Dog", "Cat"})
	assert.True(t, denied)
	assert.Nil(t, got)
}

func TestTypeScope_FilterRequestedRelationshipTypes(t *testing.T) {
	var nilScope *TypeScope

	got, denied := nilScope.FilterRequestedRelationshipTypes([]string{"references"})
	assert.False(t, denied)
	assert.Equal(t, []string{"references"}, got)

	restricted := NewTypeScope(nil, []string{"references", "contains"})
	got, denied = restricted.FilterRequestedRelationshipTypes(nil)
	assert.False(t, denied)
	assert.Equal(t, []string{"contains", "references"}, got)

	got, denied = restricted.FilterRequestedRelationshipTypes([]string{"works_at", "contains"})
	assert.False(t, denied)
	assert.Equal(t, []string{"contains"}, got)

	got, denied = restricted.FilterRequestedRelationshipTypes([]string{"works_at", "part_of"})
	assert.True(t, denied)
	assert.Nil(t, got)
}

func TestTypeScope_FilterObjectTypes(t *testing.T) {
	var nilScope *TypeScope
	assert.Equal(t, []string{"Person", "Org"}, nilScope.FilterObjectTypes([]string{"Person", "Org"}))

	restricted := NewTypeScope([]string{"Person", "Org"}, nil)
	assert.Equal(t, []string{"Org", "Person"}, restricted.FilterObjectTypes([]string{"Dog", "Org", "Person"}))
	assert.Empty(t, restricted.FilterObjectTypes([]string{"Dog", "Cat"}))
}

func TestTypeScope_FilterRelationshipTypes(t *testing.T) {
	var nilScope *TypeScope
	assert.Equal(t, []string{"references"}, nilScope.FilterRelationshipTypes([]string{"references"}))

	restricted := NewTypeScope(nil, []string{"references", "contains"})
	assert.Equal(t, []string{"contains", "references"}, restricted.FilterRelationshipTypes([]string{"works_at", "contains", "references"}))
	assert.Empty(t, restricted.FilterRelationshipTypes([]string{"works_at", "part_of"}))
}

func TestTypeScope_ContextRoundTrip(t *testing.T) {
	ctx := context.Background()

	// Absent => nil.
	assert.Nil(t, TypeScopeFromContext(ctx))

	// nil scope is a no-op: returns the same context, still nil.
	same := ContextWithTypeScope(ctx, nil)
	assert.Equal(t, ctx, same)
	assert.Nil(t, TypeScopeFromContext(same))

	// Round-trip a real scope.
	scope := NewTypeScope([]string{"Person"}, []string{"references"})
	scoped := ContextWithTypeScope(ctx, scope)
	assert.Same(t, scope, TypeScopeFromContext(scoped))

	// The original context is not mutated.
	assert.Nil(t, TypeScopeFromContext(ctx))
}

func TestTypeScope_AllowsRelationshipEndpoint(t *testing.T) {
	var nilScope *TypeScope
	assert.True(t, nilScope.AllowsRelationshipEndpoint("references", "Person"))
	assert.True(t, nilScope.AllowsRelationshipEndpoint("", ""))

	// Relationship-only restriction: object axis is unrestricted.
	relOnly := NewTypeScope(nil, []string{"references"})
	assert.True(t, relOnly.AllowsRelationshipEndpoint("references", "Person"))
	assert.False(t, relOnly.AllowsRelationshipEndpoint("works_at", "Person"))

	// Object-only restriction: relationship axis is unrestricted, but the
	// endpoint object type must still be allowed (cross-axis leak guard).
	objOnly := NewTypeScope([]string{"Person", "Org"}, nil)
	assert.True(t, objOnly.AllowsRelationshipEndpoint("references", "Person"))
	assert.False(t, objOnly.AllowsRelationshipEndpoint("references", "Dog"))

	// Both restricted.
	both := NewTypeScope([]string{"Person"}, []string{"references"})
	assert.True(t, both.AllowsRelationshipEndpoint("references", "Person"))
	assert.False(t, both.AllowsRelationshipEndpoint("references", "Dog"))
	assert.False(t, both.AllowsRelationshipEndpoint("works_at", "Person"))
}

func TestTypeScope_AllowsRelationshipRow(t *testing.T) {
	var nilScope *TypeScope
	assert.True(t, nilScope.AllowsRelationshipRow("references", "Law", "Paragraph"))

	// Relationship-only restriction: from/to object names are unrestricted.
	relOnly := NewTypeScope(nil, []string{"references"})
	assert.True(t, relOnly.AllowsRelationshipRow("references", "Law", "Paragraph"))
	assert.False(t, relOnly.AllowsRelationshipRow("works_at", "Law", "Paragraph"))

	// Object-only restriction: the from/to object names must both be allowed
	// even though the relationship axis is unrestricted.
	objOnly := NewTypeScope([]string{"Law", "Paragraph"}, nil)
	assert.True(t, objOnly.AllowsRelationshipRow("references", "Law", "Paragraph"))
	assert.False(t, objOnly.AllowsRelationshipRow("references", "Person", "Paragraph"))
	assert.False(t, objOnly.AllowsRelationshipRow("references", "Law", "Person"))

	// Both restricted.
	both := NewTypeScope([]string{"Law", "Paragraph"}, []string{"references"})
	assert.True(t, both.AllowsRelationshipRow("references", "Law", "Paragraph"))
	assert.False(t, both.AllowsRelationshipRow("references", "Person", "Paragraph"))
	assert.False(t, both.AllowsRelationshipRow("references", "Law", "Person"))
	assert.False(t, both.AllowsRelationshipRow("works_at", "Law", "Paragraph"))
}

func TestExecuteQueryKnowledge_TypeScopeGuard(t *testing.T) {
	svc := &Service{}

	// A restricted scope fails closed before any HTTP/loopback work.
	restricted := NewTypeScope([]string{"Person"}, []string{"references"})
	ctx := ContextWithTypeScope(context.Background(), restricted)

	_, err := svc.executeQueryKnowledge(ctx, "proj", map[string]any{"question": "what?"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search-knowledge is unavailable")

	// The guard runs before the question check.
	_, err = svc.executeQueryKnowledge(ctx, "proj", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search-knowledge is unavailable")

	// Object-only and relationship-only scopes also trip the guard.
	_, err = svc.executeQueryKnowledge(ContextWithTypeScope(context.Background(), NewTypeScope([]string{"Person"}, nil)), "proj", map[string]any{"question": "what?"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search-knowledge is unavailable")

	_, err = svc.executeQueryKnowledge(ContextWithTypeScope(context.Background(), NewTypeScope(nil, []string{"references"})), "proj", map[string]any{"question": "what?"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search-knowledge is unavailable")

	// Unrestricted scope: guard does not trigger, reaches the question check.
	_, err = svc.executeQueryKnowledge(context.Background(), "proj", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "question")
	assert.NotContains(t, err.Error(), "unavailable")
}
