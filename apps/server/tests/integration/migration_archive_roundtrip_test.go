package integration

// BlueprintRollbackArchiveSuite is the end-to-end regression for issue #513:
// POST /api/schemas/projects/:projectId/migrate/rollback restored zero objects
// because Repository.List omitted the migration_archive column, so every object
// was skipped. It drives the real HTTP routes (execute then rollback) against a
// live database and asserts the dropped property is archived, then restored.
//
// It runs against either an in-process test server or an external one via
// TEST_SERVER_URL. Direct DB assertions require the in-process server (or a
// reachable DB with POSTGRES_PORT set).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func TestBlueprintRollbackArchiveSuite(t *testing.T) {
	suite.Run(t, new(BlueprintRollbackArchiveSuite))
}

type BlueprintRollbackArchiveSuite struct {
	testutil.BaseSuite
}

func (s *BlueprintRollbackArchiveSuite) SetupSuite() {
	s.SetDBSuffix("blprb")
	s.BaseSuite.SetupSuite()
}

type archiveGraphRow struct {
	Properties       json.RawMessage `bun:"properties"`
	MigrationArchive json.RawMessage `bun:"migration_archive"`
}

func (s *BlueprintRollbackArchiveSuite) readObject() archiveGraphRow {
	s.T().Helper()
	s.Require().NotNil(s.DB(), "test requires direct database access")
	var rows []archiveGraphRow
	err := s.DB().NewRaw(`
		SELECT properties, migration_archive
		FROM kb.graph_objects
		WHERE project_id = uuid(?) AND type = 'Doc'
	`, s.ProjectID).Scan(s.Ctx, &rows)
	s.Require().NoError(err, "read graph object")
	s.Require().Len(rows, 1, "expected exactly one Doc object")
	return rows[0]
}

func (s *BlueprintRollbackArchiveSuite) createPack(name, version, objectSchemas string) string {
	s.T().Helper()
	resp := s.Client.POST("/api/schemas",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":                    name,
			"version":                 version,
			"description":             "migration archive roundtrip fixture",
			"objectTypeSchemas":       json.RawMessage(objectSchemas),
			"relationshipTypeSchemas": []any{},
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "create pack %s@%s: %s", name, version, resp.Body)
	var created map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &created), "parse pack response")
	id, _ := created["id"].(string)
	s.Require().NotEmpty(id, "created pack id")
	return id
}

// TestRollbackRestoresArchivedProperty drives a 1.0.0 -> 2.0.0 migration that
// drops a property, then rolls back and asserts the property is restored and
// the archive entry consumed.
func (s *BlueprintRollbackArchiveSuite) TestRollbackRestoresArchivedProperty() {
	packName := "archive-rt-" + uuid.NewString()[:8]

	fromPackID := s.createPack(packName, "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	toPackID := s.createPack(packName, "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	// Install the from-version so the Doc type is registered for the project.
	assignResp := s.Client.POST(fmt.Sprintf("/api/schemas/projects/%s/assign", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"schema_id": fromPackID, "dry_run": false, "merge": false}),
	)
	s.Require().Equal(http.StatusCreated, assignResp.StatusCode, "assign pack: %s", assignResp.Body)

	createResp := s.Client.POST("/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type":       "Doc",
			"key":        "doc-1",
			"properties": map[string]any{"title": "the doc", "alpha": "must survive rollback"},
			"labels":     []string{},
		}),
	)
	s.Require().Equal(http.StatusCreated, createResp.StatusCode, "create object: %s", createResp.Body)

	// ---- Forward migration drops alpha, archiving it. ------------------
	execResp := s.Client.POST(fmt.Sprintf("/api/schemas/projects/%s/migrate/execute", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"from_schema_id": fromPackID,
			"to_schema_id":   toPackID,
			"force":          true,
		}),
	)
	s.Require().Equal(http.StatusOK, execResp.StatusCode, "execute migration: %s", execResp.Body)
	var execResult struct {
		ObjectsMigrated int `json:"objects_migrated"`
	}
	s.Require().NoError(json.Unmarshal(execResp.Body, &execResult), "parse execute response")
	s.Assert().Equal(1, execResult.ObjectsMigrated, "forward migration: %s", execResp.Body)

	afterMigrate := s.readObject()
	var migratedProps map[string]any
	s.Require().NoError(json.Unmarshal(afterMigrate.Properties, &migratedProps))
	s.Assert().NotContains(migratedProps, "alpha", "dropped property must leave properties")
	var archive []map[string]any
	s.Require().NoError(json.Unmarshal(afterMigrate.MigrationArchive, &archive))
	s.Require().Len(archive, 1, "dropped property must be archived")
	s.Assert().Equal("2.0.0", archive[0]["to_version"])

	// ---- Rollback restores alpha and consumes the archive entry. --------
	rbResp := s.Client.POST(fmt.Sprintf("/api/schemas/projects/%s/migrate/rollback", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"to_version": "2.0.0"}),
	)
	s.Require().Equal(http.StatusOK, rbResp.StatusCode, "rollback: %s", rbResp.Body)
	var rbResult struct {
		ObjectsRestored int `json:"objects_restored"`
	}
	s.Require().NoError(json.Unmarshal(rbResp.Body, &rbResult), "parse rollback response")
	// The buggy behaviour returned 0 here while leaving the property dropped.
	s.Assert().Equal(1, rbResult.ObjectsRestored, "rollback must restore the object: %s", rbResp.Body)

	afterRollback := s.readObject()
	var restoredProps map[string]any
	s.Require().NoError(json.Unmarshal(afterRollback.Properties, &restoredProps))
	s.Assert().Equal("must survive rollback", restoredProps["alpha"], "archived property must be restored")
	s.Assert().Equal("the doc", restoredProps["title"])
	var emptied []map[string]any
	s.Require().NoError(json.Unmarshal(afterRollback.MigrationArchive, &emptied))
	s.Assert().Empty(emptied, "consumed archive entry must be removed")
}
