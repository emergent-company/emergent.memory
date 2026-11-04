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
	"regexp"
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
	out := flag.String("out", "", "output path for the dependency graph (default: devgallery_graph_gen.go in the working directory)")
	adoptionOut := flag.String("adoption-out", "", "output path for adoption findings (default: devgallery_adoption_gen.go in the working directory)")
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

	adoptSrc, err := generateAdoption(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "componentgraph:", err)
		os.Exit(1)
	}
	adoptPath := *adoptionOut
	if adoptPath == "" {
		adoptPath = "devgallery_adoption_gen.go"
	}
	if err := os.WriteFile(adoptPath, adoptSrc, 0o644); err != nil {
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
	colliding := collidingDaisyNames(defs)
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
			g.resolveRender(d, sel.X, defs, colliding)
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
func (g *graph) resolveRender(caller *componentDef, receiver ast.Expr, defs map[string]*componentDef, colliding map[string]bool) {
	switch rec := receiver.(type) {
	case *ast.CallExpr:
		// X(...).Render(...) — a component invocation is the receiver.
		if importPath, name, ok := daisyRef(caller, rec); ok {
			key := name
			if colliding[name] {
				key = daisyKey(importPath, name)
			}
			g.add(caller.name, "uses", key)
			g.add(key, "usedBy", caller.name)
			g.add(caller.name, "daisyComponents", key)
			g.add(caller.name, "daisyPackages", importPath)
			return
		}
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
			if importPath == gatewayComponentsPath {
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

// daisyRef extracts the go-daisy import path and component name from a render
// call receiver of the form pkg.Func(...), reporting ok=false when the receiver
// is not a go-daisy component invocation (an unresolved alias, a gateway-local
// components-package call, a same-package callee, or a non-selector receiver).
func daisyRef(caller *componentDef, receiver ast.Expr) (importPath, name string, ok bool) {
	rec, ok := receiver.(*ast.CallExpr)
	if !ok {
		return "", "", false
	}
	fun, ok := rec.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	pkgID, ok := fun.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	importPath = caller.imports[pkgID.Name]
	if importPath == "" || !strings.HasPrefix(importPath, daisyPrefix) {
		return "", "", false
	}
	return importPath, fun.Sel.Name, true
}

// daisyKey returns the package-qualified key for a go-daisy component: the bare
// name prefixed by "go-daisy/<pkg>#" where <pkg> is the import path's package
// name (e.g. "go-daisy/alpine#Tag" vs "go-daisy/behaviors#Tag").
func daisyKey(importPath, name string) string {
	return "go-daisy/" + filepath.Base(importPath) + "#" + name
}

// daisyRefsByName returns the go-daisy bare component names in defs mapped to
// the set of import paths that reference each name.
func daisyRefsByName(defs map[string]*componentDef) map[string]map[string]bool {
	refs := map[string]map[string]bool{}
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
			importPath, name, ok := daisyRef(d, sel.X)
			if !ok {
				return true
			}
			if refs[name] == nil {
				refs[name] = map[string]bool{}
			}
			refs[name][importPath] = true
			return true
		})
	}
	return refs
}

// collidingDaisyNames returns the go-daisy bare component names that are
// referenced from more than one go-daisy import path. These names are qualified
// by package (go-daisy/<pkg>#<Name>) so distinct components never merge into a
// single node.
func collidingDaisyNames(defs map[string]*componentDef) map[string]bool {
	colliding := map[string]bool{}
	for name, paths := range daisyRefsByName(defs) {
		if len(paths) > 1 {
			colliding[name] = true
		}
	}
	return colliding
}

// daisyBareNameCollisions scans dir and returns the go-daisy bare component
// names imported from more than one package, mapped to their sorted import
// paths. It powers the collision ratchet: the scanner qualifies these names by
// package rather than merging them, so any new collision surfaces here to be
// declared and re-qualified instead of silently collapsing two components.
func daisyBareNameCollisions(dir string) (map[string][]string, error) {
	_, defs, err := scan(dir)
	if err != nil {
		return nil, err
	}
	collisions := map[string][]string{}
	for name, paths := range daisyRefsByName(defs) {
		if len(paths) <= 1 {
			continue
		}
		sorted := make([]string, 0, len(paths))
		for p := range paths {
			sorted = append(sorted, p)
		}
		sort.Strings(sorted)
		collisions[name] = sorted
	}
	return collisions, nil
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
	b.WriteString("// components are keyed by their exported name, resolved per file from\n")
	b.WriteString("// import aliases, except a name imported from more than one go-daisy\n")
	b.WriteString("// package, which is package-qualified (\"go-daisy/<pkg>#<Name>\") so distinct\n")
	b.WriteString("// components never merge into one node. An unresolved same-package callee\n")
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

// --- adoption-gap detection ---

// adoptionFinding is one ad-hoc reuse of component-owned markup in an authored
// template: the file (basename), the 1-indexed line, the owned marker
// ("class:<name>" or "tag:<name>"), and the sorted component ids that own it.
type adoptionFinding struct {
	File   string
	Line   int
	Marker string
	Owners []string
}

var (
	// adoptionClassAttrRE matches a static class="..." attribute. The value is a
	// single quoted string, so a static class attribute never spans lines.
	adoptionClassAttrRE = regexp.MustCompile(`class="([^"]*)"`)
	// adoptionMapClassRE matches the templ.Attributes map form `"class": "..."`.
	adoptionMapClassRE = regexp.MustCompile(`"class"\s*:\s*"([^"]*)"`)
	// adoptionStringLitRE matches a double-quoted string literal. It is applied
	// only inside class={ ... } expressions to collect class tokens.
	adoptionStringLitRE = regexp.MustCompile(`"([^"]*)"`)
	// adoptionTagRE matches an opening element tag (<tag). Closing tags (</tag)
	// and comments (<!... ) do not match because a letter must follow the '<'.
	adoptionTagRE = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9-]*)`)
	// adoptionIgnoreMarker identifies the adoption:ignore directive.
	adoptionIgnoreMarker = "adoption:ignore"
)

// generateAdoption scans the authored .templ sources under dir and returns the
// generated Go source for devgallery_adoption_gen.go. Output is deterministic:
// findings are sorted by (File, Line, Marker) and de-duplicated, and owners are
// sorted.
func generateAdoption(dir string) ([]byte, error) {
	findings, err := scanAdoption(dir)
	if err != nil {
		return nil, err
	}
	src := renderAdoptionSource(componentSignatures(), findings)
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("format generated adoption source: %w", err)
	}
	return formatted, nil
}

// scanAdoption globs the gateway-root .templ sources (non-recursive, so the
// components/ package is naturally excluded) and emits a finding for every
// owned marker an authored template re-declares inline. It is a line-oriented,
// best-effort scan — the same posture as scanClientWiring — so it reads authored
// .templ sources (not generated *_templ.go, which carries no source line
// numbers for static markup).
func scanAdoption(root string) ([]adoptionFinding, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*.templ"))
	if err != nil {
		return nil, err
	}
	owners := invertSignatures(componentSignatures())
	var findings []adoptionFinding
	for _, m := range matches {
		if skipAdoptionFile(filepath.Base(m)) {
			continue
		}
		fs, err := scanAdoptionFile(m, owners)
		if err != nil {
			return nil, err
		}
		findings = append(findings, fs...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Marker < findings[j].Marker
	})
	return dedupeAdoptionFindings(findings), nil
}

// skipAdoptionFile reports whether a .templ basename is excluded from the scan:
// the gallery's own files, test fixtures, and the components-package fixtures.
func skipAdoptionFile(base string) bool {
	return strings.HasPrefix(base, "devgallery") ||
		strings.HasSuffix(base, "_test.templ") ||
		base == "fixtures_test.templ"
}

// dedupeAdoptionFindings collapses adjacent findings with the same
// (File, Line, Marker), assuming the input is sorted.
func dedupeAdoptionFindings(fs []adoptionFinding) []adoptionFinding {
	if len(fs) == 0 {
		return fs
	}
	out := fs[:1]
	for _, f := range fs[1:] {
		prev := out[len(out)-1]
		if f.File == prev.File && f.Line == prev.Line && f.Marker == prev.Marker {
			continue
		}
		out = append(out, f)
	}
	return out
}

// invertSignatures turns the component -> signature map into a marker -> owners
// map. A marker is "class:<name>" for a class token or "tag:<name>" for an
// element tag. Owners are the sorted component ids whose signature owns it.
func invertSignatures(sigs map[string]componentSignature) map[string][]string {
	ownerSet := map[string]map[string]bool{}
	for id, sig := range sigs {
		for _, c := range sig.Classes {
			marker := "class:" + c
			if ownerSet[marker] == nil {
				ownerSet[marker] = map[string]bool{}
			}
			ownerSet[marker][id] = true
		}
		for _, t := range sig.Tags {
			marker := "tag:" + t
			if ownerSet[marker] == nil {
				ownerSet[marker] = map[string]bool{}
			}
			ownerSet[marker][id] = true
		}
	}
	out := make(map[string][]string, len(ownerSet))
	for marker, set := range ownerSet {
		sorted := make([]string, 0, len(set))
		for o := range set {
			sorted = append(sorted, o)
		}
		sort.Strings(sorted)
		out[marker] = sorted
	}
	return out
}

// signatureMarkers returns the forward component -> markers map (the generated
// adoptionSignatures), with markers sorted per component.
func signatureMarkers(sigs map[string]componentSignature) map[string][]string {
	out := make(map[string][]string, len(sigs))
	for id, sig := range sigs {
		var markers []string
		for _, c := range sig.Classes {
			markers = append(markers, "class:"+c)
		}
		for _, t := range sig.Tags {
			markers = append(markers, "tag:"+t)
		}
		sort.Strings(markers)
		out[id] = markers
	}
	return out
}

// scanAdoptionFile scans one authored .templ source for owned markers.
func scanAdoptionFile(path string, owners map[string][]string) ([]adoptionFinding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	lines := strings.Split(string(data), "\n")

	var (
		findings   []adoptionFinding
		suppressed map[string]bool // markers to ignore on the following line
		classDepth int             // >0 while a class={ ... } expression spans lines
		comments   commentState    // multi-line /* */ and <!-- --> spans
	)

	emit := func(line int, marker string) {
		owned, ok := owners[marker]
		if !ok {
			return
		}
		name := markerName(marker)
		if suppressed[name] || suppressed[marker] {
			return
		}
		findings = append(findings, adoptionFinding{File: base, Line: line, Marker: marker, Owners: owned})
	}

	emitClasses := func(line int, tokens []string) {
		for _, tok := range tokens {
			emit(line, "class:"+tok)
		}
	}

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)

		// The adoption:ignore directive is an HTML comment; parse it from the
		// raw line before comment stripping so the marker list is intact. It
		// suppresses markers on the immediately following line (the next element).
		// A directive found inside a multi-line comment does not count.
		if classDepth == 0 && !comments.block && !comments.html && strings.Contains(trimmed, adoptionIgnoreMarker) {
			suppressed = parseAdoptionIgnore(trimmed)
			continue
		}

		stripped := stripTemplComments(line, &comments)

		// Multi-line class={ ... } continuation: the whole line is class
		// expression text until the brace balance closes.
		if classDepth > 0 {
			closeIdx, remaining := adoptionBraceBalance(stripped, classDepth)
			if closeIdx < 0 {
				emitClasses(lineNo, classExprTokens(stripped))
				classDepth = remaining
				continue
			}
			emitClasses(lineNo, classExprTokens(stripped[:closeIdx]))
			classDepth = 0
			stripped = stripped[closeIdx:]
		}

		// Static and map-form class attributes.
		for _, m := range adoptionClassAttrRE.FindAllStringSubmatch(stripped, -1) {
			emitClasses(lineNo, strings.Fields(m[1]))
		}
		for _, m := range adoptionMapClassRE.FindAllStringSubmatch(stripped, -1) {
			emitClasses(lineNo, strings.Fields(m[1]))
		}

		// class={ ... } expressions (single-line or opening a multi-line one).
		rest := stripped
		for {
			idx := strings.Index(rest, "class={")
			if idx < 0 {
				break
			}
			open := idx + len("class={") - 1 // index of the '{'
			closeIdx, remaining := adoptionBraceBalance(rest[open:], 0)
			if closeIdx < 0 {
				// The expression continues on a later line: emit the tokens
				// already visible on this opening line, then resume mid-string.
				emitClasses(lineNo, classExprTokens(rest[open+1:]))
				classDepth = remaining
				rest = ""
				break
			}
			emitClasses(lineNo, classExprTokens(rest[open+1:open+closeIdx]))
			rest = rest[open+closeIdx:]
		}

		// Element tags.
		for _, m := range adoptionTagRE.FindAllStringSubmatch(stripped, -1) {
			emit(lineNo, "tag:"+m[1])
		}

		suppressed = nil
	}
	return findings, nil
}

// adoptionBraceBalance scans s tracking brace depth starting at depth. It
// returns the index just past the closing brace when depth returns to zero, or
// -1 and the remaining depth when the expression is still open at end of s.
func adoptionBraceBalance(s string, depth int) (closeIdx, remaining int) {
	for j := 0; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j + 1, 0
			}
		}
	}
	return -1, depth
}

// classExprTokens returns the class tokens in a class={ ... } expression body
// (the text between the braces): every double-quoted string literal, split on
// whitespace. Best-effort: a string literal used as a comparison operand inside
// a class expression would be included, but owned markers are high-confidence
// class names, so the false-positive surface is negligible.
func classExprTokens(expr string) []string {
	var tokens []string
	for _, m := range adoptionStringLitRE.FindAllStringSubmatch(expr, -1) {
		tokens = append(tokens, strings.Fields(m[1])...)
	}
	return tokens
}

// parseAdoptionIgnore extracts the marker tokens from an adoption:ignore
// directive, e.g. `<!-- adoption:ignore badge btn -->`.
func parseAdoptionIgnore(line string) map[string]bool {
	_, after, ok := strings.Cut(line, adoptionIgnoreMarker)
	if !ok {
		return nil
	}
	rest := strings.TrimSpace(after)
	if end := strings.Index(rest, "-->"); end >= 0 {
		rest = strings.TrimSpace(rest[:end])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f] = true
	}
	return out
}

// commentState tracks comment spans that outlive a single line so a marker
// inside a multi-line /* ... */ or <!-- ... --> comment is not mistaken for
// authored markup. It is carried across the lines of one file by
// stripTemplComments.
type commentState struct {
	block bool // inside a /* ... */ comment
	html  bool // inside a <!-- ... --> comment
}

// stripTemplComments removes Go line comments (//, respecting double-quoted
// strings), block comments (/* ... */), and HTML comments (<!-- ... -->) from a
// .templ source line, advancing st across multi-line spans. It is a best-effort
// lexical strip, not a full parser: comment markers inside strings are
// preserved, and a same-line /* inside a string literal is not distinguished.
func stripTemplComments(line string, st *commentState) string {
	// Close any comment span carried over from a previous line before looking
	// for new openers on this one.
	if st.block {
		end := strings.Index(line, "*/")
		if end < 0 {
			return ""
		}
		line = line[end+2:]
		st.block = false
	}
	if st.html {
		end := strings.Index(line, "-->")
		if end < 0 {
			return ""
		}
		line = line[end+3:]
		st.html = false
	}
	line = stripLineComment(line)
	for {
		start := strings.Index(line, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(line[start+2:], "*/")
		if end < 0 {
			st.block = true
			line = line[:start]
			break
		}
		line = line[:start] + line[start+2+end+2:]
	}
	for {
		start := strings.Index(line, "<!--")
		if start < 0 {
			break
		}
		end := strings.Index(line[start+4:], "-->")
		if end < 0 {
			st.html = true
			line = line[:start]
			break
		}
		line = line[:start] + line[start+4+end+3:]
	}
	return line
}

// stripLineComment removes a // line comment from s, respecting double-quoted
// string literals so a // inside a URL or other string is not treated as a
// comment.
func stripLineComment(s string) string {
	inString := false
	for i := 0; i+1 < len(s); i++ {
		switch s[i] {
		case '"':
			inString = !inString
		case '/':
			if !inString && s[i+1] == '/' {
				return s[:i]
			}
		}
	}
	return s
}

// markerName returns a marker's bare name: the class or tag name without its
// "class:"/"tag:" prefix.
func markerName(marker string) string {
	if n, ok := strings.CutPrefix(marker, "class:"); ok {
		return n
	}
	if n, ok := strings.CutPrefix(marker, "tag:"); ok {
		return n
	}
	return marker
}

// renderAdoptionSource serializes the adoption findings deterministically.
func renderAdoptionSource(sigs map[string]componentSignature, findings []adoptionFinding) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/componentgraph. DO NOT EDIT.\n\n")
	b.WriteString("//go:generate go run ./cmd/componentgraph\n\n")
	b.WriteString("// Generated adoption-gap findings for the component gallery. Each finding\n")
	b.WriteString("// flags an authored .templ source line that re-declares inline markup owned\n")
	b.WriteString("// by a shared component (instead of rendering that component). A marker is\n")
	b.WriteString("// \"class:<name>\" for a class token or \"tag:<name>\" for an element tag;\n")
	b.WriteString("// Owners are the sorted component ids whose signature owns the marker.\n")
	b.WriteString("package main\n\n")
	b.WriteString("type AdoptionFinding struct {\n")
	b.WriteString("\tFile   string\n")
	b.WriteString("\tLine   int\n")
	b.WriteString("\tMarker string\n")
	b.WriteString("\tOwners []string\n")
	b.WriteString("}\n\n")
	writeStringSliceMap(&b, "adoptionSignatures", signatureMarkers(sigs))
	b.WriteString("var adoptionFindings = []AdoptionFinding{\n")
	for _, f := range findings {
		b.WriteString("\t{File: " + strconv.Quote(f.File) + ", Line: " + strconv.Itoa(f.Line) + ", Marker: " + strconv.Quote(f.Marker) + ", Owners: []string{")
		for i, o := range f.Owners {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(o))
		}
		b.WriteString("}},\n")
	}
	b.WriteString("}\n\n")
	return b.Bytes()
}

// writeStringSliceMap emits `var name = map[string][]string{...}` with sorted
// keys and sorted values.
func writeStringSliceMap(b *bytes.Buffer, name string, m map[string][]string) {
	b.WriteString("var " + name + " = map[string][]string{\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := m[k]
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
