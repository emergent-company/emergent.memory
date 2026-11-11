package components

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// --- dialog ---

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
			`<dt class="text-muted-faint text-[11px] font-medium tracking-wide uppercase">Project</dt>`,
			`<dd class="text-base-content mt-1 text-sm break-words">`,
			"memory",
		)
	})

	t.Run("mono", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", "abc-123", MetaRowOpts{Mono: true}))
		assertContains(t, html, `<span class="font-mono text-xs">abc-123</span>`)
	})

	t.Run("empty falls back to em dash", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", ""))
		assertContains(t, html, `<span class="text-muted-subtle text-sm">—</span>`)
	})

	t.Run("NoEmptyDash renders status copy", func(t *testing.T) {
		html := renderHTML(t, MetaRow("Voice key", "", MetaRowOpts{NoEmptyDash: true}))
		assertNotContains(t, html, `<span class="text-muted-subtle text-sm">—</span>`)
	})

	t.Run("mono does not apply to the empty fallback", func(t *testing.T) {
		html := renderHTML(t, MetaRow("ID", "", MetaRowOpts{Mono: true}))
		assertNotContains(t, html, "font-mono")
	})
}

// --- meta grid ---

func TestMetaGridRendersDefaultGrid(t *testing.T) {
	html := renderHTML(t, MetaGrid())
	assertContains(t, html, `<dl class="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">`)
}

func TestMetaGridRendersGapAndCols(t *testing.T) {
	html := renderHTML(t, MetaGrid(MetaGridOpts{GapY: "gap-y-4", ColsClass: "lg:grid-cols-3"}))
	assertContains(t, html, `<dl class="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">`)
}

// --- confirm / status circle ---

// The circle's class list is contractual: it is the component's whole API for
// this surface (the conventions spec exempts the confirm-dialog icon circle
// from the no-class-string-assertions policy).
func TestIconCircleRendersEveryTone(t *testing.T) {
	cases := []struct {
		tone  IconCircleTone
		class string
	}{
		{IconCircleToneError, "bg-error/10 text-error"},
		{IconCircleToneWarning, "bg-warning/10 text-warning"},
		{IconCircleTonePrimary, "bg-primary/10 text-primary"},
		{IconCircleToneInfo, "bg-info/10 text-info"},
		{IconCircleToneSuccess, "bg-success/10 text-success"},
	}
	for _, c := range cases {
		html := renderHTML(t, IconCircle("lucide--trash-2", c.tone))
		assertContains(t, html,
			`class="`+c.class+` grid size-10 shrink-0 place-items-center rounded-full"`,
			"lucide--trash-2",
			"size-5",
			`aria-hidden="true"`,
		)
	}
}

func TestIconCircleZeroToneIsDestructive(t *testing.T) {
	html := renderHTML(t, IconCircle("lucide--alert-triangle", ""))
	assertContains(t, html, "bg-error/10 text-error")
	assertNotContains(t, html, "bg-warning/10")
}

func TestDeleteWarningRendersSentence(t *testing.T) {
	withID := renderHTML(t, DeleteWarning("", "and its definition. Irreversible.", DeleteWarningProps{SubjectID: "delete-agent-name"}))
	assertContains(t, withID,
		"This permanently removes",
		`id="delete-agent-name"`,
		"and its definition. Irreversible.",
	)

	plain := renderHTML(t, DeleteWarning("my skill", "and its content. Irreversible."))
	assertContains(t, plain, "This permanently removes", "my skill", "and its content. Irreversible.")
	assertNotContains(t, plain, `id="`)
}

// --- table ---

func TestTableCardWrapsChildren(t *testing.T) {
	html := renderHTML(t, TableCard(nil))
	assertContains(t, html, "edge-line overflow-hidden shadow-sm", "card-body memory-density-flush")

	withChild := renderHTML(t, tableCardFixture())
	assertContains(t, withChild, "<table", "child-table")
}

// --- snippet ---

func TestSnippetCardRendersLabelValueAndCopy(t *testing.T) {
	html := renderHTML(t, SnippetCard("Claude Desktop", "mcp-config-value"))
	assertContains(t, html,
		"Claude Desktop",
		"mcp-config-value",
		`clipboardCopyText`,
		"Copy",
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
	p.BundleURL = "https://example.test/api/mcp/bundle?token=secret-value"
	p.BundleTestID = "bundle-download"
	p.BundleCopyTestID = "bundle-copy"
	p.InstallURL = "https://example.test/api/mcp/install?name=memory&token=secret-value&url=https%3A%2F%2Fexample.test%2Fmcp"
	p.InstallTestID = "install-copy"
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
		`value="secret-value"`,
		"secret-value",
		`clipboardCopyInput`,
		"Copy key",
		"https://example.test/mcp",
		"This key will not be shown again.",
		"Client setup",
		"snippet-slot",
		"Done — I've saved the key",
		// Claude Desktop block: the download anchor plus a copy control for
		// each private link that carries the key.
		`data-testid="bundle-download"`,
		`href="https://example.test/api/mcp/bundle?token=secret-value"`,
		`data-testid="bundle-copy"`,
		`data-testid="install-copy"`,
		`id="mcp-share-reveal-modal-bundle-url"`,
		`id="mcp-share-reveal-modal-install-url"`,
		`value="https://example.test/api/mcp/bundle?token=secret-value"`,
		`value="https://example.test/api/mcp/install?name=memory&amp;token=secret-value&amp;url=https%3A%2F%2Fexample.test%2Fmcp"`,
		"Bundle link (private — contains this key)",
		"Install link (private — contains this key)",
		"Copy link",
	)
	// The key-bearing URLs stay in the read-only inputs; inlining them into a
	// clipboardCopyText onclick would put the key in the markup twice.
	assertNotContains(t, html, `clipboardCopyText(&#39;https://example.test`)
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

func TestSecretRevealModalDismissLink(t *testing.T) {
	var p SecretRevealProps
	p.Heading = "Token created"
	p.SecretID = "api-token-secret"
	p.Secret = "tok"
	p.DialogID = "api-token-reveal-modal"
	p.Noun = "value"
	p.DismissHref = "/settings/tokens"
	p.AutoOpen = true

	html := renderHTML(t, SecretRevealModal(p))
	assertContains(t, html,
		`<a href="/settings/tokens"`,
		"Done — I've saved the key",
		`data-dialog-autoopen="true"`,
	)
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
	assertNotContains(t, html, "data-tip", "checked", "aria-describedby")
}

// TestToggleFieldTipIsDescribedBySwitch pins #2393: the tip stays a visual
// tooltip, but a real visually-hidden copy is referenced by the switch so
// screen-reader and keyboard users get the explanation. The copy sits outside
// the <label> so it cannot join the switch's accessible name.
func TestToggleFieldTipIsDescribedBySwitch(t *testing.T) {
	const tip = "Master switch for voice."
	html := renderHTML(t, ToggleField("enabled", "Enable voice", tip, true, nil))

	if !strings.Contains(html, `aria-describedby="toggle-tip-enabled"`) {
		t.Errorf("switch must reference its tip with aria-describedby:\n%s", html)
	}
	descAt := strings.Index(html, `id="toggle-tip-enabled"`)
	if descAt == -1 {
		t.Fatalf("tip description element not found:\n%s", html)
	}
	labelEnd := strings.Index(html, "</label>")
	if labelEnd == -1 || descAt < labelEnd {
		t.Errorf("the tip description must follow the label, outside it:\n%s", html)
	}
	if !strings.Contains(html[descAt:], `class="sr-only"`) {
		t.Error("the description must be visually hidden, not a visible paragraph")
	}
	if !strings.Contains(html[descAt:], ">"+tip+"<") {
		t.Error("the description element must contain the tip text")
	}
	if !strings.Contains(html, `data-tip="`+tip+`"`) {
		t.Error("the visual tooltip must remain for pointer users")
	}
}

// TestToggleFieldWiringStaysOnTheSwitch pins the handoff to form.ToggleInput
// (#314). The wrapper now passes one Attrs map to a bare control that forces no
// id, so the wiring the hand-rolled markup used to emit is assembled in the
// wrapper: the name-derived id, the tip reference and the caller's htmx wiring
// must all land on the <input> itself, exactly once.
func TestToggleFieldWiringStaysOnTheSwitch(t *testing.T) {
	html := renderHTML(t, ToggleField("enabled", "Enable voice", "Master switch for voice.", true,
		templ.Attributes{
			"hx-post":    "/settings/voice/enabled",
			"hx-trigger": "change",
			// A caller-supplied id must not win: the component owns the
			// name-derived one, which is what the old markup produced too.
			"id": "caller-supplied",
		}))

	start := strings.Index(html, "<input")
	if start == -1 {
		t.Fatalf("no switch rendered:\n%s", html)
	}
	end := strings.Index(html[start:], ">")
	if end == -1 {
		t.Fatalf("switch tag is not terminated:\n%s", html)
	}
	tag := html[start : start+end]
	for _, want := range []string{
		`type="checkbox"`,
		`name="enabled"`,
		`id="enabled"`,
		`checked`,
		`aria-describedby="toggle-tip-enabled"`,
		`hx-post="/settings/voice/enabled"`,
		`hx-trigger="change"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("switch tag lacks %s:\n%s", want, tag)
		}
	}
	if strings.Contains(html, "caller-supplied") {
		t.Error("a caller-supplied id must not replace the component's name-derived id")
	}
	// The switch id and the tip's description id; nothing else may carry one.
	if got := strings.Count(html, `id="`); got != 2 {
		t.Errorf("want exactly 2 id attributes (switch + tip description), got %d:\n%s", got, html)
	}
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

func TestSaveChangesButtonRendersSubmitContract(t *testing.T) {
	html := renderHTML(t, SaveChangesButton())
	assertContains(t, html,
		`type="submit"`,
		"memory-save-btn",
		"lucide--save",
		"loading loading-spinner",
		"Save changes",
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
	assertContains(t, inactive, `text-muted-strong`, "text-muted-faint")
	assertNotContains(t, inactive, `aria-current="page"`)
}

func TestSettingsRailRendersItemsAndActive(t *testing.T) {
	items := []RailItem{
		{Key: "general", Href: "/settings", Icon: "lucide--settings", Label: "General"},
		{Custom: templ.Raw(`<div class="custom-group">group</div>`)},
		{Key: "voice", Href: "/settings/voice", Icon: "lucide--mic", Label: "Voice"},
	}
	html := renderHTML(t, SettingsRail("Settings sections", "voice", items))
	assertContains(t, html,
		`aria-label="Settings sections"`,
		`href="/settings"`,
		`href="/settings/voice" aria-current="page"`,
		"General",
		"Voice",
		"custom-group",
		"group",
	)
	// The inactive plain item is not marked current.
	assertNotContains(t, html, `href="/settings" aria-current="page"`)
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
	assertContains(t, html, `class="card bg-base-100 edge-line"`, "card-body memory-density-comfortable")
}

func TestPanelCardAppendsAccentClass(t *testing.T) {
	html := renderHTML(t, PanelCard("border-primary/20", templ.Attributes{"data-testid": "token-secret-panel"}))
	assertContains(t, html,
		`class="card bg-base-100 edge-line border-primary/20"`,
		`data-testid="token-secret-panel"`,
	)
}

// --- list ---

func TestLinkRowRendersLinkAndMeta(t *testing.T) {
	html := renderHTML(t, LinkRow(templ.SafeURL("/objects/o1"), templ.Raw(`<span class="leading-slot"></span>`), "Object one", "person"))
	assertContains(t, html,
		`href="/objects/o1"`,
		"leading-slot",
		"Object one",
		"person",
		"lucide--chevron-right",
	)
}
