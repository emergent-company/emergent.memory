package agents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// TestResolveInstructionProjectInfoOptIn locks the core issue #1694 contract:
// project_info is injected only when the agent definition opts in via
// includeProjectInfo, and the injection is a single "## Knowledge base context"
// block positioned after the base/language/skills blocks and before the
// caller-supplied SystemPromptAppendix.
func TestResolveInstructionProjectInfoOptIn(t *testing.T) {
	base := "You are a knowledge graph query assistant."
	projectInfo := "Acme Corp builds widgets. The KB documents widget assembly."

	t.Run("default (flag off) injects nothing even when project info is available", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{SystemPrompt: &base},
			ProjectInfo:     projectInfo,
		}
		got := ae.resolveInstruction(req)
		if strings.Contains(got, knowledgeBaseContextHeading) {
			t.Fatalf("resolveInstruction() injected project context without opt-in: %q", got)
		}
		if want := base + "\n\n" + capabilityDisclosureInstruction(req.AgentDefinition, nil) + "\n\n" + a2ui.BasicCatalog.Instruction(); got != want {
			t.Fatalf("resolveInstruction() = %q, want exactly %q", got, want)
		}
	})

	t.Run("explicit false injects nothing", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Config:       map[string]any{includeProjectInfoConfigKey: false},
			},
			ProjectInfo: projectInfo,
		}
		if got := ae.resolveInstruction(req); strings.Contains(got, knowledgeBaseContextHeading) {
			t.Fatalf("resolveInstruction() injected project context when flag is false: %q", got)
		}
	})

	t.Run("flag on injects the block exactly once with the project text", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Config:       map[string]any{includeProjectInfoConfigKey: true},
			},
			ProjectInfo: projectInfo,
		}
		got := ae.resolveInstruction(req)
		if n := strings.Count(got, knowledgeBaseContextHeading); n != 1 {
			t.Fatalf("resolveInstruction() = %q, want exactly one context block, got %d", got, n)
		}
		if !strings.Contains(got, projectInfo) {
			t.Fatalf("resolveInstruction() = %q, want it to contain the project info %q", got, projectInfo)
		}
		want := base + "\n\n" + knowledgeBaseContextBlock(projectInfo) + "\n\n" + capabilityDisclosureInstruction(req.AgentDefinition, nil) + "\n\n" + a2ui.BasicCatalog.Instruction()
		if got != want {
			t.Fatalf("resolveInstruction() = %q, want %q", got, want)
		}
	})

	t.Run("flag on but empty project info injects nothing", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Config:       map[string]any{includeProjectInfoConfigKey: true},
			},
		}
		if got, want := ae.resolveInstruction(req), base+"\n\n"+capabilityDisclosureInstruction(req.AgentDefinition, nil)+"\n\n"+a2ui.BasicCatalog.Instruction(); got != want {
			t.Fatalf("resolveInstruction() = %q, want %q (nothing to inject)", got, want)
		}
	})

	t.Run("context block precedes the caller appendix", func(t *testing.T) {
		ae := &AgentExecutor{}
		appendix := "## Per-request constraints\n\nBe terse."
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Config:       map[string]any{includeProjectInfoConfigKey: true},
			},
			ProjectInfo:          projectInfo,
			SystemPromptAppendix: appendix,
		}
		got := ae.resolveInstruction(req)
		ci := strings.Index(got, knowledgeBaseContextHeading)
		ai := strings.Index(got, appendix)
		if ci < 0 || ai < 0 || ci > ai {
			t.Fatalf("context block (idx %d) must precede appendix (idx %d): %q", ci, ai, got)
		}
	})
}

// TestProjectInfoParityAcrossSurfaces asserts the composed system instruction is
// identical for the same agent/flag across the trigger, A2A, and
// chat-completions request shapes, and that project_info appears on exactly the
// surfaces it should. All surfaces flow through resolveInstruction, so a shared
// fixture models each surface's ExecuteRequest.
func TestProjectInfoParityAcrossSurfaces(t *testing.T) {
	base := "You are a helpful agent."
	projectInfo := "Project context: the KB is a product manual."

	surfaces := []struct {
		name string
		req  func(def *AgentDefinition) ExecuteRequest
	}{
		{
			name: "trigger",
			req: func(def *AgentDefinition) ExecuteRequest {
				return ExecuteRequest{AgentDefinition: def, ProjectID: "proj-1"}
			},
		},
		{
			name: "a2a",
			req: func(def *AgentDefinition) ExecuteRequest {
				return ExecuteRequest{AgentDefinition: def, ProjectID: "proj-1"}
			},
		},
		{
			// chat-completions carries conversation history in the appendix;
			// the project-context block must still be injected at the same
			// position relative to the agent's own prompt.
			name: "chat-completions",
			req: func(def *AgentDefinition) ExecuteRequest {
				return ExecuteRequest{
					AgentDefinition:      def,
					ProjectID:            "proj-1",
					SystemPromptAppendix: "## Conversation history\n\nUser: hi",
				}
			},
		},
	}

	t.Run("flag off: no surface injects project context", func(t *testing.T) {
		def := &AgentDefinition{SystemPrompt: &base}
		ae := &AgentExecutor{}
		for _, s := range surfaces {
			req := s.req(def)
			req.ProjectInfo = projectInfo
			if got := ae.resolveInstruction(req); strings.Contains(got, knowledgeBaseContextHeading) {
				t.Fatalf("surface %s injected project context with flag off: %q", s.name, got)
			}
		}
	})

	t.Run("flag on: every surface injects the identical block at the same position", func(t *testing.T) {
		def := &AgentDefinition{
			SystemPrompt: &base,
			Config:       map[string]any{includeProjectInfoConfigKey: true},
		}
		ae := &AgentExecutor{}

		wantPrefix := base + "\n\n" + knowledgeBaseContextBlock(projectInfo)
		var first string
		for _, s := range surfaces {
			req := s.req(def)
			req.ProjectInfo = projectInfo
			got := ae.resolveInstruction(req)

			if n := strings.Count(got, knowledgeBaseContextHeading); n != 1 {
				t.Fatalf("surface %s: want exactly one context block, got %d in %q", s.name, n, got)
			}
			// The composed prompt up to and including the context block must be
			// byte-identical across surfaces — the entry point contributes only
			// after it (e.g. conversation history).
			if !strings.HasPrefix(got, wantPrefix) {
				t.Fatalf("surface %s: composed prefix = %q, want %q", s.name, got, wantPrefix)
			}
			if first == "" {
				first = got
				continue
			}
			if s.name == "chat-completions" {
				continue // adds its own appendix after the shared prefix
			}
			if got != first {
				t.Fatalf("surface %s composed instruction = %q, want byte-identical %q", s.name, got, first)
			}
		}
	})
}

// TestResolveProjectInfoGating verifies the fetch chokepoint: without opt-in, or
// without a wired repository, no project_info is loaded (so no surface can get
// it by accident).
func TestResolveProjectInfoGating(t *testing.T) {
	ae := &AgentExecutor{} // repo is nil
	if got := ae.resolveProjectInfo(t.Context(), &AgentDefinition{}, "proj-1"); got != "" {
		t.Fatalf("resolveProjectInfo() = %q, want empty when not opted in", got)
	}
	if got := ae.resolveProjectInfo(t.Context(), nil, "proj-1"); got != "" {
		t.Fatalf("resolveProjectInfo() = %q, want empty for nil definition", got)
	}
	def := &AgentDefinition{Config: map[string]any{includeProjectInfoConfigKey: true}}
	if got := ae.resolveProjectInfo(t.Context(), def, "proj-1"); got != "" {
		t.Fatalf("resolveProjectInfo() = %q, want empty with nil repo", got)
	}
	if got := ae.resolveProjectInfo(t.Context(), def, ""); got != "" {
		t.Fatalf("resolveProjectInfo() = %q, want empty with no project id", got)
	}
}

// --- fake bun driver returning a single project_info column ---

// projectInfoDriver returns one row with a project_info column whose value is
// the DSN, modelled on the singleRowDriver helper used by other repository
// tests in this package.
type projectInfoDriver struct{}

func (projectInfoDriver) Open(dsn string) (driver.Conn, error) {
	return &projectInfoConn{value: dsn}, nil
}

type projectInfoConn struct{ value string }

func (*projectInfoConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (*projectInfoConn) Close() error              { return nil }
func (*projectInfoConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c *projectInfoConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &projectInfoRows{value: c.value}, nil
}
func (*projectInfoConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("not implemented")
}

type projectInfoRows struct {
	value string
	done  bool
}

func (*projectInfoRows) Columns() []string { return []string{"project_info"} }
func (*projectInfoRows) Close() error      { return nil }
func (r *projectInfoRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

var registerProjectInfoOnce sync.Once

func newProjectInfoRepository(t *testing.T, info string) *Repository {
	t.Helper()
	registerProjectInfoOnce.Do(func() { sql.Register("agents_project_info", projectInfoDriver{}) })
	sqldb, err := sql.Open("agents_project_info", info)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()))
}

// TestResolveProjectInfoWithRepo is the positive path: with a wired repository
// and the definition opted in, the project's project_info is returned verbatim;
// with the default (opt-out) definition the gating short-circuits it to "".
func TestResolveProjectInfoWithRepo(t *testing.T) {
	const info = "Widgets are assembled from cogs."
	ae := &AgentExecutor{repo: newProjectInfoRepository(t, info)}

	on := &AgentDefinition{Config: map[string]any{includeProjectInfoConfigKey: true}}
	if got := ae.resolveProjectInfo(t.Context(), on, "proj-1"); got != info {
		t.Fatalf("resolveProjectInfo() = %q, want %q", got, info)
	}

	off := &AgentDefinition{}
	if got := ae.resolveProjectInfo(t.Context(), off, "proj-1"); got != "" {
		t.Fatalf("resolveProjectInfo() = %q, want empty for opt-out definition", got)
	}
}
