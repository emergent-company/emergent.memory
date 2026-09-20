// Package docs_test — cli_help_test.go
//
// Tests that verify `memory <subcommand> --help` output contains the
// subcommands and flags documented on the docs site.  These tests require
// only the CLI binary — no server connection or network needed.
package docs_test

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Top-level help
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_TopLevelSubcommands verifies that `memory --help` lists all the
// subcommands referenced in the documentation.
func TestDocHelp_TopLevelSubcommands(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory --help lists all documented top-level subcommands",
		"Run `memory --help`",
		"Assert presence of every subcommand referenced in docs",
	)

	rl.Section("Run memory --help")
	out := mustRunCLI(t, "--help")
	rl.CLI("memory --help", out)

	rl.Section("Verify documented subcommands present")
	// These subcommands appear in the docs user guide and getting-started pages.
	documented := []string{
		"projects",
		"agents",
		"graph",
		"skills",
		"blueprints",
		"config",
		"status",
		"version",
		"ask",
		"login",
		"set-token",
		"provider",
		"completion",
		"tokens",
	}
	for _, sub := range documented {
		if !strings.Contains(out, sub) {
			t.Errorf("--help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Projects
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Projects verifies `memory projects --help` lists the documented
// sub-subcommands (create, list, delete, etc.).
func TestDocHelp_Projects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory projects --help lists documented operations",
		"Run `memory projects --help`",
		"Assert create, list, delete subcommands present",
	)

	rl.Section("Run memory projects --help")
	out := mustRunCLI(t, "projects", "--help")
	rl.CLI("memory projects --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "delete"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("projects --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_ProjectsCreate verifies `memory projects create --help` shows
// the --name flag as documented in getting-started.
func TestDocHelp_ProjectsCreate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory projects create --help shows --name flag",
		"Run `memory projects create --help`",
		"Assert --name flag is documented",
	)

	rl.Section("Run memory projects create --help")
	out := mustRunCLI(t, "projects", "create", "--help")
	rl.CLI("memory projects create --help", out)

	rl.Section("Verify --name flag")
	if !strings.Contains(out, "name") {
		t.Errorf("projects create --help missing documented --name flag")
	}
	rl.Printf("--name flag found in help output")
}

// ─────────────────────────────────────────────────────────────────────────────
// Agents
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Agents verifies `memory agents --help` lists documented
// subcommands (create, list, trigger, runs, questions, hooks).
//
// NOTE: "defs" is NOT under agents — it's a top-level command
// (`agent-definitions` with alias `defs`).  See TestDocHelp_Defs.
func TestDocHelp_Agents(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agents --help lists documented subcommands",
		"Run `memory agents --help`",
		"Assert create, list, trigger, runs, questions, hooks subcommands present",
	)

	rl.Section("Run memory agents --help")
	out := mustRunCLI(t, "agents", "--help")
	rl.CLI("memory agents --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "trigger", "runs", "questions", "hooks"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("agents --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_AgentsTrigger verifies `memory agents trigger --help` shows
// --project flag as documented.
func TestDocHelp_AgentsTrigger(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agents trigger --help shows --project flag",
		"Run `memory agents trigger --help`",
		"Assert --project flag present",
	)

	rl.Section("Run memory agents trigger --help")
	out := mustRunCLI(t, "agents", "trigger", "--help")
	rl.CLI("memory agents trigger --help", out)

	rl.Section("Verify --project flag")
	if !strings.Contains(out, "project") {
		t.Errorf("agents trigger --help missing documented --project flag")
	}
	rl.Printf("--project flag found in help output")
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Graph verifies `memory graph --help` lists documented subcommands
// (objects, relationships, search) matching the knowledge-graph docs page.
func TestDocHelp_Graph(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph --help lists documented subcommands",
		"Run `memory graph --help`",
		"Assert objects, relationships subcommands present",
	)

	rl.Section("Run memory graph --help")
	out := mustRunCLI(t, "graph", "--help")
	rl.CLI("memory graph --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"objects", "relationships"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("graph --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_GraphObjects verifies `memory graph objects --help` lists
// documented operations (list, create, get, update, delete).
func TestDocHelp_GraphObjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph objects --help lists documented operations",
		"Run `memory graph objects --help`",
		"Assert list subcommand present",
	)

	rl.Section("Run memory graph objects --help")
	out := mustRunCLI(t, "graph", "objects", "--help")
	rl.CLI("memory graph objects --help", out)

	rl.Section("Verify documented subcommands")
	if !strings.Contains(out, "list") {
		t.Errorf("graph objects --help missing documented 'list' subcommand")
	}
	rl.Printf("'list' subcommand found in help output")
}

// ─────────────────────────────────────────────────────────────────────────────
// Skills
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Skills verifies `memory skills --help` lists documented
// subcommands.
func TestDocHelp_Skills(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory skills --help lists documented subcommands",
		"Run `memory skills --help`",
		"Assert list subcommand present",
	)

	rl.Section("Run memory skills --help")
	out := mustRunCLI(t, "skills", "--help")
	rl.CLI("memory skills --help", out)

	rl.Section("Verify documented subcommands")
	if !strings.Contains(out, "list") {
		t.Errorf("skills --help missing documented 'list' subcommand")
	}
	rl.Printf("'list' subcommand found in help output")
}

// ─────────────────────────────────────────────────────────────────────────────
// Provider
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Provider verifies `memory provider --help` lists documented
// subcommands as referenced in the provider-setup docs page.
func TestDocHelp_Provider(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory provider --help lists documented subcommands",
		"Run `memory provider --help`",
		"Assert set subcommand present (docs: 'memory provider set')",
	)

	rl.Section("Run memory provider --help")
	out := mustRunCLI(t, "provider", "--help")
	rl.CLI("memory provider --help", out)

	rl.Section("Verify documented subcommands")
	if !strings.Contains(out, "configure") {
		t.Errorf("provider --help missing documented 'configure' subcommand")
	}
	rl.Printf("'configure' subcommand found in help output")
}

// ─────────────────────────────────────────────────────────────────────────────
// Blueprints
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Blueprints verifies `memory blueprints --help` lists the
// documented operations.
func TestDocHelp_Blueprints(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory blueprints --help lists documented operations",
		"Run `memory blueprints --help`",
		"Assert help output references blueprints functionality",
	)

	rl.Section("Run memory blueprints --help")
	out := mustRunCLI(t, "blueprints", "--help")
	rl.CLI("memory blueprints --help", out)

	rl.Section("Verify blueprints help content")
	lower := strings.ToLower(out)
	// Blueprints help should mention apply, install, or blueprint-related terms.
	if !strings.Contains(lower, "apply") && !strings.Contains(lower, "blueprint") && !strings.Contains(lower, "install") {
		t.Errorf("blueprints --help missing expected content (apply/blueprint/install), got:\n%s", truncate(out, 500))
	}
	rl.Printf("blueprints help content present")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tokens
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Tokens verifies `memory tokens --help` lists documented
// subcommands (create, list, revoke).
func TestDocHelp_Tokens(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory tokens --help lists documented subcommands",
		"Run `memory tokens --help`",
		"Assert create, list, revoke subcommands present",
	)

	rl.Section("Run memory tokens --help")
	out := mustRunCLI(t, "tokens", "--help")
	rl.CLI("memory tokens --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "revoke"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("tokens --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ask
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Ask verifies `memory ask --help` shows the expected usage, since
// the docs and constitution reference `memory ask` extensively.
func TestDocHelp_Ask(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory ask --help shows expected usage",
		"Run `memory ask --help`",
		"Assert help output references ask functionality",
	)

	rl.Section("Run memory ask --help")
	out := mustRunCLI(t, "ask", "--help")
	rl.CLI("memory ask --help", out)

	rl.Section("Verify ask help content")
	lower := strings.ToLower(out)
	// ask --help should mention ask, question, or query.
	if !strings.Contains(lower, "ask") && !strings.Contains(lower, "question") && !strings.Contains(lower, "query") {
		t.Errorf("ask --help missing expected content (ask/question/query), got:\n%s", truncate(out, 500))
	}
	rl.Printf("ask help content present")
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Definitions (top-level `defs` / `agent-definitions`)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Defs verifies `memory defs --help` (top-level alias for
// agent-definitions) lists the expected CRUD subcommands.
func TestDocHelp_Defs(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory defs --help lists CRUD subcommands",
		"Run `memory defs --help`",
		"Assert create, list, update, delete, get subcommands present",
	)

	rl.Section("Run memory defs --help")
	out := mustRunCLI(t, "defs", "--help")
	rl.CLI("memory defs --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "update", "delete", "get"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("defs --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_DefsCreateFlags verifies `memory defs create --help` shows the
// flags documented for agent definition creation.
func TestDocHelp_DefsCreateFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory defs create --help shows documented flags",
		"Run `memory defs create --help`",
		"Assert --name, --system-prompt, --model, --tools, --flow-type, --visibility flags present",
	)

	rl.Section("Run memory defs create --help")
	out := mustRunCLI(t, "defs", "create", "--help")
	rl.CLI("memory defs create --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"name", "system-prompt", "model", "tools", "flow-type", "visibility"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("defs create --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agents create flags
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_AgentsCreateFlags verifies `memory agents create --help` shows
// the flags documented for agent creation.
func TestDocHelp_AgentsCreateFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agents create --help shows documented flags",
		"Run `memory agents create --help`",
		"Assert --name, --trigger-type, --prompt flags present",
	)

	rl.Section("Run memory agents create --help")
	out := mustRunCLI(t, "agents", "create", "--help")
	rl.CLI("memory agents create --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"name", "trigger-type", "prompt"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("agents create --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agents hooks
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_AgentsHooks verifies `memory agents hooks --help` lists the
// documented hook management subcommands.
func TestDocHelp_AgentsHooks(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agents hooks --help lists hook subcommands",
		"Run `memory agents hooks --help`",
		"Assert create, list, delete subcommands present",
	)

	rl.Section("Run memory agents hooks --help")
	out := mustRunCLI(t, "agents", "hooks", "--help")
	rl.CLI("memory agents hooks --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "delete"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("agents hooks --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agents questions
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_AgentsQuestions verifies `memory agents questions --help` lists
// the documented question management subcommands.
func TestDocHelp_AgentsQuestions(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agents questions --help lists question subcommands",
		"Run `memory agents questions --help`",
		"Assert list, respond subcommands present",
	)

	rl.Section("Run memory agents questions --help")
	out := mustRunCLI(t, "agents", "questions", "--help")
	rl.CLI("memory agents questions --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"list", "respond"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("agents questions --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Skills (extended)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_SkillsAllSubcommands verifies `memory skills --help` lists all
// documented subcommands including create, get, update, delete, import, list.
func TestDocHelp_SkillsAllSubcommands(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory skills --help lists all documented subcommands",
		"Run `memory skills --help`",
		"Assert create, get, update, delete, import, list subcommands present",
	)

	rl.Section("Run memory skills --help")
	out := mustRunCLI(t, "skills", "--help")
	rl.CLI("memory skills --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "get", "update", "delete", "import", "list"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("skills --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_SkillsImportFlags verifies `memory skills import --help` shows
// the documented import flags (--from-dir, --discover, --builtin).
func TestDocHelp_SkillsImportFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory skills import --help shows documented flags",
		"Run `memory skills import --help`",
		"Assert import-related flags present",
	)

	rl.Section("Run memory skills import --help")
	out := mustRunCLI(t, "skills", "import", "--help")
	rl.CLI("memory skills import --help", out)

	rl.Section("Verify import help content")
	lower := strings.ToLower(out)
	// Import should reference at least some of: from-dir, discover, builtin, dir, path.
	if !strings.Contains(lower, "dir") && !strings.Contains(lower, "discover") &&
		!strings.Contains(lower, "builtin") && !strings.Contains(lower, "path") &&
		!strings.Contains(lower, "import") {
		t.Errorf("skills import --help missing expected flags, got:\n%s", truncate(out, 500))
	}
	rl.Printf("skills import --help contains expected content")
}

// ─────────────────────────────────────────────────────────────────────────────
// Documents
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Documents verifies `memory documents --help` lists the
// documented subcommands (upload, list, get, delete).
func TestDocHelp_Documents(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory documents --help lists documented subcommands",
		"Run `memory documents --help`",
		"Assert upload, list, get, delete subcommands present",
	)

	rl.Section("Run memory documents --help")
	out := mustRunCLI(t, "documents", "--help")
	rl.CLI("memory documents --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"upload", "list", "get", "delete"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("documents --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Query
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Query verifies `memory query --help` shows expected flags
// (--project, --mode) as documented.
func TestDocHelp_Query(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory query --help shows documented flags",
		"Run `memory query --help`",
		"Assert --project and --mode flags present",
	)

	rl.Section("Run memory query --help")
	out := mustRunCLI(t, "query", "--help")
	rl.CLI("memory query --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"project", "mode"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("query --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tokens create flags
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_TokensCreateFlags verifies `memory tokens create --help` shows
// the documented --name and --scopes flags.
func TestDocHelp_TokensCreateFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory tokens create --help shows documented flags",
		"Run `memory tokens create --help`",
		"Assert --name, --scopes flags present",
	)

	rl.Section("Run memory tokens create --help")
	out := mustRunCLI(t, "tokens", "create", "--help")
	rl.CLI("memory tokens create --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"name", "scopes"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("tokens create --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph objects (extended)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_GraphObjectsAllSubcommands verifies `memory graph objects --help`
// lists all documented subcommands (create, list, get, update, delete, edges, create-batch).
func TestDocHelp_GraphObjectsAllSubcommands(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph objects --help lists all documented subcommands",
		"Run `memory graph objects --help`",
		"Assert create, list, get, update, delete, edges, create-batch subcommands present",
	)

	rl.Section("Run memory graph objects --help")
	out := mustRunCLI(t, "graph", "objects", "--help")
	rl.CLI("memory graph objects --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "get", "update", "delete", "edges", "create-batch"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("graph objects --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Global flags
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_ProjectTokenGlobalFlag verifies that `memory --help` shows the
// --project-token global flag, which is documented as a way to authenticate
// with a project token instead of user credentials.
func TestDocHelp_ProjectTokenGlobalFlag(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory --help shows --project-token global flag",
		"Run `memory --help`",
		"Assert --project-token flag is visible in help output",
	)

	rl.Section("Run memory --help")
	out := mustRunCLI(t, "--help")
	rl.CLI("memory --help", out)

	rl.Section("Verify --project-token flag")
	if !strings.Contains(out, "project-token") {
		t.Errorf("--help missing documented --project-token global flag")
	} else {
		rl.Printf("--project-token global flag found in help output")
	}
}

// TestDocHelp_GlobalFlagsPresent verifies that `memory --help` shows all
// documented global flags.
func TestDocHelp_GlobalFlagsPresent(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory --help shows all documented global flags",
		"Run `memory --help`",
		"Assert --project, --server, --config, --output, --debug, --no-color flags present",
	)

	rl.Section("Run memory --help")
	out := mustRunCLI(t, "--help")
	rl.CLI("memory --help", out)

	rl.Section("Verify global flags")
	expected := []string{"project", "server", "config", "output", "debug", "no-color"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("--help missing documented global flag %q", flag)
		} else {
			rl.Printf("  found global flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schemas
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Schemas verifies `memory schemas --help` lists the documented
// subcommands for schema management.
func TestDocHelp_Schemas(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory schemas --help lists documented subcommands",
		"Run `memory schemas --help`",
		"Assert create, list, get, delete, install, uninstall, installed subcommands present",
	)

	rl.Section("Run memory schemas --help")
	out := mustRunCLI(t, "schemas", "--help")
	rl.CLI("memory schemas --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "get", "delete", "install", "uninstall", "installed"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("schemas --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Server
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Server verifies `memory server --help` lists the documented
// subcommands for self-hosted server management.
func TestDocHelp_Server(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server --help lists documented subcommands",
		"Run `memory server --help`",
		"Assert install, upgrade, uninstall, doctor, ctl subcommands present",
	)

	rl.Section("Run memory server --help")
	out := mustRunCLI(t, "server", "--help")
	rl.CLI("memory server --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"install", "upgrade", "uninstall", "doctor", "ctl"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("server --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP Guide
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_MCPGuide verifies `memory mcp-guide --help` shows expected
// content about MCP configuration for AI agents.
func TestDocHelp_MCPGuide(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory mcp-guide --help shows MCP configuration guidance",
		"Run `memory mcp-guide --help`",
		"Assert help output references MCP, configuration, or agent",
	)

	rl.Section("Run memory mcp-guide --help")
	out := mustRunCLI(t, "mcp-guide", "--help")
	rl.CLI("memory mcp-guide --help", out)

	rl.Section("Verify mcp-guide help content")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "mcp") {
		t.Errorf("mcp-guide --help missing 'mcp' reference, got:\n%s", truncate(out, 500))
	}
	rl.Printf("mcp-guide --help contains expected MCP references")
}

// ─────────────────────────────────────────────────────────────────────────────
// Completion
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_Completion verifies `memory completion --help` shows shell
// completion setup for documented shells (bash, zsh, fish, powershell).
func TestDocHelp_Completion(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory completion --help shows supported shells",
		"Run `memory completion --help`",
		"Assert help output references bash, zsh, fish, powershell",
	)

	rl.Section("Run memory completion --help")
	out := mustRunCLI(t, "completion", "--help")
	rl.CLI("memory completion --help", out)

	rl.Section("Verify shell references")
	lower := strings.ToLower(out)
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		if !strings.Contains(lower, shell) {
			t.Errorf("completion --help missing documented shell %q", shell)
		} else {
			rl.Printf("  found shell: %s", shell)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent-definitions alias
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_AgentDefinitionsAlias verifies that the full command name
// `memory agent-definitions` shows the same help as the `defs` alias, and that
// the aliases (agent-defs, defs) are documented in the help output.
func TestDocHelp_AgentDefinitionsAlias(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory agent-definitions --help shows aliases",
		"Run `memory agent-definitions --help`",
		"Assert help output mentions 'agent-defs' and 'defs' aliases",
		"Assert same CRUD subcommands as defs --help",
	)

	rl.Section("Run memory agent-definitions --help")
	out := mustRunCLI(t, "agent-definitions", "--help")
	rl.CLI("memory agent-definitions --help", out)

	rl.Section("Verify aliases documented")
	if !strings.Contains(out, "agent-defs") {
		t.Errorf("agent-definitions --help missing 'agent-defs' alias")
	} else {
		rl.Printf("alias 'agent-defs' found")
	}
	if !strings.Contains(out, "defs") {
		t.Errorf("agent-definitions --help missing 'defs' alias")
	} else {
		rl.Printf("alias 'defs' found")
	}

	rl.Section("Verify CRUD subcommands")
	expected := []string{"create", "delete", "get", "list", "update"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("agent-definitions --help missing subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph relationships (extended)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_GraphRelationshipsAllSubcommands verifies `memory graph relationships --help`
// lists all documented subcommands.
func TestDocHelp_GraphRelationshipsAllSubcommands(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph relationships --help lists all documented subcommands",
		"Run `memory graph relationships --help`",
		"Assert create, list, get, delete, create-batch subcommands present",
	)

	rl.Section("Run memory graph relationships --help")
	out := mustRunCLI(t, "graph", "relationships", "--help")
	rl.CLI("memory graph relationships --help", out)

	rl.Section("Verify documented subcommands")
	expected := []string{"create", "list", "get", "delete", "create-batch"}
	for _, sub := range expected {
		if !strings.Contains(out, sub) {
			t.Errorf("graph relationships --help missing documented subcommand %q", sub)
		} else {
			rl.Printf("  found: %s", sub)
		}
	}
}

// TestDocHelp_GraphRelationshipsCreateFlags verifies `memory graph relationships create --help`
// shows the documented flags (--from, --to, --type).
func TestDocHelp_GraphRelationshipsCreateFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph relationships create --help shows documented flags",
		"Run `memory graph relationships create --help`",
		"Assert --from, --to, --type flags present",
	)

	rl.Section("Run memory graph relationships create --help")
	out := mustRunCLI(t, "graph", "relationships", "create", "--help")
	rl.CLI("memory graph relationships create --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"from", "to", "type"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("graph relationships create --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Documents upload flags
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_DocumentsUploadFlags verifies `memory documents upload --help`
// shows the documented --auto-extract flag.
func TestDocHelp_DocumentsUploadFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory documents upload --help shows documented flags",
		"Run `memory documents upload --help`",
		"Assert --auto-extract flag present",
	)

	rl.Section("Run memory documents upload --help")
	out := mustRunCLI(t, "documents", "upload", "--help")
	rl.CLI("memory documents upload --help", out)

	rl.Section("Verify documented flags")
	if !strings.Contains(out, "auto-extract") {
		t.Errorf("documents upload --help missing documented --auto-extract flag")
	} else {
		rl.Printf("--auto-extract flag found in help output")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph objects create flags
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_GraphObjectsCreateFlags verifies `memory graph objects create --help`
// shows the documented flags (--type, --name, --description, --properties).
func TestDocHelp_GraphObjectsCreateFlags(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory graph objects create --help shows documented flags",
		"Run `memory graph objects create --help`",
		"Assert --type, --name, --description, --properties flags present",
	)

	rl.Section("Run memory graph objects create --help")
	out := mustRunCLI(t, "graph", "objects", "create", "--help")
	rl.CLI("memory graph objects create --help", out)

	rl.Section("Verify documented flags")
	expected := []string{"type", "name", "description", "properties"}
	for _, flag := range expected {
		if !strings.Contains(out, flag) {
			t.Errorf("graph objects create --help missing documented flag %q", flag)
		} else {
			rl.Printf("  found flag: --%s", flag)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Top-level command completeness
// ─────────────────────────────────────────────────────────────────────────────

// TestDocHelp_AllTopLevelCommandsPresent is a comprehensive check that every
// top-level command shown in `memory --help` is accounted for.  This catches
// new commands added to the CLI that haven't been documented yet.
func TestDocHelp_AllTopLevelCommandsPresent(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify all top-level commands from --help are accounted for",
		"Run `memory --help`",
		"Assert every expected top-level command is present",
	)

	rl.Section("Run memory --help")
	out := mustRunCLI(t, "--help")
	rl.CLI("memory --help", out)

	rl.Section("Verify all known top-level commands")
	// Comprehensive list of all top-level commands from the CLI.
	allCommands := []string{
		"projects",
		"agents",
		"agent-definitions",
		"graph",
		"skills",
		"blueprints",
		"documents",
		"query",
		"schemas",
		"tokens",
		"provider",
		"config",
		"status",
		"version",
		"ask",
		"login",
		"logout",
		"set-token",
		"completion",
		"server",
		"upgrade",
		"browse",
		"embeddings",
		"mcp-guide",
		"traces",
		"adk-sessions",
		"install-memory-skills",
	}
	for _, cmd := range allCommands {
		if !strings.Contains(out, cmd) {
			t.Errorf("--help missing expected top-level command %q", cmd)
		} else {
			rl.Printf("  found: %s", cmd)
		}
	}
}
