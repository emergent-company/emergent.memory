// Package routeguard extracts the Echo route table for apps/server/domain and
// derives each route's authority tier from its applied middleware chain, so a
// checked-in route→authority table can be kept honest by CI.
//
// The extractor is an AST scan (go/ast) of apps/server/domain/*/*.go. Runtime
// enumeration via e.Routes() was deliberately NOT used: Echo bakes the group
// middleware chain into the final handler with no introspection API, so
// e.Routes() cannot reveal the authority tier at all — and the existing
// testutil boot path is an incomplete manual mirror of production (it omits
// devtools, docs, backups, journal, A2A, share, and the extraction control
// routes, and adds test-only /api/test/* routes). The single source of truth
// for production registration is the fx-wired Register* functions in domain/,
// which this scanner reads directly.
package routeguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Level is a route authority tier.
type Level string

// The authority tiers, ordered by strength. public < auth < project-token <
// project-member < superadmin-full. There is no superadmin-any tier: no such
// middleware exists in pkg/auth (only RequireSuperadminFull, which checks the
// superadmin_full role).
const (
	LevelPublic         Level = "public"
	LevelAuth           Level = "auth"
	LevelProjectToken   Level = "project-token"
	LevelProjectMember  Level = "project-member"
	LevelSuperadminFull Level = "superadmin-full"
)

// levelRank orders the tiers so derive can take the strongest present.
var levelRank = map[Level]int{
	LevelPublic:         0,
	LevelAuth:           1,
	LevelProjectToken:   2,
	LevelProjectMember:  3,
	LevelSuperadminFull: 4,
}

// ValidLevels is the set of levels the guard accepts in a declared table.
var ValidLevels = map[Level]bool{
	LevelPublic:         true,
	LevelAuth:           true,
	LevelProjectToken:   true,
	LevelProjectMember:  true,
	LevelSuperadminFull: true,
}

// Route is a single (method, path) route with its derived authority tier.
type Route struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	Level  Level  `yaml:"level"`
	Note   string `yaml:"note"`
}

// Result is the outcome of Extract.
type Result struct {
	// Routes is sorted by (Path, Method).
	Routes []Route
	// Unclassified lists every registration pattern the extractor could not
	// classify. Any non-empty Unclassified is a guard failure (fail closed).
	Unclassified []string
}

// middleware records one middleware function applied to a route/group.
type middleware struct {
	label string // display label, e.g. "RequireAuth", "RequireAPITokenScopes(projects:read)"
	tier  Level  // empty for decorative/neutral middleware
}

// groupState is the accumulated prefix + middleware chain of an Echo group.
type groupState struct {
	prefix string
	mw     []middleware
}

// Extract scans serverDir/domain for route registrations and returns the sorted
// route table with derived tiers. hardErr is non-nil only for genuine parse
// failures (a .go file that cannot be parsed); unclassifiable registration
// patterns are reported in Result.Unclassified instead.
func Extract(serverDir string) (*Result, error) {
	domainDir := filepath.Join(serverDir, "domain")

	files, err := domainGoFiles(domainDir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	type parsed struct {
		path string
		file *ast.File
	}
	var parsedFiles []parsed
	for _, path := range files {
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		parsedFiles = append(parsedFiles, parsed{path: path, file: f})
	}

	// Pass 1: discover every route-registration function — a FuncDecl whose
	// first parameter is *echo.Echo. This is the complete, convention-
	// independent inventory (RegisterRoutes, RegisterRoutesManual,
	// RegisterAdminRoutes, RegisterA2ARoutes, RegisterShareRoutes,
	// RegisterBranchesRoutes, RegisterMCPHostingRoutes, etc.).
	regFuncs := map[string]bool{}
	for _, p := range parsedFiles {
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type == nil || len(fn.Type.Params.List) == 0 {
				continue
			}
			if isEchoType(fn.Type.Params.List[0].Type) {
				regFuncs[fn.Name.Name] = true
			}
		}
	}

	// Pass 2: walk each registration function body.
	res := &Result{}
	for _, p := range parsedFiles {
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type == nil || len(fn.Type.Params.List) == 0 {
				continue
			}
			if !isEchoType(fn.Type.Params.List[0].Type) {
				continue
			}
			echoParam := fn.Type.Params.List[0].Names[0].Name
			x := &extractor{
				fset:      fset,
				file:      p.path,
				echoParam: echoParam,
				regFuncs:  regFuncs,
				groups:    map[string]*groupState{},
			}
			if fn.Body != nil {
				x.walkStmt(fn.Body)
			}
			res.Routes = append(res.Routes, x.routes...)
			res.Unclassified = append(res.Unclassified, x.unclassified...)
		}
	}

	// Deduplicate exact (method, path) duplicates — a symptom of double
	// registration that would otherwise silently pass as "declared twice".
	seen := map[string]string{}
	var dedup []Route
	for _, r := range res.Routes {
		key := r.Method + " " + r.Path
		if prev, ok := seen[key]; ok {
			if prev != string(r.Level) {
				res.Unclassified = append(res.Unclassified,
					fmt.Sprintf("%s %s registered twice with conflicting levels (%s vs %s)", r.Method, r.Path, prev, r.Level))
			}
			continue
		}
		seen[key] = string(r.Level)
		dedup = append(dedup, r)
	}
	res.Routes = dedup

	sort.Slice(res.Routes, func(i, j int) bool {
		if res.Routes[i].Path != res.Routes[j].Path {
			return res.Routes[i].Path < res.Routes[j].Path
		}
		return res.Routes[i].Method < res.Routes[j].Method
	})

	return res, nil
}

// domainGoFiles returns the .go files (excluding tests) under domainDir.
func domainGoFiles(domainDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(domainDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// isEchoType reports whether expr is *echo.Echo.
func isEchoType(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "echo" && sel.Sel.Name == "Echo"
}

// routeMethods are the Echo HTTP verb methods plus the multi-method helpers.
var routeMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// decorativeReceivers are package qualifiers whose calls inside a route
// function are side-effect (logging) calls, never route registrations.
var decorativeReceivers = map[string]bool{
	"log": true, "slog": true, "fmt": true,
}

// extractor walks a single registration function body.
type extractor struct {
	fset      *token.FileSet
	file      string
	echoParam string
	regFuncs  map[string]bool
	groups    map[string]*groupState
	routes    []Route
	// unclassified collects fail-closed diagnostics.
	unclassified []string
}

func (x *extractor) failClosed(format string, args ...any) {
	x.unclassified = append(x.unclassified, fmt.Sprintf(format, args...))
}

func (x *extractor) pos(n ast.Node) string {
	return x.fset.Position(n.Pos()).String()
}

// walkStmt processes one statement inside a registration function.
func (x *extractor) walkStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, sub := range s.List {
			x.walkStmt(sub)
		}
	case *ast.IfStmt:
		// Debug-gated routes (e.g. devtools /coverage) are enumerated
		// unconditionally; the table records the full potential surface.
		x.walkStmt(s.Body)
		if s.Else != nil {
			x.walkStmt(s.Else)
		}
	case *ast.ForStmt:
		x.walkStmt(s.Body)
	case *ast.RangeStmt:
		x.walkStmt(s.Body)
	case *ast.ExprStmt:
		x.handleCall(s.X)
	case *ast.AssignStmt:
		x.handleAssign(s)
	case *ast.DeclStmt:
		x.handleDecl(s)
	}
}

// handleAssign processes `x := e.Group(...)` / `x := g.Group(...)`.
func (x *extractor) handleAssign(s *ast.AssignStmt) {
	if len(s.Rhs) != 1 || len(s.Lhs) != 1 {
		return
	}
	lhs, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	x.defineGroupFromCall(lhs.Name, s.Rhs[0])
}

// handleDecl processes `var x = e.Group(...)`.
func (x *extractor) handleDecl(s *ast.DeclStmt) {
	gen, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Values) != 1 || len(vs.Names) != 1 {
			continue
		}
		x.defineGroupFromCall(vs.Names[0].Name, vs.Values[0])
	}
}

// defineGroupFromCall records a group variable when rhs is `<receiver>.Group(...)`.
func (x *extractor) defineGroupFromCall(name string, rhs ast.Expr) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Group" {
		return
	}
	prefix, mw, ok := x.parseGroup(sel.X, call)
	if !ok {
		return
	}
	x.groups[name] = &groupState{prefix: prefix, mw: mw}
}

// parseGroup resolves the receiver (echo root or a parent group) and extracts
// the group prefix plus its inline middleware arguments.
func (x *extractor) parseGroup(receiver ast.Expr, call *ast.CallExpr) (string, []middleware, bool) {
	prefix := ""
	var mw []middleware
	switch rec := receiver.(type) {
	case *ast.Ident:
		if rec.Name == x.echoParam {
			// root group
		} else if g, ok := x.groups[rec.Name]; ok {
			prefix = g.prefix
			mw = append(mw, g.mw...)
		} else {
			x.failClosed("%s: group receiver %q is not a known group or the Echo root", x.pos(call), rec.Name)
			return "", nil, false
		}
	default:
		x.failClosed("%s: unsupported group receiver expression", x.pos(call))
		return "", nil, false
	}

	if len(call.Args) == 0 {
		x.failClosed("%s: Group call has no path argument", x.pos(call))
		return "", nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		x.failClosed("%s: Group prefix is not a string literal", x.pos(call.Args[0]))
		return "", nil, false
	}
	seg, err := unquote(lit.Value)
	if err != nil {
		x.failClosed("%s: Group prefix %q is not a valid string: %v", x.pos(call.Args[0]), lit.Value, err)
		return "", nil, false
	}
	prefix += seg

	for _, a := range call.Args[1:] {
		m, ok := x.classifyMiddleware(a)
		if !ok {
			x.failClosed("%s: unrecognized group middleware %s", x.pos(a), exprString(a))
			continue
		}
		mw = append(mw, m)
	}
	return prefix, mw, true
}

// handleCall processes a statement-level call expression.
func (x *extractor) handleCall(expr ast.Expr) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		x.handleBareCall(call)
		return
	}
	method := sel.Sel.Name
	switch {
	case method == "Use":
		x.handleUse(sel.X, call)
	case routeMethods[method]:
		x.handleRoute(method, sel.X, call)
	case method == "Match":
		x.handleRoute(method, sel.X, call)
	case method == "Any":
		x.handleRoute(method, sel.X, call)
	case method == "Route":
		x.handleRoute(method, sel.X, call)
	default:
		x.handleSelectCall(sel, call)
	}
}

// handleUse processes `<receiver>.Use(mw...)`.
func (x *extractor) handleUse(receiver ast.Expr, call *ast.CallExpr) {
	if id, ok := receiver.(*ast.Ident); ok && id.Name == x.echoParam {
		// Global middleware on the Echo root. No domain applies an auth gate
		// globally today; if one ever does it changes every route's tier, which
		// this per-route model cannot express — fail closed.
		for _, a := range call.Args {
			m, ok := x.classifyMiddleware(a)
			if !ok {
				x.failClosed("%s: unrecognized global middleware %s", x.pos(a), exprString(a))
				continue
			}
			if m.tier != "" {
				x.failClosed("%s: global auth middleware %s cannot be expressed per-route", x.pos(a), m.label)
			}
			// decorative global middleware (e.g. otelecho) is ignored.
		}
		return
	}
	g, ok := x.resolveGroup(receiver, call)
	if !ok {
		return
	}
	for _, a := range call.Args {
		m, ok := x.classifyMiddleware(a)
		if !ok {
			x.failClosed("%s: unrecognized middleware %s", x.pos(a), exprString(a))
			continue
		}
		g.mw = append(g.mw, m)
	}
}

// resolveGroup returns the group state for a receiver, or fails closed.
func (x *extractor) resolveGroup(receiver ast.Expr, call *ast.CallExpr) (*groupState, bool) {
	id, ok := receiver.(*ast.Ident)
	if !ok {
		x.failClosed("%s: unsupported route receiver expression", x.pos(call))
		return nil, false
	}
	g, ok := x.groups[id.Name]
	if !ok {
		x.failClosed("%s: route receiver %q is not a known group", x.pos(call), id.Name)
		return nil, false
	}
	return g, true
}

// handleRoute processes `<receiver>.VERB(path, handler, mw...)`,
// `<receiver>.Match(methods, path, handler, mw...)`,
// `<receiver>.Any(path, handler, mw...)`, and
// `<receiver>.Route(methods, path, handler, mw...)`.
func (x *extractor) handleRoute(method string, receiver ast.Expr, call *ast.CallExpr) {
	var prefix string
	var baseMW []middleware
	if id, ok := receiver.(*ast.Ident); ok && id.Name == x.echoParam {
		// route registered directly on the Echo root.
	} else if g, ok := x.resolveGroup(receiver, call); ok {
		prefix = g.prefix
		baseMW = g.mw
	} else {
		return
	}

	argIdx := 0
	var methods []string
	switch method {
	case "Match", "Route":
		if len(call.Args) == 0 {
			x.failClosed("%s: %s call has no method list", x.pos(call), method)
			return
		}
		var ok bool
		methods, ok = stringSlice(call.Args[0])
		if !ok {
			x.failClosed("%s: %s method list is not a string literal slice", x.pos(call.Args[0]), method)
			return
		}
		argIdx = 1
	case "Any":
		methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE"}
	default:
		methods = []string{method}
	}

	if argIdx >= len(call.Args) {
		x.failClosed("%s: %s call has no path argument", x.pos(call), method)
		return
	}
	lit, ok := call.Args[argIdx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		x.failClosed("%s: route path is not a string literal", x.pos(call.Args[argIdx]))
		return
	}
	seg, err := unquote(lit.Value)
	if err != nil {
		x.failClosed("%s: route path %q is not a valid string: %v", x.pos(call.Args[argIdx]), lit.Value, err)
		return
	}
	full := prefix + seg

	mw := append([]middleware{}, baseMW...)
	// handler is at argIdx+1; inline middleware starts at argIdx+2.
	for _, a := range call.Args[argIdx+2:] {
		m, ok := x.classifyMiddleware(a)
		if !ok {
			x.failClosed("%s: unrecognized inline middleware %s", x.pos(a), exprString(a))
			continue
		}
		mw = append(mw, m)
	}

	level, note := derive(mw)
	for _, m := range methods {
		x.routes = append(x.routes, Route{Method: m, Path: full, Level: level, Note: note})
	}
}

// handleBareCall processes a statement that is a bare identifier call, e.g. a
// delegation `RegisterRoutes(e, h, authMiddleware)` or a helper that may
// register routes.
func (x *extractor) handleBareCall(call *ast.CallExpr) {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return
	}
	if x.regFuncs[ident.Name] {
		// Delegation to another registration function, scanned independently.
		return
	}
	if referencesEcho(call.Args, x.echoParam) {
		x.failClosed("%s: unrecognized route-registering call %s(...)", x.pos(call), ident.Name)
	}
	// Any other bare call (no echo arg) is a side effect, not route registration.
}

// handleSelectCall processes a statement-level method call that is not an Echo
// verb/group/use, e.g. log.Info(...) or an unknown helper.
func (x *extractor) handleSelectCall(sel *ast.SelectorExpr, call *ast.CallExpr) {
	if id, ok := sel.X.(*ast.Ident); ok && decorativeReceivers[id.Name] {
		return
	}
	if referencesEcho(call.Args, x.echoParam) {
		x.failClosed("%s: unrecognized route-registering call %s", x.pos(call), exprString(sel))
	}
}

// referencesEcho reports whether any argument is (or contains) a reference to
// the Echo root parameter.
func referencesEcho(args []ast.Expr, echoParam string) bool {
	for _, a := range args {
		if containsIdent(a, echoParam) {
			return true
		}
	}
	return false
}

func containsIdent(expr ast.Expr, name string) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == name
	case *ast.CallExpr:
		if containsIdent(e.Fun, name) {
			return true
		}
		for _, a := range e.Args {
			if containsIdent(a, name) {
				return true
			}
		}
	case *ast.SelectorExpr:
		return containsIdent(e.X, name)
	case *ast.StarExpr:
		return containsIdent(e.X, name)
	case *ast.UnaryExpr:
		return containsIdent(e.X, name)
	case *ast.ParenExpr:
		return containsIdent(e.X, name)
	case *ast.IndexExpr:
		if containsIdent(e.X, name) || containsIdent(e.Index, name) {
			return true
		}
	case *ast.CompositeLit:
		for _, el := range e.Elts {
			if containsIdent(el, name) {
				return true
			}
		}
	}
	return false
}

// classifyMiddleware maps a middleware expression to (label, tier, ok).
// ok is false when the middleware cannot be classified — a fail-closed signal.
func (x *extractor) classifyMiddleware(expr ast.Expr) (middleware, bool) {
	// A middleware function passed as a bare reference (not called), e.g.
	// `extended.Use(a2aErrorEnvelopeMiddleware)`.
	if ident, ok := expr.(*ast.Ident); ok {
		switch ident.Name {
		case "a2aErrorEnvelopeMiddleware", "ToolAuditMiddleware", "ToolRestrictionMiddleware":
			return middleware{ident.Name, ""}, true
		}
		return middleware{}, false
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return middleware{}, false
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "RequireAuth":
			return middleware{"RequireAuth", LevelAuth}, true
		case "RequireProjectTokenScope":
			return middleware{"RequireProjectTokenScope", LevelProjectToken}, true
		case "RequireProjectMember":
			return middleware{"RequireProjectMember", LevelProjectMember}, true
		case "RequireSuperadminFull":
			return middleware{"RequireSuperadminFull", LevelSuperadminFull}, true
		case "RequireProjectID":
			return middleware{"RequireProjectID", ""}, true
		case "RequireScopes", "RequireAPITokenScopes":
			return middleware{sel.Sel.Name + scopeArgs(call), ""}, true
		case "RequireShareLink":
			return middleware{"RequireShareLink", ""}, true
		case "RequireShareIdentity":
			return middleware{"RequireShareIdentity", ""}, true
		case "Middleware":
			// otelecho.Middleware / middleware.* are decorative global middleware.
			if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "otelecho" || id.Name == "middleware") {
				return middleware{"otel/echo middleware", ""}, true
			}
		}
		return middleware{}, false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return middleware{}, false
	}
	switch ident.Name {
	case "a2aStreamingAuthMiddleware":
		sc := scopeArgs(call)
		label := "a2aStreamingAuthMiddleware"
		if sc != "" {
			label += "(" + sc + ")"
		}
		// Wraps RequireAuth + optional RequireAPITokenScopes.
		return middleware{label, LevelAuth}, true
	case "a2aErrorEnvelopeMiddleware":
		return middleware{"a2aErrorEnvelopeMiddleware", ""}, true
	case "ToolAuditMiddleware", "ToolRestrictionMiddleware":
		return middleware{ident.Name, ""}, true
	}
	return middleware{}, false
}

// scopeArgs renders the string-literal arguments of a middleware call, e.g.
// "(projects:read)" or "" when there are no literal args.
func scopeArgs(call *ast.CallExpr) string {
	var parts []string
	for _, a := range call.Args {
		lit, ok := a.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s, err := unquote(lit.Value)
		if err != nil {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// derive maps a middleware chain to its authority tier and a reviewable note.
func derive(mw []middleware) (Level, string) {
	lvl := LevelPublic
	labels := make([]string, 0, len(mw))
	for _, m := range mw {
		if m.tier != "" && levelRank[m.tier] > levelRank[lvl] {
			lvl = m.tier
		}
		labels = append(labels, m.label)
	}
	return lvl, strings.Join(labels, ", ")
}

// stringSlice extracts []string{"GET","POST"} literals from a composite literal.
func stringSlice(expr ast.Expr) ([]string, bool) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	var out []string
	for _, el := range lit.Elts {
		bl, ok := el.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return nil, false
		}
		s, err := unquote(bl.Value)
		if err != nil {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// unquote decodes a Go string literal (supports raw and interpreted forms).
func unquote(lit string) (string, error) {
	return strconv.Unquote(lit)
}

// exprString renders an expression for diagnostics (best-effort).
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.CallExpr:
		switch fn := e.Fun.(type) {
		case *ast.Ident:
			return fn.Name + "(...)"
		case *ast.SelectorExpr:
			return exprString(fn.X) + "." + fn.Sel.Name + "(...)"
		}
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.BasicLit:
		return e.Value
	}
	return "?"
}
