package main

import (
	"strings"
	"testing"
)

// TestRenderBlueprintImportModelMapPage asserts the model-mapping page renders
// the import contract: the page anchor, the per-agent selects named
// `model.<agent>`, the configured model options, the project-default option,
// the submit/cancel hooks, and the cancel link target. It also asserts the
// accessible association between each select and its issue warning. It does
// not pin class strings.
func TestRenderBlueprintImportModelMapPage(t *testing.T) {
	data := BlueprintModelMapData{
		URL:   "https://github.com/org/repo",
		Ref:   "v1.2.3",
		Token: "ghp_secret",
		Rows: []BlueprintModelMapRow{
			{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "Set a provider for this model (expected provider/model)."},
			{AgentName: "editor", SourceModel: "anthropic/claude-3", Issue: "The 'anthropic' provider is not configured in this project."},
		},
		Models: []Model{
			{Provider: "openai", ModelName: "gpt-4o"},
			{Provider: "deepseek", ModelName: "deepseek-v4-pro"},
		},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))

	for _, want := range []string{
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
		"Set a provider for this model (expected provider/model).",
		"anthropic",
		"is not configured in this project.",
		`href="/blueprints"`,
		// The summary states how many models block the install.
		"2 agent models need mapping before this blueprint can be installed.",
		// Each select is associated with its issue warning.
		`id="model-issue.writer"`,
		`aria-describedby="model-issue.writer"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("map page missing %q", want)
		}
	}

	if got := strings.Count(html, `data-testid="blueprint-model-map-select"`); got != 2 {
		t.Errorf("select count = %d, want 2 (one per unresolved agent)", got)
	}
	// The select id must match the form-control label `for` attribute.
	if got := strings.Count(html, `id="model.writer"`); got != 1 {
		t.Errorf(`id="model.writer" count = %d, want 1`, got)
	}
	// No providers configured → no empty-state guidance.
	if strings.Contains(html, `href="/settings/providers"`) {
		t.Errorf("map page must not show the no-models guidance when models exist")
	}
}

// TestRenderBlueprintImportModelMapPageOmitsEmptyToken verifies the hidden token
// input is omitted when no token is supplied (no empty credential field).
func TestRenderBlueprintImportModelMapPageOmitsEmptyToken(t *testing.T) {
	data := BlueprintModelMapData{
		URL: "https://github.com/org/repo",
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
		URL: "https://github.com/org/repo",
		Rows: []BlueprintModelMapRow{
			{AgentName: "writer", SourceModel: "gemini-2.5-flash", Issue: "Set a provider for this model (expected provider/model)."},
		},
	}
	html := renderHTML(t, BlueprintImportModelMapPage(data))

	for _, want := range []string{
		"1 agent model needs mapping before this blueprint can be installed.",
		`href="/settings/providers"`,
		"Project default",
		`name="model.writer"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty-model map page missing %q", want)
		}
	}
}
