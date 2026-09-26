package mcpauthzguard

import (
	"fmt"
	"sort"
	"strings"
)

// skillRepoRule classifies one method reachable on the raw skills.Repository
// handle. The guard is fail-closed: any method on the handle that is not
// present in this map is unclassified and fails the guard, forcing the author
// to either add an Authorize* pairing or record an explicit, justified rule.
type skillRepoRule struct {
	// requiresAuthorizer lists the shared Authorize* methods, any one of which
	// must also be called in the same function before this method's side effect.
	// Empty means the method is safe on its own (read-only, project-scoped, or
	// the authorizer itself).
	requiresAuthorizer []string
	// why records the classification rationale, printed in failures.
	why string
}

// skillRepoRules is the classification table for skills.Repository. It mirrors
// the shared-authorizer seam documented in docs/security/mcp-authz-class-decision.md
// (#1054): the REST handlers and the MCP tools must both go through the same
// AuthorizeSkillAccess / AuthorizeSkillWrite helpers, so neither entrypoint can
// bypass the other's gate.
//
// Note: AuthorizeSkillWrite subsumes AuthorizeSkillAccess (it delegates to it),
// so a FindByID gated by AuthorizeSkillWrite is still a read-gated access.
var skillRepoRules = map[string]skillRepoRule{
	"AuthorizeSkillAccess": {why: "shared read authorizer (the anchor itself)"},
	"AuthorizeSkillWrite":  {why: "shared write authorizer (the anchor itself)"},
	"MaxContentSize":       {why: "pure config read, no data access"},
	"FindAll":              {why: "read-only, project-scoped list/name lookup"},
	"FindByID":             {requiresAuthorizer: []string{"AuthorizeSkillAccess", "AuthorizeSkillWrite"}, why: "by-UUID read can cross a project boundary; must be gated by an Authorize* helper"},
	"Create":               {why: "project-scoped create (ProjectID forced to the caller's project); cannot cross a project boundary"},
	"Update":               {requiresAuthorizer: []string{"AuthorizeSkillWrite"}, why: "by-UUID mutation can cross a project boundary; must be gated by AuthorizeSkillWrite"},
	"Delete":               {requiresAuthorizer: []string{"AuthorizeSkillWrite"}, why: "by-UUID mutation can cross a project boundary; must be gated by AuthorizeSkillWrite"},
}

// Violation is a single guard failure.
type Violation struct {
	Kind   string // "unclassified-method", "missing-authorizer", "undeclared-raw-sql", "vanished-raw-sql"
	File   string
	Line   int
	Func   string
	Method string
	Detail string
}

// Check applies the method-level rules and the raw-SQL census. It returns the
// violations; an empty slice means the guard passes.
func Check(res *Result, table *Table) []Violation {
	var vs []Violation

	// Group repository calls by (file, func) so the authorizer-pairing rule can
	// see sibling calls in the same function.
	funcMethods := map[string]map[string]bool{}
	addFuncMethod := func(file, fn, method string) {
		key := file + "\x00" + fn
		if funcMethods[key] == nil {
			funcMethods[key] = map[string]bool{}
		}
		funcMethods[key][method] = true
	}
	for _, c := range res.RepoCalls {
		addFuncMethod(c.File, c.Func, c.Method)
	}

	for _, c := range res.RepoCalls {
		rule, ok := skillRepoRules[c.Method]
		if !ok {
			vs = append(vs, Violation{
				Kind:   "unclassified-method",
				File:   c.File,
				Line:   c.Line,
				Func:   c.Func,
				Method: c.Method,
				Detail: "a call on the raw skills.Repository handle uses a method that is not classified; classify it (with a justified rule, pairing it with the shared Authorize* helper where it can cross a project boundary) or route through the domain service",
			})
			continue
		}
		if len(rule.requiresAuthorizer) > 0 {
			paired := false
			for _, authz := range rule.requiresAuthorizer {
				if funcMethods[c.File+"\x00"+c.Func][authz] {
					paired = true
					break
				}
			}
			if !paired {
				vs = append(vs, Violation{
					Kind:   "missing-authorizer",
					File:   c.File,
					Line:   c.Line,
					Func:   c.Func,
					Method: c.Method,
					Detail: fmt.Sprintf("skills.Repository.%s must be paired with %s in the same function (the shared authorizer REST also uses); this is the #1040 shape", c.Method, strings.Join(rule.requiresAuthorizer, "/")),
				})
			}
		}
	}

	// Raw-SQL census: every function that issues s.db.* raw SQL must be
	// declared in the table with a justification; an undeclared function is a
	// new, unreviewed raw-SQL access site.
	declaredRaw := map[string]string{}
	for _, e := range table.RawSQL {
		declaredRaw[e.File+"\x00"+e.Func] = e.Why
	}
	seen := map[string]bool{}
	for _, f := range res.RawSQLFuncs {
		key := f.File + "\x00" + f.Func
		seen[key] = true
		if _, ok := declaredRaw[key]; !ok {
			vs = append(vs, Violation{
				Kind:   "undeclared-raw-sql",
				File:   f.File,
				Func:   f.Func,
				Method: strings.Join(f.Methods, ", "),
				Detail: "a function in domain/mcp issues raw s.db.* SQL (against tables no import rule can see) and is not declared in mcp-authz.yaml; classify it with a justification, or route through the domain service",
			})
		}
	}
	for key := range declaredRaw {
		if !seen[key] {
			parts := strings.SplitN(key, "\x00", 2)
			vs = append(vs, Violation{
				Kind:   "vanished-raw-sql",
				File:   parts[0],
				Func:   parts[1],
				Detail: "declared raw-SQL function no longer issues raw SQL; remove the stale entry from mcp-authz.yaml",
			})
		}
	}

	sort.Slice(vs, func(i, j int) bool {
		if vs[i].File != vs[j].File {
			return vs[i].File < vs[j].File
		}
		if vs[i].Line != vs[j].Line {
			return vs[i].Line < vs[j].Line
		}
		if vs[i].Kind != vs[j].Kind {
			return vs[i].Kind < vs[j].Kind
		}
		return vs[i].Method < vs[j].Method
	})
	return vs
}

// FormatViolations renders the guard failure message.
func FormatViolations(vs []Violation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mcp-authz guard failed: %d violation(s)\n", len(vs))
	for _, v := range vs {
		switch v.Kind {
		case "unclassified-method":
			fmt.Fprintf(&b, "  - %s:%d [%s] skills.Repository.%s: %s\n", v.File, v.Line, v.Func, v.Method, v.Detail)
		case "missing-authorizer":
			fmt.Fprintf(&b, "  - %s:%d [%s] skills.Repository.%s: %s\n", v.File, v.Line, v.Func, v.Method, v.Detail)
		case "undeclared-raw-sql":
			fmt.Fprintf(&b, "  - %s [%s] raw s.db SQL (%s): %s\n", v.File, v.Func, v.Method, v.Detail)
		case "vanished-raw-sql":
			fmt.Fprintf(&b, "  - %s [%s]: %s\n", v.File, v.Func, v.Detail)
		}
	}
	fmt.Fprintf(&b, "\nRemediation: expected only when a new raw access site is added intentionally.\n")
	fmt.Fprintf(&b, "For a new skills.Repository method: add a justified rule in internal/mcpauthzguard\n")
	fmt.Fprintf(&b, "(pairing it with the shared Authorize* helper where it can cross a project boundary).\n")
	fmt.Fprintf(&b, "For a new raw s.db SQL site: regenerate the census with\n")
	fmt.Fprintf(&b, "  go run ./cmd/mcp-authz-guard -update\n")
	fmt.Fprintf(&b, "and review the diff so every new site names its justification.\n")
	return b.String()
}
