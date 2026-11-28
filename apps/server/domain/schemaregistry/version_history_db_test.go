package schemaregistry_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/schemaregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TypeVersionHistorySuite pins the read path from #1236: after an operator
// repair carries another registry row's version history onto the winning row,
// the history endpoint must return the carried versions even though their
// stored type_name still holds the old spelling.
type TypeVersionHistorySuite struct {
	testutil.BaseSuite
}

func TestTypeVersionHistorySuite(t *testing.T) {
	suite.Run(t, new(TypeVersionHistorySuite))
}

func (s *TypeVersionHistorySuite) SetupSuite() {
	s.SetDBSuffix("schemaregistry_version_history")
	s.BaseSuite.SetupSuite()
}

func (s *TypeVersionHistorySuite) TestHistoryIncludesVersionsWrittenUnderOldSpelling() {
	s.SkipIfExternalServer("requires direct database access to seed version rows")

	registryID := uuid.NewString()

	// The registry row is at version 3; the trigger records that snapshot.
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.project_object_schema_registry
			(id, project_id, type_name, source, schema_version, json_schema, enabled, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, 'Person', 'custom', 3, '{"type":"object"}'::jsonb, true, NOW(), NOW())
	`, registryID, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Simulate the repair: two older snapshots now belong to the winning
	// registry row but keep the spelling in force when they were written.
	for _, version := range []int{1, 2} {
		_, err = s.DB().NewRaw(`
			INSERT INTO kb.project_object_schema_registry_versions
				(registry_id, project_id, type_name, schema_version, source, json_schema, enabled, created_at)
			VALUES (?::uuid, ?::uuid, 'person', ?, 'custom', '{"type":"object"}'::jsonb, true, NOW())
		`, registryID, s.ProjectID, version).Exec(s.Ctx)
		s.Require().NoError(err)
	}

	repo := schemaregistry.NewRepository(s.DB())
	history, err := repo.GetTypeVersionHistory(s.Ctx, s.ProjectID, "Person")
	s.Require().NoError(err)

	s.Require().Len(history.Versions, 3, "carried versions keyed by registry_id must not be dropped")
	s.Equal(3, history.CurrentVersion)

	s.Equal(3, history.Versions[0].SchemaVersion)
	s.Equal("Person", history.Versions[0].TypeName)
	s.True(history.Versions[0].Active)

	s.Equal(2, history.Versions[1].SchemaVersion)
	s.Equal("person", history.Versions[1].TypeName)
	s.False(history.Versions[1].Active)

	s.Equal(1, history.Versions[2].SchemaVersion)
	s.Equal("person", history.Versions[2].TypeName)
	s.False(history.Versions[2].Active)
}
