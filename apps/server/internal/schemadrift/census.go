package schemadrift

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// censusModel is one bun model struct found in the source tree, as enumerated
// by a static parse of every .go file. It records the table name (from the
// `bun:"table:..."` tag on the embedded bun.BaseModel field) and the column
// names derived from the fields' `bun` tags.
type censusModel struct {
	Table   string // schema-qualified table name, e.g. "kb.email_jobs"
	Pkg     string // package name
	Name    string // struct name; empty for anonymous structs
	Columns []string
}

// censusRoots are the directories (relative to the module root) scanned for
// models. They cover every package that defines bun models.
var censusRoots = []string{"domain", "pkg", "internal"}

// census returns every bun model struct in the source tree, keyed by table
// name. A struct is a model when it embeds a field (typically bun.BaseModel)
// carrying a `bun:"table:..."` tag. Columns are the non-embedded fields with a
// `bun` tag other than "-".
//
// Enumeration method and its limits:
//   - It parses every non-test .go file under censusRoots with go/parser and
//     walks top-level type declarations only. A model declared as a
//     *function-local* type (e.g. domain/chunking's `embeddingJob` and
//     domain/discoveryjobs' anonymous `row` projections) is NOT enumerated.
//     Those projections target tables that are also covered by a registered,
//     exported model (kb.chunk_embedding_jobs, kb.graph_schemas,
//     kb.project_schemas), so their absence does not drop a table from coverage.
//   - Column names for fields whose `bun` tag has an empty name are derived
//     with the same underscore() algorithm bun uses, so they match reflection.
func census() (map[string]censusModel, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	out := map[string]censusModel{}

	for _, rel := range censusRoots {
		dir := filepath.Join(root, rel)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || strings.HasSuffix(path, "_test.go") || !strings.HasSuffix(path, ".go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					table, cols := structTableAndColumns(st)
					if table == "" {
						continue
					}
					m := censusModel{
						Table:   table,
						Pkg:     f.Name.Name,
						Name:    ts.Name.Name,
						Columns: cols,
					}
					out[table] = m
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// structTableAndColumns extracts the schema-qualified table name and the column
// names from a struct type's field tags.
func structTableAndColumns(st *ast.StructType) (string, []string) {
	table := ""
	var cols []string
	for _, field := range st.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		bt := tag.Get("bun")
		if bt == "" {
			continue
		}

		// The embedded bun.BaseModel field carries the table tag; it is not a
		// column itself.
		if t, ok := tableFromTag(bt); ok {
			table = t
			continue
		}
		if bt == "-" {
			continue
		}

		name := bt
		if i := strings.Index(bt, ","); i >= 0 {
			name = bt[:i]
		}
		if name == "" {
			for _, n := range field.Names {
				name = underscore(n.Name)
			}
		}
		if name == "" {
			continue
		}
		cols = append(cols, name)
	}
	return table, cols
}

// tableFromTag returns the table name from a bun tag's `table:` option.
func tableFromTag(bt string) (string, bool) {
	for _, part := range strings.Split(bt, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "table:") {
			return strings.TrimPrefix(part, "table:"), true
		}
	}
	return "", false
}

// underscore converts "CamelCasedString" to "camel_cased_string", matching bun's
// own default column-name derivation (used when a field's bun tag has no name).
func underscore(s string) string {
	r := make([]byte, 0, len(s)+5)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && i+1 < len(s) && ((s[i-1] >= 'a' && s[i-1] <= 'z') || (s[i+1] >= 'a' && s[i+1] <= 'z')) {
				r = append(r, '_', c+32)
			} else {
				r = append(r, c+32)
			}
		} else {
			r = append(r, c)
		}
	}
	return string(r)
}

// moduleRoot returns the directory containing this package's go.mod, located by
// walking up from this file's own source path.
func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
