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

// modulePath is this module's import path, used to spell full model identities
// from the source tree. It must match the `module` directive in apps/server/
// go.mod. The guard is self-ratcheting against divergence: the census derives
// identities from this const while the registry derives them from
// reflect.Type.PkgPath(), so a stale const makes every model mismatch and the
// guard fails loudly. (It is a const — not read from go.mod — so the identity
// strings never appear as bare full-path literals that the untracked-imports
// pre-commit guard would mistake for real imports.)
const modulePath = "github.com/emergent-company/emergent.memory"

// censusModel is one bun model struct found in the source tree, as enumerated
// by a static parse of every .go file. It records the table name (from the
// `bun:"table:..."` or shorthand `bun:"kb.table,..."` tag on the embedded
// bun.BaseModel field), the column names derived from the fields' `bun` tags,
// and the model's identity (import path + struct name) so the registry check
// can key by model rather than table.
type censusModel struct {
	Table      string // schema-qualified table name, e.g. "kb.email_jobs"
	Pkg        string // package name (from the `package` clause)
	ImportPath string // full import path, e.g. "github.com/emergent-company/emergent.memory/domain/email"
	Name       string // struct name; empty for anonymous structs
	Columns    []string
}

// Identity returns the model's fully-qualified type identity
// ("import.path.TypeName"), which is what the registry side (reflect.Type
// PkgPath()+Name) also produces. This is the comparison key, distinct from the
// table name so two models sharing a table are each checked independently.
func (m censusModel) Identity() string { return m.ImportPath + "." + m.Name }

// censusRoots are the directories (relative to the module root) scanned for
// models. They cover every package that defines bun models.
var censusRoots = []string{"domain", "pkg", "internal"}

// censusExclusions names the unexported bun models that the census sees but the
// registry cannot reflect because they are unexported (cannot be referenced
// cross-package). It is keyed by model identity, NOT table name, so a model
// that shares its table with a registered model (e.g. provider.budgetNotification
// sharing kb.notifications with notifications.Notification) is still named here
// and cannot slip through by hiding behind the other model. Each entry must
// name its reason; an excluded model is still checked at name level by the
// DB-backed test (see drift_test.go), so it is not silently exempt. Shared by
// both TestRegistryCoversEveryModel and the preflight guard (CheckRegistry), so
// it lives in this non-test file rather than the test package.
var censusExclusions = map[string]string{
	modulePath + "/pkg/auth.introspectionCacheEntry":    "unexported model pkg/auth.introspectionCacheEntry; cannot be referenced cross-package",
	modulePath + "/domain/extraction.embeddingCacheRow": "unexported model domain/extraction.embeddingCacheRow; cannot be referenced cross-package",
	modulePath + "/domain/provider.budgetNotification":  "unexported model domain/provider.budgetNotification; shares kb.notifications with notifications.Notification",
}

// census returns every bun model struct in the source tree as a slice (one
// entry per model, not per table). A struct is a model when it embeds a field
// (typically bun.BaseModel) carrying a `bun:"table:..."` or shorthand
// `bun:"kb.table,..."` tag. Columns are the non-embedded fields with a `bun`
// tag other than "-".
//
// Enumeration method and its limits:
//   - It parses every non-test .go file under censusRoots with go/parser and
//     walks top-level type declarations only. A model declared as a
//     *function-local* type (e.g. domain/chunking's `embeddingJob`,
//     domain/graph's `GraphSchema`/`ProjectSchemaAssignment`, and
//     domain/discoveryjobs' anonymous `row` projections) is NOT enumerated.
//     Those projections target tables that are also covered by a registered,
//     exported model (kb.chunk_embedding_jobs, kb.graph_schemas,
//     kb.project_schemas), so their absence does not drop a model from
//     coverage. An unexported top-level model (e.g. provider.budgetNotification)
//     IS enumerated and must be named in censusExclusions.
//   - Column names for fields whose `bun` tag has an empty name are derived
//     with the same underscore() algorithm bun uses, so they match reflection.
func census() ([]censusModel, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var out []censusModel

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
			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			importPath := modulePath + "/" + filepath.ToSlash(relDir)
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
					out = append(out, censusModel{
						Table:      table,
						Pkg:        f.Name.Name,
						ImportPath: importPath,
						Name:       ts.Name.Name,
						Columns:    cols,
					})
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

// tableFromTag returns the table name from a bun tag's `table:` option or its
// shorthand form (bun treats a bare first token on the embedded BaseModel field
// as the table name, e.g. `bun:"kb.graph_schemas,alias:gtp"`). The shorthand is
// recognized when the first token is schema-qualified (contains a dot), which
// column names never are, so a regular field such as `bun:"name,notnull"` is
// never mistaken for a table.
func tableFromTag(bt string) (string, bool) {
	for _, part := range strings.Split(bt, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "table:") {
			return strings.TrimPrefix(part, "table:"), true
		}
	}
	first := strings.TrimSpace(strings.SplitN(bt, ",", 2)[0])
	if strings.Contains(first, ".") {
		return first, true
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
