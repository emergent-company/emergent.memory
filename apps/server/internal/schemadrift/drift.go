package schemadrift

import (
	"reflect"
	"sort"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

// nullabilityAllowlist records deferred, pre-existing "model NOT NULL vs
// migrated column nullable" mismatches that are intentionally not fixed in this
// change. Each entry names its tracking issue. The guard still observes the
// mismatch (MatchedNullabilityAllowlist) and fails on any stale entry that no
// longer matches, so this list cannot silently grow or go out of date.
var nullabilityAllowlist = map[string]string{
	"kb.adk_states.user_id": "model bunsession.ADKState marks user_id pk (hence NOT NULL) but the migrated column is nullable (empty for app scope) and the real PK is id, which the model omits — tracked in #1093",
}

// schemaColumn is one column of a migrated table, as reported by
// information_schema.columns.
type schemaColumn struct {
	Name     string
	Nullable bool   // is_nullable == 'YES'
	UDTName  string // udt_name, e.g. "uuid", "jsonb", "text", "int8", "_text"
	DataType string // data_type, e.g. "ARRAY", "USER-DEFINED", "character varying"
}

// modelField is one column-mapped field of a bun model.
type modelField struct {
	Model           string // "pkg.Type"
	Name            string // SQL column name
	GoType          string
	CanHoldNull     bool   // Go type can scan a NULL (pointer/slice/map/interface or nullzero)
	NotNull         bool   // bun inferred NOT NULL (notnull/pk/autoincrement/identity tag)
	SQLType         string // field.UserSQLType (explicit type tag, else discovered)
	HasExplicitType bool   // field tag carries an explicit type: option
	IsArray         bool
}

// modelTable is a bun model's table and its column-mapped fields.
type modelTable struct {
	Schema string
	Name   string
	Fields []modelField
}

// qualified returns "schema.name".
func (t modelTable) qualified() string { return t.Schema + "." + t.Name }

// reflectModels turns each registered model into its bun table + fields, using
// bun's own dialect so the column names and SQL types match exactly what bun
// would emit at runtime (including `notnull`, `pk`, `type:` overrides and
// array detection).
func reflectModels(db *bun.DB, models []any) []modelTable {
	out := make([]modelTable, 0, len(models))
	for _, m := range models {
		t := db.Table(reflect.TypeOf(m))
		mt := modelTable{
			Schema: t.Schema,
			Name:   strings.TrimPrefix(t.Name, t.Schema+"."),
		}
		for _, f := range t.Fields {
			mt.Fields = append(mt.Fields, modelField{
				Model:           t.TypeName,
				Name:            f.Name,
				GoType:          f.StructField.Type.String(),
				CanHoldNull:     canHoldNull(f),
				NotNull:         f.NotNull,
				SQLType:         f.UserSQLType,
				HasExplicitType: f.Tag.HasOption("type"),
				IsArray:         isArray(f),
			})
		}
		out = append(out, mt)
	}
	return out
}

// canHoldNull reports whether a bun field's Go type can scan a SQL NULL without
// erroring. Pointers, slices, maps and interfaces can; value types (time.Time,
// uuid.UUID, string, int, bool) cannot unless the `nullzero` option lets bun
// write the zero value instead of NULL.
func canHoldNull(f *schema.Field) bool {
	if f.Tag.HasOption("nullzero") {
		return true
	}
	if f.IsPtr {
		return true
	}
	switch f.StructField.Type.Kind() {
	case reflect.Slice, reflect.Map, reflect.Interface:
		return true
	}
	return false
}

// isArray reports whether a bun field maps to a PostgreSQL array column, via
// the `array` tag option or an explicit `[]`-suffixed type. A slice Go type
// alone is NOT evidence of an array column: jsonb columns are frequently backed
// by Go slices/maps (e.g. `[]map[string]any` with `type:jsonb`).
func isArray(f *schema.Field) bool {
	if f.Tag.HasOption("array") {
		return true
	}
	return strings.HasSuffix(f.UserSQLType, "[]")
}

// driftReport aggregates the divergences between the registered models and the
// migrated schema, split into failing classes (phantom columns, nullability
// mismatches, type mismatches) and non-blocking reports (unused columns,
// nullable columns scanned by non-nullable struct fields).
type driftReport struct {
	// Failing.
	PhantomColumns      []string // struct expects a column the schema lacks
	NullabilityMismatch []string // struct NOT NULL but schema nullable
	TypeMismatch        []string // explicit type: tag vs migrated column type

	// Non-blocking (reported, not failed).
	UnusedColumns    []string // schema column no struct references
	NullableScanRisk []string // schema nullable but struct field cannot hold NULL

	// MatchedNullabilityAllowlist records the nullabilityAllowlist keys that
	// actually matched a real mismatch, so the test can fail on stale entries.
	MatchedNullabilityAllowlist []string

	// CoveredTables is the set of tables with at least one registered model;
	// PresentTables is the set of tables in the migrated kb/core schemas.
	CoveredTables []string
	PresentTables []string
}

// compare checks the registered models against the migrated schema.
// schemaTables maps "schema.table" to its columns (from information_schema).
func compare(modelTables []modelTable, schemaTables map[string]map[string]schemaColumn) driftReport {
	var rep driftReport

	// Group model fields by table so multiple structs that map to the same
	// table (e.g. email.EmailJob and superadmin.EmailJob) are compared as a
	// union, not each against the full column set.
	byTable := map[string][]modelField{}
	for _, mt := range modelTables {
		byTable[mt.qualified()] = append(byTable[mt.qualified()], mt.Fields...)
	}

	rep.CoveredTables = sortedKeys(byTable)
	rep.PresentTables = sortedKeys(schemaTables)

	for _, mt := range modelTables {
		cols, ok := schemaTables[mt.qualified()]
		if !ok {
			// A registered model whose table does not exist in the schema is a
			// hard drift: report every column it expects as phantom.
			for _, f := range mt.Fields {
				rep.PhantomColumns = append(rep.PhantomColumns,
					mt.qualified()+"."+f.Name+" (model "+f.Model+": table "+mt.qualified()+" does not exist)")
			}
			continue
		}
		for _, f := range mt.Fields {
			sc, ok := cols[f.Name]
			if !ok {
				rep.PhantomColumns = append(rep.PhantomColumns,
					mt.qualified()+"."+f.Name+" (model "+f.Model+")")
				continue
			}
			checkField(&rep, mt, f, sc)
		}
	}

	// Non-blocking: schema columns no struct references, per table with a model.
	// Tables with no model at all are reported separately by the caller as
	// "present but no bun model", so they are not double-reported here.
	for table, cols := range schemaTables {
		if len(byTable[table]) == 0 {
			continue
		}
		used := map[string]bool{}
		for _, f := range byTable[table] {
			used[f.Name] = true
		}
		var unused []string
		for name := range cols {
			if !used[name] {
				unused = append(unused, name)
			}
		}
		sort.Strings(unused)
		for _, name := range unused {
			rep.UnusedColumns = append(rep.UnusedColumns, table+"."+name)
		}
	}

	sort.Strings(rep.PhantomColumns)
	sort.Strings(rep.NullabilityMismatch)
	sort.Strings(rep.TypeMismatch)
	sort.Strings(rep.NullableScanRisk)
	sort.Strings(rep.MatchedNullabilityAllowlist)
	return rep
}

// checkField applies the nullability and type rules for one struct field that
// exists in the schema.
func checkField(rep *driftReport, mt modelTable, f modelField, sc schemaColumn) {
	// Nullability: the model declares NOT NULL (notnull/pk/autoincrement) but
	// the migrated column is nullable — the schema has drifted more permissive.
	if f.NotNull && sc.Nullable {
		key := mt.qualified() + "." + f.Name
		if _, ok := nullabilityAllowlist[key]; ok {
			rep.MatchedNullabilityAllowlist = append(rep.MatchedNullabilityAllowlist, key)
			return
		}
		rep.NullabilityMismatch = append(rep.NullabilityMismatch,
			key+" (model "+f.Model+" is NOT NULL, column is nullable)")
		return
	}

	// Non-blocking: the column is nullable but the struct field cannot hold a
	// NULL, so a NULL would break a Scan. Reported, not failed, because many
	// partial-view models intentionally omit nullzero and the column is only
	// ever non-NULL for those rows.
	if !f.NotNull && sc.Nullable && !f.CanHoldNull {
		rep.NullableScanRisk = append(rep.NullableScanRisk,
			mt.qualified()+"."+f.Name+" (model "+f.Model+", Go type "+f.GoType+")")
	}

	// Type: only fields with an explicit type: tag encode the author's intent
	// about the column type; discovered types are bun's loose defaults and are
	// not compared (string→VARCHAR vs a TEXT column is a benign supertype).
	if !f.HasExplicitType {
		return
	}
	if want, ok := expectedTypes(f.SQLType, f.IsArray); ok && !typeMatches(want, sc) {
		rep.TypeMismatch = append(rep.TypeMismatch,
			mt.qualified()+"."+f.Name+" (model "+f.Model+": type "+f.SQLType+" vs column "+sc.UDTName+")")
	}
}

// typeMatches reports whether a migrated column's type is compatible with the
// set of expected PostgreSQL type names.
func typeMatches(want []string, sc schemaColumn) bool {
	if len(want) == 1 && want[0] == "__array__" {
		// PostgreSQL array columns report udt_name as "_<elem>" (e.g. "_text").
		return strings.HasPrefix(sc.UDTName, "_") || sc.DataType == "ARRAY"
	}
	for _, w := range want {
		if strings.EqualFold(w, sc.UDTName) {
			return true
		}
	}
	return false
}

// expectedTypes maps a bun SQL type to the PostgreSQL type names that are
// compatible with it. ok is false for types we deliberately do not check.
func expectedTypes(sqlType string, isArray bool) ([]string, bool) {
	if isArray {
		return []string{"__array__"}, true
	}
	switch strings.ToLower(strings.TrimSpace(sqlType)) {
	case "smallint", "int2":
		return []string{"int2"}, true
	case "integer", "int", "int4":
		return []string{"int4"}, true
	case "bigint", "int8":
		return []string{"int8"}, true
	case "real", "float4":
		return []string{"float4"}, true
	case "double precision", "double", "float8":
		return []string{"float8"}, true
	case "boolean", "bool":
		return []string{"bool"}, true
	case "varchar", "text", "char", "bpchar", "character varying":
		return []string{"text", "varchar", "bpchar", "char"}, true
	case "uuid":
		return []string{"uuid"}, true
	case "json", "jsonb":
		return []string{"json", "jsonb"}, true
	case "bytea", "blob":
		return []string{"bytea"}, true
	case "timestamp", "timestamp without time zone":
		return []string{"timestamp"}, true
	case "timestamptz", "timestamp with time zone":
		return []string{"timestamptz"}, true
	case "date":
		return []string{"date"}, true
	case "tsvector":
		return []string{"tsvector"}, true
	case "numeric", "decimal":
		return []string{"numeric"}, true
	case "time", "time without time zone":
		return []string{"time"}, true
	case "timetz", "time with time zone":
		return []string{"timetz"}, true
	case "inet":
		return []string{"inet"}, true
	default:
		return nil, false
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
