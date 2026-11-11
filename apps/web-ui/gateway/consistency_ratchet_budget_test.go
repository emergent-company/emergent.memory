package main

// consistency_ratchet_budget_test.go holds the seeded budget tables for the
// consistency ratchet. Both tables are declared tight (exact equality) against
// the counts the corresponding scanners produce on the current tree, so the
// ratchet fails when authored markup drifts either direction — new inline markup
// must be adopted or budgeted with a justification, and a budget entry that no
// longer matches must be tightened in the same change.
//
// These tables are test-only: they live in a _test.go file and do not ship.

// adoptionBudget declares the tolerated inline reuse of component-owned markup.
// Each entry carries the justification for tolerating it (the same reasoning the
// gallery's adoptionAllowlist records), so a future author can read why the
// ad-hoc markup is acceptable instead of adopting the shared component.
//
// A note must name the *pinned* library gap that blocks adoption, and it has to
// be re-read when the go-daisy pin moves: five of these originally cited a gap
// that v0.16.0 already closed (Alert.Body, Badge.Href/Position/DirectLabel), so
// the real blocker is now narrower and different — mostly the absent
// arbitrary-class prop (the share_manage card note was already accurate). A
// justification that describes a fixed gap is how an adoptable site stays
// unadopted for a year.
func adoptionBudget() []adoptionBudgetEntry {
	return []adoptionBudgetEntry{
		{file: "chat.templ", marker: "class:alert", count: 1,
			note: "chat.js reveals the banner by toggling the hidden class on #chat-model-warning; ui.Alert has a Body slot (so composed content is fine) but no Class prop, so a client-side hidden toggle cannot be expressed"},
		{file: "inbox.templ", marker: "class:badge", count: 1,
			note: "tab count badge needs tabular-nums so the digits do not jitter when a live refresh changes the count; ui.Badge has DirectLabel for the direct-text count but no Class prop"},
		{file: "notifications_bell.templ", marker: "class:badge", count: 1,
			note: "unread indicator badge: ui.Badge has Position, DirectLabel and Attrs for indicator-item positioning and the data-notifications-badge hook, but no Class prop for tabular-nums"},
		{file: "runs.templ", marker: "class:badge", count: 2,
			note: "link-styled ghost badges: ui.Badge has Href and DirectLabel, but no Class prop, so the hover:opacity-70 affordance cannot be expressed"},
		{file: "share_manage.templ", marker: "class:card", count: 1,
			note: "<form class='card border border-line'>; ui.CardRaw has no <form> element variant (As: div/a/button/section/article/li), so the form element itself cannot be the card"},
		{file: "share_page.templ", marker: "class:alert", count: 1,
			note: "the inline terminal is role=status, not role=alert, and needs its own share-inline-terminal/items-start/gap-3/hidden classes; ui.Alert hardcodes role=alert and has no Class prop"},
		// The deploy portal is a standalone public document rendered before any
		// session/app shell exists. It intentionally uses daisy primitives
		// directly: the shared components assume the shell/layout context and
		// several lack the Class/role knobs this landing page needs.
		{file: "deploy_portal.templ", marker: "class:alert", count: 5,
			note: "standalone pre-session portal; ui.Alert hardcodes role=alert and has no Class prop for the landing states"},
		{file: "deploy_portal.templ", marker: "class:badge", count: 1,
			note: "standalone pre-session portal; ui.Badge has no Class prop for the landing eyebrow"},
		{file: "deploy_portal.templ", marker: "class:btn", count: 3,
			note: "standalone pre-session portal; ui.Button requires shell context and cannot render the disabled OAuth CTA branch cleanly"},
		{file: "deploy_portal.templ", marker: "class:card", count: 4,
			note: "standalone pre-session portal; ui.Card* assumes the app shell and has no Class prop for the step/workspace cards"},
	}
}

// patternBudget declares the tolerated count of each drift token per authored
// file, seeded at the current baseline. The note names the canonical token to
// normalise toward as entries are paid down.
func patternBudget() []patternBudgetEntry {
	return []patternBudgetEntry{
		// legacy-bordered — daisyUI legacy border classes; canonical is the
		// shared form-control component (components/form).
		{pattern: patternLegacyBordered, file: "approvals.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "board.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "chat.templ", count: 4, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "datasources.templ", count: 7, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "datasources_oauth.templ", count: 9, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "documents.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "documents_chunks.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "inbox.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "migrations.templ", count: 4, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "objects.templ", count: 2, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "project_settings.templ", count: 1, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "share_page.templ", count: 3, note: "normalise to the shared form-control component"},
		{pattern: patternLegacyBordered, file: "sidepanel.templ", count: 1, note: "normalise to the shared form-control component"},

		// surface-opacity-drift — canonical is bg-base-200/40.
		{pattern: patternSurfaceOpacity, file: "agent.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "backups.templ", count: 5, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "blueprints.templ", count: 2, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "board.templ", count: 3, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "chat.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "documents.templ", count: 2, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "documents_chunks.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "embeddings.templ", count: 4, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "inbox.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "mcp_nodes.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "mcp_shares.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "project_settings.templ", count: 4, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "proposal.templ", count: 2, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "schedules.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "schema.templ", count: 1, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "sessions.templ", count: 2, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "share_manage.templ", count: 3, note: "normalise to bg-base-200/40"},
		{pattern: patternSurfaceOpacity, file: "trace_waterfall.templ", count: 1, note: "muted bg-base-200/30 fill on a collapsible <details> (ui.SoftBox has no details element); normalise to bg-base-200/40"},

		// border-token-drift — canonical is border-base-content/10.
		{pattern: patternBorderTokenDrift, file: "agent.templ", count: 3, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "agent_mcp_endpoint.templ", count: 2, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "api_tokens.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "blueprints.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "blueprints_model_map.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "board.templ", count: 3, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "chat.templ", count: 5, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "inbox.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "mcp_servers.templ", count: 4, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "org_context.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "org_members_ui.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "schedules.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "schema.templ", count: 1, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "share_manage.templ", count: 2, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "share_page.templ", count: 5, note: "normalise to border-base-content/10"},
		{pattern: patternBorderTokenDrift, file: "ui.templ", count: 1, note: "normalise to border-base-content/10"},

		// empty-note — the muted-paragraph note surface; canonical is ui.EmptyNote.
		{pattern: patternEmptyNote, file: "object_preview.templ", count: 1, note: "normalise to ui.EmptyNote; 1 residual: icon-led note with gap-1 + size-3 — EmptyNote's icon gap is fixed at 1.5"},
		{pattern: patternEmptyNote, file: "schema.templ", count: 1, note: "normalise to ui.EmptyNote; 1 residual: bordered centred empty-properties box (rounded-box border-dashed text-center) — not a note"},

		// dialog-title — the h3 text-lg font-semibold dialog heading shape;
		// canonical is ui.DialogTitle. Fully adopted across root and components/:
		// no residual, so no entry.
		//
		// dialog-title-section — the h2 text-lg font-semibold section/card
		// heading shape. These are section headings, not dialog titles; there is
		// no canonical primitive for a bare section heading, so they are a
		// recorded residual rather than an adoptable surface.
		{pattern: patternDialogTitleSection, file: "agent.templ", count: 1, note: "h2 agent-name card heading — a section heading, not a dialog title; no canonical primitive"},
		{pattern: patternDialogTitleSection, file: "backups.templ", count: 1, note: "h2 \"All backups\" list section heading with a right-aligned create action — ui.SectionProps has no action slot; adopt the shared header+action helper when it lands"},
		{pattern: patternDialogTitleSection, file: "blueprints.templ", count: 1, note: "h2 \"Changes\" panel heading — a section heading, not a dialog title; no canonical primitive"},
		{pattern: patternDialogTitleSection, file: "schema.templ", count: 2, note: "h2 \"Appearance\"/\"Properties\" section headings — not dialog titles; no canonical primitive"},
		{pattern: patternDialogTitleSection, file: "share_manage.templ", count: 2, note: "h2 \"Share links\"/\"New share link\" section headings — not dialog titles; no canonical primitive"},
		{pattern: patternDialogTitleSection, file: "share_page.templ", count: 1, note: "h2 \"Chat with Memory\" section heading — not a dialog title; no canonical primitive"},

		// soft-box-surface — rounded-box + canonical static bg-base-200/40 fill;
		// canonical is ui.SoftBox. Every remaining entry is a recorded residual
		// with its reason: a non-div element, an off-axis border/fill, a dynamic
		// conditional fill, or a Go-string class list.
		{pattern: patternSoftBoxSurface, file: "agent.templ", count: 1, note: "border-dashed (agent tool-group empty state) — ui.SoftBox is a solid hairline only"},
		{pattern: patternSoftBoxSurface, file: "backups.templ", count: 2, note: "non-div <label> backup-include rows — ui.SoftBox renders a <div>"},
		{pattern: patternSoftBoxSurface, file: "mcp_nodes.templ", count: 1, note: "selected/unselected conditional fill (templ.KV bg-primary vs bg-base-200/40) — no data-driven surface"},
		{pattern: patternSoftBoxSurface, file: "mcp_servers.templ", count: 3, note: "non-div <details> row + DetailsBase Go-string literal + border-dashed group"},
		{pattern: patternSoftBoxSurface, file: "ui.templ", count: 1, note: "non-div <pre> code block — ui.SoftBox renders a <div>"},

		// clipboard — the hand-rolled copy affordance (data-copy-* wiring, page
		// copy-script ids, navigator.clipboard body); canonical is form.ClipboardCopy.
		{pattern: patternClipboard, file: "object_preview.templ", count: 1, note: "icon-only copy with its own data-testid/sr-only target wired by object-preview.js — not a form.ClipboardCopy field"},
		{pattern: patternClipboard, file: "share_manage.templ", count: 10, note: "reveal-once flow inlines the key via Mono, which form.ClipboardCopy documents as unsuitable for on-demand secrets — stays a bespoke data-reveal-url fetch"},
	}
}
