package schemaregistry_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/schemaregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// RelationshipTypeUpsertSuite pins the repository-level round trip for #535:
// editing a relationship type's metadata through UpsertRelationshipType must
// not drop the declaration's property definitions (which write-time enum
// validation relies on).
type RelationshipTypeUpsertSuite struct {
	testutil.BaseSuite
}

func TestRelationshipTypeUpsertSuite(t *testing.T) {
	suite.Run(t, new(RelationshipTypeUpsertSuite))
}

func (s *RelationshipTypeUpsertSuite) SetupSuite() {
	s.SetDBSuffix("schemaregistry_reltype_upsert")
	s.BaseSuite.SetupSuite()
}

// seedActiveSchema installs one active schema for the suite's project with the
// given relationship_type_schemas document.
func (s *RelationshipTypeUpsertSuite) seedActiveSchema(schemaID, relationshipJSON string) {
	s.T().Helper()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, object_type_schemas, relationship_type_schemas, created_at, updated_at)
		VALUES (?::uuid, 'reltype-upsert-pack', '1.0.0', '[]'::jsonb, ?::jsonb, NOW(), NOW())
	`, schemaID, relationshipJSON).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.project_schemas (id, project_id, schema_id, active, installed_at, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, ?::uuid, true, NOW(), NOW(), NOW())
	`, uuid.NewString(), s.ProjectID, schemaID).Exec(s.Ctx)
	s.Require().NoError(err)
}

func (s *RelationshipTypeUpsertSuite) storedRelationshipTypes(schemaID string) map[string]schemaregistry.RelationshipSchema {
	s.T().Helper()
	var row struct {
		RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas"`
	}
	err := s.DB().NewRaw(
		`SELECT relationship_type_schemas FROM kb.graph_schemas WHERE id = ?::uuid`, schemaID,
	).Scan(s.Ctx, &row)
	s.Require().NoError(err)

	var schemas map[string]schemaregistry.RelationshipSchema
	s.Require().NoError(json.Unmarshal(row.RelationshipTypeSchemas, &schemas))
	return schemas
}

func (s *RelationshipTypeUpsertSuite) TestUpsertPreservesPropertyDefinitions() {
	schemaID := uuid.NewString()
	s.seedActiveSchema(schemaID, `{
		"MEMBER_OF": {
			"label": "Member of old",
			"inverseType": "HAS_MEMBER",
			"sourceTypes": ["Person"],
			"targetTypes": ["Org"],
			"properties": {
				"role": {"type": "string", "enum": ["owner", "member"]},
				"tags": {"type": "array", "items": {"type": "string", "enum": ["a", "b"]}}
			},
			"required": ["role"]
		}
	}`)

	repo := schemaregistry.NewRepository(s.DB())
	info, err := repo.UpsertRelationshipType(s.Ctx, s.ProjectID, &schemaregistry.CreateRelationshipTypeRequest{
		Name:  "MEMBER_OF",
		Label: "Member of",
	})
	s.Require().NoError(err)
	s.Require().NotNil(info.Label)
	s.Equal("Member of", *info.Label)
	s.Require().NotNil(info.InverseType)
	s.Equal("HAS_MEMBER", *info.InverseType)

	stored := s.storedRelationshipTypes(schemaID)
	rs, ok := stored["MEMBER_OF"]
	s.Require().True(ok, "relationship type must still be present")

	s.Equal("Member of", rs.Label, "request-provided label must be applied")
	s.Equal("HAS_MEMBER", rs.InverseType, "inverseType must survive the upsert")
	s.Equal([]string{"Person"}, rs.GetSourceTypes())
	s.Equal([]string{"Org"}, rs.GetTargetTypes())
	s.Equal([]string{"role"}, rs.Required, "required property list must survive the upsert")

	s.Require().Contains(rs.Properties, "role")
	role := rs.Properties["role"]
	s.Equal("string", role["type"])
	s.Equal([]any{"owner", "member"}, role["enum"], "role enum must survive the upsert")

	s.Require().Contains(rs.Properties, "tags")
	tags := rs.Properties["tags"]
	s.Equal("array", tags["type"])
	items, ok := tags["items"].(map[string]any)
	s.Require().True(ok, "items declaration must survive the upsert, got %#v", tags["items"])
	s.Equal([]any{"a", "b"}, items["enum"])
}
