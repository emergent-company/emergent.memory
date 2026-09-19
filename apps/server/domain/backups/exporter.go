package backups

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/pgutils"
	"github.com/uptrace/bun"
)

// Exporter handles exporting database data to NDJSON format.
type Exporter struct {
	db  *bun.DB
	log *slog.Logger
}

// NewExporter creates a new database exporter.
func NewExporter(db *bun.DB, log *slog.Logger) *Exporter {
	return &Exporter{
		db:  db,
		log: log.With(slog.String("component", "backups.exporter")),
	}
}

// ExportOptions configures what data to export.
type ExportOptions struct {
	ProjectID      string
	IncludeChat    bool
	IncludeJournal bool
	IncludeDeleted bool
}

// exportGate identifies the opt-in flag that gates a table's export.
type exportGate string

const (
	gateNone    exportGate = ""
	gateChat    exportGate = "chat"
	gateJournal exportGate = "journal"
)

// tableConfig describes one NDJSON table export inside a backup archive.
// The base table is always aliased as `t`; join tables use their own alias.
type tableConfig struct {
	name          string            // NDJSON filename (no extension), e.g. "documents"
	table         string            // schema-qualified table, e.g. "kb.documents"; empty for derived files
	join          string            // optional raw INNER JOIN clause (references alias t)
	projectFilter string            // optional raw WHERE fragment scoping to a project (`?` binds ProjectID)
	vectorColumns []string          // columns of UDT vector/halfvec; cast ::text then parsed to []float32
	deletedColumn string            // optional soft-delete column filtered out unless IncludeDeleted
	// columnExprs overrides the default `t."col"` SELECT expression for named
	// columns. It is applied only when IncludeDeleted is false, so a plain
	// column is exported when soft-deleted rows are included. Each value MUST
	// alias its result AS "col" to keep the NDJSON key stable.
	columnExprs map[string]string
	// leftJoin is a raw LEFT JOIN clause appended only when columnExprs is
	// active, supplying the aliases those expressions reference.
	leftJoin   string
	extraWhere string     // optional static WHERE fragment (no placeholders)
	orderBy    string     // optional batching ORDER BY column; defaults to "id"
	gate       exportGate
	derived    bool // true when rows are derived in Go rather than streamed from `table`
}

// allExportTables returns the curated, ordered export surface (see openspec
// change backup-restore, Decision 2). Order matters: it is the archive order
// of the database/*.ndjson entries.
func allExportTables() []tableConfig {
	byProject := "t.project_id = ?"

	return []tableConfig{
		// --- Original eight ---
		{name: "documents", table: "kb.documents", projectFilter: byProject, deletedColumn: "deleted_at"},
		{name: "chunks", table: "kb.chunks", projectFilter: "d.project_id = ?", vectorColumns: []string{"embedding"},
			join: "INNER JOIN kb.documents d ON d.id = t.document_id", deletedColumn: "deleted_at"},
		{name: "graph_objects", table: "kb.graph_objects", projectFilter: byProject, vectorColumns: []string{"embedding_v2"}, deletedColumn: "deleted_at"},
		{name: "graph_relationships", table: "kb.graph_relationships", projectFilter: byProject, vectorColumns: []string{"embedding"}, deletedColumn: "deleted_at"},
		{name: "chat_conversations", table: "kb.chat_conversations", projectFilter: byProject, deletedColumn: "deleted_at", gate: gateChat,
			leftJoin: "LEFT JOIN kb.graph_objects go ON go.id = t.object_id",
			columnExprs: map[string]string{
				// object_id → kb.graph_objects(id). A soft-deleted object is
				// excluded from the archive, so a conversation pointing at one
				// would dangle on restore and violate the FK. Null the optional
				// pointer; keep the conversation. Not applied when soft-deleted
				// rows are exported (IncludeDeleted), since the object is present.
				"object_id": `CASE WHEN go.id IS NULL OR go.deleted_at IS NOT NULL THEN NULL ELSE t."object_id" END AS "object_id"`,
			}},
		{name: "chat_messages", table: "kb.chat_messages", projectFilter: "c.project_id = ?",
			join: "INNER JOIN kb.chat_conversations c ON c.id = t.conversation_id", deletedColumn: "deleted_at", gate: gateChat},
		{name: "object_extraction_jobs", table: "kb.object_extraction_jobs", projectFilter: byProject,
			extraWhere: "t.status IN ('completed', 'failed')"},
		{name: "project_memberships", table: "kb.project_memberships", projectFilter: byProject},

		// --- Graph / schema state ---
		{name: "object_type_schemas", table: "kb.object_type_schemas", projectFilter: byProject},
		{name: "graph_schemas", table: "kb.graph_schemas", projectFilter: byProject},
		{name: "project_schemas", table: "kb.project_schemas", projectFilter: byProject, deletedColumn: "removed_at"},
		{name: "project_object_schema_registry", table: "kb.project_object_schema_registry", projectFilter: byProject},
		{name: "project_edge_schema_registry", table: "kb.project_edge_schema_registry", projectFilter: byProject},
		{name: "schema_migration_jobs", table: "kb.schema_migration_jobs", projectFilter: byProject},
		{name: "schema_migration_runs", table: "kb.schema_migration_runs", projectFilter: byProject},

		// --- Branches (branch_lineage is re-derived, not streamed) ---
		{name: "branches", table: "kb.branches", projectFilter: byProject},
		{name: "branch_lineage", derived: true},

		// --- Config ---
		{name: "project_settings", table: "kb.project_settings", projectFilter: byProject},
		{name: "project_model_config", table: "kb.project_model_config", projectFilter: byProject, orderBy: "project_id"},
		{name: "project_provider_configs", table: "kb.project_provider_configs", projectFilter: byProject},
		{name: "embedding_policies", table: "kb.embedding_policies", projectFilter: byProject},

		// --- Agents / skills ---
		{name: "agents", table: "kb.agents", projectFilter: byProject},
		{name: "agent_definitions", table: "kb.agent_definitions", projectFilter: byProject},
		{name: "agent_webhook_hooks", table: "kb.agent_webhook_hooks", projectFilter: byProject},
		// project_id = ? excludes org-level (project_id IS NULL) and global skills.
		{name: "skills", table: "kb.skills", projectFilter: byProject, vectorColumns: []string{"description_embedding"}},
		{name: "mcp_servers", table: "kb.mcp_servers", projectFilter: byProject},

		// --- Taxonomy ---
		{name: "tags", table: "kb.tags", projectFilter: byProject},
		{name: "tasks", table: "kb.tasks", projectFilter: byProject},
		{name: "external_sources", table: "kb.external_sources", projectFilter: byProject},
		{name: "product_versions", table: "kb.product_versions", projectFilter: byProject},
		{name: "sandbox_images", table: "kb.sandbox_images", projectFilter: byProject},

		// --- Journal (opt-in, gated by includeJournal) ---
		{name: "project_journal", table: "kb.project_journal", projectFilter: byProject, gate: gateJournal},
		{name: "project_journal_notes", table: "kb.project_journal_notes", projectFilter: byProject, gate: gateJournal},
	}
}

// enabledExportTables filters the curated surface by the opt-in gates.
func enabledExportTables(opts ExportOptions) []tableConfig {
	var out []tableConfig
	for _, cfg := range allExportTables() {
		switch cfg.gate {
		case gateChat:
			if !opts.IncludeChat {
				continue
			}
		case gateJournal:
			if !opts.IncludeJournal {
				continue
			}
		}
		out = append(out, cfg)
	}
	return out
}

// exportTable streams one table's rows as NDJSON (called per-entry by the
// creator so zip entries are created and written in immediate succession).
func (e *Exporter) exportTable(ctx context.Context, cfg tableConfig, w io.Writer, opts ExportOptions) (int, error) {
	schema, table := splitTable(cfg.table)

	cols, err := e.tableColumns(ctx, schema, table)
	if err != nil {
		return 0, fmt.Errorf("export %s: resolve columns: %w", cfg.name, err)
	}

	colSet := make(map[string]bool, len(cols))
	for _, col := range cols {
		colSet[col.Name] = true
	}
	exprs, vectorCols := projectionColumns(cols, cfg, opts.IncludeDeleted)

	query := e.db.NewSelect().
		TableExpr(cfg.table + " AS t").
		ColumnExpr(strings.Join(exprs, ", "))

	if cfg.join != "" {
		query = query.Join(cfg.join)
	}
	// leftJoin supplies the aliases referenced by columnExprs, which only apply
	// when soft-deleted rows are excluded.
	if cfg.leftJoin != "" && !opts.IncludeDeleted && len(cfg.columnExprs) > 0 {
		query = query.Join(cfg.leftJoin)
	}
	if cfg.projectFilter != "" {
		query = query.Where(cfg.projectFilter, opts.ProjectID)
	}
	// deletedColumn is only applied when the live schema actually has it, so
	// legacy configs degrade gracefully on schemas without soft deletes.
	if f := deletedColumnFilter(cfg, opts.IncludeDeleted, colSet); f != "" {
		query = query.Where(f)
	}
	if cfg.extraWhere != "" {
		query = query.Where(cfg.extraWhere)
	}
	order := cfg.orderBy
	if order == "" {
		order = "id"
	}
	if colSet[order] {
		query = query.OrderExpr("t." + quoteIdent(order) + " ASC")
	}

	return e.streamQuery(ctx, query, w, cfg.name, vectorCols)
}

// selectColumnExpr returns the SELECT expression for a column and whether it is
// a vector column (needing post-parse to []float32). jsonb/json/array columns
// are cast ::text because pgx scans them into any as raw []byte, which
// json.Marshal base64-encodes; casting yields a plain string that round-trips.
func selectColumnExpr(col colInfo, vectorListed bool) (expr string, isVector bool) {
	if vectorListed || col.UDTName == "vector" || col.UDTName == "halfvec" {
		return fmt.Sprintf("t.%s::text AS %s", quoteIdent(col.Name), quoteIdent(col.Name)), true
	}
	if col.UDTName == "jsonb" || col.DataType == "jsonb" || col.DataType == "json" {
		return fmt.Sprintf("t.%s::text AS %s", quoteIdent(col.Name), quoteIdent(col.Name)), false
	}
	if col.DataType == "ARRAY" {
		return fmt.Sprintf("t.%s::text AS %s", quoteIdent(col.Name), quoteIdent(col.Name)), false
	}
	return "t." + quoteIdent(col.Name), false
}

// softNullResolution computes the extra LEFT JOIN clauses and per-column SELECT
// overrides for cfg's softNull entries. When includeDeleted is true the
// referenced rows are exported too, so no join or override is produced.
// Columns absent from colSet are skipped so legacy schemas degrade gracefully.
func softNullResolution(cfg tableConfig, includeDeleted bool, colSet map[string]bool) (joins []string, exprs map[string]string) {
	if includeDeleted || len(cfg.softNull) == 0 {
		return nil, nil
	}
	exprs = make(map[string]string, len(cfg.softNull))
	for col, ref := range cfg.softNull {
		if !colSet[col] {
			continue
		}
		joins = append(joins, ref.join)
		exprs[col] = softNullCaseExpr(col, ref.alias)
	}
	if len(exprs) == 0 {
		return nil, nil
	}
	return joins, exprs
}

// softNullCaseExpr builds the SELECT expression that emits a column as NULL
// when the referenced row is missing or soft-deleted, and the raw column value
// otherwise. The referenced row's identity and soft-delete columns are `id` and
// `deleted_at` respectively.
func softNullCaseExpr(col, alias string) string {
	return fmt.Sprintf("CASE WHEN %s.id IS NULL OR %s.deleted_at IS NOT NULL THEN NULL ELSE t.%s END AS %s",
		alias, alias, quoteIdent(col), quoteIdent(col))
}

// deletedColumnFilter returns the WHERE fragment that excludes soft-deleted
// rows, or "" when there is nothing to filter (IncludeDeleted is true, no
// deletedColumn is configured, or the live schema lacks the column).
func deletedColumnFilter(cfg tableConfig, includeDeleted bool, colSet map[string]bool) string {
	if includeDeleted || cfg.deletedColumn == "" || !colSet[cfg.deletedColumn] {
		return ""
	}
	return "t." + quoteIdent(cfg.deletedColumn) + " IS NULL"
}

// branchRow is the minimal shape of kb.branches needed to re-derive lineage.
type branchRow struct {
	ID             string    `bun:"id"`
	ParentBranchID *string   `bun:"parent_branch_id"`
	CreatedAt      time.Time `bun:"created_at"`
}

// exportBranchLineage re-derives kb.branch_lineage from kb.branches
// (parent_branch_id) because lineage rows carry no project_id. The shape
// mirrors what branches.Store.EnsureBranchLineage inserts: a self row at
// depth 0 plus one row per ancestor at hop distance.
func (e *Exporter) exportBranchLineage(ctx context.Context, w io.Writer, projectID string) (int, error) {
	var branches []branchRow
	if err := e.db.NewSelect().
		Table("kb.branches").
		Column("id", "parent_branch_id", "created_at").
		Where("project_id = ?", projectID).
		Order("created_at ASC").
		Scan(ctx, &branches); err != nil {
		return 0, fmt.Errorf("export branch_lineage: load branches: %w", err)
	}

	byID := make(map[string]branchRow, len(branches))
	for _, b := range branches {
		byID[b.ID] = b
	}

	encoder := json.NewEncoder(w)
	count := 0
	for _, b := range branches {
		// Self lineage row (depth 0).
		if err := encoder.Encode(branchLineageRow(b.ID, b.ID, 0, b.CreatedAt)); err != nil {
			return count, fmt.Errorf("encode branch_lineage row: %w", err)
		}
		count++

		// Walk parent links: one row per ancestor at hop distance.
		depth := 1
		ancestorID := b.ParentBranchID
		for ancestorID != nil {
			anc, ok := byID[*ancestorID]
			if !ok {
				// Parent is outside this project (or orphaned); stop walking.
				break
			}
			if err := encoder.Encode(branchLineageRow(b.ID, anc.ID, depth, b.CreatedAt)); err != nil {
				return count, fmt.Errorf("encode branch_lineage row: %w", err)
			}
			count++
			ancestorID = anc.ParentBranchID
			depth++
		}
	}

	e.log.Debug("derived branch_lineage",
		slog.String("project_id", projectID),
		slog.Int("rows", count),
	)
	return count, nil
}

// branchLineageRow renders one NDJSON record with the kb.branch_lineage
// column names so the importer can insert rows directly.
func branchLineageRow(branchID, ancestorID string, depth int, createdAt time.Time) map[string]any {
	return map[string]any{
		"branch_id":          branchID,
		"ancestor_branch_id": ancestorID,
		"depth":              depth,
		"created_at":         createdAt,
	}
}

// colInfo describes one table column needed to serialize its rows faithfully.
type colInfo struct {
	Name     string `bun:"column_name"`
	DataType string `bun:"data_type"`
	UDTName  string `bun:"udt_name"`
}

// tableColumns returns the column names and types of a table in ordinal order.
func (e *Exporter) tableColumns(ctx context.Context, schema, table string) ([]colInfo, error) {
	var cols []colInfo
	err := e.db.NewSelect().
		TableExpr("information_schema.columns").
		Column("column_name", "data_type", "udt_name").
		Where("table_schema = ?", schema).
		Where("table_name = ?", table).
		Order("ordinal_position").
		Scan(ctx, &cols)
	if err != nil {
		return nil, err
	}
	return cols, nil
}

// streamQuery executes a query in ordered batches and streams rows as NDJSON.
func (e *Exporter) streamQuery(ctx context.Context, query *bun.SelectQuery, w io.Writer, tableName string, vectorCols []string) (int, error) {
	encoder := json.NewEncoder(w)
	count := 0
	const batchSize = 1000

	var offset int
	for {
		// Fetch batch
		var rows []map[string]any
		err := query.
			Limit(batchSize).
			Offset(offset).
			Scan(ctx, &rows)

		if err != nil {
			e.log.Error("failed to export table",
				slog.String("table", tableName),
				slog.Int("offset", offset),
				slog.Any("error", err),
			)
			return count, fmt.Errorf("export %s: %w", tableName, err)
		}

		// No more rows
		if len(rows) == 0 {
			break
		}

		// Write each row as NDJSON
		for _, row := range rows {
			if err := serializeVectorColumns(row, vectorCols); err != nil {
				return count, fmt.Errorf("export %s row: %w", tableName, err)
			}
			if err := encoder.Encode(row); err != nil {
				e.log.Error("failed to encode row",
					slog.String("table", tableName),
					slog.Any("error", err),
				)
				return count, fmt.Errorf("encode %s row: %w", tableName, err)
			}
			count++
		}

		offset += batchSize

		// Check for cancellation
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
	}

	e.log.Debug("exported table",
		slog.String("table", tableName),
		slog.Int("rows", count),
	)

	return count, nil
}

// serializeVectorColumns converts ::text-cast vector values into []float32 so
// they JSON-encode as arrays of floats instead of bracketed strings.
func serializeVectorColumns(row map[string]any, vectorCols []string) error {
	for _, col := range vectorCols {
		v, ok := row[col]
		if !ok || v == nil {
			continue
		}
		var s string
		switch t := v.(type) {
		case string:
			s = t
		case []byte:
			s = string(t)
		default:
			// Not a scanned text value; leave as-is.
			continue
		}
		parsed, err := pgutils.ParseVector(s)
		if err != nil {
			return fmt.Errorf("parse vector column %q: %w", col, err)
		}
		row[col] = parsed
	}
	return nil
}

// splitTable splits "kb.documents" into schema and table name.
func splitTable(qualified string) (string, string) {
	schema, table, ok := strings.Cut(qualified, ".")
	if !ok {
		return "kb", qualified
	}
	return schema, table
}

// quoteIdent double-quotes a SQL identifier.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
