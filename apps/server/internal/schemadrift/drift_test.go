package schemadrift

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// columnsQuery returns every column in the kb and core schemas from
// information_schema, keyed by "schema.table".
const columnsQuery = `
SELECT table_schema, table_name, column_name, is_nullable, udt_name, data_type
FROM information_schema.columns
WHERE table_schema IN ('kb', 'core')
ORDER BY table_schema, table_name, ordinal_position`

const tablesQuery = `
SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_schema IN ('kb', 'core') AND table_type = 'BASE TABLE'
ORDER BY table_schema, table_name`

type colRow struct {
	Schema   string `bun:"table_schema"`
	Table    string `bun:"table_name"`
	Column   string `bun:"column_name"`
	Nullable string `bun:"is_nullable"`
	UDT      string `bun:"udt_name"`
	DataType string `bun:"data_type"`
}

// loadSchema reads the migrated schema (kb and core) into a map keyed by
// "schema.table".
func loadSchema(ctx context.Context, t *testing.T, db *testdb.TestDB) map[string]map[string]schemaColumn {
	t.Helper()

	var rows []colRow
	if err := db.DB.NewRaw(columnsQuery).Scan(ctx, &rows); err != nil {
		t.Fatalf("query information_schema.columns: %v", err)
	}

	// A table with zero columns cannot exist, but listing tables separately
	// ensures PresentTables reflects every base table even if a future query
	// filters columns.
	var tableRows []colRow
	if err := db.DB.NewRaw(tablesQuery).Scan(ctx, &tableRows); err != nil {
		t.Fatalf("query information_schema.tables: %v", err)
	}

	schema := map[string]map[string]schemaColumn{}
	for _, r := range rows {
		key := r.Schema + "." + r.Table
		if schema[key] == nil {
			schema[key] = map[string]schemaColumn{}
		}
		schema[key][r.Column] = schemaColumn{
			Name:     r.Column,
			Nullable: strings.EqualFold(r.Nullable, "YES"),
			UDTName:  r.UDT,
			DataType: r.DataType,
		}
	}
	for _, r := range tableRows {
		key := r.Schema + "." + r.Table
		if schema[key] == nil {
			schema[key] = map[string]schemaColumn{}
		}
	}
	return schema
}

// TestModelsMatchMigratedSchema is the drift guard: it reflects over every
// registered bun model and compares its columns (names, nullability, explicit
// types) against the real schema produced by applying the embedded migrations
// to head.
func TestModelsMatchMigratedSchema(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "schemadrift")
	defer db.Close()

	schema := loadSchema(ctx, t, db)
	modelTables := reflectModels(db.DB, models)

	rep := compare(modelTables, schema)

	for _, m := range rep.PhantomColumns {
		t.Errorf("phantom column: %s", m)
	}
	for _, m := range rep.NullabilityMismatch {
		t.Errorf("nullability mismatch: %s", m)
	}
	for _, m := range rep.TypeMismatch {
		t.Errorf("type mismatch: %s", m)
	}

	// Allowlisted nullability mismatches: log them (with their reason) and fail
	// on any allowlist entry that did not match a real mismatch (a stale entry).
	for _, key := range rep.MatchedNullabilityAllowlist {
		t.Logf("allowlisted nullability mismatch (%s): %s", nullabilityAllowlist[key], key)
	}
	matched := map[string]bool{}
	for _, key := range rep.MatchedNullabilityAllowlist {
		matched[key] = true
	}
	for key := range nullabilityAllowlist {
		if !matched[key] {
			t.Errorf("stale nullability allowlist entry no longer matches a real mismatch: %s", key)
		}
	}

	for _, c := range rep.UnusedColumns {
		t.Logf("unused column (schema has it, no struct references it): %s", c)
	}
	for _, c := range rep.NullableScanRisk {
		t.Logf("nullable scan risk (column nullable, struct field cannot hold NULL): %s", c)
	}

	presentSet := map[string]bool{}
	for _, table := range rep.PresentTables {
		presentSet[table] = true
	}
	coveredSet := map[string]bool{}
	for _, table := range rep.CoveredTables {
		coveredSet[table] = true
	}
	var unmodeled []string
	for table := range presentSet {
		if coveredSet[table] {
			continue
		}
		if _, ok := censusExclusions[table]; ok {
			continue
		}
		unmodeled = append(unmodeled, table)
	}
	sort.Strings(unmodeled)
	for _, table := range unmodeled {
		t.Logf("table present in schema with no bun model: %s", table)
	}

	t.Logf("coverage: %d tables with a model, %d tables in kb/core schema",
		len(rep.CoveredTables), len(rep.PresentTables))

	checkUnexportedTables(t, schema)
}

// checkUnexportedTables compares the census-derived columns of the unexported
// models (which the registry cannot reflect over) against the migrated schema at
// name level only. This still catches the phantom-column / missing-column class
// of drift for those tables.
func checkUnexportedTables(t *testing.T, schema map[string]map[string]schemaColumn) {
	c, err := census()
	if err != nil {
		t.Fatalf("census for unexported models: %v", err)
	}

	for table, reason := range censusExclusions {
		m, ok := c[table]
		if !ok {
			t.Fatalf("excluded table %s (%s) not found by census", table, reason)
		}
		cols, ok := schema[table]
		if !ok {
			t.Errorf("excluded table %s (%s) is missing from the migrated schema", table, reason)
			continue
		}
		used := map[string]bool{}
		for _, name := range m.Columns {
			used[name] = true
			if _, ok := cols[name]; !ok {
				t.Errorf("phantom column on unexported model %s.%s: %s.%s", m.Pkg, m.Name, table, name)
			}
		}
		var unused []string
		for name := range cols {
			if !used[name] {
				unused = append(unused, name)
			}
		}
		sort.Strings(unused)
		for _, name := range unused {
			t.Logf("unused column (unexported model %s.%s): %s.%s", m.Pkg, m.Name, table, name)
		}
	}
}
