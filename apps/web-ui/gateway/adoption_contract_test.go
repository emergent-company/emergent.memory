package main

import (
	"strings"
	"testing"
)

// These tests lock the documented shell normalizations from the
// refactor-webui-component-adoption change: the three raw <dialog class="modal">
// shells now render through ui.Dialog (carrying hx-boost="false" on the dialog
// root), and the two agent MCP endpoint lists now render through TableCard's
// standard card shell. Identity, ARIA, form wiring, and submit behaviour are
// preserved in both cases.

// TestDeriveDialogsRenderThroughDialog asserts the three migrated derive
// dialogs render through ui.Dialog, carrying hx-boost="false" on the <dialog>
// root while preserving their ids, aria wiring, and form action/method.
func TestDeriveDialogsRenderThroughDialog(t *testing.T) {
	t.Run("derive blueprint", func(t *testing.T) {
		html := renderHTML(t, deriveBlueprintDialog())
		assertContainsHTML(t, html,
			`<dialog id="derive-blueprint-modal" class="modal" hx-boost="false"`,
			`aria-labelledby="derive-blueprint-title"`,
			`method="dialog" class="modal-backdrop"`,
			`action="/schema/blueprints/derive"`,
			`data-dialog-close="derive-blueprint-modal"`,
		)
	})

	t.Run("derive warning", func(t *testing.T) {
		view := &SchemaEditView{
			Detail:     &ObjectTypeDetail{Name: "person"},
			Provenance: &ProvenanceInfo{BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"},
		}
		html := renderHTML(t, deriveWarningDialog(view))
		assertContainsHTML(t, html,
			`<dialog id="derive-warning-modal" class="modal" hx-boost="false"`,
			`aria-labelledby="derive-warning-title"`,
			`aria-describedby="derive-warning-desc"`,
			`method="dialog" class="modal-backdrop"`,
			"Save anyway",
			"Keep editing",
		)
	})

	t.Run("derive version", func(t *testing.T) {
		d := &BlueprintDetail{ID: "bp1", Name: "personal-memory"}
		html := renderHTML(t, deriveVersionDialog(d))
		assertContainsHTML(t, html,
			`<dialog id="derive-version-modal" class="modal" hx-boost="false"`,
			`aria-labelledby="derive-version-title"`,
			`method="dialog" class="modal-backdrop"`,
			`action="/schema/blueprints/bp1/versions/derive"`,
			`data-dialog-close="derive-version-modal"`,
		)
	})
}

// TestAgentMCPListsRenderTableCardShell asserts the two hand-rolled bordered
// list shells now render the app's standard card shell while keeping their
// data-testid and margin.
func TestAgentMCPListsRenderTableCardShell(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: agentMCPFixture()}
	e := agentMCPTestServer(s)

	body := agentMCPGet(e, "/agents/a1/settings/mcp").Body.String()
	assertContainsHTML(t, body,
		`class="card bg-base-100 card-border overflow-hidden shadow-sm" data-testid="agent-mcp-key-list"`,
		`class="card bg-base-100 card-border overflow-hidden shadow-sm" data-testid="agent-mcp-session-list"`,
		`card-body memory-density-flush`,
		`class="mt-4"`,
		`class="mt-3"`,
	)
}

// assertContainsHTML fails unless every want substring appears in got.
func assertContainsHTML(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in rendered output:\n%s", want, got)
		}
	}
}
