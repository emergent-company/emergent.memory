# 2026-09-08 — PRG feedback + titles under hx-boost (object save bug)

## Goal

User reported: editing an object and hitting "Save changes" does nothing — no
toast, nothing saved after refresh
(`/objects/6d4d24c3-07d1-4941-a778-b85506ca41e6`). Fix the save, add e2e
coverage, then generalize the root-cause fix app-wide and repair the two e2e
specs that exposed adjacent hx-boost defects.

## Outcome

Done. Three commits:

- `9da00a0` — object edit save feedback (toast on success + real error surfacing) + e2e.
- `f189e69` — global PRG toast + `<title>` sync under the hx-boost shell.
- `8c8dea4` — org-delete full-load PRG + `project-create-ui` e2e locator fix.

Root cause was three-layered: (1) the memory API (`api.dev.emergent-company.ai`)
threw a `502` burst 19:27–19:30 that made the save POST fail; (2) `uiObjectUpdate`
swallowed the error (silent 303 back, no `?err=`), and success carried no flash —
the user got zero feedback; (3) even with a flash, the hx-boost shell swaps
`#main-content` in place, so the sessionStorage-only flash (flushed on
`DOMContentLoaded`) never fired and `<title>` went stale (partials have no head).

## Decisions

- Surface PRG feedback on object save (success `?updated=1` → "Object updated."
  toast; failure `?err=<reason>` with the real message) — matches the app-wide
  skills/schedules/docs convention `uiObjectUpdate` had skipped.
- Keep redirecting to the new version id after edit (not canonical) — the
  `enhance-object-detail-view` design D4 already decided this deliberately;
  `GetByAnyID` resolves old ids to the head version.
- Make toasts global by upgrading `flashToast`: push straight into the Alpine
  `#toast-container` queue when the shell is loaded (boosted swap), fall back to
  the sessionStorage stash only on cold full loads — no per-page changes needed.
- Carry a `<title>` on every page partial (`page()` → `partialWithTitle`); htmx
  extracts it, applies it to `document.title`, and removes it before the swap —
  fixes stale titles on boosted nav and history restore.
- Revert the earlier per-form workarounds once the global mechanism landed
  (object form `hx-boost="false"`, object-detail `HX-Trigger` header,
  `setToastTriggerHeader` extraction) — one mechanism instead of a one-off.
- Org delete (danger zone) keeps a native full-load PRG via `hx-boost="false"`
  on its form + `onsubmit="return confirm()"` — deleting the current org changes
  session context that lives in the shell *outside* `#main-content`; only a full
  load re-renders the topbar. Precedent: project-transfer modalShell.
- "New project" e2e locator aligned to the committed UI (an `<a href="#">` that
  opens the create dialog) instead of changing the markup — that anchor is
  deliberate (design + unit tests) and another lane owns the org-context UI.

## Changes

- `gateway/objects.go` — `uiObjectUpdate`: success redirect carries `?updated=1`;
  failure uses `redirectWithError` (was a silent same-page 303). `uiObject` GET:
  reads `?updated=1` → passes flash message into `ObjectDetailPage`.
- `gateway/ui.go` — `flashToast` now renders an inline script that pushes into the
  Alpine queue when the shell is loaded and stashes to sessionStorage otherwise
  (JSON-escaped so message text can't break the `<script>`); new
  `partialWithTitle` wrapper; `page()` prefixes partial (boost + history-restore)
  responses with an escaped `<title>`.
- `gateway/objects.templ` — net no change vs. base (hx-boost workaround added in
  `9da00a0`, reverted in `f189e69`).
- `gateway/settings_handlers.go` — net no change vs. base (`toastTrigger`
  extraction added + reverted).
- `gateway/org_context.templ` — org delete form: `hx-confirm` →
  `hx-boost="false"` + `onsubmit="return confirm(...)"` so the delete is a true
  PRG full load and the topbar drops the deleted org.
- `gateway/objects_test.go` — update redirect expectations (`?updated=1` /
  `?err=`), flash presence on `?updated=1`/`?err` detail GETs, new
  `TestUIObjectPartialIncludesTitle` (partial opens with exactly one `<title>`,
  no full-shell markup).
- `gateway/refactor_exact_test.go` — golden `flashToasts` HTML updated for the new
  immediate-or-stash script.
- `tests/e2e/specs/object-edit-ui.spec.ts` (new) — create Person → edit status →
  save → asserts `?updated=1` URL, "Object updated." toast, value persists across
  reload. Runs on the boosted (SPA) path.
- `tests/e2e/specs/project-create-ui.spec.ts` — "New project" locator:
  `getByRole('button')` → `getByRole('link')`.
- `openspec/changes/enhance-object-detail-view/design.md` — D4 note rewritten to
  describe the global PRG/toast/title mechanism.

## Verification

- `go build ./...`, `templ generate`, `go test .` (gateway), `task lint` — all green.
- e2e `object-edit-ui.spec.ts` + `object-create-ui.spec.ts` (mutations) — pass.
- e2e `project-create-ui.spec.ts` + `org-delete-ui.spec.ts` (mutations) — pass.
- Full `--project=chromium` — 45 passed.
- Full `--project=mutations` — 22 passed, 2 failed
  (`agent-model-warning-ui.spec.ts` warning-severity + no-alert cases; see task
  `e2e-agent-model-warning-provider-env`).

## Open questions / follow-ups

- Context-changing navigations beyond org delete may still leave shell chrome
  stale under boost (org/project switch via GET links, sign-out) — audit → task
  `boost-context-stale-shell`.
- Object store behavior: dev memory forks a new version id per save (even a
  no-op), old ids resolve to the head version. Design accepts this (D4), but the
  versioned-store mechanics live in `/root/emergent.memory` (a separate repo) and
  were only partially verified here.
- The `agent-model-warning-ui` e2e pair (other lane's committed P0 spec) fails in
  this dev env because the deepseek provider key/catalog can't be configured;
  its self-skip logic doesn't trigger — tracked as a task, owned by that lane.

## Tasks

- [hx-boost-title-sync](../tasks/hx-boost-title-sync.md) — DONE (resolved here via partial `<title>`; backlog row flipped)
- [boost-context-stale-shell](../tasks/boost-context-stale-shell.md) — audit remaining context-changing navs for stale shell chrome
- [e2e-agent-model-warning-provider-env](../tasks/e2e-agent-model-warning-provider-env.md) — fix env-dependent failures in the agent-model-warning e2e pair
