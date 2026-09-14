# 2026-09-05 — Two-line project switcher trigger

## Goal
Make the project switcher trigger button two-line: org name on the first line (styled like the dropdown org header), project name on the second.

## Outcome
Done. Trigger renders org over project in two lines. Committed `50f13e1`, pushed to `master`. Layout delegated to @designer; test assertions updated by orchestrator.

## Decisions
- Two-line org-over-project (not inline "Org / Project") — mirrors the dropdown's org-header/project-row hierarchy.
- Org line reuses the dropdown header styling (`text-[10px] font-medium tracking-[0.14em] uppercase text-base-content/60`) — visual consistency with the picker menu.
- Org-less project → bare project name only (no empty org line) — avoids dead vertical space.
- Active org, no project → org line over muted "Select project" — communicates state without new copy.
- Routed layout/design to @designer — UI/visual work, per lane rules.

## Changes
- `gateway/auth_ui.templ` — `projectSwitcher` trigger body: replaced single-line `{org} / {project}` with a two-line `flex flex-col` block (org line + project line), with per-case fallbacks.
- `gateway/auth_ui_test.go` — updated 4 trigger assertions from `"Org / Project"` string match to org+project two-line presence; updated 2 comments.

## Verification
- `templ generate` — regenerated (generated `*_templ.go` files are gitignored).
- `go build ./...` — pass.
- `go test ./...` — pass (web-ui + webui).
- `golangci-lint run ./...` — only pre-existing `oidc_test.go` unused `fmt` (parallel-session WIP, not this change).

## Open questions / follow-ups
- Manual browser check of the two-line trigger layout (truncation, height, icon centering, fallbacks) — not yet done; render tests assert string presence only.

## Tasks
- [verify-two-line-project-switcher](../tasks/verify-two-line-project-switcher.md) — manual browser verification.
