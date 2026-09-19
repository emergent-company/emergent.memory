package backups

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Restorer applies a validated backup archive to a project in either
// overwrite (same project, transactional wipe-then-insert) or clone (new
// project, full ID remap) mode.
type Restorer struct {
	db       *bun.DB
	storage  *storage.Service
	repo     *Repository
	creator  *Creator
	importer *Importer
	log      *slog.Logger
}

// NewRestorer creates a new restore orchestrator.
func NewRestorer(
	db *bun.DB,
	storage *storage.Service,
	repo *Repository,
	creator *Creator,
	importer *Importer,
	log *slog.Logger,
) *Restorer {
	return &Restorer{
		db:       db,
		storage:  storage,
		repo:     repo,
		creator:  creator,
		importer: importer,
		log:      log.With(slog.String("component", "backups.restorer")),
	}
}

// refAction describes how a clone-mode foreign-key column is resolved against
// the remap table when its value cannot be found there.
type refAction string

const (
	refRemap refAction = "remap" // default: rewrite via remap map, leave raw if unmapped
	refNull  refAction = "null"  // NULL the column when unmapped
	refSkip  refAction = "skip"  // drop the entire row when this column is unmapped
	refFail  refAction = "fail"  // error out the restore
)

// refPolicy is the per-column clone-mode resolution policy.
type refPolicy struct {
	action refAction
}

// restoreTableSpec describes how one table is wiped, ordered, and inserted.
// Vector columns need no explicit spec: they are detected by UDT from
// information_schema and cast back with ?::vector.
type restoreTableSpec struct {
	name     string               // NDJSON file / table base name
	selfRefs []string             // columns that reference other rows of the SAME table
	refs     map[string]refPolicy // column -> policy; absent column == refRemap
	wipeSQL  string               // DELETE scoped to the project; ? = project_id
}

// restoreTableOrder returns the dependency-safe insert order. References to
// tables that are NOT part of the snapshot (global schemas, acp_sessions,
// org/global skills, users) are either preserved or handled specially.
func restoreTableOrder() []restoreTableSpec {
	byProject := func(table string) string {
		return fmt.Sprintf("DELETE FROM %s WHERE project_id = ?", table)
	}

	table := func(name string) restoreTableSpec {
		return restoreTableSpec{name: name, wipeSQL: byProject("kb." + name)}
	}
	// nullRefs declares columns that are NULLed on clone when their value is
	// not resolvable through the remap (nullable dangling project-local refs).
	nullRefs := func(cols ...string) map[string]refPolicy {
		refs := make(map[string]refPolicy, len(cols))
		for _, c := range cols {
			refs[c] = refPolicy{action: refNull}
		}
		return refs
	}
	// skipRefs declares columns that drop the whole row on clone when their
	// value is not resolvable (global rows absent from the archive).
	skipRefs := func(cols ...string) map[string]refPolicy {
		refs := make(map[string]refPolicy, len(cols))
		for _, c := range cols {
			refs[c] = refPolicy{action: refSkip}
		}
		return refs
	}

	return []restoreTableSpec{
		// object_type_schemas can reference itself through supersedes_id /
		// canonical_id, so self references are ordered during insert.
		{name: "object_type_schemas", selfRefs: []string{"supersedes_id", "canonical_id"},
			refs:    nullRefs("supersedes_id", "canonical_id"),
			wipeSQL: byProject("kb.object_type_schemas")},
		{name: "graph_schemas", selfRefs: []string{"parent_version_id"},
			refs:    nullRefs("parent_version_id"),
			wipeSQL: byProject("kb.graph_schemas")},
		{name: "project_schemas", refs: skipRefs("schema_id"), wipeSQL: byProject("kb.project_schemas")},
		table("project_object_schema_registry"),
		{name: "project_edge_schema_registry", refs: skipRefs("schema_id"), wipeSQL: byProject("kb.project_edge_schema_registry")},
		table("schema_migration_jobs"),
		table("schema_migration_runs"),
		table("external_sources"),
		{name: "product_versions", selfRefs: []string{"base_product_version_id"},
			refs:    nullRefs("base_product_version_id"),
			wipeSQL: byProject("kb.product_versions")},
		{name: "documents", selfRefs: []string{"parent_document_id"},
			refs:    nullRefs("parent_document_id"),
			wipeSQL: byProject("kb.documents")},
		{name: "chunks", wipeSQL: "DELETE FROM kb.chunks WHERE document_id IN (SELECT id FROM kb.documents WHERE project_id = ?)"},
		// canonical_id is NOT NULL on graph_objects, so it must resolve through
		// the remap; only nullable dangling refs are nulled on clone.
		{name: "graph_objects", selfRefs: []string{"canonical_id", "supersedes_id", "merged_to_canonical_id"},
			refs:    nullRefs("supersedes_id", "merged_to_canonical_id"),
			wipeSQL: byProject("kb.graph_objects")},
		{name: "graph_relationships", selfRefs: []string{"canonical_id", "supersedes_id"},
			refs:    nullRefs("supersedes_id"),
			wipeSQL: byProject("kb.graph_relationships")},
		table("agent_definitions"),
		{name: "agents", refs: nullRefs("agent_definition_id"), wipeSQL: byProject("kb.agents")},
		table("agent_webhook_hooks"),
		{name: "chat_conversations", refs: nullRefs("acp_session_id", "object_id", "agent_definition_id"),
			wipeSQL: byProject("kb.chat_conversations")},
		{name: "chat_messages", wipeSQL: "DELETE FROM kb.chat_messages WHERE conversation_id IN (SELECT id FROM kb.chat_conversations WHERE project_id = ?)"},
		{name: "branches", selfRefs: []string{"parent_branch_id"},
			refs:    nullRefs("parent_branch_id"),
			wipeSQL: byProject("kb.branches")},
		{name: "branch_lineage", wipeSQL: "DELETE FROM kb.branch_lineage WHERE branch_id IN (SELECT id FROM kb.branches WHERE project_id = ?)"},
		{name: "object_extraction_jobs", selfRefs: []string{"reprocessing_of"},
			refs:    nullRefs("document_id", "chunk_id", "staging_branch_id", "reprocessing_of"),
			wipeSQL: byProject("kb.object_extraction_jobs")},
		table("embedding_policies"),
		table("skills"),
		table("mcp_servers"),
		table("tags"),
		table("tasks"),
		table("sandbox_images"),
		table("project_settings"),
		table("project_model_config"),
		table("project_provider_configs"),
		table("project_memberships"),
		table("project_journal"),
		{name: "project_journal_notes", wipeSQL: byProject("kb.project_journal_notes")},
	}
}

// dbColumn is a column of a table pulled from information_schema.
type dbColumn struct {
	Name     string `bun:"column_name"`
	DataType string `bun:"data_type"`
	UDTName  string `bun:"udt_name"`
}

// Restore runs a restore job to completion, recording status transitions.
func (r *Restorer) Restore(ctx context.Context, job *Restore, req RestoreRequest) error {
	job.Status = RestoreStatusRunning
	job.Progress = 5
	if err := r.repo.UpdateRestore(ctx, job); err != nil {
		return fmt.Errorf("update restore to running: %w", err)
	}

	if err := r.restore(ctx, job, req); err != nil {
		job.Status = RestoreStatusFailed
		msg := err.Error()
		job.ErrorMessage = &msg
		_ = r.repo.UpdateRestore(ctx, job)
		return err
	}

	now := time.Now()
	job.Status = RestoreStatusCompleted
	job.Progress = 100
	job.CompletedAt = &now
	if err := r.repo.UpdateRestore(ctx, job); err != nil {
		return fmt.Errorf("update restore to completed: %w", err)
	}

	r.log.Info("restore completed",
		slog.String("restore_id", job.ID),
		slog.String("mode", job.Mode),
	)
	return nil
}

func (r *Restorer) restore(ctx context.Context, job *Restore, req RestoreRequest) error {
	backup, err := r.fetchBackup(ctx, job.BackupID)
	if err != nil {
		return err
	}
	if backup.Status != BackupStatusReady {
		return fmt.Errorf("backup %s is not ready (status %s)", backup.ID, backup.Status)
	}

	archive, err := r.importer.Load(ctx, backup.StorageKey)
	if err != nil {
		return fmt.Errorf("validate backup archive: %w", err)
	}

	job.SourceProjectID = &backup.ProjectID

	switch job.Mode {
	case RestoreModeOverwrite:
		target := backup.ProjectID
		job.TargetProjectID = &target
		return r.restoreOverwrite(ctx, job, req, backup, archive)
	case RestoreModeClone:
		return r.restoreClone(ctx, job, req, backup, archive)
	default:
		return fmt.Errorf("unsupported restore mode %q", job.Mode)
	}
}

func (r *Restorer) fetchBackup(ctx context.Context, backupID string) (*Backup, error) {
	var backup Backup
	err := r.db.NewSelect().
		Model(&backup).
		Where("id = ?", backupID).
		Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("backup %s not found", backupID)
	}
	if err != nil {
		return nil, fmt.Errorf("get backup: %w", err)
	}
	return &backup, nil
}

// restoreOverwrite implements Decision 6 + 7: optional pre-restore snapshot,
// then one transaction that wipes the project's rows and inserts the snapshot.
func (r *Restorer) restoreOverwrite(ctx context.Context, job *Restore, req RestoreRequest, backup *Backup, archive *Archive) error {
	projectID := backup.ProjectID

	// 4.7 — pre-restore snapshot of the live project before any modification.
	if req.PreRestoreSnapshot {
		job.Progress = 10
		_ = r.repo.UpdateRestore(ctx, job)
		if err := r.createPreRestoreSnapshot(ctx, backup); err != nil {
			return fmt.Errorf("create pre-restore snapshot: %w", err)
		}
	}

	job.Progress = 20
	_ = r.repo.UpdateRestore(ctx, job)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin restore transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rolled back on every error path

	// FK-safe wipe of every table present in the snapshot (reverse topo order).
	if err := r.wipeProject(ctx, tx, archive, projectID); err != nil {
		return fmt.Errorf("wipe project data: %w", err)
	}

	// Insert snapshot rows. In overwrite mode IDs are preserved as-is.
	if err := r.insertArchive(ctx, tx, job, archive, nil, nil); err != nil {
		return fmt.Errorf("insert project data: %w", err)
	}

	// Re-upload files before commit so a failure rolls the wipe back too.
	job.Progress = 90
	_ = r.repo.UpdateRestore(ctx, job)
	if err := r.uploadFiles(ctx, archive); err != nil {
		return fmt.Errorf("restore files: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit restore transaction: %w", err)
	}
	return nil
}

// restoreClone implements Decision 8: create a fresh project row and remap
// every entity UUID, rewriting FKs through the remap table.
func (r *Restorer) restoreClone(ctx context.Context, job *Restore, req RestoreRequest, backup *Backup, archive *Archive) error {
	projectID := uuid.New().String()
	name := req.TargetProjectName
	if name == "" {
		name = fmt.Sprintf("%s (restored %s)", backup.ProjectName, time.Now().Format("2006-01-02"))
	}

	sourceProjectID := backup.ProjectID
	// remap holds source UUID → new UUID. The project row anchors it.
	remap := map[string]string{sourceProjectID: projectID}

	// Cross-org membership filtering needs the set of users in the target org.
	sameOrg := backup.OrganizationID == req.TargetOrgID
	var targetOrgUsers map[string]bool
	if !sameOrg {
		var err error
		targetOrgUsers, err = r.orgMemberUserIDs(ctx, req.TargetOrgID)
		if err != nil {
			return fmt.Errorf("resolve target org members: %w", err)
		}
	}

	job.Progress = 20
	job.TargetProjectID = &projectID
	_ = r.repo.UpdateRestore(ctx, job)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clone transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Insert the new project row from the exported kb.projects row.
	projectRow := make(map[string]any, len(archive.ProjectConfig()))
	for k, v := range archive.ProjectConfig() {
		projectRow[k] = v
	}
	projectRow["id"] = projectID
	projectRow["organization_id"] = req.TargetOrgID
	projectRow["name"] = name
	projectRow["deleted_at"] = nil
	projectRow["deleted_by"] = nil

	cols, err := r.tableColumns(ctx, "projects")
	if err != nil {
		return fmt.Errorf("resolve kb.projects columns: %w", err)
	}
	if err := r.insertRow(ctx, tx, "kb.projects", cols, projectRow); err != nil {
		return fmt.Errorf("insert project: %w", err)
	}

	// Insert snapshot rows with ID remapping.
	membershipCtx := &membershipFilter{
		sameOrg:         sameOrg,
		targetOrgUsers:  targetOrgUsers,
		createdBy:       req.CreatedBy,
		targetProjectID: projectID,
	}
	if err := r.insertArchive(ctx, tx, job, archive, remap, membershipCtx); err != nil {
		return fmt.Errorf("insert clone data: %w", err)
	}

	job.Progress = 90
	_ = r.repo.UpdateRestore(ctx, job)
	if err := r.uploadFiles(ctx, archive); err != nil {
		return fmt.Errorf("restore files: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit clone transaction: %w", err)
	}
	return nil
}

// membershipFilter controls how project_memberships rows are cloned.
type membershipFilter struct {
	sameOrg         bool
	targetOrgUsers  map[string]bool
	createdBy       string
	targetProjectID string
}

// createPreRestoreSnapshot backs up the live project before an overwrite and
// marks the snapshot in the backup's includes JSONB.
func (r *Restorer) createPreRestoreSnapshot(ctx context.Context, backup *Backup) error {
	snapshotID := uuid.New().String()

	var projectName string
	if err := r.db.NewSelect().
		Table("kb.projects").
		Column("name").
		Where("id = ?", backup.ProjectID).
		Scan(ctx, &projectName); err != nil {
		return fmt.Errorf("get project name for pre-restore snapshot: %w", err)
	}

	snapshot := &Backup{
		ID:             snapshotID,
		OrganizationID: backup.OrganizationID,
		ProjectID:      backup.ProjectID,
		ProjectName:    projectName,
		StorageKey:     GenerateStorageKey(backup.OrganizationID, snapshotID),
		Status:         BackupStatusCreating,
		Progress:       0,
		BackupType:     BackupTypeFull,
		Includes: map[string]any{
			"documents":   true,
			"chunks":      true,
			"graph":       true,
			"chat":        true,
			"journal":     true,
			"deleted":     true,
			"pre_restore": true,
		},
		CreatedAt: time.Now(),
		ExpiresAt: ptrTime(time.Now().AddDate(0, 3, 0)), // pre-restore snapshots are kept 90 days
	}
	if backup.CreatedBy != nil {
		snapshot.CreatedBy = backup.CreatedBy
	}

	if err := r.repo.Create(ctx, snapshot); err != nil {
		return fmt.Errorf("create pre-restore snapshot record: %w", err)
	}

	opts := CreateBackupOptions{
		BackupID:       snapshotID,
		ProjectID:      backup.ProjectID,
		ProjectName:    projectName,
		OrganizationID: backup.OrganizationID,
		IncludeChat:    true,
		IncludeJournal: true,
	}
	if err := r.creator.CreateBackup(ctx, opts); err != nil {
		return fmt.Errorf("run pre-restore snapshot: %w", err)
	}
	return nil
}

// orgMemberUserIDs returns the set of user IDs with a membership in an org.
func (r *Restorer) orgMemberUserIDs(ctx context.Context, orgID string) (map[string]bool, error) {
	var userIDs []string
	err := r.db.NewSelect().
		Table("kb.organization_memberships").
		Column("user_id").
		Where("organization_id = ?", orgID).
		Scan(ctx, &userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		out[id] = true
	}
	return out, nil
}

// wipeProject deletes existing rows for every snapshot table present in the
// archive, in reverse dependency order.
func (r *Restorer) wipeProject(ctx context.Context, tx bun.Tx, archive *Archive, projectID string) error {
	specs := restoreTableOrder()
	for i := len(specs) - 1; i >= 0; i-- {
		spec := specs[i]
		if !archive.HasTable(spec.name) {
			continue
		}
		if _, err := tx.NewRaw(spec.wipeSQL, projectID).Exec(ctx); err != nil {
			return fmt.Errorf("wipe %s: %w", spec.name, err)
		}
	}
	return nil
}

// insertArchive inserts each snapshot table present in the archive, in
// dependency order. remap is nil for overwrite (identity); when set every row
// UUID is replaced and FK references are rewritten through it.
func (r *Restorer) insertArchive(ctx context.Context, tx bun.Tx, job *Restore, archive *Archive, remap map[string]string, membership *membershipFilter) error {
	specs := restoreTableOrder()
	stats := map[string]any{}

	for i, spec := range specs {
		if !archive.HasTable(spec.name) {
			continue
		}
		rows, err := archive.Rows(spec.name)
		if err != nil {
			return err
		}

		if spec.name == "project_memberships" && membership != nil {
			rows = r.filterCloneMemberships(rows, membership)
		}

		inserted, err := r.insertTableRows(ctx, tx, spec, rows, remap)
		if err != nil {
			return fmt.Errorf("insert %s: %w", spec.name, err)
		}
		stats[spec.name] = inserted

		// Coarse progress: wipe phase done at 20, inserts span 20..90.
		if len(specs) > 0 {
			job.Progress = 20 + int(70*(i+1)/len(specs))
			if job.Progress > 90 {
				job.Progress = 90
			}
		}
		job.Stats = stats
		_ = r.repo.UpdateRestore(ctx, job)
	}
	return nil
}

// filterCloneMemberships drops memberships for users who do not resolve to the
// target org on a cross-org clone and ensures the restorer is a member.
func (r *Restorer) filterCloneMemberships(rows []map[string]any, m *membershipFilter) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	for _, row := range rows {
		userID := stringValue(row["user_id"])
		if userID == "" {
			continue
		}
		if !m.sameOrg && !m.targetOrgUsers[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, row)
	}
	if m.createdBy != "" && !seen[m.createdBy] {
		out = append(out, map[string]any{
			"project_id": m.targetProjectID,
			"user_id":    m.createdBy,
			"role":       "project_admin",
			"created_at": time.Now().Format(time.RFC3339Nano),
		})
	}
	return out
}

// tableColumns resolves the current schema columns of kb.<table>.
func (r *Restorer) tableColumns(ctx context.Context, table string) ([]dbColumn, error) {
	var cols []dbColumn
	err := r.db.NewSelect().
		TableExpr("information_schema.columns").
		Column("column_name", "data_type", "udt_name").
		Where("table_schema = ?", "kb").
		Where("table_name = ?", table).
		Order("ordinal_position").
		Scan(ctx, &cols)
	if err != nil {
		return nil, err
	}
	return cols, nil
}

// insertTableRows inserts NDJSON rows for one table.
func (r *Restorer) insertTableRows(ctx context.Context, tx bun.Tx, spec restoreTableSpec, rows []map[string]any, remap map[string]string) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	cols, err := r.tableColumns(ctx, spec.name)
	if err != nil {
		return 0, err
	}

	// Same-table FK references (selfRefs) require parents before children.
	rows = orderSelfReferences(rows, cols, spec.selfRefs)

	clone := remap != nil

	count := 0
	for _, row := range rows {
		if clone {
			remapRowUUIDs(row, remap)
			skip, err := applyRefs(row, remap, spec.refs)
			if err != nil {
				return count, fmt.Errorf("insert %s: %w", spec.name, err)
			}
			if skip {
				r.log.Warn("skipping clone row with unresolvable reference",
					slog.String("table", spec.name),
				)
				continue
			}
		}
		if err := r.insertRow(ctx, tx, "kb."+spec.name, cols, row); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// remapRowUUIDs registers a fresh UUID for the row's PK on clone so the row's
// id anchors every subsequent reference to it in the remap table.
func remapRowUUIDs(row map[string]any, remap map[string]string) {
	// New PK for this row (id is the PK on every curated table except
	// project_model_config, whose PK is project_id and is handled via remap).
	if v := stringValue(row["id"]); v != "" {
		if nid, ok := remap[v]; ok {
			row["id"] = nid
		} else {
			nid := uuid.New().String()
			remap[v] = nid
			row["id"] = nid
		}
	}
}

// applyRefs rewrites every column value that matches a remapped source UUID and
// applies the per-column resolution policy to values that cannot be resolved.
// The default (absent) policy is refRemap: the raw value is left untouched.
func applyRefs(row map[string]any, remap map[string]string, refs map[string]refPolicy) (skip bool, err error) {
	for key, val := range row {
		s := stringValue(val)
		if s == "" {
			continue
		}
		if nid, ok := remap[s]; ok {
			row[key] = nid
			continue
		}
		policy, ok := refs[key]
		if !ok {
			// refRemap (default): preserve the raw value.
			continue
		}
		switch policy.action {
		case refRemap:
			// Explicit remap: preserve the raw value.
		case refNull:
			row[key] = nil
		case refSkip:
			return true, nil
		case refFail:
			return false, fmt.Errorf("unresolved reference %s=%q", key, s)
		}
	}
	return false, nil
}

// orderSelfReferences returns rows sorted so same-table parents precede the
// rows that reference them (stable DFS over selfRefs).
func orderSelfReferences(rows []map[string]any, cols []dbColumn, selfRefs []string) []map[string]any {
	hasID := false
	for _, c := range cols {
		if c.Name == "id" {
			hasID = true
			break
		}
	}
	if !hasID || len(selfRefs) == 0 {
		return rows
	}

	byID := make(map[string]map[string]any, len(rows))
	refsByID := make(map[string][]string, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		id := stringValue(row["id"])
		if id != "" {
			byID[id] = row
			order = append(order, id)
		}
		var refs []string
		for _, col := range selfRefs {
			ref := stringValue(row[col])
			if ref != "" && ref != id {
				refs = append(refs, ref)
			}
		}
		refsByID[id] = refs
	}

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(rows))
	out := make([]map[string]any, 0, len(rows))

	var visit func(id string)
	visit = func(id string) {
		if id == "" {
			return
		}
		switch state[id] {
		case done:
			return
		case visiting:
			return // cycle: emit anyway to avoid infinite recursion
		}
		state[id] = visiting
		for _, ref := range refsByID[id] {
			if _, ok := byID[ref]; ok {
				visit(ref)
			}
		}
		state[id] = done
		out = append(out, byID[id])
	}

	for _, id := range order {
		visit(id)
	}
	return out
}

// insertRow builds a generic INSERT from a schema column list and one row map.
// Vector/jsonb/array/bytea columns get explicit casts matching the export
// serialization; all other values are bound as text and cast to the column
// type so pgx never has to guess a parameter type.
func (r *Restorer) insertRow(ctx context.Context, tx bun.Tx, table string, cols []dbColumn, row map[string]any) error {
	colSet := make(map[string]bool, len(row))
	for k := range row {
		colSet[k] = true
	}

	var colNames []string
	var placeholders []string
	var args []any

	for _, col := range cols {
		if !colSet[col.Name] {
			continue
		}
		v := row[col.Name]
		expr, arg, isNull, err := bindValue(col, v)
		if err != nil {
			return fmt.Errorf("column %s: %w", col.Name, err)
		}
		colNames = append(colNames, quoteIdent(col.Name))
		if isNull {
			placeholders = append(placeholders, "NULL")
			continue
		}
		placeholders = append(placeholders, expr)
		args = append(args, arg)
	}

	if len(colNames) == 0 {
		return nil
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)
	if _, err := tx.NewRaw(sql, args...).Exec(ctx); err != nil {
		return err
	}
	return nil
}

// bindValue converts a decoded NDJSON value into a SQL expression + argument.
func bindValue(col dbColumn, v any) (expr string, arg any, isNull bool, err error) {
	if v == nil {
		return "NULL", nil, true, nil
	}

	switch {
	case col.UDTName == "vector" || col.UDTName == "halfvec":
		vec, ok := toFloat32Slice(v)
		if !ok {
			return "", nil, false, fmt.Errorf("expected float array for vector column, got %T", v)
		}
		if len(vec) == 0 {
			return "NULL", nil, true, nil
		}
		return "?::" + col.UDTName, pgutils.FormatVector(vec), false, nil

	case col.UDTName == "jsonb" || col.DataType == "jsonb" || col.DataType == "json":
		if s, ok := v.(string); ok {
			// Exporter casts jsonb::text, so the raw JSON text arrives as a string.
			return "?::jsonb", s, false, nil
		}
		b, mErr := json.Marshal(v)
		if mErr != nil {
			return "", nil, false, mErr
		}
		return "?::jsonb", string(b), false, nil

	case col.DataType == "ARRAY":
		// pgx scans text[]/uuid[] into any as its array-literal text form
		// (e.g. "{a,b,c}"), which is already a valid PostgreSQL array literal.
		// A JSON array ([]any) may also arrive; render it to a literal. JSON-to-
		// array SQL casts do not exist in Postgres, so cast via col.UDTName
		// (_text, _uuid, ...).
		switch t := v.(type) {
		case string:
			return "?::" + col.UDTName, t, false, nil
		case []any:
			lit, fErr := formatArrayLiteral(t)
			if fErr != nil {
				return "", nil, false, fErr
			}
			return "?::" + col.UDTName, lit, false, nil
		default:
			return "", nil, false, fmt.Errorf("expected array literal, got %T", v)
		}

	case col.UDTName == "bytea" || col.DataType == "bytea":
		raw, dErr := base64.StdEncoding.DecodeString(stringValue(v))
		if dErr != nil {
			return "", nil, false, fmt.Errorf("decode bytea: %w", dErr)
		}
		return "decode(?::text, 'hex')", hex.EncodeToString(raw), false, nil

	case col.DataType == "boolean":
		return "?::boolean", boolString(v), false, nil

	case isNumericType(col.DataType):
		return "?::" + col.DataType, numericString(v), false, nil

	default:
		return "?::" + col.DataType, scalarString(v), false, nil
	}
}

func isNumericType(dataType string) bool {
	switch dataType {
	case "smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision", "oid":
		return true
	}
	return false
}

// scalarString renders a decoded JSON scalar as its canonical text form.
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func numericString(v any) string {
	return scalarString(v)
}

func boolString(v any) string {
	if b, ok := v.(bool); ok {
		return strconv.FormatBool(b)
	}
	return scalarString(v)
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return scalarString(v)
}

// toFloat32Slice converts a decoded JSON float array into []float32.
func toFloat32Slice(v any) ([]float32, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]float32, 0, len(arr))
	for _, e := range arr {
		f, ok := toFloat64(e)
		if !ok {
			return nil, false
		}
		out = append(out, float32(f))
	}
	return out, true
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

// formatArrayLiteral renders a decoded JSON array as a PostgreSQL array
// literal (e.g. `{a,b}`) for casting with `?::<udt>` where the udt is the
// column's array type (_text, _uuid, ...). Elements are double-quoted so
// backslashes, quotes, commas, and spaces survive; NULL stays bare.
func formatArrayLiteral(v any) (string, error) {
	arr, ok := v.([]any)
	if !ok {
		return "", fmt.Errorf("expected JSON array, got %T", v)
	}
	if len(arr) == 0 {
		return "{}", nil
	}

	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		switch t := e.(type) {
		case nil:
			parts = append(parts, "NULL")
		case string:
			parts = append(parts, quoteArrayElement(t))
		case float64:
			parts = append(parts, strconv.FormatFloat(t, 'f', -1, 64))
		case bool:
			parts = append(parts, strconv.FormatBool(t))
		default:
			parts = append(parts, quoteArrayElement(fmt.Sprintf("%v", t)))
		}
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

// quoteArrayElement double-quotes an array element, escaping backslashes first
// and then double quotes (PostgreSQL array input syntax).
func quoteArrayElement(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// uploadFiles re-uploads every archive file to MinIO under its original
// storage_key.
func (r *Restorer) uploadFiles(ctx context.Context, archive *Archive) error {
	for _, f := range archive.Files() {
		contentType := "application/octet-stream"
		if f.MimeType != nil && *f.MimeType != "" {
			contentType = *f.MimeType
		}
		opts := storage.UploadOptions{
			ContentType: contentType,
		}
		if f.Filename != "" {
			opts.ContentDisposition = fmt.Sprintf(`attachment; filename="%s"`, f.Filename)
		}
		if _, err := r.storage.Upload(ctx, f.StorageKey, bytes.NewReader(f.Payload), int64(len(f.Payload)), opts); err != nil {
			return fmt.Errorf("upload %s: %w", f.StorageKey, err)
		}
	}
	return nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
