package suites

// BackupsImportSuite exercises the backup archive import feature (PR #610).
//
// This suite is opt-in: it requires a server with object storage (MinIO)
// enabled, which the default e2e harness does not provision. Run it with
// BACKUP_IMPORT_E2E=1 (see SetupSuite). The round-trip under test is
// create -> download -> import -> clone:
//
//  1. Create a real backup of the default project and wait for it to reach
//     status "ready".
//  2. Download the backup archive bytes through the presigned-URL redirect.
//  3. Re-upload those bytes to the import endpoint, which validates the ZIP
//     manifest/checksums and registers a ready, imported backup whose
//     projectId is the archive's source project.
//  4. Clone-restore the imported backup into a new project and assert the clone
//     exists with the expected org and a distinct project id.
//
// The negative cases assert the endpoint's rejection contract: non-ZIP (415),
// ZIP-magic-but-invalid archive (400), non-member (403), and missing file (400).

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/emergent/api-tests/testutil"
)

// BackupsImportSuite tests backup archive import and clone restore.
type BackupsImportSuite struct {
	BaseSuite

	backupIDs  []string // kb.backups rows created during a test (source + imported)
	projectIDs []string // kb.projects rows created by clone restore
	storage    *testutil.Storage
}

func TestBackupsImportSuite(t *testing.T) {
	RunSuite(t, new(BackupsImportSuite))
}

// SetupSuite gates the whole suite behind an explicit opt-in. The round-trip
// under test requires object storage (MinIO), which the default e2e harness
// does not provision; without this gate TestImportBackupRoundTrip would create
// a backup, poll for 60s, and fail the entire run. Enable the suite with
// BACKUP_IMPORT_E2E=1 once the harness provisions MinIO (and sets
// STORAGE_ENDPOINT / STORAGE_ACCESS_KEY / STORAGE_SECRET_KEY).
func (s *BackupsImportSuite) SetupSuite() {
	if os.Getenv("BACKUP_IMPORT_E2E") != "1" {
		s.T().Skip("storage-backed backup import e2e is opt-in; set BACKUP_IMPORT_E2E=1 to run")
	}
	s.BaseSuite.SetupSuite()

	// Best-effort object-storage client for teardown cleanup.
	s.storage, _ = testutil.NewStorage(testutil.LoadStorageConfig())
}

// SetupTest runs before each test.
func (s *BackupsImportSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.backupIDs = nil
	s.projectIDs = nil
}

// TearDownTest deletes backups and cloned projects created during the test so
// reruns are idempotent. Deleting a backup cascades to kb.restores; deleting a
// project cascades to its documents/chunks/memberships.
//
// The backup DELETE endpoint soft-deletes only (it never removes the archive
// from object storage), so the raw SQL row deletes would orphan every source
// and imported ZIP. The storage object is removed first, best-effort.
func (s *BackupsImportSuite) TearDownTest() {
	ctx := s.Ctx
	orgID := testutil.DefaultTestOrg.ID

	for _, id := range uniqueStrings(s.backupIDs) {
		if s.storage != nil {
			_ = s.storage.DeleteBackup(ctx, orgID, id)
		}
		_, _ = s.DB.NewRaw(`DELETE FROM kb.backups WHERE id = ?`, id).Exec(ctx)
	}
	for _, id := range uniqueStrings(s.projectIDs) {
		_, _ = s.DB.NewRaw(`DELETE FROM kb.projects WHERE id = ?`, id).Exec(ctx)
	}
}

// uniqueStrings deduplicates and drops empty entries, so teardown tolerates a
// clone id recorded both from the restore response and from a name-based query.
func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// importPath returns the import endpoint path for the default org.
func (s *BackupsImportSuite) importPath() string {
	return "/api/v1/organizations/" + testutil.DefaultTestOrg.ID + "/backups/import"
}

// pollBackupReady polls the backup until it reaches "ready" (or timeout).
// It returns the last body so callers can report it on failure.
func (s *BackupsImportSuite) pollBackupReady(backupID string, timeout time.Duration) map[string]any {
	path := "/api/v1/organizations/" + testutil.DefaultTestOrg.ID + "/backups/" + backupID
	deadline := time.Now().Add(timeout)

	var last map[string]any
	for time.Now().Before(deadline) {
		resp, err := s.Client.GET(path, s.AdminAuth())
		if err == nil {
			body, jerr := resp.JSONMap()
			if jerr == nil {
				last = body
				if status, _ := body["status"].(string); status == "ready" {
					return body
				}
			}
		}
		time.Sleep(time.Second)
	}
	return last
}

// pollRestoreDone polls the restore job until it reaches a terminal status.
func (s *BackupsImportSuite) pollRestoreDone(restoreID string, timeout time.Duration) map[string]any {
	path := "/api/v1/restores/" + restoreID
	deadline := time.Now().Add(timeout)

	var last map[string]any
	for time.Now().Before(deadline) {
		resp, err := s.Client.GET(path, s.AdminAuth())
		if err == nil {
			body, jerr := resp.JSONMap()
			if jerr == nil {
				last = body
				if status, _ := body["status"].(string); status == "completed" || status == "failed" {
					return body
				}
			}
		}
		time.Sleep(time.Second)
	}
	return last
}

// TestImportBackupRoundTrip runs the create -> download -> import -> clone flow
// using a real archive produced by the server (no hand-rolled manifest).
func (s *BackupsImportSuite) TestImportBackupRoundTrip() {
	orgID := testutil.DefaultTestOrg.ID
	projectID := testutil.DefaultTestProject.ID

	// 1. Create a backup of the default project.
	createResp, err := s.Client.POST("/api/v1/projects/"+projectID+"/backups", map[string]any{},
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusAccepted, createResp.StatusCode, "create backup: %s", createResp.BodyString())

	createBody, err := createResp.JSONMap()
	s.Require().NoError(err)
	sourceBackupID, ok := createBody["id"].(string)
	s.Require().True(ok && sourceBackupID != "", "create backup must return an id: %s", createResp.BodyString())
	s.backupIDs = append(s.backupIDs, sourceBackupID)

	// Poll until the backup reaches "ready".
	readyBody := s.pollBackupReady(sourceBackupID, 60*time.Second)
	status, _ := readyBody["status"].(string)
	s.Require().Equal("ready", status, "backup never became ready, last body: %v", readyBody)

	// 2. Download the archive bytes. The client follows the 302 to the
	// presigned MinIO URL automatically; resp.Body() is the ZIP.
	downloadResp, err := s.Client.GET("/api/v1/organizations/"+orgID+"/backups/"+sourceBackupID+"/download", s.AdminAuth())
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, downloadResp.StatusCode, "download should follow redirect to 200, got %d: %s", downloadResp.StatusCode, downloadResp.BodyString())

	archiveBytes := downloadResp.Body()
	s.Require().True(bytes.HasPrefix(archiveBytes, []byte("PK")), "downloaded archive must be a ZIP (PK magic), got %q", archiveBytes[:min(len(archiveBytes), 8)])

	// 3. Import the archive bytes.
	importResp, err := s.Client.PostMultipart(s.importPath(), "file", "backup.zip", archiveBytes, s.AdminAuth())
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, importResp.StatusCode, "import: %s", importResp.BodyString())

	importBody, err := importResp.JSONMap()
	s.Require().NoError(err)
	s.Equal("ready", importBody["status"], "imported backup status: %s", importResp.BodyString())
	s.Equal(true, importBody["imported"], "imported backup must be flagged imported: %s", importResp.BodyString())
	s.Equal(projectID, importBody["projectId"], "imported backup projectId must be the archive source project: %s", importResp.BodyString())

	sizeBytes, ok := importBody["sizeBytes"].(float64)
	s.Require().True(ok, "imported backup must include numeric sizeBytes: %s", importResp.BodyString())
	s.Require().Greater(sizeBytes, float64(0), "imported backup sizeBytes must be > 0")

	importedBackupID, ok := importBody["id"].(string)
	s.Require().True(ok && importedBackupID != "", "imported backup must return an id: %s", importResp.BodyString())
	s.backupIDs = append(s.backupIDs, importedBackupID)

	// 4. Clone restore the imported backup into a new project.
	targetName := fmt.Sprintf("import-clone-%d", time.Now().UnixNano())
	restoreResp, err := s.Client.POST("/api/v1/organizations/"+orgID+"/restore", map[string]any{
		"backupId":          importedBackupID,
		"targetProjectName": targetName,
	}, s.AdminAuth())
	s.Require().NoError(err)
	s.Require().Equal(http.StatusAccepted, restoreResp.StatusCode, "clone restore: %s", restoreResp.BodyString())

	restoreBody, err := restoreResp.JSONMap()
	s.Require().NoError(err)
	restoreID, ok := restoreBody["id"].(string)
	s.Require().True(ok && restoreID != "", "clone restore must return an id: %s", restoreResp.BodyString())

	restoreDone := s.pollRestoreDone(restoreID, 90*time.Second)
	restoreStatus, _ := restoreDone["status"].(string)
	if restoreStatus == "failed" {
		s.Require().FailNow("clone restore failed", "restore errorMessage: %v", restoreDone["errorMessage"])
	}
	s.Require().Equal("completed", restoreStatus, "clone restore did not complete: %v", restoreDone)

	// 5. The completed clone restore created a new project. Record every
	// created project id for teardown BEFORE asserting, so a failed assertion
	// (or a response that omits targetProjectId) never leaks the clone.
	targetProjectID, ok := restoreDone["targetProjectId"].(string)

	// The target name is unique per run, so look the clone up by name rather
	// than trusting the response field alone for cleanup.
	var created []string
	err = s.DB.NewRaw(`SELECT id FROM kb.projects WHERE name = ?`, targetName).Scan(s.Ctx, &created)
	s.Require().NoError(err)
	s.projectIDs = append(s.projectIDs, created...)

	s.Require().True(ok && targetProjectID != "", "completed clone restore must report targetProjectId: %v", restoreDone)

	// Assert the clone produced a new project with the expected org.
	var rows []struct {
		ID             string `bun:"id"`
		OrganizationID string `bun:"organization_id"`
	}
	err = s.DB.NewRaw(`SELECT id, organization_id FROM kb.projects WHERE id = ?`, targetProjectID).Scan(s.Ctx, &rows)
	s.Require().NoError(err)
	s.Require().Len(rows, 1, "clone should create exactly one project with id %q", targetProjectID)
	s.Equal(orgID, rows[0].OrganizationID, "clone project must belong to the target org")
	s.NotEqual(projectID, rows[0].ID, "clone project id must differ from the source project id")

	// 6. #592 regression assertions on the clone's schema links.
	//
	// Clone-restore skips kb.project_schemas rows whose schema_id cannot be
	// resolved through the clone id-remap table (those reference the SOURCE
	// deployment's global builtin kb.graph_schemas UUID, absent from a
	// project-scoped archive). The target's own trg_projects_install_builtins
	// trigger then links the new clone to the TARGET's builtin row. Within a
	// single deployment the source/target builtin UUIDs coincide, so the
	// cross-deployment UUID divergence itself is covered only by the DB
	// integration test apps/server/domain/backups/restorer_clone_db_test.go;
	// this API suite proves the link resolves and is never dangling.

	// (a) The clone links at least one builtin schema via the target's own
	// builtin graph_schemas row (target provisioning produced the link, not the
	// archive's foreign builtin UUID).
	var builtinLinks []string
	err = s.DB.NewRaw(`
		SELECT gs.id
		FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON gs.id = ps.schema_id
		WHERE ps.project_id = ? AND gs.source = 'builtin'
	`, targetProjectID).Scan(s.Ctx, &builtinLinks)
	s.Require().NoError(err)
	s.Require().NotEmpty(builtinLinks, "clone must link at least one builtin schema (target provisioning)")

	// (b) No kb.project_schemas row for the clone references a schema_id with no
	// matching kb.graph_schemas.id — i.e. no dangling/foreign schema link. This
	// is the exact #592 failure mode (project_schemas_schema_id_fkey).
	var dangling int
	err = s.DB.NewRaw(`
		SELECT count(*)
		FROM kb.project_schemas ps
		LEFT JOIN kb.graph_schemas gs ON gs.id = ps.schema_id
		WHERE ps.project_id = ? AND gs.id IS NULL
	`, targetProjectID).Scan(s.Ctx, &dangling)
	s.Require().NoError(err)
	s.Require().Zero(dangling, "clone must have no dangling project_schemas schema links (#592)")
}

// TestImportRejectsNonZip asserts a non-ZIP upload is rejected with 415.
func (s *BackupsImportSuite) TestImportRejectsNonZip() {
	resp, err := s.Client.PostMultipart(s.importPath(), "file", "x.txt", []byte("not a zip"), s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusUnsupportedMediaType, resp.StatusCode, "non-ZIP upload: %s", resp.BodyString())
}

// TestImportRejectsInvalidZip asserts a ZIP-magic-but-invalid archive is
// rejected with 400 (passes the magic check, fails manifest/checksum parsing).
func (s *BackupsImportSuite) TestImportRejectsInvalidZip() {
	payload := append([]byte("PK\x03\x04"), []byte("this is not a real zip archive, just garbage after the magic")...)
	resp, err := s.Client.PostMultipart(s.importPath(), "file", "backup.zip", payload, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode, "invalid ZIP upload: %s", resp.BodyString())
}

// TestImportRejectsNonMember asserts a user who is not an org member gets 403.
func (s *BackupsImportSuite) TestImportRejectsNonMember() {
	resp, err := s.Client.PostMultipart(s.importPath(), "file", "backup.zip", []byte("PK\x03\x04junk"), s.AllScopesAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusForbidden, resp.StatusCode, "non-member import: %s", resp.BodyString())
}

// TestImportRejectsMissingFile asserts a multipart body with no `file` field is
// rejected with 400.
func (s *BackupsImportSuite) TestImportRejectsMissingFile() {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	s.Require().NoError(writer.Close())

	resp, err := s.Client.PostMultipartRaw(s.importPath(), writer.FormDataContentType(), buf.Bytes(), s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode, "missing file upload: %s", resp.BodyString())
}
