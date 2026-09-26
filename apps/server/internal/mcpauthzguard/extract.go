// Package mcpauthzguard enforces the domain/mcp authorization boundary at the
// method level, not just the import level. The #1092 depguard rule stops mcp
// from *importing* a new sibling domain's store/repository, but it cannot stop
// a tool from calling a method on an *already allow-listed* raw handle in a way
// that skips the domain's shared Authorize* helper — the exact shape of #1040,
// where the MCP skills delete path called skills.Repository.Delete directly and
// destroyed another project's skill.
//
// The extractor is an AST scan (go/ast) of apps/server/domain/mcp/*.go. Regex
// is deliberately not used: receiver/selector/method resolution needs the real
// call graph shape, and a regex over source text produces false positives and
// negatives that would make the guard untrustworthy.
package mcpauthzguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepositoryCall is a single method call on the raw skills.Repository handle
// (Service.skillsRepo) reached from an mcp tool function.
type RepositoryCall struct {
	File   string // repo-root-relative path
	Line   int    // 1-based
	Func   string // enclosing function name ("" for top-level/literal bodies)
	Method string // the repository method name, e.g. "Delete"
}

// RawSQLFunc records one function in domain/mcp that issues a raw SQL call on
// the Service's bun.IDB handle (s.db.*) — raw SQL against another domain's
// tables, which no import rule can see.
type RawSQLFunc struct {
	File    string   // repo-root-relative path
	Func    string   // enclosing function name
	Methods []string // distinct bun builder methods used, e.g. NewRaw, RunInTx
}

// Result is the outcome of Extract.
type Result struct {
	// RepoCalls is sorted by (File, Line).
	RepoCalls []RepositoryCall
	// RawSQLFuncs is sorted by (File, Func).
	RawSQLFuncs []RawSQLFunc
}

// Extract scans serverDir/domain/mcp for raw store/repository and raw-SQL
// access sites and returns them. hardErr is non-nil only for genuine parse
// failures (a .go file that cannot be parsed).
func Extract(serverDir string) (*Result, error) {
	mcpDir := filepath.Join(serverDir, "domain", "mcp")

	files, err := mcpGoFiles(mcpDir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	res := &Result{}

	for _, path := range files {
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		rel, err := filepath.Rel(filepath.Join(serverDir, "..", ".."), path)
		if err != nil {
			rel = path
		}
		// Keep a stable repo-root-relative path for the table.
		rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")

		x := &scanner{fset: fset, file: rel}
		for _, decl := range f.Decls {
			x.scanDecl(decl)
		}
		res.RepoCalls = append(res.RepoCalls, x.repoCalls...)
		res.RawSQLFuncs = append(res.RawSQLFuncs, x.rawSQL...)
	}

	sort.Slice(res.RepoCalls, func(i, j int) bool {
		if res.RepoCalls[i].File != res.RepoCalls[j].File {
			return res.RepoCalls[i].File < res.RepoCalls[j].File
		}
		return res.RepoCalls[i].Line < res.RepoCalls[j].Line
	})
	sort.Slice(res.RawSQLFuncs, func(i, j int) bool {
		if res.RawSQLFuncs[i].File != res.RawSQLFuncs[j].File {
			return res.RawSQLFuncs[i].File < res.RawSQLFuncs[j].File
		}
		return res.RawSQLFuncs[i].Func < res.RawSQLFuncs[j].Func
	})

	return res, nil
}

// mcpGoFiles returns the .go files (excluding tests) under mcpDir.
func mcpGoFiles(mcpDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(mcpDir, func(path string, d os.DirEntry, err error) error {
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

// scanner walks one file's declarations, tracking the enclosing function name.
type scanner struct {
	fset  *token.FileSet
	file  string
	funcs []string // stack of enclosing function names

	repoCalls []RepositoryCall
	rawSQL    []RawSQLFunc
}

func (x *scanner) curFunc() string {
	if len(x.funcs) == 0 {
		return ""
	}
	return x.funcs[len(x.funcs)-1]
}

func (x *scanner) scanDecl(decl ast.Decl) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok {
		return
	}
	x.funcs = append(x.funcs, fn.Name.Name)
	if fn.Body != nil {
		x.walkStmt(fn.Body)
	}
	x.funcs = x.funcs[:len(x.funcs)-1]
}

func (x *scanner) walkStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, sub := range s.List {
			x.walkStmt(sub)
		}
	case *ast.IfStmt:
		if s.Init != nil {
			x.walkStmt(s.Init)
		}
		x.scanExpr(s.Cond)
		x.walkStmt(s.Body)
		if s.Else != nil {
			x.walkStmt(s.Else)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			x.walkStmt(s.Init)
		}
		x.scanExpr(s.Cond)
		if s.Post != nil {
			x.walkStmt(s.Post)
		}
		x.walkStmt(s.Body)
	case *ast.RangeStmt:
		x.scanExpr(s.X)
		x.walkStmt(s.Body)
	case *ast.SwitchStmt:
		x.walkStmt(s.Body)
	case *ast.TypeSwitchStmt:
		x.walkStmt(s.Body)
	case *ast.SelectStmt:
		x.walkStmt(s.Body)
	case *ast.CaseClause:
		for _, sub := range s.Body {
			x.walkStmt(sub)
		}
	case *ast.CommClause:
		for _, sub := range s.Body {
			x.walkStmt(sub)
		}
	case *ast.ExprStmt:
		x.scanExpr(s.X)
	case *ast.AssignStmt:
		for _, r := range s.Rhs {
			x.scanExpr(r)
		}
		for _, l := range s.Lhs {
			x.scanExpr(l)
		}
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			x.scanExpr(r)
		}
	case *ast.DeferStmt:
		x.scanExpr(s.Call)
	case *ast.GoStmt:
		x.scanExpr(s.Call)
	case *ast.SendStmt:
		x.scanExpr(s.Value)
	case *ast.DeclStmt:
		x.scanDeclStmt(s)
	case *ast.LabeledStmt:
		x.walkStmt(s.Stmt)
	case *ast.IncDecStmt:
		x.scanExpr(s.X)
	}
}

func (x *scanner) scanDeclStmt(s *ast.DeclStmt) {
	gen, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, v := range vs.Values {
			x.scanExpr(v)
		}
	}
}

// scanExpr walks an expression, recording repository and raw-SQL calls.
func (x *scanner) scanExpr(e ast.Expr) {
	switch n := e.(type) {
	case *ast.CallExpr:
		x.scanCall(n)
		x.scanExpr(n.Fun)
		for _, a := range n.Args {
			x.scanExpr(a)
		}
	case *ast.FuncLit:
		// Anonymous closures inherit the enclosing function name; walk the body.
		if n.Body != nil {
			x.walkStmt(n.Body)
		}
	case *ast.SelectorExpr:
		x.scanExpr(n.X)
	case *ast.StarExpr:
		x.scanExpr(n.X)
	case *ast.UnaryExpr:
		x.scanExpr(n.X)
	case *ast.BinaryExpr:
		x.scanExpr(n.X)
		x.scanExpr(n.Y)
	case *ast.ParenExpr:
		x.scanExpr(n.X)
	case *ast.IndexExpr:
		x.scanExpr(n.X)
		x.scanExpr(n.Index)
	case *ast.IndexListExpr:
		x.scanExpr(n.X)
	case *ast.CompositeLit:
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				x.scanExpr(kv.Value)
			} else {
				x.scanExpr(elt)
			}
		}
	case *ast.SliceExpr:
		x.scanExpr(n.X)
	case *ast.TypeAssertExpr:
		x.scanExpr(n.X)
	}
}

// scanCall inspects one call expression for a raw handle access.
func (x *scanner) scanCall(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	method := sel.Sel.Name

	// Case 1: <recv>.skillsRepo.<Method>(...) — the raw skills.Repository handle.
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "skillsRepo" {
		x.repoCalls = append(x.repoCalls, RepositoryCall{
			File:   x.file,
			Line:   x.fset.Position(call.Pos()).Line,
			Func:   x.curFunc(),
			Method: method,
		})
		return
	}

	// Case 2: s.db.<Method>(...) — raw SQL on the Service's bun.IDB handle.
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "db" {
		if recv, ok := inner.X.(*ast.Ident); ok && recv.Name == "s" {
			x.recordRawSQL(method)
			return
		}
	}
}

// recordRawSQL folds a raw-SQL call into the per-function census.
func (x *scanner) recordRawSQL(method string) {
	fn := x.curFunc()
	for i := range x.rawSQL {
		if x.rawSQL[i].File == x.file && x.rawSQL[i].Func == fn {
			x.rawSQL[i].Methods = appendIfMissing(x.rawSQL[i].Methods, method)
			return
		}
	}
	x.rawSQL = append(x.rawSQL, RawSQLFunc{
		File:    x.file,
		Func:    fn,
		Methods: []string{method},
	})
}

func appendIfMissing(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
