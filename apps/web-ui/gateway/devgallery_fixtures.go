package main

import (
	"context"
	"io"

	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
	ui "github.com/emergent-company/go-daisy/components/ui"
)

// gallerySeed returns the hand-maintained catalog seed: every reusable
// components/ package entry (L1 domain adapters + L2 app composites) and a
// curated subset of the go-daisy primitives the app renders (L0). A package-main
// composite earns a place only when reused across two or more page features or
// when it encapsulates an invariant; today none qualify, and the exclusion of
// single-page exported helpers (e.g. BoardColumns) is test-pinned.
//
// Fixtures are inert: href="#", type="button" buttons, no live endpoint ids,
// no real mutation wiring. Components with no curated fixture get an authored
// zero-value closure whose emptiness/panic is flagged "needs fixture".
func gallerySeed() []GalleryEntry {
	return []GalleryEntry{
		// --- components/ package: domain adapters (L1) ---
		{
			ID: "StatusBadge", Slug: "status-badge", Name: "StatusBadge", Layer: LayerAdapter,
			Category: "Badges", Subcategory: "status", SourceFile: "components/badge.templ",
			PropsType:   "StatusBadgeOpts",
			Description: "A status badge from an explicit intent; the caller maps its domain status vocabulary to an intent and icon.",
			Render: func() templ.Component {
				return components.StatusBadge("Ready", ui.BadgeSuccess, "lucide--circle-check")
			},
			Stories: map[string]func() templ.Component{
				"warning": func() templ.Component {
					return components.StatusBadge("Pending", ui.BadgeWarning, "lucide--triangle-alert")
				},
				"error": func() templ.Component { return components.StatusBadge("Failed", ui.BadgeError, "lucide--circle-x") },
			},
		},
		{
			ID: "MetaRow", Slug: "meta-row", Name: "MetaRow", Layer: LayerAdapter,
			Category: "Metadata", Subcategory: "definition-list", SourceFile: "components/meta.templ",
			PropsType:   "MetaRowOpts",
			Description: "One label/value pair of a definition list; the single definition of the meta row label style.",
			Render: func() templ.Component {
				return components.MetaRow("Project", "memory")
			},
		},

		// --- components/ package: app composites (L2) ---
		{
			ID: "PanelCard", Slug: "panel-card", Name: "PanelCard", Layer: LayerComposite,
			Category: "Cards", Subcategory: "panel", SourceFile: "components/panel.templ",
			PropsType:   "extraClass string, attrs templ.Attributes",
			Description: "The standard bordered panel with the shared body padding.",
			Render: func() templ.Component {
				return withChildren(components.PanelCard("", nil), templ.Raw(`<p class="text-sm">Sample panel content</p>`))
			},
		},
		{
			ID: "TableCard", Slug: "table-card", Name: "TableCard", Layer: LayerComposite,
			Category: "Cards", Subcategory: "table", SourceFile: "components/table.templ",
			PropsType:   "attrs templ.Attributes",
			Description: "The bordered card that wraps a list table; the body carries no padding so the table meets the edges.",
			Render: func() templ.Component {
				return withChildren(components.TableCard(nil), templ.Raw(`
					<table class="table table-sm">
						<thead><tr><th>Name</th><th>Type</th></tr></thead>
						<tbody>
							<tr><td>memory</td><td>project</td></tr>
							<tr><td>voice-key</td><td>credential</td></tr>
						</tbody>
					</table>`))
			},
		},
		{
			ID: "MetaGrid", Slug: "meta-grid", Name: "MetaGrid", Layer: LayerComposite,
			Category: "Metadata", Subcategory: "definition-list", SourceFile: "components/meta.templ",
			PropsType:   "MetaGridOpts",
			Description: "A definition-list grid of MetaRow / MetaRowCustom cells with a single layout definition.",
			Render: func() templ.Component {
				return withChildren(components.MetaGrid(),
					join(components.MetaRow("Project", "memory"), components.MetaRow("ID", "abc-123", components.MetaRowOpts{Mono: true})))
			},
		},
		{
			ID: "MetaRowCustom", Slug: "meta-row-custom", Name: "MetaRowCustom", Layer: LayerComposite,
			Category: "Metadata", Subcategory: "definition-list", SourceFile: "components/meta.templ",
			PropsType:   "MetaRowOpts",
			Description: "A meta label with caller-supplied content in the value slot.",
			Render: func() templ.Component {
				return withChildren(components.MetaRowCustom("Status"), templ.Raw(`<span class="text-sm">Ready</span>`))
			},
		},
		{
			ID: "EmptyDash", Slug: "empty-dash", Name: "EmptyDash", Layer: LayerComposite,
			Category: "Metadata", Subcategory: "fallback", SourceFile: "components/meta.templ",
			PropsType:   "—",
			Description: "The muted em-dash fallback for an absent value.",
			Render:      func() templ.Component { return components.EmptyDash() },
		},
		{
			ID: "MetaChip", Slug: "meta-chip", Name: "MetaChip", Layer: LayerComposite,
			Category: "Metadata", Subcategory: "chip", SourceFile: "components/chip.templ",
			PropsType:   "icon, text string",
			Description: "An inline icon-plus-text metadata chip.",
			Render:      func() templ.Component { return components.MetaChip("lucide--wrench", "3 tools") },
		},
		{
			ID: "ListRow", Slug: "list-row", Name: "ListRow", Layer: LayerComposite,
			Category: "Lists", Subcategory: "row", SourceFile: "components/list.templ",
			PropsType:   "href templ.SafeURL, leading templ.Component, title, meta string",
			Description: "A clickable list-page row: leading tile, title/meta column, trailing content, and an automatic chevron.",
			Render: func() templ.Component {
				leading := templ.Raw(`<span class="grid size-9 shrink-0 place-items-center rounded-box bg-primary/10 text-primary"><span class="iconify lucide--box size-4" aria-hidden="true"></span></span>`)
				return components.ListRow(templ.SafeURL("#"), leading, "Object one", "person · 2h ago")
			},
		},
		{
			ID: "SubNav", Slug: "sub-nav", Name: "SubNav", Layer: LayerComposite,
			Category: "Navigation", Subcategory: "rail", SourceFile: "components/nav.templ",
			PropsType:   "label string",
			Description: "A sub-navigation rail: horizontal on small screens, vertical on desktop.",
			Render: func() templ.Component {
				return withChildren(components.SubNav("Sections"),
					join(components.SubNavItem("#", true, "lucide--layout-dashboard", "Dashboard"),
						components.SubNavItem("#", false, "lucide--box", "Sandbox")))
			},
		},
		{
			ID: "SubNavItem", Slug: "sub-nav-item", Name: "SubNavItem", Layer: LayerComposite,
			Category: "Navigation", Subcategory: "item", SourceFile: "components/nav.templ",
			PropsType:   "href string, isActive bool, icon, label string",
			Description: "One sub-navigation entry, marking the active destination.",
			Render: func() templ.Component {
				return components.SubNavItem("#", true, "lucide--layout-dashboard", "Dashboard")
			},
		},
		{
			ID: "SettingsRail", Slug: "settings-rail", Name: "SettingsRail", Layer: LayerComposite,
			Category: "Navigation", Subcategory: "rail", SourceFile: "components/nav.templ",
			PropsType:   "[]RailItem",
			Description: "A data-driven sub-navigation rail: the SubNav wrapper plus one SubNavItem per plain item.",
			Render: func() templ.Component {
				return components.SettingsRail("Settings sections", "general", []components.RailItem{
					{Key: "general", Href: "#", Icon: "lucide--settings", Label: "General"},
					{Key: "voice", Href: "#", Icon: "lucide--mic", Label: "Voice"},
				})
			},
		},
		{
			ID: "ToggleField", Slug: "toggle-field", Name: "ToggleField", Layer: LayerComposite,
			Category: "Forms", Subcategory: "toggle", SourceFile: "components/form.templ",
			PropsType:   "name, label, tip string, checked bool, attrs templ.Attributes",
			Description: "A settings row pairing a toggle with a label and optional explanatory tip.",
			Render: func() templ.Component {
				return components.ToggleField("gallery-demo", "Enable feature", "Master switch for the feature.", true, nil)
			},
		},
		{
			ID: "SelectOptionGroups", Slug: "select-option-groups", Name: "SelectOptionGroups", Layer: LayerComposite,
			Category: "Forms", Subcategory: "select", SourceFile: "components/form.templ",
			PropsType:   "[]SelectOptionGroup",
			Description: "Grouped options for a select control.",
			Render: func() templ.Component {
				return components.SelectOptionGroups([]components.SelectOptionGroup{
					{Label: "openai", Options: []components.SelectOption{{Value: "openai/gpt-4o", Label: "gpt-4o", Selected: true}}},
					{Label: "anthropic", Options: []components.SelectOption{{Value: "anthropic/claude", Label: "claude"}}},
				})
			},
		},
		{
			ID: "SaveChangesButton", Slug: "save-changes-button", Name: "SaveChangesButton", Layer: LayerComposite,
			Category: "Forms", Subcategory: "submit", SourceFile: "components/form.templ",
			PropsType:   "—",
			Description: "The shared primary submit button every settings and mutation form footer uses.",
			Render:      func() templ.Component { return components.SaveChangesButton() },
		},
		{
			ID: "StickyFormActions", Slug: "sticky-form-actions", Name: "StickyFormActions", Layer: LayerComposite,
			Category: "Forms", Subcategory: "footer", SourceFile: "components/form.templ",
			PropsType:   "StickyFormActionsProps",
			Description: "The pinned footer for a page-level form: sticky to the bottom of the scroll area while the form is taller than the viewport, inline once it fits.",
			Render: func() templ.Component {
				return withChildren(components.StickyFormActions(components.StickyFormActionsProps{}),
					join(
						withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonGhost, Size: ui.ButtonSM}), templ.Raw("Cancel")),
						components.SaveChangesButton(),
					))
			},
		},
		{
			ID: "ConfirmIcon", Slug: "confirm-icon", Name: "ConfirmIcon", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "icon", SourceFile: "components/confirm.templ",
			PropsType:   "icon string",
			Description: "The destructive-confirm icon circle: the error-tinted rounded square holding an icon.",
			Render:      func() templ.Component { return components.ConfirmIcon("") },
		},
		{
			ID: "DeleteWarning", Slug: "delete-warning", Name: "DeleteWarning", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "text", SourceFile: "components/confirm.templ",
			PropsType:   "DeleteWarningProps",
			Description: "The delete-confirm detail paragraph shared by destructive dialogs.",
			Render: func() templ.Component {
				return components.DeleteWarning("my skill", "and its content. Irreversible.")
			},
		},
		{
			ID: "SnippetCard", Slug: "snippet-card", Name: "SnippetCard", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "code", SourceFile: "components/snippet.templ",
			PropsType:   "label, copyTargetID string, code templ.Component",
			Description: "A client-configuration snippet with a copy control.",
			Render: func() templ.Component {
				return components.SnippetCard("Claude Desktop", "gallery-snippet", templ.Raw(`{"mcpServers":{"memory":{"url":"https://example.invalid"}}}`))
			},
		},
		{
			ID: "SecretRevealModal", Slug: "secret-reveal-modal", Name: "SecretRevealModal", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "secret", SourceFile: "components/secret.templ",
			PropsType:   "SecretRevealProps",
			Description: "The one-time secret reveal as a modal dialog.",
			// The catalog's one overlay entry: it needs app.js to open, so it is
			// marked interactive and opens on its "open" story, which sets
			// AutoOpen. The default render stays closed (AutoOpen false).
			Interactive:    true,
			DefaultVariant: "open",
			Render: func() templ.Component {
				return components.SecretRevealModal(secretRevealFixtureProps(true, false))
			},
			Stories: map[string]func() templ.Component{
				"open": func() templ.Component {
					return components.SecretRevealModal(secretRevealFixtureProps(true, true))
				},
			},
		},
		{
			ID: "SecretRevealPanel", Slug: "secret-reveal-panel", Name: "SecretRevealPanel", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "secret", SourceFile: "components/secret.templ",
			PropsType:   "SecretRevealProps",
			Description: "The same reveal anatomy inline, for pages that show the secret in the body.",
			Render: func() templ.Component {
				return components.SecretRevealPanel(secretRevealFixtureProps(false, false))
			},
		},
		{
			ID: "AlertModal", Slug: "alert-modal", Name: "AlertModal", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "dialog", SourceFile: "components/alert_modal.templ",
			PropsType:   "attrs templ.Attributes",
			Description: "The app-wide result dialog MemoryApp.alert(kind, title, message) fills and opens in place; provider/model test outcomes stay readable instead of a transient toast.",
			// A native <dialog> is closed on render, so the preview needs app.js
			// to auto-open it. The default render stays closed; the "open" story
			// passes DialogAutoOpen, mirroring SecretRevealModal.
			Interactive:    true,
			DefaultVariant: "open",
			Render:         func() templ.Component { return components.AlertModal(nil) },
			Stories: map[string]func() templ.Component{
				"open": func() templ.Component { return components.AlertModal(components.DialogAutoOpen()) },
			},
		},
		{
			ID: "ApprovalCard", Slug: "approval-card", Name: "ApprovalCard", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "approval", SourceFile: "components/approval_card.templ",
			PropsType:   "ApprovalCardProps",
			Description: "The canonical pending-approval card: the gated tool, its redacted arguments, an optional decision badge, and the decision controls.",
			Render: func() templ.Component {
				return components.ApprovalCard(components.ApprovalCardProps{
					ToolName: "web_search",
					ArgsJSON: `{"query": "memory platform release notes"}`,
					Attrs:    templ.Attributes{"data-testid": "gallery-approval"},
				})
			},
			Stories: map[string]func() templ.Component{
				"approved": func() templ.Component {
					return components.ApprovalCard(components.ApprovalCardProps{
						ToolName: "set_field",
						Decision: "approved",
						Message:  "Approved after review.",
						Mode:     components.ApprovalCardDecided,
					})
				},
				"rejected": func() templ.Component {
					return components.ApprovalCard(components.ApprovalCardProps{
						ToolName: "set_field",
						Decision: "rejected",
						Message:  "The proposed value is out of scope.",
						Mode:     components.ApprovalCardDecided,
					})
				},
			},
		},
		{
			ID: "QuestionCard", Slug: "question-card", Name: "QuestionCard", Layer: LayerComposite,
			Category: "Feedback", Subcategory: "question", SourceFile: "components/question_card.templ",
			PropsType:   "QuestionCardProps",
			Description: "The canonical pending-question card: the prompt, the answer controls (choice rows or free text), and the submit footer.",
			Render: func() templ.Component {
				return components.QuestionCard(components.QuestionCardProps{
					Prompt: "Which environment should I deploy to?",
					Options: []components.QuestionCardOption{
						{Label: "Staging", Value: "staging", Description: "The staging cluster"},
						{Label: "Production", Value: "production"},
					},
					Attrs: templ.Attributes{"data-testid": "gallery-question"},
				})
			},
			Stories: map[string]func() templ.Component{
				"free-text": func() templ.Component {
					return components.QuestionCard(components.QuestionCardProps{
						Prompt: "What should the new project be called?",
						Text:   &components.QuestionCardText{Placeholder: "Project name", MaxLength: 120, AriaLabel: "Project name"},
					})
				},
				"answered": func() templ.Component {
					return components.QuestionCard(components.QuestionCardProps{
						Prompt:   "Which environment should I deploy to?",
						Options:  []components.QuestionCardOption{{Label: "Staging", Value: "staging"}},
						Mode:     components.QuestionCardAnswered,
						Selected: []string{"staging"},
					})
				},
			},
		},

		// --- go-daisy primitives (L0) — curated subset ---
		{
			ID: "Button", Slug: "button", Name: "Button", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "action", SourceFile: "<go-daisy ui>",
			PropsType:   "ButtonProps",
			Description: "A full-featured daisyUI button using typed props.",
			Render: func() templ.Component {
				return withChildren(ui.Button(ui.ButtonProps{Type: ui.ButtonTypeButton, Variant: ui.ButtonPrimary, Size: ui.ButtonSM, Icon: "lucide--save"}), templ.Raw("Save changes"))
			},
		},
		{
			ID: "Badge", Slug: "badge", Name: "Badge", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "badge", SourceFile: "<go-daisy ui>",
			PropsType:   "BadgeProps",
			Description: "A daisyUI badge with intent, style, size, icon, and dot variants.",
			Render: func() templ.Component {
				return ui.Badge(ui.BadgeProps{Label: "Ready", Variant: ui.BadgeSuccess, Size: ui.BadgeSizeSM, Icon: "lucide--circle-check"})
			},
		},
		{
			ID: "CardRaw", Slug: "card-raw", Name: "CardRaw", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "card", SourceFile: "<go-daisy ui>",
			PropsType:   "cardClass, bodyClass string, attrs templ.Attributes",
			Description: "A raw card with explicit card/body classes.",
			Render: func() templ.Component {
				return withChildren(ui.CardRaw("card-border", "memory-density-comfortable", nil), templ.Raw(`<p class="text-sm">Card content</p>`))
			},
		},
		{
			ID: "IconSpan", Slug: "icon-span", Name: "IconSpan", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "icon", SourceFile: "<go-daisy ui>",
			PropsType:   "name, size string, color ...string",
			Description: "An Iconify icon span with configurable size and optional color.",
			Render:      func() templ.Component { return ui.IconSpan("lucide--box", "size-5") },
		},
		{
			ID: "IconTile", Slug: "icon-tile", Name: "IconTile", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "icon", SourceFile: "<go-daisy ui>",
			PropsType:   "IconTileProps",
			Description: "A colored rounded-square icon tile holding an icon.",
			Render: func() templ.Component {
				return ui.IconTile("lucide--key-round", ui.IconTileProps{Tone: "primary"})
			},
		},
		{
			ID: "Alert", Slug: "alert", Name: "Alert", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "feedback", SourceFile: "<go-daisy ui>",
			PropsType:   "AlertProps",
			Description: "A daisyUI alert banner.",
			Render: func() templ.Component {
				return ui.Alert(ui.AlertProps{Type: ui.AlertWarning, Style: ui.AlertStyleOutline, Icon: "lucide--triangle-alert", Message: "Something needs attention"})
			},
		},
		{
			ID: "Eyebrow", Slug: "eyebrow", Name: "Eyebrow", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "heading", SourceFile: "<go-daisy ui>",
			PropsType:   "EyebrowProps",
			Description: "An uppercase kicker label above a section heading or value.",
			Render: func() templ.Component {
				return ui.Eyebrow("Overview", ui.EyebrowProps{Size: "text-xs"})
			},
		},
		{
			ID: "InfoTip", Slug: "info-tip", Name: "InfoTip", Layer: LayerDaisy,
			Category: "go-daisy", Subcategory: "tooltip", SourceFile: "<go-daisy ui>",
			PropsType:   "InfoTipProps",
			Description: "A CSS-only daisyUI tooltip wrapping an info icon.",
			Render:      func() templ.Component { return ui.InfoTip("Deploys take ~2 minutes") },
		},
	}
}

// secretRevealFixtureProps builds inert sample props for the secret reveal
// components. modal selects the dialog variant; autoOpen sets the dialog's
// AutoOpen flag (used only by the interactive "open" story). Both carry no live
// endpoint ids so the preview cannot mutate the backend.
func secretRevealFixtureProps(modal, autoOpen bool) components.SecretRevealProps {
	p := components.SecretRevealProps{
		Heading:    "MCP share created",
		Name:       "share-a",
		SecretID:   "gallery-secret",
		Secret:     "emt_this-is-an-inert-sample-value",
		CopyLabel:  "Copy key",
		EndpointID: "gallery-endpoint",
		Endpoint:   "https://example.invalid/mcp",
		Warning:    "This key will not be shown again.",
		AutoOpen:   autoOpen,
	}
	if modal {
		p.DialogID = "gallery-reveal-modal"
		p.Snippets = templ.Raw(`<div class="snippet-slot">{"mcpServers":{"memory":{"url":"https://example.invalid/mcp"}}}</div>`)
	}
	return p
}

// withChildren wraps a component so it renders with the given child content.
func withChildren(c templ.Component, child templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return c.Render(templ.WithChildren(ctx, child), w)
	})
}

// join renders a sequence of components as one component.
func join(comps ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		for _, c := range comps {
			if err := c.Render(ctx, w); err != nil {
				return err
			}
		}
		return nil
	})
}
