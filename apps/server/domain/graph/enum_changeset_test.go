package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

func TestChangesetObjectVersionEnforcesEnum(t *testing.T) {
	schemas := &ExtractionSchemas{ObjectSchemas: map[string]agents.ObjectSchema{
		"Task": {
			Name: "Task",
			Properties: map[string]agents.PropertyDef{
				"status":  {Type: "string", Enum: []any{"draft", "published"}},
				"country": {Type: "country", Enum: []any{"de"}},
			},
		},
	}}
	head := &GraphObject{Type: "Task", Properties: map[string]any{"status": "draft"}}

	version, err := buildChangesetObjectVersion(head, &ChangesetObjectItem{Properties: map[string]any{
		"status":  "published",
		"country": "de",
	}}, uuid.New(), schemas, "source", nil, false, nil)
	require.NoError(t, err)
	assert.Equal(t, "published", version.Properties["status"])
	assert.Equal(t, "DE", version.Properties["country"])

	_, err = buildChangesetObjectVersion(head, &ChangesetObjectItem{Properties: map[string]any{
		"status": "archived",
	}}, uuid.New(), schemas, "source", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status")
	assert.Contains(t, err.Error(), "draft")

	legacy := &GraphObject{Type: "Task", Properties: map[string]any{"status": "archived"}}
	version, err = buildChangesetObjectVersion(legacy, &ChangesetObjectItem{}, uuid.New(), schemas, "source", nil, false, nil)
	require.NoError(t, err, "a push that omits properties must not re-validate an untouched legacy value")
	assert.Equal(t, "archived", version.Properties["status"])
}

func TestChangesetRelationshipEnforcesEnum(t *testing.T) {
	schemas := &ExtractionSchemas{RelationshipSchemas: map[string]agents.RelationshipSchema{
		relationshipSchemaKey("MEMBER_OF", "Person", "Org"): {
			Properties: map[string]agents.PropertyDef{
				"role": {Type: "string", Enum: []any{"owner", "member"}},
			},
		},
	}}

	validated, err := validateChangesetRelationshipProperties(
		map[string]any{"role": "owner"}, schemas, "MEMBER_OF", "Person", "Org")
	require.NoError(t, err)
	assert.Equal(t, "owner", validated["role"])

	_, err = validateChangesetRelationshipProperties(
		map[string]any{"role": "admin"}, schemas, "MEMBER_OF", "Person", "Org")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role")

	// A source push that omits properties leaves the inherited value alone.
	inherited := map[string]any{"role": "admin"}
	got, err := validateChangesetRelationshipProperties(inherited, nil, "MEMBER_OF", "Person", "Org")
	require.NoError(t, err)
	assert.Equal(t, "admin", got["role"])
}
