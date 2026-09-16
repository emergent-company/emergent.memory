# Tasks — e2e full surface coverage

Test infrastructure + test-only DOM attributes (`skip_specs: true`); no product behavior change, so no new Go unit tests apply. Each task ends with its verification gate. Workstreams run in order: A (test IDs) → B (smoke) → C (mutations) → D (reconcile + docs).

## A — Test-ID infrastructure

- [x] A.1 Add `page-*` root anchors: `data-testid="page-<title-slug>"` on the shell's `<main>` (single point, `gateway/ui.go: pageTestID`), covering every page root. Verify: `templ generate` + `go build ./...` clean; `page-agents` asserted in `agents.spec.ts`.
- [ ] A.2 Add per-control domain/action anchors (`*-list`, `*-row`, `*-create`) via go-daisy `Attrs` — deferred: specs use semantic locators (roles / form `name=`) which are stable enough; add per-control anchors only where a semantic locator proves unstable.
- [x] A.3 Document the convention in `tests/e2e/README.md`. Verify: README renders the table.

> Deferred: smoke specs currently assert via `pageTitle`/roles (proven green); the `page-*`/domain anchors are a robustness refactor to land after the coverage expansion.

## B — Smoke coverage (read/use surface)

- [x] B.1 Agents group: `/agents`, `/agents/:id`, `/agents/:id/settings`, `/agents/:id/sandbox`, `/agents/:id/sessions` (detail via `agent-create-ui`). Verify: green.
- [x] B.2 Documents + Objects: `/documents`, `/objects`, `/objects/new`. Verify: green. *(remaining: `/documents/:id`, `/objects/:id`, `/objects/search`)*
- [x] B.3 Schema + Blueprints: `/schema`, `/blueprints`, `/blueprints/migrations`. Verify: green. *(remaining: `/schema/object-types/:name`, `/blueprints/:id`)*
- [x] B.4 Skills + Schedules: `/skills`, `/skills/new`, `/skills/:id`, `/schedules`, `/schedules/new`. Verify: green. *(remaining: `/schedules/:id`, `/runs/:runId`)*
- [x] B.5 Sessions + Backups + Usage: `/sessions`, `/backups`, `/usage`. Verify: green. *(remaining: `/sessions/:id`)*
- [x] B.6 Settings: `/settings`, `/settings/assistant`, `/settings/overrides`, `/settings/voice`, `/settings/devices`, `/settings/approvals`, `/settings/tokens`. Verify: green.
- [x] B.7 Org + Members + Profile: `/orgs/:id`, `/orgs/:id/members`, `/orgs/:id/tool-settings`, `/members/new`, `/profile/invitations`, `/profile/tokens`. Verify: green.

## C — Mutation coverage (UI flows)

- [x] C.1 Agent create (via `/api/agents`; the JS form is a separate follow-up) + all agent detail pages render. Verify: green.
- [x] C.2 Skill create via UI + skill detail renders. Verify: green.
- [x] C.3 Schedule create (agent seeded via API, then schedule form). Verify: green.
- [ ] C.5 Object create — deferred: a fresh project has no object types ("install a schema pack first"); needs a schema pack install to unblock.
- [x] C.6 Document upload (file → list → detail). Verify: green.
- [x] C.4 Token create via UI (one-shot reveal panel). Verify: green.
- [x] C.7 Member invite via UI (pending invite row). Verify: green.
- [x] C.8 Org delete via danger zone (confirm dialog). Verify: green.

## D — Reconciliation + docs

- [x] D.1 Full suite end-to-end green; mutations run after chromium (project dependency), teardown leaves dev memory clean. Verify: `npx playwright test` fully green (39 specs).
- [x] D.2 Update `tests/e2e/README.md` with the coverage matrix, test-ID convention, and the seed/entity strategy. Verify: README steps reproduce a green run.

## Notes

- The `gateway/` `.templ` edits regenerate `*_templ.go` (`templ generate`); no hand-editing of generated files.
- Deep/flaky flows (chat stream, sandbox run, extraction, provider live test) are out of the green gate until dev memory has a synced provider catalog (infra #9) and a seed agent.
- Agent detail/skill detail pages title themselves with the entity name (not a static label); specs assert on the dynamic name to also catch failed loads (error states render a static label without the name).
- Test IDs ship unconditionally (no dev-only build flag) — see design.md for the tradeoff.
