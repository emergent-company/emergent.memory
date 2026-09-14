# 2026-09-08 — Project transfer between orgs (org-view action)

## Goal

Give the organization view a way to move a project into another organization. Plan the
change spec-first (OpenSpec `transfer-project`), implement it in the gateway UI, and get
it working end to end against the live Memory service — including the service-side
handoff that the gateway client contract depends on.

## Outcome

Done. Full stack live.

- OpenSpec change `transfer-project` created and planned (proposal/spec/design/tasks) and
  implemented: all 10 tasks complete, tasks.md fully checked off.
- Gateway feature shipped in two commits: `f595e04` (planning artifacts) + `a77c96a`
  (feat).
- Service side was delegated out of this repo: opened `emergent.memory` issue #386 for the
  missing `POST /api/projects/{id}/transfer`; it was subsequently implemented, merged
  (`2292b41`, version 0.66.0), and deployed by another session/team. Verified live.
- Permanent e2e added: `e207baa` (`tests/e2e/specs/project-transfer-ui.spec.ts`).

## Decisions

- Mirror the existing project-delete flow (route `POST /projects/transfer`, handler shape,
  HTMX + PRG dual path, `?moved=` flash back to the source org) — consistency, low review
  cost; delete was the closest sibling.
- Dedicated backend call `POST /api/projects/{id}/transfer` (`{"orgId": dest}`) rather than
  folding org reassignment into `UpdateProject` — reparenting is an org-scope authorization
  decision, not a settings patch.
- Transfer action gated UI-side on `org_admin` of the source org + ≥1 candidate destination
  (derived from the orgs-and-projects access tree); the Memory service stays authoritative —
  matches the repo's "backend enforces, UI reflects" convention.
- Local guard rejects `destinationOrgId == source` (and missing/unknown destination) with an
  error flash and zero backend calls — spec requirement, cheap no-op protection.
- Dialog submits a **plain form POST** (not hx-post) — `modalShell` sets `hx-boost=false`,
  matching the new-project dialog idiom; handler still answers HTMX with `HX-Redirect`.
- One shared `modalShell` destination picker on the page (row buttons hand themselves to a
  tiny `openTransferProjectDialog`; dataset/textContent, never innerHTML) — no per-row
  dialog duplication, no app.js change.

## Changes

- `openspec/changes/transfer-project/` (new) — proposal, `specs/project-transfer/spec.md`
  (5 ADDED requirements), design, tasks.
- `gateway/backend.go` — `TransferProject` added to `MemoryBackend`.
- `gateway/memory.go` — `MemoryClient.TransferProject` → `POST /api/projects/{id}/transfer`.
- `gateway/main.go` — `POST /projects/transfer` route.
- `gateway/org_context.go` — `uiTransferProject` handler; `orgPageData` gains
  `TransferOrgs`/`CanTransfer`; `transferState` + `transferMovedMessage` helpers; `uiOrg`
  now fetches the access tree.
- `gateway/org_context.templ` — Transfer item in the per-project row popover (gated on
  `CanTransfer`) + shared `transferProjectModal` (destination `<select>`, source excluded).
- `gateway/memory_test.go`, `gateway/org_context_test.go`, `gateway/handlers_test.go` —
  client httptest, handler guards/success/error against the fake backend, and
  transfer-state gating tests.
- `tests/e2e/specs/project-transfer-ui.spec.ts` (new) — permanent mutation-spec covering
  affordance → dialog → cancel no-op → transfer → success flash + reparent (API-checked),
  self-cleaning.
- External: `emergent-company/emergent.memory` issue #386 → `2292b41 feat(server): add
  project transfer endpoint`, bumped to 0.66.0.

## Verification

- `PATH="/root/go/bin:$PATH" templ generate` (from `gateway/`) — success.
- `go build ./...` (from `gateway/`) — clean.
- `go test ./...` (from `gateway/`) — full suite ok.
- `task lint` (golangci-lint run ./...) — 0 issues.
- Pre-deploy live e2e (temp spec) — gateway + org view render, dialog candidates correct,
  cancel no-op, zero console errors; submit → graceful error flash (endpoint absent on
  api.dev at that time).
- Post-deploy live e2e — `TRANSFER-LIVE http=303 outcome=success inDestOrg=true`; project
  verified under the destination org via API.
- Permanent spec `project-transfer-ui.spec.ts` — `2 passed` (setup + transfer) against the
  dev gateway/backend; committed.

## Open questions / follow-ups

- The `transfer-project` OpenSpec change is fully checked off but not yet archived; its
  `project-transfer` delta spec should be synced to `openspec/specs/` at archive time.
  Tracked as `archive-transfer-project`.
- Visual screenshot QA was not possible this session (model without image input); layout
  intentionally mirrors existing dialogs/menus and DOM assertions passed.

## Tasks

- [archive-transfer-project](../tasks/archive-transfer-project.md) — sync the
  `project-transfer` delta spec + archive the change.
