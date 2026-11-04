package main

import (
	"strings"
	"testing"
)

// TestRenderBlueprintImportModelMapPage asserts the model-review page renders
// the generic contract: the page anchor, the confirm form action, the per-agent
// selects named `model.<agent>`, the configured model options, the
// project-default option, the "Blueprint: <model>" source badge, the submit/
// cancel hooks, and the cancel link target. It also asserts the accessible
// association between each select and its issue warning, and the per-row
// "Configure a provider" link shown when a row's pinned model needs attention
// and models are configured. It does not pin class strings.
func TestRenderBlueprintImportModelMapPage(t *testing.T) {
	data := BlueprintModelMapData{
		Action: "/blueprints/import/confirm",
		URL:    "https://github.com/org/repo",
		Ref:    "v1.2.3",
		Token:  "ghp_secret",
		Rows: []BlueprintModelMapRow{
			{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "Set a provider for this model (expected provider/model)."},
			{AgentName: "editor", SourceModel: "anthropic/claude-3", Issue: "The 'anthropic' provider is not configured in this project.", Selected: "openai/gpt-4o"},
		},
		Models: []Model{
			{Provider: "openai", ModelName: "gpt-4o"},
			{Provider: "deepseek", ModelName: "deepseek-v4-pro"},
		},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))

	for _, want := range []string{
		`action="/blueprints/import/confirm"`,
		`data-testid="blueprint-import-map"`,
		`data-testid="blueprint-model-map-row"`,
		`data-testid="blueprint-model-map-select"`,
		`data-testid="blueprint-model-map-submit"`,
		`data-testid="blueprint-model-map-cancel"`,
		`name="model.writer"`,
		`name="model.editor"`,
		`value="openai/gpt-4o"`,
		`value="deepseek/deepseek-v4-pro"`,
		`name="url"`,
		`name="ref"`,
		`name="token"`,
		`value="ghp_secret"`,
		"Project default",
		// The source model renders as a neutral "Blueprint:" badge.
		"Blueprint: gemini-2.5-flash",
		"Blueprint: anthropic/claude-3",
		"Set a provider for this model (expected provider/model).",
		"anthropic",
		"is not configured in this project.",
		`href="/blueprints"`,
		// A pre-filled selection outside the catalog renders a "(current)" fallback
		// is unnecessary here (openai/gpt-4o is in the catalog) — instead it renders
		// selected on the matching option.
		`value="openai/gpt-4o" selected`,
		// The summary states how many models block the install.
		"2 agent models to review before this blueprint is installed.",
		// Each select is associated with its issue warning.
		`id="model-issue.writer"`,
		`aria-describedby="model-issue.writer"`,
		// Rows with an issue and configured models link to the providers page.
		`href="/settings/providers"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("map page missing %q", want)
		}
	}

	if got := strings.Count(html, `data-testid="blueprint-model-map-select"`); got != 2 {
		t.Errorf("select count = %d, want 2 (one per pinned agent)", got)
	}
	// The select id must match the form-control label `for` attribute.
	if got := strings.Count(html, `id="model.writer"`); got != 1 {
		t.Errorf(`id="model.writer" count = %d, want 1`, got)
	}
	// Each row with an issue contributes its own "Configure a provider" link.
	if got := strings.Count(html, `href="/settings/providers"`); got != 2 {
		t.Errorf(`href="/settings/providers" count = %d, want 2 (one per unresolved row)`, got)
	}
}

// TestRenderBlueprintInstallModelMapPage asserts the install variant of the
// review page: it posts to the install confirm route and carries the blueprint
// id through a hidden input (no url/ref/token inputs).
func TestRenderBlueprintInstallModelMapPage(t *testing.T) {
	data := BlueprintModelMapData{
		Action:      "/blueprints/install/confirm",
		BlueprintID: "bp-operator",
		Rows: []BlueprintModelMapRow{
			{AgentName: "operator", SourceModel: "openai/deepseek-v4-pro", Selected: "openai/deepseek-v4-pro"},
		},
		Models: []Model{{Provider: "openai", ModelName: "deepseek-v4-pro"}},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))

	for _, want := range []string{
		`action="/blueprints/install/confirm"`,
		`name="id"`,
		`value="bp-operator"`,
		`name="model.operator"`,
		`value="openai/deepseek-v4-pro" selected`,
		"Blueprint: openai/deepseek-v4-pro",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("install map page missing %q", want)
		}
	}
	// The install path must not carry the import-source hidden inputs.
	for _, absent := range []string{`name="url"`, `name="ref"`, `name="token"`} {
		if strings.Contains(html, absent) {
			t.Errorf("install map page must omit %q: %s", absent, html)
		}
	}
}

// TestRenderBlueprintImportModelMapPageOmitsEmptyToken verifies the hidden token
// input is omitted when no token is supplied (no empty credential field).
func TestRenderBlueprintImportModelMapPageOmitsEmptyToken(t *testing.T) {
	data := BlueprintModelMapData{
		Action: "/blueprints/import/confirm",
		URL:    "https://github.com/org/repo",
		Rows: []BlueprintModelMapRow{
			{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "Set a provider for this model (expected provider/model)."},
		},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))
	if strings.Contains(html, `name="token"`) {
		t.Errorf("map page must omit the token input when no token is set: %s", html)
	}
}

// TestRenderBlueprintImportModelMapPageNoModels asserts the empty state: when
// the project has no configured models, the page guides the user to configure a
// provider (linking to the providers settings route) while still offering the
// project-default option, and the summary uses the singular wording.
func TestRenderBlueprintImportModelMapPageNoModels(t *testing.T) {
	data := BlueprintModelMapData{
		Action: "/blueprints/import/confirm",
		URL:    "https://github.com/org/repo",
		Rows: []BlueprintModelMapRow{
			{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "Set a provider for this model (expected provider/model)."},
		},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))

	for _, want := range []string{
		"1 agent model to review before this blueprint is installed.",
		`href="/settings/providers"`,
		"Project default",
		`name="model.writer"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty-model map page missing %q", want)
		}
	}
}

// TestRenderBlueprintImportModelMapPageSelectedFallback asserts a pre-filled
// selection outside the catalog renders as a synthetic "(current)" option so it
// stays selectable (mirroring providerModelSelect).
func TestRenderBlueprintImportModelMapPageSelectedFallback(t *testing.T) {
	data := BlueprintModelMapData{
		Action: "/blueprints/install/confirm",
		Rows: []BlueprintModelMapRow{
			{AgentName: "operator", SourceModel: "openai/deepseek-v4-flash", Selected: "openai/deepseek-v4-flash"},
		},
		Models: []Model{{Provider: "openai", ModelName: "deepseek-v4-pro"}},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))
	if !strings.Contains(html, `value="openai/deepseek-v4-flash" selected>openai/deepseek-v4-flash (current)`) {
		t.Errorf("selected-out-of-catalog model must render a (current) fallback: %s", html)
	}
}
