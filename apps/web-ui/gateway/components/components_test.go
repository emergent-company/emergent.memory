package components

import (
	"testing"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// --- dialog ---

func TestDialogOpenScriptDelegatesToMemoryApp(t *testing.T) {
	html := renderHTML(t, DialogOpenScript())
	assertContains(t, html, "function openDialogByID", "MemoryApp.openDialog")
}

func TestDialogAutoOpenAttribute(t *testing.T) {
	attrs := DialogAutoOpen()
	if got, ok := attrs["data-dialog-autoopen"]; !ok || got != "true" {
		t.Fatalf("DialogAutoOpen() = %v, want data-dialog-autoopen=true", attrs)
	}
}

// --- meta ---

func TestMetaRowRendersLabelAndValue(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		html := renderHTML(t, MetaRow("Project", "memory"))
		assertContains(t, html,
			`<dt class="text-base-content/45 text-[11px] font-medium tracking-wide uppercase">Project</dt>`,
			`<dd class="text-base-content/85 mt-1 text-sm break-words">`,
			"memory",
		)
	})

	t.Run("mono", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", "abc-123", MetaRowOpts{Mono: true}))
		assertContains(t, html, `<span class="font-mono text-xs">abc-123</span>`)
	})

	t.Run("empty falls back to em dash", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", ""))
		assertContains(t, html, `<span class="text-base-content/30 text-sm">—</span>`)
	})

	t.Run("NoEmptyDash renders status copy", func(t *testing.T) {
		html := renderHTML(t, MetaRow("Voice key", "", MetaRowOpts{NoEmptyDash: true}))
		assertNotContains(t, html, `<span class="text-base-content/30 text-sm">—</span>`)
	})

	t.Run("mono does not apply to the empty fallback", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", "", MetaRowOpts{Mono: true}))
		assertNotContains(t, html, "font-mono")
	})
}

// --- table ---

func TestTableCardWrapsChildren(t *testing.T) {
	html := renderHTML(t, TableCard(nil))
	assertContains(t, html, "card-border overflow-hidden shadow-sm", "card-body p-0")

	withChild := renderHTML(t, tableCardFixture())
	assertContains(t, withChild, "<table", "child-table")
}

// --- snippet ---

func TestSnippetCardRendersLabelCodeAndCopyTarget(t *testing.T) {
	html := renderHTML(t, SnippetCard("Claude Desktop", "snippet-1", templ.Raw("{\"mcpServers\":{}}")))
	assertContains(t, html,
		"Claude Desktop",
		`id="snippet-1"`,
		`data-copy-target="#snippet-1"`,
		`class="rounded-box bg-base-100 overflow-x-auto whitespace-pre-wrap p-2.5 font-mono text-[11px] leading-relaxed"`,
		"Copy Claude Desktop config",
	)
}

// --- secret reveal ---

func TestSecretRevealModalRendersRevealAnatomy(t *testing.T) {
	var p SecretRevealProps
	p.Heading = "MCP share created"
	p.Name = "share-a"
	p.SecretID = "mcp-share-key"
	p.Secret = "secret-value"
	p.EndpointID = "mcp-share-endpoint"
	p.Endpoint = "https://example.test/mcp"
	p.Warning = "This key will not be shown again."
	p.DialogID = "mcp-share-reveal-modal"
	p.Attrs = templ.Attributes{"data-mcp-share-reveal": "true"}
	p.Snippets = templ.Raw("<div class=\"snippet-slot\">snippet</div>")
	p.AutoOpen = true

	html := renderHTML(t, SecretRevealModal(p))
	assertContains(t, html,
		`<dialog id="mcp-share-reveal-modal"`,
		`data-mcp-share-reveal="true"`,
		`data-dialog-autoopen="true"`,
		"mcp-share-reveal-modal",
		"MCP share created",
		"share-a — this key is shown only once. Store it somewhere safe.",
		`id="mcp-share-key"`,
		"secret-value",
		`data-copy-target="#mcp-share-key"`,
		"Copy key",
		`id="mcp-share-endpoint"`,
		"https://example.test/mcp",
		"This key will not be shown again.",
		"Client setup",
		"snippet-slot",
		"Done — I've saved the key",
	)
}

func TestSecretRevealModalNounAndTestID(t *testing.T) {
	var p SecretRevealProps
	p.Heading = "Token created"
	p.SecretID = "api-token-secret"
	p.SecretTestID = "token-secret-panel"
	p.Secret = "tok"
	p.DialogID = "d"
	p.Noun = "value"

	html := renderHTML(t, SecretRevealModal(p))
	assertContains(t, html,
		"This value is shown only once. Store it somewhere safe.",
		`data-testid="token-secret-panel"`,
	)
	// AutoOpen is false, so the client must not auto-open this dialog.
	assertNotContains(t, html, "data-dialog-autoopen", "Client setup")
}

func TestSecretRevealPanelRendersInline(t *testing.T) {
	var p SecretRevealProps
	p.Heading = "Token created"
	p.Name = "my-token"
	p.SecretID = "api-token-secret"
	p.Secret = "tok"
	p.Attrs = templ.Attributes{"data-testid": "token-secret-panel"}
	p.Footer = templ.Raw(`<p class="dismiss-note">This token will not appear again.</p>`)

	html := renderHTML(t, SecretRevealPanel(p))
	assertContains(t, html,
		"card-border",
		`data-testid="token-secret-panel"`,
		"Token created",
		"my-token — this key is shown only once. Store it somewhere safe.",
		`id="api-token-secret"`,
		"tok",
		"dismiss-note",
	)
	// The panel variant is not a dialog and carries no snippet section.
	assertNotContains(t, html, "<dialog", "Client setup", "modal-backdrop")
}

// --- form ---

func TestToggleFieldRendersToggleRow(t *testing.T) {
	html := renderHTML(t, ToggleField("enabled", "Enable voice", "Master switch for voice.", true, templ.Attributes{"hx-post": "/settings/voice/enabled"}))
	assertContains(t, html,
		`id="enabled"`,
		`name="enabled"`,
		`class="toggle"`,
		"checked",
		"Enable voice",
		`data-tip="Master switch for voice."`,
		`hx-post="/settings/voice/enabled"`,
	)
}

func TestToggleFieldOmitsEmptyTip(t *testing.T) {
	html := renderHTML(t, ToggleField("enabled", "Enable voice", "", false, nil))
	assertNotContains(t, html, "data-tip", "checked")
}

func TestSelectOptionGroups(t *testing.T) {
	groups := []SelectOptionGroup{
		{Label: "openai", Options: []SelectOption{
			{Value: "openai/gpt-4o", Label: "gpt-4o"},
			{Value: "openai/o3", Label: "o3", Selected: true},
		}},
		{Label: "anthropic", Options: []SelectOption{
			{Value: "anthropic/claude", Label: "claude"},
		}},
	}
	html := renderHTML(t, SelectOptionGroups(groups))
	assertContains(t, html,
		`<optgroup label="openai">`,
		`<optgroup label="anthropic">`,
		`<option value="openai/gpt-4o">gpt-4o</option>`,
		`<option value="openai/o3" selected>o3</option>`,
		`<option value="anthropic/claude">claude</option>`,
	)
}

// --- nav ---

func TestSubNavRendersItemsWithActiveState(t *testing.T) {
	html := renderHTML(t, subNavFixture())
	assertContains(t, html,
		`aria-label="Agent sections"`,
		`href="/agents/a1"`,
		`aria-current="page"`,
		"Dashboard",
		"Sandbox",
	)
}

func TestSubNavItemActiveAndInactive(t *testing.T) {
	active := renderHTML(t, SubNavItem("/agents/a1", true, "lucide--layout-dashboard", "Dashboard"))
	assertContains(t, active, `bg-base-200 font-medium text-base-content`, `aria-current="page"`, "text-primary")

	inactive := renderHTML(t, SubNavItem("/agents/a1", false, "lucide--layout-dashboard", "Dashboard"))
	assertContains(t, inactive, `text-base-content/60`, "text-base-content/40")
	assertNotContains(t, inactive, `aria-current="page"`)
}

// --- badge / chip / panel ---

func TestStatusBadgeRendersIntentAndIcon(t *testing.T) {
	html := renderHTML(t, StatusBadge("draft", ui.BadgeWarning, "lucide--triangle-alert"))
	assertContains(t, html, "badge", "badge-warning", "badge-sm", "lucide--triangle-alert", "draft")
}

func TestMetaChipRendersIconAndText(t *testing.T) {
	html := renderHTML(t, MetaChip("lucide--wrench", "3 tools"))
	assertContains(t, html, `class="inline-flex items-center gap-1"`, "lucide--wrench", "size-3.5", "3 tools")
}

func TestPanelCardRendersStandardTreatment(t *testing.T) {
	html := renderHTML(t, PanelCard("", nil))
	assertContains(t, html, `class="card bg-base-100 card-border"`, "card-body p-5")
}

func TestPanelCardAppendsAccentClass(t *testing.T) {
	html := renderHTML(t, PanelCard("border-primary/20", templ.Attributes{"data-testid": "token-secret-panel"}))
	assertContains(t, html,
		`class="card bg-base-100 card-border border-primary/20"`,
		`data-testid="token-secret-panel"`,
	)
}

// --- list ---

func TestListRowRendersLinkAndMeta(t *testing.T) {
	html := renderHTML(t, ListRow(templ.SafeURL("/objects/o1"), templ.Raw(`<span class="leading-slot"></span>`), "Object one", "person"))
	assertContains(t, html,
		`href="/objects/o1"`,
		"leading-slot",
		"Object one",
		"person",
		"lucide--chevron-right",
	)
}
