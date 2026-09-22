// Package api_test — backup_helpers_test.go
//
// Durable verification affordances for the backup-restore feature (see
// openspec/changes/backup-restore/verification.md §3).
//
//   - seedRichProject   deterministically creates a "rich" project (documents
//     with real MinIO-backed files, deterministic chunk rows with embeddings,
//     graph objects + relationships, an object-type schema, a branch, an
//     agent, a project-owned skill, a product version + tag, and project
//     memberships) through the existing HTTP helpers and direct Postgres
//     inserts (via framework.MemoryDB()).
//
//   - projectFingerprint returns a normalized per-table fingerprint: for every
//     curated table a row count plus a content-multiset hash (SHA-256 of the
//     sorted per-row hashes computed over content columns). Primary keys,
//     foreign keys, project/org scoping columns, self-identity columns
//     (canonical_id / supersedes_id / merged_to_canonical_id), derived search
//     columns (tsv/fts) and audit timestamps are excluded so the fingerprint
//     is stable across clone ID/FK remapping and importer timestamp
//     regeneration while still detecting every meaningful content change.
//
// The curated table list baked into projectFingerprint lives in
// `fingerprintTables` — the single source of truth reused by every round-trip
// scenario.
//
// Backup/restore are HTTP-only (no CLI), so per Constitution Rule 1 these
// tests drive the HTTP surface with the same helpers branches_test.go uses.
package api_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Timeouts / polling
// ─────────────────────────────────────────────────────────────────────────────

const (
	backupReadyTimeout  = 2 * time.Minute
	restoreDoneTimeout  = 3 * time.Minute
	restorePollInterval = 300 * time.Millisecond
	backupPollInterval  = 500 * time.Millisecond
	settleTimeout       = 60 * time.Second
	settleSampleGap     = 2 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Fingerprint types + curated table list
// ─────────────────────────────────────────────────────────────────────────────

// tableFingerprint is the normalized per-table evidence for a project.
type tableFingerprint struct {
	Count       int64  `json:"count"`
	ContentHash string `json:"contentHash"` // SHA-256 hex of sorted row-content multiset
}

// projectFP is the normalized per-table fingerprint of a project.
type projectFP struct {
	ProjectID   string                       `json:"projectId"`
	GeneratedAt time.Time                    `json:"generatedAt"`
	Tables      map[string]*tableFingerprint `json:"tables"`
	OverallHash string                       `json:"overallHash"`
}

// fingerprintTableDef declares one curated table to fingerprint.
type fingerprintTableDef struct {
	Schema string
	Table  string
	// Join is an optional SQL join clause appended to "FROM <schema>.<table> c"
	// (c is the fixed alias of the fingerprinted table). Empty = no join.
	Join string
	// FilterColumn is the column (optionally alias-qualified) to filter by
	// project. Empty defaults to "c.project_id" (direct project_id column).
	FilterColumn  string
	ExtraExcluded []string // additional non-content columns for this table
}

func (d fingerprintTableDef) qualified() string { return d.Schema + "." + d.Table }
func (d fingerprintTableDef) key() string       { return d.Schema + "." + d.Table }

// fromClause renders the FROM clause: the table aliased as "c", plus any join.
func (d fingerprintTableDef) fromClause() string {
	from := d.qualified() + " c"
	if d.Join != "" {
		from += " " + d.Join
	}
	return from
}

// projectFilter returns the SQL expression that scopes rows to a project.
func (d fingerprintTableDef) projectFilter() string {
	if d.FilterColumn != "" {
		return d.FilterColumn
	}
	return "c.project_id"
}

// fingerprintTables is the curated table list the backup-restore round-trip
// evidence is computed over. It mirrors the seeded surface: documents (with
// files), chunks, graph objects + relationships, object-type schemas,
// branches, agents, project-owned skills, product versions + tags, and
// project memberships. Report this list back when reconciling against the
// server implementation lane.
var fingerprintTables = []fingerprintTableDef{
	{Schema: "kb", Table: "documents", ExtraExcluded: []string{
		"parent_document_id", "external_source_id", "data_source_integration_id",
	}},
	// kb.chunks has no project_id column — it belongs to a project through its
	// document, so scope it via a join on kb.documents.
	{Schema: "kb", Table: "chunks", Join: "INNER JOIN kb.documents d ON d.id = c.document_id",
		FilterColumn: "d.project_id", ExtraExcluded: []string{"tsv"}},
	{Schema: "kb", Table: "graph_objects", ExtraExcluded: []string{
		"fts", "actor_id", "reviewed_by", "branch_id", "extraction_job_id",
	}},
	{Schema: "kb", Table: "graph_relationships", ExtraExcluded: []string{"branch_id", "src_id", "dst_id"}},
	{Schema: "kb", Table: "object_type_schemas"},
	{Schema: "kb", Table: "branches", ExtraExcluded: []string{"parent_branch_id"}},
	{Schema: "kb", Table: "agents", ExtraExcluded: []string{"agent_definition_id"}},
	{Schema: "kb", Table: "skills"},
	{Schema: "kb", Table: "product_versions"},
	{Schema: "kb", Table: "tags", ExtraExcluded: []string{"product_version_id"}},
	{Schema: "kb", Table: "project_memberships"},
}

// fingerprintExcludedAlways lists columns excluded from row content hashing for
// every table: the primary key, project/org scoping, self-identity columns
// that the clone importer remaps, and audit timestamps whose regeneration by
// the importer must not count as a fidelity difference.
var fingerprintExcludedAlways = map[string]bool{
	"id": true, "project_id": true, "org_id": true,
	"canonical_id": true, "supersedes_id": true, "merged_to_canonical_id": true,
	"created_at": true, "updated_at": true, "deleted_at": true,
	"completed_at": true, "expires_at": true, "last_run_at": true,
	"embedding_updated_at": true, "valid_from": true, "valid_to": true,
	"reviewed_at": true, "conversion_completed_at": true, "merged_at": true,
}

// ─────────────────────────────────────────────────────────────────────────────
// projectFingerprint implementation
// ─────────────────────────────────────────────────────────────────────────────

// projectFingerprint computes the normalized per-table fingerprint for
// projectID. Callers must have already obtained a DB handle via
// framework.MemoryDB(). On DB failure the test is failed through the runlog.
func projectFingerprint(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID string) *projectFP {
	t.Helper()
	fp, err := fingerprintCore(db, projectID)
	if err != nil {
		rl.Failf("projectFingerprint(%s): %v", projectID, err)
	}
	var parts []string
	for _, def := range fingerprintTables {
		tf := fp.Tables[def.key()]
		parts = append(parts, fmt.Sprintf("%s: count=%d hash=%s", def.key(), tf.Count, shortHash(tf.ContentHash)))
	}
	rl.Printf("fingerprint %s overall=%s :: %s", projectID, shortHash(fp.OverallHash), strings.Join(parts, "; "))
	return fp
}

func fingerprintCore(db *sql.DB, projectID string) (*projectFP, error) {
	fp := &projectFP{
		ProjectID:   projectID,
		GeneratedAt: time.Now().UTC(),
		Tables:      make(map[string]*tableFingerprint, len(fingerprintTables)),
	}
	for _, def := range fingerprintTables {
		tf, err := fingerprintTable(db, def, projectID)
		if err != nil {
			return nil, fmt.Errorf("fingerprint %s: %w", def.qualified(), err)
		}
		fp.Tables[def.key()] = tf
	}
	fp.OverallHash = overallMultisetHash(fp)
	return fp, nil
}

func fingerprintTable(db *sql.DB, def fingerprintTableDef, projectID string) (*tableFingerprint, error) {
	// 1. Column list in ordinal order.
	cols, err := queryStrings(db,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`,
		def.Schema, def.Table)
	if err != nil {
		return nil, err
	}

	// 2. PK + FK columns (excluded from content hashing).
	excluded := make(map[string]bool, len(fingerprintExcludedAlways)+len(def.ExtraExcluded))
	for c := range fingerprintExcludedAlways {
		excluded[c] = true
	}
	for _, c := range def.ExtraExcluded {
		excluded[c] = true
	}
	pkCols, err := constraintColumns(db, def, "PRIMARY KEY")
	if err != nil {
		return nil, err
	}
	fkCols, err := constraintColumns(db, def, "FOREIGN KEY")
	if err != nil {
		return nil, err
	}
	for _, c := range append(pkCols, fkCols...) {
		excluded[c] = true
	}

	// 3. Content columns = everything else.
	var contentCols []string
	for _, c := range cols {
		if !excluded[c] {
			contentCols = append(contentCols, c)
		}
	}

	// 4. Row count.
	var count int64
	if err := db.QueryRow(
		`SELECT count(*) FROM `+def.fromClause()+` WHERE `+def.projectFilter()+` = $1`, projectID,
	).Scan(&count); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	tf := &tableFingerprint{Count: count}
	if len(contentCols) == 0 {
		tf.ContentHash = sha256Hex(nil)
		return tf, nil
	}

	// 5. Per-row content hashes, sorted into a multiset hash. Columns are
	// alias-qualified with the base table alias "c" so joined tables sharing
	// column names (e.g. documents) cannot cause ambiguity.
	quoted := make([]string, len(contentCols))
	for i, c := range contentCols {
		quoted[i] = `c."` + c + `"`
	}
	rows, err := db.Query(
		`SELECT `+strings.Join(quoted, ", ")+` FROM `+def.fromClause()+` WHERE `+def.projectFilter()+` = $1`, projectID)
	if err != nil {
		return nil, fmt.Errorf("select content: %w", err)
	}
	defer rows.Close()

	rowHashes := make([]string, 0, count)
	dest := make([]any, len(contentCols))
	ptrs := make([]any, len(contentCols))
	for rows.Next() {
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		parts := make([]string, len(contentCols))
		for i, v := range dest {
			s := valueString(v)
			parts[i] = fmt.Sprintf("%d:%s", len(s), s)
		}
		rowHashes = append(rowHashes, sha256Hex([]byte(strings.Join(parts, "\x1f"))))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	sort.Strings(rowHashes)
	h := sha256.New()
	for _, rh := range rowHashes {
		fmt.Fprintf(h, "%d:%s\n", len(rh), rh)
	}
	tf.ContentHash = hex.EncodeToString(h.Sum(nil))
	return tf, nil
}

// constraintColumns returns column names participating in a given constraint
// type (PRIMARY KEY or FOREIGN KEY) for the table.
func constraintColumns(db *sql.DB, def fingerprintTableDef, constraintType string) ([]string, error) {
	return queryStrings(db,
		`SELECT kcu.column_name
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON kcu.constraint_name = tc.constraint_name
		  AND kcu.table_schema = tc.table_schema
		 WHERE tc.table_schema = $1 AND tc.table_name = $2 AND tc.constraint_type = $3`,
		def.Schema, def.Table, constraintType)
}

func queryStrings(db *sql.DB, q string, args ...any) ([]string, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "\x00"
	case []byte:
		return string(x)
	case string:
		return x
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case bool:
		if x {
			return "1"
		}
		return "0"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	default:
		return fmt.Sprintf("%T:%v", x, x)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// overallMultisetHash hashes the per-table content hashes so the whole
// fingerprint has a single digest.
func overallMultisetHash(fp *projectFP) string {
	var keys []string
	for k := range fp.Tables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		tf := fp.Tables[k]
		fmt.Fprintf(h, "%s count=%d hash=%s\n", k, tf.Count, tf.ContentHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// requireFingerprintsEqual asserts two fingerprints match for every curated
// table. ignore, when non-nil, names tables (schema.table keys) that must not
// be compared (used by the cross-org scenario for project_memberships).
func requireFingerprintsEqual(t *testing.T, rl *framework.RunLog, got, want *projectFP, ignore map[string]bool) {
	t.Helper()
	failed := false
	for _, def := range fingerprintTables {
		if ignore != nil && ignore[def.key()] {
			continue
		}
		g, okG := got.Tables[def.key()]
		w, okW := want.Tables[def.key()]
		switch {
		case !okG || !okW:
			t.Errorf("%s: fingerprint missing (got=%v want=%v)", def.key(), okG, okW)
			failed = true
		case g.Count != w.Count:
			t.Errorf("%s: row count differs: got %d, want %d", def.key(), g.Count, w.Count)
			failed = true
		case g.ContentHash != w.ContentHash:
			t.Errorf("%s: content multiset differs: got %s, want %s", def.key(), shortHash(g.ContentHash), shortHash(w.ContentHash))
			failed = true
		}
	}
	if failed {
		rl.Printf("fingerprint MISMATCH between %s and %s", got.ProjectID, want.ProjectID)
		return
	}
	rl.Printf("fingerprints equal: %s == %s (overall %s)", got.ProjectID, want.ProjectID, shortHash(want.OverallHash))
}

// assertNoSharedUUIDs verifies the clone shares zero entity UUIDs with the
// source project across the curated tables (spec: clone remaps every entity
// UUID and never reuses an original ID).
func assertNoSharedUUIDs(t *testing.T, rl *framework.RunLog, db *sql.DB, sourceID, cloneID string) {
	t.Helper()
	bad := 0
	for _, def := range fingerprintTables {
		srcIDs, err := tableRowIDs(db, def, sourceID)
		if err != nil {
			rl.Failf("shared-uuid check %s (source): %v", def.qualified(), err)
		}
		cloneIDs, err := tableRowIDs(db, def, cloneID)
		if err != nil {
			rl.Failf("shared-uuid check %s (clone): %v", def.qualified(), err)
		}
		cloneSet := make(map[string]bool, len(cloneIDs))
		for _, id := range cloneIDs {
			cloneSet[id] = true
		}
		shared := 0
		for _, id := range srcIDs {
			if cloneSet[id] {
				shared++
			}
		}
		if shared > 0 {
			t.Errorf("%s: %d entity UUID(s) shared between source and clone", def.qualified(), shared)
			bad++
		}
	}
	if bad == 0 {
		rl.Printf("zero shared entity UUIDs between source %s and clone %s", sourceID, cloneID)
	}
}

// tableRowIDs returns the id of every row of def belonging to projectID, using
// the same fromClause/projectFilter scoping as the fingerprint queries so
// tables without a direct project_id column (kb.chunks) are handled too.
func tableRowIDs(db *sql.DB, def fingerprintTableDef, projectID string) ([]string, error) {
	rows, err := db.Query(
		`SELECT c.id FROM `+def.fromClause()+` WHERE `+def.projectFilter()+` = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Logged HTTP helpers (org-scoped)
// ─────────────────────────────────────────────────────────────────────────────

// doOrgAPILogged is like doAPILogged but for org-scoped routes: it adds an
// X-Org-ID header and logs an http_call event to the runlog.
func doOrgAPILogged(t *testing.T, rl *framework.RunLog, method, path, orgID string, reqBody []byte) *http.Response {
	t.Helper()
	resp := doAPIWithOrg(t, method, path, e2eTestToken(), "", orgID, reqBody)

	details := map[string]any{
		"method":      method,
		"url":         apiURL(path),
		"org_id":      orgID,
		"status_code": resp.StatusCode,
	}
	if len(reqBody) > 0 {
		var parsed any
		if json.Unmarshal(reqBody, &parsed) == nil {
			details["request_body"] = parsed
		}
	}

	bodyStr := framework.ReadBody(t, resp)
	var parsed any
	if json.Unmarshal([]byte(bodyStr), &parsed) == nil {
		details["response_body"] = parsed
	} else if len(bodyStr) < 500 {
		details["response_body"] = bodyStr
	}
	resp.Body = io.NopCloser(strings.NewReader(bodyStr))

	rl.Event("http_call", fmt.Sprintf("%s %s", method, path), details)
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// Backup / restore wire types
// ─────────────────────────────────────────────────────────────────────────────

// backupRecord mirrors the Backup entity returned by the backups API.
type backupRecord struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId"`
	ProjectID      string         `json:"projectId"`
	ProjectName    string         `json:"projectName"`
	Status         string         `json:"status"`
	Progress       int            `json:"progress"`
	ErrorMessage   *string        `json:"errorMessage,omitempty"`
	BackupType     string         `json:"backupType,omitempty"`
	Includes       map[string]any `json:"includes,omitempty"`
	Stats          map[string]any `json:"stats,omitempty"`
	CreatedAt      string         `json:"createdAt,omitempty"`
}

// restoreRecord mirrors the Restore job entity (design.md decision 10).
type restoreRecord struct {
	ID              string         `json:"id"`
	OrganizationID  string         `json:"organizationId"`
	BackupID        string         `json:"backupId"`
	Mode            string         `json:"mode"`
	SourceProjectID string         `json:"sourceProjectId"`
	TargetProjectID string         `json:"targetProjectId"`
	Status          string         `json:"status"`
	Progress        int            `json:"progress"`
	ErrorMessage    *string        `json:"errorMessage,omitempty"`
	Stats           map[string]any `json:"stats,omitempty"`
	CreatedAt       string         `json:"createdAt,omitempty"`
	CompletedAt     *string        `json:"completedAt,omitempty"`
}

func validRestoreStatus(s string) bool {
	switch s {
	case "pending", "running", "completed", "failed":
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Backup creation / polling
// ─────────────────────────────────────────────────────────────────────────────

// createProjectBackup POSTs to the existing backup-creation endpoint and
// returns the backup ID (HTTP 202).
func createProjectBackup(t *testing.T, rl *framework.RunLog, projectID string) string {
	t.Helper()
	body := jsonBody(map[string]any{
		"includeDeleted": false,
		"includeChat":    false,
		"retentionDays":  30,
	})
	resp := doAPILogged(t, rl, "POST", "/api/v1/projects/"+projectID+"/backups", e2eTestToken(), projectID, body)
	b := mustStatus(t, resp, http.StatusAccepted)
	var bk backupRecord
	parseBodyJSON(t, b, &bk)
	if bk.ID == "" {
		rl.Failf("create backup: response had no id\nbody: %s", b)
	}
	rl.Printf("backup creation accepted: id=%s status=%s", bk.ID, bk.Status)
	return bk.ID
}

// waitBackupReady polls GET /organizations/{orgId}/backups/{backupId} until the
// backup reaches status "ready". Returns the final backup record.
func waitBackupReady(t *testing.T, rl *framework.RunLog, orgID, backupID string) *backupRecord {
	t.Helper()
	deadline := time.Now().Add(backupReadyTimeout)
	var last *backupRecord
	for {
		resp := doOrgAPILogged(t, rl, "GET", "/api/v1/organizations/"+orgID+"/backups/"+backupID, orgID, nil)
		b := mustStatus(t, resp, http.StatusOK)
		var bk backupRecord
		parseBodyJSON(t, b, &bk)
		last = &bk
		rl.Printf("backup %s status=%s progress=%d", backupID, bk.Status, bk.Progress)
		switch bk.Status {
		case "ready":
			return &bk
		case "failed":
			msg := ""
			if bk.ErrorMessage != nil {
				msg = *bk.ErrorMessage
			}
			rl.Failf("backup %s failed: %s", backupID, msg)
		}
		if time.Now().After(deadline) {
			rl.Failf("timed out waiting for backup %s to become ready (last status=%s)", backupID, last.Status)
		}
		time.Sleep(backupPollInterval)
	}
}

// listOrgBackups returns the org's backups, optionally filtered by project.
func listOrgBackups(t *testing.T, rl *framework.RunLog, orgID, projectID string) []backupRecord {
	t.Helper()
	path := "/api/v1/organizations/" + orgID + "/backups"
	if projectID != "" {
		path += "?project_id=" + projectID
	}
	resp := doOrgAPILogged(t, rl, "GET", path, orgID, nil)
	b := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Backups []backupRecord `json:"backups"`
		Total   int            `json:"total"`
	}
	parseBodyJSON(t, b, &result)
	return result.Backups
}

// ─────────────────────────────────────────────────────────────────────────────
// Restore orchestration
// ─────────────────────────────────────────────────────────────────────────────

// startOverwriteRestore POSTs /api/v1/projects/{projectId}/restore in
// overwrite mode. Returns the 202 restore record (status must be "pending").
func startOverwriteRestore(t *testing.T, rl *framework.RunLog, projectID, backupID string) *restoreRecord {
	t.Helper()
	body := jsonBody(map[string]any{
		"backupId":           backupID,
		"mode":               "overwrite",
		"preRestoreSnapshot": true,
		"includeJournal":     false,
	})
	resp := doAPILogged(t, rl, "POST", "/api/v1/projects/"+projectID+"/restore", e2eTestToken(), projectID, body)
	b := mustStatus(t, resp, http.StatusAccepted)
	var job restoreRecord
	parseBodyJSON(t, b, &job)
	assertPendingRestoreRecord(t, rl, &job, "overwrite", backupID)
	return &job
}

// startCloneRestore POSTs /api/v1/organizations/{orgId}/restore in clone mode.
// orgID is the target org (may differ from the backup's source org). Returns
// the 202 restore record.
func startCloneRestore(t *testing.T, rl *framework.RunLog, orgID, backupID, targetProjectName string) *restoreRecord {
	t.Helper()
	body := jsonBody(map[string]any{
		"backupId":          backupID,
		"mode":              "clone",
		"targetProjectName": targetProjectName,
		"includeJournal":    false,
	})
	resp := doOrgAPILogged(t, rl, "POST", "/api/v1/organizations/"+orgID+"/restore", orgID, body)
	b := mustStatus(t, resp, http.StatusAccepted)
	var job restoreRecord
	parseBodyJSON(t, b, &job)
	assertPendingRestoreRecord(t, rl, &job, "clone", backupID)
	return &job
}

func assertPendingRestoreRecord(t *testing.T, rl *framework.RunLog, job *restoreRecord, wantMode, wantBackupID string) {
	t.Helper()
	if job.ID == "" {
		rl.Failf("restore accepted but response had no job id")
	}
	if job.Status != "pending" {
		rl.Failf("expected restore job status %q on 202, got %q (id=%s)", "pending", job.Status, job.ID)
	}
	if job.Mode != wantMode {
		rl.Failf("expected restore mode %q, got %q", wantMode, job.Mode)
	}
	if wantBackupID != "" && job.BackupID != wantBackupID {
		rl.Failf("expected restore backupId %q, got %q", wantBackupID, job.BackupID)
	}
	if job.Progress < 0 || job.Progress > 100 {
		rl.Failf("restore job progress out of range: %d", job.Progress)
	}
	rl.Printf("restore job accepted: id=%s mode=%s status=%s progress=%d", job.ID, job.Mode, job.Status, job.Progress)
}

// getRestoreJob polls GET /api/v1/restores/{restoreId}. orgID supplies the
// X-Org-ID context for the job's owning organization.
func getRestoreJob(t *testing.T, rl *framework.RunLog, orgID, restoreID string) *restoreRecord {
	t.Helper()
	resp := doOrgAPILogged(t, rl, "GET", "/api/v1/restores/"+restoreID, orgID, nil)
	b := mustStatus(t, resp, http.StatusOK)
	var job restoreRecord
	parseBodyJSON(t, b, &job)
	return &job
}

// waitRestoreTerminal polls until the restore job reaches a terminal status.
// Returns the final job and the ordered list of distinct statuses observed via
// GET (the 202 snapshot is passed in for transition accounting).
func waitRestoreTerminal(t *testing.T, rl *framework.RunLog, orgID string, initial *restoreRecord) (*restoreRecord, []string) {
	t.Helper()
	deadline := time.Now().Add(restoreDoneTimeout)
	seen := []string{"pending"} // 202 response always reports pending per contract
	if initial.Status != "pending" {
		seen = []string{initial.Status}
	}
	lastProgress := initial.Progress
	var last *restoreRecord
	for {
		time.Sleep(restorePollInterval)
		job := getRestoreJob(t, rl, orgID, initial.ID)
		last = job

		if !validRestoreStatus(job.Status) {
			rl.Failf("restore %s returned invalid status %q", job.ID, job.Status)
		}
		if job.Progress < 0 || job.Progress > 100 {
			rl.Failf("restore %s returned out-of-range progress %d", job.ID, job.Progress)
		}
		if job.Progress < lastProgress {
			rl.Failf("restore %s progress regressed from %d to %d", job.ID, lastProgress, job.Progress)
		}
		lastProgress = job.Progress

		newStatus := true
		for _, s := range seen {
			if s == job.Status {
				newStatus = false
				break
			}
		}
		if newStatus {
			seen = append(seen, job.Status)
		}
		rl.Printf("restore %s status=%s progress=%d", job.ID, job.Status, job.Progress)

		switch job.Status {
		case "completed":
			rl.Printf("restore %s completed (observed transitions: %s)", job.ID, strings.Join(seen, " -> "))
			return job, seen
		case "failed":
			msg := ""
			if job.ErrorMessage != nil {
				msg = *job.ErrorMessage
			}
			rl.Failf("restore %s failed: %s", job.ID, msg)
		}
		if time.Now().After(deadline) {
			rl.Failf("timed out waiting for restore %s to reach terminal status (last=%s)", job.ID, last.Status)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// seedRichProject — deterministic "rich project" fixture
// ─────────────────────────────────────────────────────────────────────────────

// richProjectSeed captures the IDs a seed created so tests can assert on them.
type richProjectSeed struct {
	OrgID            string
	ProjectID        string
	UploadedDocIDs   []string // documents backed by real MinIO file payloads
	RawDocID         string   // DB-only content document that hosts the deterministic chunk rows
	ObjectIDs        []string
	RelationshipIDs  []string
	ObjectSchemaID   string
	BranchID         string
	AgentID          string
	SkillID          string
	ProductVersionID string
	TagID            string
	RestorerUserID   string
	SecondMemberID   string // "" when no second core user profile exists
}

// seedRichProject creates an org + project and populates it deterministically
// with documents (real file uploads), deterministic chunk rows (with an
// explicit embedding), graph objects + relationships (one with an explicit
// embedding), an object-type schema, a branch, an agent, a project-owned
// skill, a product version + tag, and project memberships. It then waits for
// the project state to settle so async parsing/chunking/embedding workers
// cannot mutate rows between fingerprinting, backup, and restore.
func seedRichProject(t *testing.T, rl *framework.RunLog) *richProjectSeed {
	t.Helper()
	db := mustMemoryDB(t, rl)

	rl.Section("Create org + project")
	projectID, orgID := setupProjectLogged(t, rl)
	shortPID := projectID
	if len(shortPID) > 8 {
		shortPID = projectID[:8]
	}
	seed := &richProjectSeed{OrgID: orgID, ProjectID: projectID}

	rl.Section("Upload seed documents with files")
	alphaContent := "alpha seed report\nline 2 with distinct tokens and words for deterministic content\nfinal line alpha.\n"
	betaContent := "beta seed notes\ndistinct body with different tokens than alpha\nfinal line beta.\n"
	alphaDoc := uploadSeedDocument(t, rl, projectID, fmt.Sprintf("restore-seed-%s-alpha.txt", shortPID), alphaContent)
	betaDoc := uploadSeedDocument(t, rl, projectID, fmt.Sprintf("restore-seed-%s-beta.txt", shortPID), betaContent)
	seed.UploadedDocIDs = []string{alphaDoc, betaDoc}
	rl.Printf("uploaded file documents: %s, %s", alphaDoc, betaDoc)

	rl.Section("Insert content document (DB-only, no parse/file pipeline)")
	rawDoc := insertSeedDocumentRow(t, rl, db, projectID, fmt.Sprintf("restore-seed-%s-raw.txt", shortPID))
	seed.RawDocID = rawDoc
	rl.Printf("inserted DB-only content document: %s", rawDoc)

	rl.Section("Insert deterministic chunk rows (with embeddings)")
	// Chunks attach to the DB-only document: no parse/chunking worker will ever
	// touch it, so these rows are deterministic in every environment.
	seedChunkWithEmbedding(t, rl, db, projectID, rawDoc, 0, "alpha chunk zero: deterministic retrieval text.", true)
	seedChunkWithEmbedding(t, rl, db, projectID, rawDoc, 1, "alpha chunk one: secondary deterministic text.", false)
	seedChunkWithEmbedding(t, rl, db, projectID, rawDoc, 2, "beta chunk zero: distinct deterministic text.", false)
	seedChunkWithEmbedding(t, rl, db, projectID, rawDoc, 3, "beta chunk one: more distinct deterministic text.", false)
	rl.Printf("inserted 4 deterministic chunk rows on document %s", rawDoc)

	rl.Section("Create graph objects + relationships")
	obj1 := createGraphObject(t, projectID, "Feature", "restore-feature-"+shortPID, map[string]any{
		"priority": "high", "score": 3.5,
	})
	obj2 := createGraphObject(t, projectID, "Decision", "restore-decision-"+shortPID, map[string]any{
		"rationale": "deterministic decision body", "approved": true,
	})
	obj3 := createGraphObject(t, projectID, "Belief", "restore-belief-"+shortPID, map[string]any{
		"certainty": 0.75,
	})
	rel1 := createRelationship(t, projectID, obj1, obj2, "DEPENDS_ON", map[string]any{"weight": 1.0})
	rel2 := createRelationship(t, projectID, obj2, obj3, "IMPLEMENTS", nil)
	seed.ObjectIDs = []string{obj1, obj2, obj3}
	seed.RelationshipIDs = []string{rel1, rel2}
	rl.Printf("created graph objects %s, %s, %s and relationships %s, %s", obj1, obj2, obj3, rel1, rel2)

	rl.Section("Set explicit embeddings on one object and one relationship")
	updateGraphObjectEmbedding(t, rl, db, projectID, obj1)
	updateGraphRelationshipEmbedding(t, rl, db, projectID, rel1)
	rl.Printf("set vector embeddings on graph object %s and relationship %s", obj1, rel1)

	rl.Section("Insert object-type schema")
	schemaID := insertSeedObjectTypeSchema(t, rl, db, projectID, "RestoreNote-"+strings.ReplaceAll(shortPID, "-", ""))
	seed.ObjectSchemaID = schemaID
	rl.Printf("inserted object-type schema %s", schemaID)

	rl.Section("Insert project branch")
	branchID := insertSeedBranch(t, rl, db, projectID, "restore-branch-"+shortPID)
	seed.BranchID = branchID
	rl.Printf("inserted branch %s", branchID)

	rl.Section("Create agent via API")
	agentResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), e2eTestToken(), projectID, jsonBody(map[string]any{
		"name":         "restore-seed-agent-" + shortPID,
		"strategyType": "extraction",
		"cronSchedule": "0 0 * * *",
	}))
	agentBody := mustStatus(t, agentResp, http.StatusCreated)
	var agentEnvelope map[string]any
	parseBodyJSON(t, agentBody, &agentEnvelope)
	agentData, _ := agentEnvelope["data"].(map[string]any)
	agentID, _ := agentData["id"].(string)
	if agentID == "" {
		rl.Failf("agent create response had no data.id\nbody: %s", agentBody)
	}
	seed.AgentID = agentID
	rl.Printf("created agent %s", agentID)

	rl.Section("Create project-owned skill via API")
	skill := createProjectSkillHelper(t, projectID, "restore-seed-skill-"+shortPID,
		"deterministic project skill for backup-restore seeding",
		"# Seed skill\n\nDeterministic body used by the backup-restore e2e fixture.\n")
	skillID, _ := skill["id"].(string)
	if skillID == "" {
		rl.Failf("project skill create returned no id: %v", skill)
	}
	seed.SkillID = skillID
	rl.Printf("created project-owned skill %s", skillID)

	rl.Section("Insert product version + tag")
	pvID, tagID := insertSeedTag(t, rl, db, projectID)
	seed.ProductVersionID = pvID
	seed.TagID = tagID
	rl.Printf("inserted product version %s and tag %s", pvID, tagID)

	rl.Section("Seed project memberships")
	restorerID := currentUserProfileID(t, rl)
	seed.RestorerUserID = restorerID
	insertSeedMembership(t, rl, db, projectID, restorerID, "owner")
	if second := secondMemberUserID(db, restorerID); second != "" {
		insertSeedMembership(t, rl, db, projectID, second, "editor")
		seed.SecondMemberID = second
		rl.Printf("added second membership for user %s (role editor)", second)
	} else {
		rl.Printf("no second core user profile available; memberships limited to restorer")
	}

	rl.Section("Wait for async jobs to settle")
	waitProjectSettled(t, rl, db, projectID)

	rl.Printf("seedRichProject complete: org=%s project=%s", orgID, projectID)
	return seed
}

// mustMemoryDB returns the shared test DB handle or skips the test.
func mustMemoryDB(t *testing.T, rl *framework.RunLog) *sql.DB {
	t.Helper()
	db, err := framework.MemoryDB()
	if err != nil {
		framework.DoSkipf(t, rl, "memory DB not available: %v", err)
	}
	return db
}

func uploadSeedDocument(t *testing.T, rl *framework.RunLog, projectID, filename, content string) string {
	t.Helper()
	resp := doMultipartWithFile(t, "/api/documents/upload", e2eTestToken(), projectID, "file", filename, []byte(content))
	body := framework.ReadBody(t, resp)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		rl.Failf("upload %s: unexpected status %d: %s", filename, resp.StatusCode, body)
	}
	var result struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		rl.Failf("upload %s: unmarshal: %v\nbody: %s", filename, err, body)
	}
	if result.Document.ID == "" {
		rl.Failf("upload %s: response had no document.id\nbody: %s", filename, body)
	}
	rl.CLIStep("Upload seed document",
		fmt.Sprintf("POST /api/documents/upload file=%s", filename),
		fmt.Sprintf("HTTP %d document_id=%s", resp.StatusCode, result.Document.ID))
	return result.Document.ID
}

// vector768 renders a deterministic 768-dim pgvector literal using values that
// are exactly representable as float32, so Postgres text output round-trips
// byte-for-byte after a raw-copy restore.
func vector768(seed int) string {
	values := []string{"0.125", "0.25", "0.375", "0.5", "-0.125", "-0.25", "-0.375", "0.625"}
	parts := make([]string, 768)
	for i := range parts {
		parts[i] = values[(i+seed)%len(values)]
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// insertSeedDocumentRow inserts a DB-only content document (no file, no parse
// job, no chunking pipeline involvement) so tests get a fully deterministic
// document/chunk surface in every environment.
func insertSeedDocumentRow(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, filename string) string {
	t.Helper()
	var id string
	content := "deterministic raw document body hosted only in kb.documents.\nsecond line with distinct tokens.\n"
	err := db.QueryRow(
		`INSERT INTO kb.documents (project_id, filename, content, metadata)
		 VALUES ($1, $2, $3, '{"provenance":"e2e-seed"}'::jsonb) RETURNING id`,
		projectID, filename, content,
	).Scan(&id)
	if err != nil {
		rl.Failf("insert content document row: %v", err)
	}
	return id
}

func seedChunkWithEmbedding(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, docID string, idx int, text string, withEmbedding bool) {
	t.Helper()
	var id string
	meta := fmt.Sprintf(`{"provenance":"e2e-seed","docIdx":%d}`, idx)
	if withEmbedding {
		err := db.QueryRow(
			`INSERT INTO kb.chunks (document_id, chunk_index, text, metadata, embedding)
			 VALUES ($1, $2, $3, $4::jsonb, $5::vector) RETURNING id`,
			docID, idx, text, meta, vector768(idx),
		).Scan(&id)
		if err != nil {
			rl.Failf("insert chunk: %v", err)
		}
	} else {
		err := db.QueryRow(
			`INSERT INTO kb.chunks (document_id, chunk_index, text, metadata)
			 VALUES ($1, $2, $3, $4::jsonb) RETURNING id`,
			docID, idx, text, meta,
		).Scan(&id)
		if err != nil {
			rl.Failf("insert chunk: %v", err)
		}
	}
	rl.Printf("seeded chunk %s (doc=%s idx=%d embed=%v)", id, docID, idx, withEmbedding)
}

func updateGraphObjectEmbedding(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, objectID string) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE kb.graph_objects SET embedding_v2 = $1::vector WHERE id = $2 AND project_id = $3`,
		vector768(3), objectID, projectID); err != nil {
		rl.Failf("update graph object embedding: %v", err)
	}
	rl.Printf("set embedding on graph object %s", objectID)
}

func updateGraphRelationshipEmbedding(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, relID string) {
	t.Helper()
	if _, err := db.Exec(
		`UPDATE kb.graph_relationships SET embedding = $1::vector WHERE id = $2 AND project_id = $3`,
		vector768(5), relID, projectID); err != nil {
		rl.Failf("update relationship embedding: %v", err)
	}
	rl.Printf("set embedding on relationship %s", relID)
}

func insertSeedObjectTypeSchema(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, typeName string) string {
	t.Helper()
	var id string
	jsonSchema := fmt.Sprintf(
		`{"type":"object","title":%q,"properties":{"name":{"type":"string"},"score":{"type":"number"}},"required":["name"]}`,
		typeName)
	if err := db.QueryRow(
		`INSERT INTO kb.object_type_schemas (project_id, type, version, json_schema)
		 VALUES ($1, $2, 1, $3::jsonb) RETURNING id`,
		projectID, typeName, jsonSchema,
	).Scan(&id); err != nil {
		rl.Failf("insert object-type schema: %v", err)
	}
	return id
}

func insertSeedBranch(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, name string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO kb.branches (project_id, name, description) VALUES ($1, $2, $3) RETURNING id`,
		projectID, name, "deterministic e2e branch for backup-restore seeding",
	).Scan(&id); err != nil {
		rl.Failf("insert branch: %v", err)
	}
	return id
}

func insertSeedTag(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID string) (productVersionID, tagID string) {
	t.Helper()
	if err := db.QueryRow(
		`INSERT INTO kb.product_versions (project_id, name, description)
		 VALUES ($1, $2, $3) RETURNING id`,
		projectID, "restore-seed-version", "deterministic seeded product version",
	).Scan(&productVersionID); err != nil {
		rl.Failf("insert product version: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO kb.tags (project_id, product_version_id, name, description)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		projectID, productVersionID, "restore-seed-tag", "deterministic seeded tag",
	).Scan(&tagID); err != nil {
		rl.Failf("insert tag: %v", err)
	}
	return productVersionID, tagID
}

// currentUserProfileID fetches the authenticated user's core profile UUID via
// GET /api/user/profile (the id referenced by project_memberships.user_id).
func currentUserProfileID(t *testing.T, rl *framework.RunLog) string {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/user/profile", e2eTestToken(), "", nil)
	b := mustStatus(t, resp, http.StatusOK)
	var profile struct {
		ID string `json:"id"`
	}
	parseBodyJSON(t, b, &profile)
	if profile.ID == "" {
		rl.Failf("user profile response had no id\nbody: %s", b)
	}
	rl.Printf("current user profile id=%s", profile.ID)
	return profile.ID
}

func secondMemberUserID(db *sql.DB, currentID string) string {
	var id string
	err := db.QueryRow(
		`SELECT id FROM core.user_profiles WHERE id <> $1 ORDER BY created_at ASC, id ASC LIMIT 1`,
		currentID,
	).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

func insertSeedMembership(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID, userID, role string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO kb.project_memberships (project_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO NOTHING`,
		projectID, userID, role); err != nil {
		rl.Failf("insert project membership: %v", err)
	}
}

// waitProjectSettled polls the fingerprint until two consecutive samples (2s
// apart) are identical, so async parsing/chunking/embedding workers finish
// mutating curated tables before a backup is created.
func waitProjectSettled(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID string) {
	t.Helper()
	prev, err := fingerprintCore(db, projectID)
	if err != nil {
		rl.Failf("settle fingerprint: %v", err)
	}
	deadline := time.Now().Add(settleTimeout)
	for {
		time.Sleep(settleSampleGap)
		cur, err := fingerprintCore(db, projectID)
		if err != nil {
			rl.Failf("settle fingerprint: %v", err)
		}
		if cur.OverallHash == prev.OverallHash {
			rl.Printf("project %s settled (state stable across %s)", projectID, settleSampleGap)
			return
		}
		prev = cur
		if time.Now().After(deadline) {
			rl.Printf("project %s did not fully settle within %s; proceeding with last observed state", projectID, settleTimeout)
			return
		}
	}
}

// projectExistsInDB reports whether a project row with the given id exists.
func projectExistsInDB(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM kb.projects WHERE id = $1`, projectID).Scan(&n); err != nil {
		rl.Failf("project exists check: %v", err)
	}
	return n > 0
}

// projectOrgID returns the owning organization of a project.
func projectOrgID(t *testing.T, rl *framework.RunLog, db *sql.DB, projectID string) string {
	t.Helper()
	var orgID string
	if err := db.QueryRow(`SELECT organization_id FROM kb.projects WHERE id = $1`, projectID).Scan(&orgID); err != nil {
		rl.Failf("project org lookup: %v", err)
	}
	return orgID
}
