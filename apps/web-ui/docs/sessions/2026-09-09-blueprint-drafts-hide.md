# 2026-09-09 — Hide empty blueprint drafts section

## Goal
Hide the whole "Drafts" section of the blueprints gallery when there are no draft blueprints. Empty-state card was clutter when the section had nothing to show.

## Outcome
Done. `/blueprints` no longer renders the Drafts section (heading + empty-state card "No blueprints in progress") when the drafts list is empty. Rendered normally when drafts exist.

## Decisions
- Guard the section with `if len(drafts) > 0` instead of keeping the empty-state card — matches the existing `upgradesSection` pattern (`upgradesSection` already hides itself when empty), and the "Create one with the assistant" empty-state copy had no CTA anyway.
- Dropped `cardListProps` Empty/EmptyIcon/EmptyTitle/EmptyDesc from `draftsSection` — unreachable once the guard renders the card only when drafts exist.
- Updated the render test to assert absence ("Drafts" and its old empty-state copy must NOT appear when drafts is nil) rather than presence — the section is now conditionally hidden.

## Changes
- `gateway/blueprints.templ` — `draftsSection` now renders `@cardList("Drafts", ...)` only inside `if len(drafts) > 0`; comment notes hidden-when-empty.
- `gateway/blueprints_test.go` — `TestRenderBlueprintsPage` empty-state case: removed "No blueprints in progress" from expected-present strings; added assertions that `Drafts` / `No blueprints in progress` are absent. Populated-drafts case unchanged and still passing.
- `gateway/blueprints_templ.go` — regenerated (git-ignored generated file).

## Verification
- `PATH="/root/go/bin:$PATH" templ generate` — ok (regenerated `blueprints_templ.go`)
- `go build ./...` — ok
- `go test . -run TestRenderBlueprintsPage -count=1` — ok
- `go test . -count=1` (full gateway suite) — ok, 2.236s
- `PATH="/root/go/bin:$PATH" golangci-lint run ./...` — 0 issues

Note: no dev-server restart/browser pass — the browser (user-side) can't reach this server's localhost and the running `:8095` instance belongs to a parallel session. Render tests cover both states.

## Open questions / follow-ups
- None from this change. Related but out of scope: `openspec/changes/ui-data-authoring/` (B.2.2, not implemented) plans a "New draft" action in the gallery; if/when landed, the section may warrant a always-visible form again — revisit then.

## Tasks
- none
