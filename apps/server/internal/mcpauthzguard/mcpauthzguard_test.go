package mcpauthzguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckRepositoryPairing(t *testing.T) {
	t.Run("delete without authorizer fails", func(t *testing.T) {
		res := &Result{RepoCalls: []RepositoryCall{
			{File: "skills_tools.go", Line: 10, Func: "executeDeleteSkill", Method: "Delete"},
		}}
		vs := Check(res, &Table{})
		if len(vs) != 1 || vs[0].Kind != "missing-authorizer" {
			t.Fatalf("expected one missing-authorizer violation, got %v", vs)
		}
	})

	t.Run("delete with AuthorizeSkillWrite passes", func(t *testing.T) {
		res := &Result{RepoCalls: []RepositoryCall{
			{File: "skills_tools.go", Line: 10, Func: "executeDeleteSkill", Method: "Delete"},
			{File: "skills_tools.go", Line: 8, Func: "executeDeleteSkill", Method: "AuthorizeSkillWrite"},
		}}
		vs := Check(res, &Table{})
		if len(vs) != 0 {
			t.Fatalf("expected no violations, got %v", vs)
		}
	})

	t.Run("FindByID gated by AuthorizeSkillWrite passes", func(t *testing.T) {
		res := &Result{RepoCalls: []RepositoryCall{
			{File: "skills_tools.go", Line: 10, Func: "executeUpdateSkill", Method: "FindByID"},
			{File: "skills_tools.go", Line: 12, Func: "executeUpdateSkill", Method: "AuthorizeSkillWrite"},
		}}
		vs := Check(res, &Table{})
		if len(vs) != 0 {
			t.Fatalf("expected no violations, got %v", vs)
		}
	})

	t.Run("unclassified method fails closed", func(t *testing.T) {
		res := &Result{RepoCalls: []RepositoryCall{
			{File: "skills_tools.go", Line: 10, Func: "executeListSkills", Method: "FindForAgent"},
		}}
		vs := Check(res, &Table{})
		if len(vs) != 1 || vs[0].Kind != "unclassified-method" {
			t.Fatalf("expected one unclassified-method violation, got %v", vs)
		}
	})
}

func TestCheckRawSQLCensus(t *testing.T) {
	table := &Table{RawSQL: []RawSQLEntry{
		{File: "service.go", Func: "executeQueryEntities", Why: "read-only"},
	}}

	t.Run("undeclared raw sql fails", func(t *testing.T) {
		res := &Result{RawSQLFuncs: []RawSQLFunc{
			{File: "service.go", Func: "executeNewRawThing", Methods: []string{"NewRaw"}},
		}}
		vs := Check(res, table)
		if len(vs) != 2 { // undeclared-raw-sql + vanished-raw-sql (the declared one is now missing)
			t.Fatalf("expected 2 violations, got %v", vs)
		}
	})

	t.Run("vanished raw sql fails", func(t *testing.T) {
		res := &Result{}
		vs := Check(res, table)
		if len(vs) != 1 || vs[0].Kind != "vanished-raw-sql" {
			t.Fatalf("expected one vanished-raw-sql violation, got %v", vs)
		}
	})

	t.Run("clean passes", func(t *testing.T) {
		res := &Result{RawSQLFuncs: []RawSQLFunc{
			{File: "service.go", Func: "executeQueryEntities", Methods: []string{"NewSelect"}},
		}}
		vs := Check(res, table)
		if len(vs) != 0 {
			t.Fatalf("expected no violations, got %v", vs)
		}
	})
}

func TestExtractFixture(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("domain/mcp/skills_tools.go", `package mcp

func (s *Service) executeDeleteSkill() {
	if err := s.skillsRepo.AuthorizeSkillWrite(); err != nil {
		return
	}
	if err := s.skillsRepo.Delete(); err != nil {
		return
	}
}
`)
	write("domain/mcp/query.go", `package mcp

func (s *Service) run() {
	_ = s.db.NewSelect().Table("kb.graph_objects")
}
`)

	res, err := Extract(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RepoCalls) != 2 {
		t.Fatalf("expected 2 repo calls (AuthorizeSkillWrite + Delete), got %d", len(res.RepoCalls))
	}
	var methods []string
	for _, c := range res.RepoCalls {
		methods = append(methods, c.Method)
	}
	if !contains(methods, "Delete") || !contains(methods, "AuthorizeSkillWrite") {
		t.Fatalf("expected Delete and AuthorizeSkillWrite, got %v", methods)
	}
	if len(res.RawSQLFuncs) != 1 {
		t.Fatalf("expected 1 raw sql function, got %d", len(res.RawSQLFuncs))
	}
	if res.RawSQLFuncs[0].Func != "run" {
		t.Fatalf("expected func run, got %q", res.RawSQLFuncs[0].Func)
	}
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}
