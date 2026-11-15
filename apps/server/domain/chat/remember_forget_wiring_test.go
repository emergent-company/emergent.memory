package chat

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestRememberForgetRuntimeAgentWiring pins issue #790. The six Remember/Forget
// run-kickoff sites (sync/async × RememberStream, RememberFile, ForgetStream)
// must resolve their runtime row through the shared
// rememberForgetRuntimeAgent helper, which reuses the definition's existing
// linked row. The pre-#776 shape minted a feature-named row carrying
// agent_definition_id at each site, which collided with the definition's own
// runtime row (kb.uq_agents_project_agent_definition, 23505) and surfaced as a
// 500.
//
// The behaviour is covered against a real database by
// TestRememberForgetReusesSourceTriggerBoundRuntimeRow; this test is the wiring
// pin that makes a revert of ANY ONE site fail, because the DB test alone only
// proves the helper is correct, not that every handler leg still calls it. It is
// the same fail-first shape as domain/agents/a2ui_wiring_test.go.
func TestRememberForgetRuntimeAgentWiring(t *testing.T) {
	const file = "handler.go"
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}

	// Each stream handler contains exactly one sync and one async kickoff.
	sites := map[string]int{
		"RememberStream": 2,
		"RememberFile":   2,
		"ForgetStream":   2,
	}
	for fn, want := range sites {
		body := chatFuncBody(t, file, string(src), fn)
		if body == "" {
			t.Errorf("%s: function %s not found", file, fn)
			continue
		}
		if got := strings.Count(body, "h.rememberForgetRuntimeAgent("); got != want {
			t.Errorf("%s: %s resolves its runtime agent %d time(s) through h.rememberForgetRuntimeAgent, want %d — each sync/async kickoff must reuse the definition's row (#790)",
				file, fn, got, want)
		}
		// The pre-fix shape: a feature-named row minted at the site, carrying
		// the definition link.
		for _, marker := range []string{`"remember-session:"`, `"forget-session:"`} {
			if strings.Contains(body, marker) {
				t.Errorf("%s: %s mints a feature-named runtime row (%s) instead of reusing the definition's row (#790)",
					file, fn, marker)
			}
		}
	}

	// The helper is the single resolution point: it must resolve through the
	// definition link, not by name.
	helper := chatFuncBody(t, file, string(src), "rememberForgetRuntimeAgent")
	if helper == "" {
		t.Fatalf("%s: helper rememberForgetRuntimeAgent not found", file)
	}
	if !strings.Contains(helper, "h.agentRepo.FindOrCreateRuntimeAgentForDefinition(") {
		t.Errorf("%s: rememberForgetRuntimeAgent must resolve the row via h.agentRepo.FindOrCreateRuntimeAgentForDefinition (#790)", file)
	}
}

// chatFuncBody returns the gofmt'd source of the named function's body, or ""
// when the function is absent from src.
func chatFuncBody(t *testing.T, file, src, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Body == nil {
			continue
		}
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, fd.Body); err != nil {
			t.Fatalf("format %s body: %v", file, err)
		}
		return buf.String()
	}
	return ""
}
