// Command componentgraph statically derives the gateway's reusable-component
// dependency graph from render call sites and writes devgallery_graph_gen.go.
//
// It is invoked via `go:generate go run ./cmd/componentgraph` (see the generated
// file's header) and MUST run after `templ generate`, because the component
// definitions it walks are the generated `*_templ.go` files plus the hand-written
// `.go` factories (e.g. flashToast / partialWithTitle in ui.go).
//
// It never reads go-daisy source (vendor/ is gitignored and absent on a fresh
// checkout): go-daisy nodes and packages are derived solely from the gateway's
// own call sites and per-file import aliases.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	templImportPath       = "github.com/a-h/templ"
	daisyPrefix           = "github.com/emergent-company/go-daisy/"
	gatewayComponentsPath = "github.com/emergent-company/emergent.memory/apps/web-ui/components"
)

// graph accumulates edges across the two passes. Uses and used-by are mirrored
// (a render call records both the forward uses edge and the reverse used-by
// edge); go-daisy references are recorded separately so the registry can add L0
// entries on demand and report external dependencies at package granularity.
type graph struct {
	uses            map[string]map[string]bool // canonical id -> ids it renders (gateway, daisy, or a package leaf)
	usedBy          map[string]map[string]bool // canonical id -> call-site names that render it
	daisyComponents map[string]map[string]bool // canonical id -> go-daisy component ids referenced
	daisyPackages   map[string]map[string]bool // canonical id -> go-daisy import paths referenced
}

func newGraph() *graph {
	return &graph{
		uses:            map[string]map[string]bool{},
		usedBy:          map[string]map[string]bool{},
		daisyComponents: map[string]map[string]bool{},
		daisyPackages:   map[string]map[string]bool{},
	}
}

// componentDef is one collected templ component definition.
type componentDef struct {
	name    string            // Go identifier (canonical id)
	pkg     string            // package name ("main" or "components")
	imports map[string]string // identifier -> import path, for the defining file
	body    *ast.BlockStmt    // nil for declarations without a body
}

// parsedFile couples a parsed AST with its file path and package name.
type parsedFile struct {
	path string
	pkg  string
	file *ast.File
}

func main() {
	out := flag.String("out", "", "output path (default: devgallery_graph_gen.go in the working directory)")
	flag.Parse()

	src, err := generateGraph(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "componentgraph:", err)
		os.Exit(1)
	}
	path := *out
	if path == "" {
		path = "devgallery_graph_gen.go"
	}
	if err := os.WriteFile(path, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "componentgraph:", err)
		os.Exit(1)
	}
}

// generateGraph scans the gateway source rooted at dir (the package-main files
// and the components/ subpackage) and returns the generated Go source for
// devgallery_graph_gen.go. Output is deterministic: keys and values are sorted
// and de-duplicated.
func generateGraph(dir string) ([]byte, error) {
	g, err := buildGraph(dir)
	if err != nil {
		return nil, err
	}
	src := renderSource(g)
	// Normalize so the committed file is always gofmt-clean (gofmt elides the
	// redundant []string element type in the map literals).
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return formatted, nil
}

// buildGraph parses the source under dir, collects component definitions, and
// walks their bodies for render call sites.
func buildGraph(dir string) (*graph, error) {
	_, defs, err := scan(dir)
	if err != nil {
		return nil, err
	}
	g := newGraph()
	for _, d := range defs {
		if d.body == nil {
			continue
		}
		ast.Inspect(d.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Render" {
				return true
			}
			g.resolveRender(d, sel.X, defs)
			return true
		})
	}
	return g, nil
}

// scan parses the package-main and components/ sources under dir, returning the
// parsed files and the collected component definitions (indexed by canonical
// id). `_test.go` files and the generated graph file itself are excluded.
func scan(dir string) ([]parsedFile, map[string]*componentDef, error) {
	fset := token.NewFileSet()
	var files []parsedFile
	patterns := []string{
		filepath.Join(dir, "*.go"),
		filepath.Join(dir, "components", "*.go"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range matches {
			base := filepath.Base(m)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			// The components package's test-only fixtures (tableCardFixture,
			// subNavFixture) are authored in fixtures_test.templ, whose generated
			// file is fixtures_test_templ.go — it does not carry the _test.go
			// suffix, so exclude it explicitly so test-only defs never enter the
			// catalog.
			if strings.Contains(base, "fixtures_test") {
				continue
			}
			if base == "devgallery_graph_gen.go" {
				continue
			}
			af, err := parser.ParseFile(fset, m, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", m, err)
			}
			files = append(files, parsedFile{path: m, pkg: af.Name.Name, file: af})
		}
	}

	defs := map[string]*componentDef{}
	for _, f := range files {
		imports := collectImports(f.file)
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			if !isComponentFunc(fd, imports) {
				continue
			}
			// First definition wins; canonical ids are package-unqualified, so
			// a later same-name definition (a test fixture, a helper) is dropped
			// rather than silently replacing the catalog definition.
			if _, exists := defs[fd.Name.Name]; !exists {
				defs[fd.Name.Name] = &componentDef{
					name:    fd.Name.Name,
					pkg:     f.pkg,
					imports: imports,
					body:    fd.Body,
				}
			}
		}
	}
	return files, defs, nil
}

// isComponentFunc reports whether fd is a function whose exact return type is
// templ.Component. It rejects templ.Attributes / templ.ComponentScript /
// templ.CSSClass / templ.ComponentFunc returners because their SelectorExpr
// Sel is not "Component".
func isComponentFunc(fd *ast.FuncDecl, imports map[string]string) bool {
	if fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
		return false
	}
	sel, ok := fd.Type.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Component" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return imports[id.Name] == templImportPath
}

// collectImports maps each import's local identifier (its alias, or the package
// name for an unaliased import) to its import path. This covers the aliased
// `ui`/`fm` imports and the unaliased nav/table/layout/form imports alike.
func collectImports(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		m[name] = path
	}
	return m
}

// resolveRender classifies a `.Render(...)` receiver found in caller's body and
// records the corresponding edges.
func (g *graph) resolveRender(caller *componentDef, receiver ast.Expr, defs map[string]*componentDef) {
	switch rec := receiver.(type) {
	case *ast.CallExpr:
		// X(...).Render(...) — a component invocation is the receiver.
		switch fun := rec.Fun.(type) {
		case *ast.SelectorExpr:
			// pkg.Func(...).Render(...)
			pkgID, ok := fun.X.(*ast.Ident)
			if !ok {
				return // (obj).Method(...) — not an import alias
			}
			importPath := caller.imports[pkgID.Name]
			if importPath == "" {
				return // unresolved alias — skip rather than fabricate an edge
			}
			name := fun.Sel.Name
			switch {
			case strings.HasPrefix(importPath, daisyPrefix):
				g.add(caller.name, "uses", name)
				g.add(name, "usedBy", caller.name)
				g.add(caller.name, "daisyComponents", name)
				g.add(caller.name, "daisyPackages", importPath)
			case importPath == gatewayComponentsPath:
				g.add(caller.name, "uses", name)
				g.add(name, "usedBy", caller.name)
			}
		case *ast.Ident:
			// same-package unqualified call: Func(...).Render(...)
			name := fun.Name
			if _, ok := defs[name]; ok {
				// Only exported callees are catalog candidates: a private helper
				// (metaRowValue, secretRevealEndpoint, flashToast, …) is not a
				// catalog entry, so it must not emit a uses edge (spec R3: a uses
				// edge points at a catalog entry). Its used-by entry would be
				// dead data too, so skip it entirely.
				if !ast.IsExported(name) {
					return
				}
				g.add(caller.name, "uses", name)
				g.add(name, "usedBy", caller.name)
			} else {
				// Unresolved same-package callee -> package-granularity leaf
				// (never a dangling component edge).
				g.add(caller.name, "uses", caller.pkg)
			}
		}
	case *ast.Ident, *ast.SelectorExpr:
		// component-typed parameter (param.Render(ctx, buf)) or a package-level
		// component value — inherently dynamic; skip. Generated child-closure
		// renders (templ_7745c5c3_VarN.Render) also land here.
	}
}

// add records value under key in the graph map named kind.
func (g *graph) add(key, kind, value string) {
	var m map[string]map[string]bool
	switch kind {
	case "uses":
		m = g.uses
	case "usedBy":
		m = g.usedBy
	case "daisyComponents":
		m = g.daisyComponents
	case "daisyPackages":
		m = g.daisyPackages
	default:
		panic("componentgraph: unknown graph kind " + kind)
	}
	if m[key] == nil {
		m[key] = map[string]bool{}
	}
	m[key][value] = true
}

// renderSource serializes the graph deterministically.
func renderSource(g *graph) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/componentgraph. DO NOT EDIT.\n\n")
	b.WriteString("//go:generate go run ./cmd/componentgraph\n\n")
	b.WriteString("// Generated dependency graph for the component gallery. Canonical id =\n")
	b.WriteString("// the Go identifier as written (e.g. \"PanelCard\", \"Button\"). go-daisy\n")
	b.WriteString("// components are keyed by their exported name (no package qualifier),\n")
	b.WriteString("// resolved per file from import aliases. An unresolved same-package callee\n")
	b.WriteString("// is recorded as a package-granularity leaf using the bare package name\n")
	b.WriteString("// (\"main\", \"components\").\n")
	b.WriteString("package main\n\n")
	writeMap(&b, "graphUses", g.uses)
	writeMap(&b, "graphUsedBy", g.usedBy)
	writeMap(&b, "graphDaisyComponents", g.daisyComponents)
	writeMap(&b, "graphDaisyPackages", g.daisyPackages)
	return b.Bytes()
}

// writeMap emits `var name = map[string][]string{...}` with sorted keys and
// sorted, de-duplicated values.
func writeMap(b *bytes.Buffer, name string, m map[string]map[string]bool) {
	b.WriteString("var " + name + " = map[string][]string{\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := make([]string, 0, len(m[k]))
		for v := range m[k] {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		b.WriteString("\t" + strconv.Quote(k) + ": []string{")
		for i, v := range vals {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(v))
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\n")
}
