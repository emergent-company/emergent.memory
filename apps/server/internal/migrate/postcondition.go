package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// InvalidIndex identifies an index that is not marked both valid and ready in
// the PostgreSQL catalog — the residue a killed CREATE INDEX CONCURRENTLY
// leaves behind (issue #734, migration 00164 recorded applied without the
// index actually existing).
type InvalidIndex struct {
	Schema string
	Table  string
	Index  string
}

// InvalidIndexQuery is a read-only query over pg_index/pg_class/pg_namespace
// that returns indexes in the kb and core schemas which are NOT
// (indisvalid AND indisready).
const InvalidIndexQuery = `
SELECT
    n.nspname AS schema,
    c.relname AS table,
    i.relname AS index
FROM pg_index x
JOIN pg_class c ON c.oid = x.indrelid
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname IN ('kb', 'core')
  AND NOT (x.indisvalid AND x.indisready)
ORDER BY n.nspname, c.relname, i.relname`

// FindInvalidIndexes returns indexes in the kb and core schemas that are not
// both valid and ready. It uses database/sql only and performs no writes.
func FindInvalidIndexes(ctx context.Context, db *sql.DB) ([]InvalidIndex, error) {
	rows, err := db.QueryContext(ctx, InvalidIndexQuery)
	if err != nil {
		return nil, fmt.Errorf("querying invalid indexes: %w", err)
	}
	defer rows.Close()

	var found []InvalidIndex
	for rows.Next() {
		var ii InvalidIndex
		if err := rows.Scan(&ii.Schema, &ii.Table, &ii.Index); err != nil {
			return nil, fmt.Errorf("scanning invalid index row: %w", err)
		}
		found = append(found, ii)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating invalid index rows: %w", err)
	}
	return found, nil
}

// VerifyPostConditions checks for invalid indexes and, when any are found, logs
// loudly and returns a descriptive error.
func (m *Migrator) VerifyPostConditions(ctx context.Context) error {
	found, err := FindInvalidIndexes(ctx, m.db.DB)
	if err != nil {
		return fmt.Errorf("verifying post-conditions: %w", err)
	}
	if len(found) == 0 {
		m.logger.Debug("post-conditions verified: no invalid indexes")
		return nil
	}

	m.logger.Error("post-conditions failed: invalid indexes detected (indisvalid/indisready false)",
		zap.Strings("invalid_indexes", indexNames(found)),
		zap.String("inspect_query", InvalidIndexQuery),
		zap.String("remediation", "REINDEX INDEX CONCURRENTLY <index>, or re-run the owning forward migration"))

	return invalidIndexesError(found)
}

// invalidIndexesError builds the descriptive error for invalid-index detection.
func invalidIndexesError(found []InvalidIndex) error {
	return fmt.Errorf(
		"post-conditions failed: %d invalid index(es) detected: %s; remediation: REINDEX INDEX CONCURRENTLY <index>, or re-run the owning forward migration",
		len(found), strings.Join(indexNames(found), ", "))
}

// indexNames renders each invalid index as "schema.table -> index".
func indexNames(found []InvalidIndex) []string {
	names := make([]string, 0, len(found))
	for _, ii := range found {
		names = append(names, fmt.Sprintf("%s.%s -> %s", ii.Schema, ii.Table, ii.Index))
	}
	return names
}

// MarkAppliedBypassNotice returns the loud warning emitted when a version is
// recorded without running its migration. It states that post-conditions are
// NOT verified, that mark-applied / a manual INSERT bypasses goose, and gives
// the exact verification query and remediation.
func MarkAppliedBypassNotice(version int64) string {
	return fmt.Sprintf(`WARNING: migration version %d was recorded without running its migration.
Post-conditions are NOT verified for manually recorded versions: 'mark-applied'
(or a manual INSERT into goose_db_version) bypasses goose, so any schema objects
the migration would have created (e.g. indexes) are NOT checked.

To verify post-conditions manually, run this query and look for rows:
%s

Remediation for any invalid index found:
  REINDEX INDEX CONCURRENTLY <index>
  or re-run the owning forward migration.`, version, InvalidIndexQuery)
}
