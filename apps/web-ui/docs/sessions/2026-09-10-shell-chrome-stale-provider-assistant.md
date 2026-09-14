# 2026-09-10 — Stale shell chrome: provider warning badge + assistant button

## Goal

Fix two user-reported stale-chrome bugs that both required a manual page refresh:

1. A "no provider configured" warning in the shell did not disappear after the
   user set up a provider.
2. The topbar **Assistant** link/button did not appear after the user configured
   an assistant agent; a manual refresh was needed.

## Root cause

The app shell (sidebar + topbar + sidepanel, `ui.templ` `appShell`) is rendered
only on a full page load. In-content navigation is `hx-boost` and swaps only
`#main-content` (or, for the provider-config form, a boosted 303 that htmx
follows as a GET and swaps into `#main-content`).

- The zero-provider warning badge lives in the **sidebar** (`sidebar_user.templ`
  `sidebarNavRow`, `/settings` row) — outside `#main-content`.
- The **Assistant** button + sidepanel are gated by `assistantAgent` in
  `appShell` — outside `#main-content`, and the assistant save was an
  `hx-swap="none"` toast.

So both mutations updated backend state and the page body, but left the shell
chrome stale until a full reload. This is the same class as the session-context
stale-shell bug ([boost-context-stale-shell](../tasks/boost-context-stale-shell.md)
/ [2026-09-08-prg-toast-titles](2026-09-08-prg-toast-titles.md)), which established
`render.RedirectAfterMutation` (`HX-Redirect` for HTMX, `303` for plain POSTs) as
the deterministic fix.

## Fix

Make the three shell-chrome-affecting settings mutations true full-load PRGs via
`github.com/emergent-company/go-daisy/render.RedirectAfterMutation`:

- `uiProjectProviderConfig` — provider add/update success (was a plain 303).
- `uiProjectProviderRemove` — provider remove success (was a plain 303).
- `uiProjectSettingsAssistant` — assistant set/clear success (was a toast).

`projectHasNoProviders`' 10s cache is already invalidated on provider save/remove,
so the full reload re-reads and renders the correct badge.

## Decisions

- **Full reload, not out-of-band chrome swaps.** The repo has no OOB-swap
  precedent and the shell/sidepanel carry JS init; the documented doctrine is
  full-load PRG for anything outside `#main-content`. Full reload also re-inits
  the sidepanel cleanly. The visible state change (badge clears / Assistant
  button appears) is its own feedback; the assistant reload keeps the
  "Assistant settings saved." flash via `?updated=1`.
- Plain (non-JS) POSTs keep the 303/`Location` behavior — `RedirectAfterMutation`
  branches on `HX-Request`, so existing non-HTMX tests/behavior are unchanged.

## Changes

Gateway only (`gateway/`):

- `settings_providers.go` — add `render` import; `uiProjectProviderConfig` and
  `uiProjectProviderRemove` success paths → `render.RedirectAfterMutation(..., "/settings/providers?updated=1")`; doc comments note the shell-chrome rationale.
- `settings_handlers.go` — add `render` import; `uiProjectSettingsAssistant`
  success → `render.RedirectAfterMutation(..., "/settings/assistant?updated=1")`;
  doc comment updated.
- `settings_providers_test.go` — `TestUIProviderConfigPersistsHTMXFullLoad`,
  `TestUIProviderRemoveHTMXFullLoad` (HX-Request → 200 + `HX-Redirect`).
- `settings_handlers_test.go` — `TestUIProjectSettingsAssistantFullLoad` (HTMX →
  `HX-Redirect`; plain POST → 303), covering both set and clear branches.
- `docs/spec/04-go-application.md` — shell-chrome bullet expanded from
  session-context-only to all chrome outside `#main-content`.
- `tests/e2e/scenarios/provider-openai-litellm-config.spec.ts` — extended: the
  sidebar zero-provider warning icon must be **visible** on the fresh project
  (initial) and **gone** right after the UI provider save, with no manual reload
  (`sidebarProviderWarning` helper scopes `#_layout-sidebar a[href="/settings"]`).
- `tests/e2e/specs/settings/assistant-settings-ui.spec.ts` (new, `-ui` → mutations
  suite) — fresh scratch project + agent: topbar `[data-testid="sidepanel-toggle"]`
  is absent initially, appears after selecting the assistant agent, and is gone
  again after clearing it — all without `page.reload()`.

No templ changes; no `templ generate` needed.

## Verification

- `gateway`: `go build ./...` clean; `go test ./... -count=1` green.
- `golangci-lint run ./...` clean (`task lint` unavailable: `lefthook` not on PATH).
- e2e against the live dev gateway (`alfred-dev.tail0358fa.ts.net:8095`):
  - `--project=mutations specs/settings/assistant-settings-ui.spec.ts` → 2 passed.
  - `--project=scenarios scenarios/provider-openai-litellm-config.spec.ts` →
    2 passed (real `E2E_OPENAI_API_KEY`/LiteLLM; sidebar icon visible → cleared).
  - `--project=mutations specs/settings/provider-config-ui.spec.ts` → 2 passed
    (error path unchanged).
- No manual browser run needed; the e2e specs drive the real UI and assert the
  no-reload chrome update.

## Open questions / follow-ups

- Same class, not fixed here (user did not report): inline **project name** save
  (`POST /settings/project/name`) changes the topbar project-switcher label but is
  an `hx-post ... hx-swap="none"` toast, so the topbar name stays stale until a
  reload. Candidate for the same `RedirectAfterMutation` treatment or an OOB
  chrome-refresh mechanism if the audit is extended.
- If shell-chrome mutations grow, consider a generic OOB "refresh the chrome"
  event/partial instead of per-handler full reloads.
