// Command schema-dbml renders docs/schema.dbml from a live PostgreSQL catalog.
//
// It replaces the Nx-era `npm run db:docs:generate`; that tooling is long gone,
// and the checked-in DBML had been frozen at 36 tables until this command
// existed.
//
// The output is deterministic: tables are ordered by schema then name, columns
// keep their catalog order, and references follow constraint order. Regenerating
// an unchanged database therefore produces no diff.
//
// Usage:
//
//	go run ./apps/server/cmd/schema-dbml -database-url "$DATABASE_URL"
//	go run ./apps/server/cmd/schema-dbml -database-url "$DATABASE_URL" -check
//
// -check exits 1 when the file on disk differs from the catalog.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultOutput = "docs/schema.dbml"

// schemas documents the namespaces the product owns. pg_catalog,
// information_schema and extension-owned schemas are excluded.
var schemas = []string{"core", "kb", "public"}

const tableColumnsSQL = `
select n.nspname, c.relname, a.attname,
       format_type(a.atttypid, a.atttypmod) as coltype,
       a.attnotnull,
       coalesce(pg_get_expr(d.adbin, d.adrelid), '') as default_expr,
       coalesce(col_description(c.oid, a.attnum), '') as comment
from pg_attribute a
join pg_class c on c.oid = a.attrelid
join pg_namespace n on n.oid = c.relnamespace
left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
where c.relkind in ('r', 'p')
  and n.nspname = any($1)
  and a.attnum > 0
  and not a.attisdropped
order by n.nspname, c.relname, a.attnum`

const constraintsSQL = `
select n.nspname, c.relname, con.conname, con.contype,
       coalesce((
         select string_agg(a.attname, ',' order by k.ord)
         from unnest(con.conkey) with ordinality as k(attnum, ord)
         join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.attnum
       ), '') as cols
from pg_constraint con
join pg_class c on c.oid = con.conrelid
join pg_namespace n on n.oid = c.relnamespace
where con.contype in ('p', 'u')
  and n.nspname = any($1)
order by n.nspname, c.relname, con.conname`

const fksSQL = `
select n.nspname, c.relname,
       coalesce((
         select string_agg(a.attname, ',' order by k.ord)
         from unnest(con.conkey) with ordinality as k(attnum, ord)
         join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.attnum
       ), '') as cols,
       fn.nspname as ref_schema, fc.relname as ref_table,
       coalesce((
         select string_agg(a.attname, ',' order by k.ord)
         from unnest(con.confkey) with ordinality as k(attnum, ord)
         join pg_attribute a on a.attrelid = con.confrelid and a.attnum = k.attnum
       ), '') as ref_cols
from pg_constraint con
join pg_class c on c.oid = con.conrelid
join pg_namespace n on n.oid = c.relnamespace
join pg_class fc on fc.oid = con.confrelid
join pg_namespace fn on fn.oid = fc.relnamespace
where con.contype = 'f'
  and n.nspname = any($1)
order by n.nspname, c.relname, con.conname`

const indexesSQL = `
select n.nspname, t.relname, ic.relname as index_name, ix.indisunique,
       coalesce((
         select string_agg(pg_get_indexdef(ix.indexrelid, k, true), E'\x1f' order by k)
         from generate_series(1, ix.indnkeyatts) as k
       ), '') as cols,
       coalesce(pg_get_expr(ix.indpred, ix.indrelid), '') as predicate
from pg_index ix
join pg_class ic on ic.oid = ix.indexrelid
join pg_class t on t.oid = ix.indrelid
join pg_namespace n on n.oid = t.relnamespace
where n.nspname = any($1)
  and not ix.indisprimary
  and not exists (
    select 1 from pg_constraint con
    where con.conindid = ix.indexrelid and con.contype in ('p', 'u')
  )
order by n.nspname, t.relname, ic.relname`

const enumsSQL = `
select n.nspname, t.typname, e.enumlabel
from pg_enum e
join pg_type t on t.oid = e.enumtypid
join pg_namespace n on n.oid = t.typnamespace
where n.nspname = any($1)
order by n.nspname, t.typname, e.enumsortorder`

const tableCommentsSQL = `
select n.nspname, c.relname, obj_description(c.oid, 'pg_class')
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where c.relkind in ('r', 'p')
  and n.nspname = any($1)
  and obj_description(c.oid, 'pg_class') is not null
order by n.nspname, c.relname`

type tableKey struct {
	schema, table string
}

type column struct {
	name, typ, defaultExpr, comment string
	notNull                         bool
}

type index struct {
	name   string
	unique bool
	cols   []string
	// predicate is a partial index's WHERE expression. DBML has no predicate
	// syntax, so it is emitted as a note: without it an index that is unique
	// only among active rows reads as a plain unique index.
	predicate string
}

type foreignKey struct {
	key                 tableKey
	cols, refCols       []string
	refSchema, refTable string
}

// catalog holds everything read from the database before rendering.
type catalog struct {
	columns         map[tableKey][]column
	primaryKey      map[tableKey]map[string]bool
	uniqueSingle    map[tableKey]map[string]bool
	uniqueComposite map[tableKey][][]string
	indexes         map[tableKey][]index
	fks             []foreignKey
	enums           map[tableKey][]string
	comments        map[tableKey]string
}

func main() {
	out := flag.String("out", defaultOutput, "DBML output path")
	check := flag.Bool("check", false, "exit 1 when the output path is stale")
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	flag.Parse()

	if *databaseURL == "" {
		fmt.Fprintln(os.Stderr, "schema-dbml: -database-url or DATABASE_URL is required")
		os.Exit(2)
	}

	db, err := sql.Open("pgx", *databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema-dbml: connect: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	rendered, tableCount, err := render(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema-dbml: %v\n", err)
		os.Exit(1)
	}

	if *check {
		current, readErr := os.ReadFile(*out)
		if readErr != nil && !os.IsNotExist(readErr) {
			fmt.Fprintf(os.Stderr, "schema-dbml: read %s: %v\n", *out, readErr)
			os.Exit(1)
		}
		if string(current) != rendered {
			fmt.Fprintf(os.Stderr, "schema-dbml: %s is stale — regenerate it\n", *out)
			os.Exit(1)
		}
		fmt.Printf("schema-dbml: %s is current\n", *out)
		return
	}

	if err := os.WriteFile(*out, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "schema-dbml: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("schema-dbml: wrote %s (%d tables)\n", *out, tableCount)
}

func render(db *sql.DB) (string, int, error) {
	c, err := readCatalog(db)
	if err != nil {
		return "", 0, err
	}

	order := make([]tableKey, 0, len(c.columns))
	for key := range c.columns {
		order = append(order, key)
	}
	sortTableKeys(order)

	var b strings.Builder
	b.WriteString("// Generated from the live PostgreSQL catalog by apps/server/cmd/schema-dbml.\n")
	b.WriteString("// Do not hand-edit: apply migrations, then regenerate.\n\n")

	enumKeys := make([]tableKey, 0, len(c.enums))
	for key := range c.enums {
		enumKeys = append(enumKeys, key)
	}
	sortTableKeys(enumKeys)
	for _, key := range enumKeys {
		fmt.Fprintf(&b, "Enum %s {\n", qualify(key.schema, key.table))
		for _, label := range c.enums[key] {
			fmt.Fprintf(&b, "  %s\n", quote(label))
		}
		b.WriteString("}\n\n")
	}

	for _, key := range order {
		fmt.Fprintf(&b, "Table %s {\n", qualify(key.schema, key.table))
		pk := c.primaryKey[key]
		uniq := c.uniqueSingle[key]
		for _, col := range c.columns[key] {
			var settings []string
			switch {
			case pk[col.name]:
				settings = append(settings, "pk")
			case col.notNull:
				settings = append(settings, "not null")
			}
			if uniq[col.name] && !pk[col.name] {
				settings = append(settings, "unique")
			}
			if setting := defaultSetting(col.typ, col.defaultExpr); setting != "" {
				settings = append(settings, setting)
			}
			if col.comment != "" {
				settings = append(settings, "note: '"+escapeString(collapse(col.comment))+"'")
			}
			suffix := ""
			if len(settings) > 0 {
				suffix = " [" + strings.Join(settings, ", ") + "]"
			}
			fmt.Fprintf(&b, "  %s %s%s\n", quote(col.name), mapType(col.typ), suffix)
		}

		composite := c.uniqueComposite[key]
		idx := c.indexes[key]
		if len(composite) > 0 || len(idx) > 0 {
			b.WriteString("\n  Indexes {\n")
			for _, cols := range composite {
				fmt.Fprintf(&b, "    %s [unique]\n", renderIndexCols(cols))
			}
			for _, ix := range idx {
				fmt.Fprintf(&b, "    %s %s\n", renderIndexCols(ix.cols), indexSetting(ix))
			}
			b.WriteString("  }\n")
		}
		b.WriteString("}\n")
		if comment := c.comments[key]; comment != "" {
			fmt.Fprintf(&b, "// %s.%s: %s\n", key.schema, key.table, collapse(comment))
		}
		b.WriteString("\n")
	}

	for _, line := range renderRefs(c) {
		b.WriteString(line + "\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n", len(order), nil
}

// renderRefs emits one DBML Ref per foreign key. A single-column key references
// a single column; a composite key keeps its column tuple, because splitting it
// into per-column refs would describe references PostgreSQL rejects (there is no
// matching unique constraint on the parent's individual columns).
//
// Every key is rendered many-to-one (">"). DBML's one-to-one operator ("-") is
// direction-less: dbml2sql has to pick which side holds the foreign key and can
// place it on the parent, producing DDL that inverts the relationship. Keeping
// ">" preserves the direction the catalog actually records.
func renderRefs(c *catalog) []string {
	if len(c.fks) == 0 {
		return nil
	}
	lines := []string{"// References"}
	seen := map[string]bool{}
	for _, fk := range c.fks {
		if len(fk.cols) == 0 || len(fk.cols) != len(fk.refCols) {
			continue
		}
		line := fmt.Sprintf("Ref: %s > %s",
			refSide(fk.key, fk.cols), refSide(tableKey{fk.refSchema, fk.refTable}, fk.refCols))
		if seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return lines
}

func refSide(key tableKey, cols []string) string {
	if len(cols) == 1 {
		return qualify(key.schema, key.table) + "." + quote(cols[0])
	}
	quoted := make([]string, len(cols))
	for i, col := range cols {
		quoted[i] = quote(col)
	}
	return qualify(key.schema, key.table) + ".(" + strings.Join(quoted, ", ") + ")"
}

func readCatalog(db *sql.DB) (*catalog, error) {
	c := &catalog{
		columns:         map[tableKey][]column{},
		primaryKey:      map[tableKey]map[string]bool{},
		uniqueSingle:    map[tableKey]map[string]bool{},
		uniqueComposite: map[tableKey][][]string{},
		indexes:         map[tableKey][]index{},
		enums:           map[tableKey][]string{},
		comments:        map[tableKey]string{},
	}

	if err := query(db, tableColumnsSQL, func(rows *sql.Rows) error {
		var key tableKey
		var col column
		if err := rows.Scan(&key.schema, &key.table, &col.name, &col.typ, &col.notNull, &col.defaultExpr, &col.comment); err != nil {
			return err
		}
		c.columns[key] = append(c.columns[key], col)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}

	if err := query(db, constraintsSQL, func(rows *sql.Rows) error {
		var key tableKey
		var name, contype, cols string
		if err := rows.Scan(&key.schema, &key.table, &name, &contype, &cols); err != nil {
			return err
		}
		parts := splitList(cols)
		if contype == "p" {
			if c.primaryKey[key] == nil {
				c.primaryKey[key] = map[string]bool{}
			}
			for _, p := range parts {
				c.primaryKey[key][p] = true
			}
			return nil
		}
		if len(parts) == 1 {
			if c.uniqueSingle[key] == nil {
				c.uniqueSingle[key] = map[string]bool{}
			}
			c.uniqueSingle[key][parts[0]] = true
			return nil
		}
		if len(parts) > 1 {
			c.uniqueComposite[key] = append(c.uniqueComposite[key], parts)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("constraints: %w", err)
	}

	if err := query(db, fksSQL, func(rows *sql.Rows) error {
		var key tableKey
		var cols, refSchema, refTable, refCols string
		if err := rows.Scan(&key.schema, &key.table, &cols, &refSchema, &refTable, &refCols); err != nil {
			return err
		}
		c.fks = append(c.fks, foreignKey{
			key:       key,
			cols:      splitList(cols),
			refSchema: refSchema,
			refTable:  refTable,
			refCols:   splitList(refCols),
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreign keys: %w", err)
	}

	if err := query(db, indexesSQL, func(rows *sql.Rows) error {
		var key tableKey
		var ix index
		var cols string
		if err := rows.Scan(&key.schema, &key.table, &ix.name, &ix.unique, &cols, &ix.predicate); err != nil {
			return err
		}
		ix.cols = splitListSep(cols, "\x1f")
		c.indexes[key] = append(c.indexes[key], ix)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("indexes: %w", err)
	}

	if err := query(db, enumsSQL, func(rows *sql.Rows) error {
		var key tableKey
		var label string
		if err := rows.Scan(&key.schema, &key.table, &label); err != nil {
			return err
		}
		c.enums[key] = append(c.enums[key], label)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("enums: %w", err)
	}

	if err := query(db, tableCommentsSQL, func(rows *sql.Rows) error {
		var key tableKey
		var comment string
		if err := rows.Scan(&key.schema, &key.table, &comment); err != nil {
			return err
		}
		c.comments[key] = comment
		return nil
	}); err != nil {
		return nil, fmt.Errorf("table comments: %w", err)
	}

	return c, nil
}

func query(db *sql.DB, sqlText string, each func(*sql.Rows) error) error {
	rows, err := db.Query(sqlText, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

var simpleIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func renderIndexCols(cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		if simpleIdent.MatchString(c) {
			parts[i] = c
		} else {
			parts[i] = "`" + strings.ReplaceAll(c, "`", "\\`") + "`"
		}
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func qualify(schema, name string) string { return quote(schema) + "." + quote(name) }

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func splitListSep(s, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, sep)
}

var typeAliases = []struct{ pattern, replacement string }{
	{`^character varying`, `varchar`},
	{`^character\(`, `char(`},
	{`^timestamp with time zone$`, `timestamptz`},
	{`^timestamp without time zone$`, `timestamp`},
	{`^time with time zone$`, `timetz`},
	{`^time without time zone$`, `time`},
	// float8, not `double`: DBML passes type names through verbatim and PostgreSQL
	// has no bare `double`.
	{`^double precision$`, `float8`},
	{`^bit varying`, `varbit`},
}

func mapType(colType string) string {
	out := colType
	for _, alias := range typeAliases {
		if re, err := regexp.Compile(alias.pattern); err == nil {
			out = re.ReplaceAllString(out, alias.replacement)
		}
	}
	return out
}

var (
	castLiteral = regexp.MustCompile(`^'(.*)'::[\w .\[\]"]+$`)
	castSuffix  = regexp.MustCompile(`::[\w .\[\]"]+$`)
	numericExpr = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

func formatDefault(expr string) string {
	expr = collapse(expr)
	if m := castLiteral.FindStringSubmatch(expr); m != nil {
		return "default: '" + escapeString(m[1]) + "'"
	}
	expr = castSuffix.ReplaceAllString(expr, "")
	switch expr {
	case "true", "false":
		return "default: " + expr
	case "NULL":
		return "default: null"
	}
	if numericExpr.MatchString(expr) {
		return "default: " + expr
	}
	return "default: `" + strings.ReplaceAll(expr, "`", "\\`") + "`"
}

// defaultSetting renders a column default. A serial/identity column becomes
// `increment`: emitting the nextval() expression instead would reference a
// sequence the document never creates.
func defaultSetting(colType, expr string) string {
	if expr == "" {
		return ""
	}
	if isSequenceDefault(expr) && isIntegerType(colType) {
		return "increment"
	}
	return formatDefault(expr)
}

var sequenceDefault = regexp.MustCompile(`^nextval\('.*'::regclass\)$`)

func isSequenceDefault(expr string) bool { return sequenceDefault.MatchString(collapse(expr)) }

func isIntegerType(colType string) bool {
	switch colType {
	case "integer", "bigint", "smallint":
		return true
	}
	return false
}

// indexSetting renders one index's DBML settings, including a partial index's
// predicate as a note.
func indexSetting(ix index) string {
	parts := []string{"name: " + quote(ix.name)}
	if ix.unique {
		parts = append(parts, "unique")
	}
	if ix.predicate != "" {
		parts = append(parts, "note: '"+escapeString(collapse(ix.predicate))+"'")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "'", `\'`)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func sortTableKeys(keys []tableKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].schema != keys[j].schema {
			return keys[i].schema < keys[j].schema
		}
		return keys[i].table < keys[j].table
	})
}
