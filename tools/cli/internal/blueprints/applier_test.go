package blueprints_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkprojects "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/blueprints"
)

// ──────────────────────────────────────────────────────────────────────────────
// Mock SDK clients
// ──────────────────────────────────────────────────────────────────────────────
//
// We don't mock the entire client struct; instead we use a thin wrapper approach:
// the Blueprinter accepts concrete *sdk.Client pointers. For unit tests we need a
// way to intercept calls without spinning up a real server.
//
// Strategy: build a real (but "dead") SDK client pair and override the blueprinter
// through a subtype that intercepts calls. Because the Blueprinter is an exported
// struct with exported methods, we test it at the functional level by using
// a fake HTTP server. However, to keep tests hermetic and simple, we instead
// test the Blueprinter's behaviour through a table-driven approach with a custom
// testBlueprintsApplier that holds call counters and preset responses — essentially
// a hand-rolled test double that mirrors the Blueprinter's interface.
//
// For simplicity (and to avoid needing a full HTTP roundtrip), the blueprinter
// tests below focus on:
//   • dry-run output contains expected lines and makes no API calls
//   • integration of loader + blueprinter with a stubbed blueprinter
//
// Full API-call behaviour is validated by the loader tests (which do real I/O)
// and by the server's own test suite for the endpoint itself.

// ──────────────────────────────────────────────────────────────────────────────
// Blueprinter dry-run test (dry-run outputs correct lines, no API calls)
// ──────────────────────────────────────────────────────────────────────────────

func TestBlueprintsApplier_DryRun(t *testing.T) {
	packs := []blueprints.PackFile{
		{Name: "test-pack", Version: "1.0.0", SourceFile: "schemas/test.yaml",
			ObjectTypes: []blueprints.ObjectTypeDef{{Name: "Doc"}}},
	}
	agents := []blueprints.AgentFile{
		{Name: "my-bot", SourceFile: "agents/my-bot.yaml"},
	}

	var buf bytes.Buffer
	// nil SDK clients — dry-run must not call them
	a := blueprints.NewBlueprintsApplier(nil, "", nil, nil, nil, true /* dryRun */, false /* upgrade */, &buf)

	results, err := a.Run(context.Background(), nil, packs, agents, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] prefix in output, got:\n%s", out)
	}
	if !strings.Contains(out, "test-pack") {
		t.Errorf("expected pack name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "my-bot") {
		t.Errorf("expected agent name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Dry run complete") {
		t.Errorf("expected 'Dry run complete' in output, got:\n%s", out)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (1 pack + 1 agent), got %d", len(results))
	}
}

// TestBlueprintsApplier_DryRunWithUpgrade verifies that --dry-run + --upgrade
// still makes no API calls and includes "create or update" in the output.
func TestBlueprintsApplier_DryRunWithUpgrade(t *testing.T) {
	packs := []blueprints.PackFile{
		{Name: "p", Version: "2.0.0", SourceFile: "schemas/p.yaml",
			ObjectTypes: []blueprints.ObjectTypeDef{{Name: "X"}}},
	}

	var buf bytes.Buffer
	a := blueprints.NewBlueprintsApplier(nil, "", nil, nil, nil, true, true, &buf)
	results, err := a.Run(context.Background(), nil, packs, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "create or update") {
		t.Errorf("expected 'create or update' in dry-run+upgrade output, got:\n%s", out)
	}
	_ = results
}

// TestBlueprintsApplier_DryRunWithProjectInfo verifies that a non-empty ProjectFile
// causes a dry-run line about setting project info.
func TestBlueprintsApplier_DryRunWithProjectInfo(t *testing.T) {
	pf := &blueprints.ProjectFile{
		ProjectInfo: "This is a test project.",
		SourceFile:  "project.yaml",
	}

	var buf bytes.Buffer
	a := blueprints.NewBlueprintsApplier(nil, "test-project-id", nil, nil, nil, true, false, &buf)
	results, err := a.Run(context.Background(), pf, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] prefix in output, got:\n%s", out)
	}
	if !strings.Contains(out, "project_info") {
		t.Errorf("expected 'project_info' in dry-run output, got:\n%s", out)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (project info), got %d", len(results))
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Compile-time check: ensure Blueprinter can be constructed with real SDK client types
// ──────────────────────────────────────────────────────────────────────────────

func TestBlueprintsApplier_AcceptsSDKClientTypes(t *testing.T) {
	// This is a compile-time assertion; if the types don't match, this file won't
	// compile. We pass typed nils to confirm the constructor signature is correct.
	var pr *sdkprojects.Client
	var sc *sdkschemas.Client
	var ag *sdkagents.Client
	var sk *sdkskills.Client

	a := blueprints.NewBlueprintsApplier(pr, "proj-id", sc, ag, sk, true, false, nil)
	if a == nil {
		t.Fatal("expected non-nil blueprinter")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// PrintDiscoverySummary tests
// ──────────────────────────────────────────────────────────────────────────────

func TestPrintDiscoverySummary_Full(t *testing.T) {
	packs := []blueprints.PackFile{
		{
			Name:    "test-pack",
			Version: "1.0.0",
			ObjectTypes: []blueprints.ObjectTypeDef{
				{Name: "Doc"}, {Name: "Note"}, {Name: "Tag"},
			},
			RelationshipTypes: []blueprints.RelationshipTypeDef{
				{Name: "has_tag"}, {Name: "references"},
			},
		},
	}
	agents := []blueprints.AgentFile{{Name: "bot"}}
	skills := []blueprints.SkillFile{{Name: "skill-a"}, {Name: "skill-b"}}
	objects := []blueprints.SeedObjectRecord{{Type: "Doc", Key: "d1"}}
	rels := []blueprints.SeedRelationshipRecord{{Type: "has_tag", SrcKey: "d1", DstKey: "t1"}}
	project := &blueprints.ProjectFile{ProjectInfo: "Test project info"}

	var buf bytes.Buffer
	blueprints.PrintDiscoverySummary(&buf, project, packs, agents, skills, objects, rels)
	out := buf.String()

	if !strings.Contains(out, "Discovered:") {
		t.Errorf("expected 'Discovered:' prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "1 schema pack(s)") {
		t.Errorf("expected '1 schema pack(s)', got:\n%s", out)
	}
	if !strings.Contains(out, "3 object types") {
		t.Errorf("expected '3 object types', got:\n%s", out)
	}
	if !strings.Contains(out, "2 relationship types") {
		t.Errorf("expected '2 relationship types', got:\n%s", out)
	}
	if !strings.Contains(out, "1 agent(s)") {
		t.Errorf("expected '1 agent(s)', got:\n%s", out)
	}
	if !strings.Contains(out, "2 skill(s)") {
		t.Errorf("expected '2 skill(s)', got:\n%s", out)
	}
	if !strings.Contains(out, "project info") {
		t.Errorf("expected 'project info', got:\n%s", out)
	}
}

func TestPrintDiscoverySummary_Empty(t *testing.T) {
	var buf bytes.Buffer
	blueprints.PrintDiscoverySummary(&buf, nil, nil, nil, nil, nil, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for no resources, got:\n%s", buf.String())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// PrintInspect tests
// ──────────────────────────────────────────────────────────────────────────────

func TestPrintInspect_ShowsPackContents(t *testing.T) {
	packs := []blueprints.PackFile{
		{
			Name:        "code-structure",
			Version:     "1.0.0",
			Description: "A test schema pack for code structure",
			ObjectTypes: []blueprints.ObjectTypeDef{
				{Name: "App", Label: "Application", Properties: map[string]any{"name": "string", "port": "number"}},
				{Name: "Module", Label: "Module"},
			},
			RelationshipTypes: []blueprints.RelationshipTypeDef{
				{Name: "contains_module", Label: "Contains Module", SourceType: "App", TargetType: "Module"},
			},
		},
	}

	var buf bytes.Buffer
	blueprints.PrintInspect(&buf, nil, packs, nil, nil, nil, nil)
	out := buf.String()

	if !strings.Contains(out, `Pack "code-structure"`) {
		t.Errorf("expected pack name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Object types (2)") {
		t.Errorf("expected 'Object types (2)', got:\n%s", out)
	}
	if !strings.Contains(out, "Application (2 properties)") {
		t.Errorf("expected 'Application (2 properties)', got:\n%s", out)
	}
	if !strings.Contains(out, "Module") {
		t.Errorf("expected 'Module' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Relationship types (1)") {
		t.Errorf("expected 'Relationship types (1)', got:\n%s", out)
	}
	if !strings.Contains(out, "Contains Module (App -> Module)") {
		t.Errorf("expected 'Contains Module (App -> Module)', got:\n%s", out)
	}
	if !strings.Contains(out, "Totals:") {
		t.Errorf("expected 'Totals:' line, got:\n%s", out)
	}
}
