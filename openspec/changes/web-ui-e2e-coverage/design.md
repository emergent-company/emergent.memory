# Design — Web UI E2E Coverage

## Context

The existing suite is mature where it exists: a single `setup` project performs real Zitadel OIDC login once and persists `storageState` to `.auth/state.json` plus bootstrap tenant state to `test-results/.bootstrap.json`; every other project depends on it. Writers live in the `mutations` project (`*-ui.spec.ts`, `workers: 1`) and read-surface assertions live in `chromium`. Live-dependency journeys live in `scenarios` and are env-gated.

The gap is not infrastructure — it is coverage. The decisions below are therefore about *how to add ~30 specs without degrading the suite*, not about rebuilding the harness.

## Goals / Non-Goals

**Goals**

- Behavioral coverage for every mutating Web UI route reachable by a logged-in user.
- Interaction (not just render) assertions for the ~34 currently render-only tests.
- Stable locators: locator churn should not be the reason a coverage test breaks.
- Each phase independently mergeable and independently reviewable.
- No new flakiness: deterministic ordering, explicit cleanup, no cross-project state leakage.

**Non-Goals**

- Rebuilding the harness, adding a `webServer` auto-start, or replacing the shared-`storageState` auth model.
- Coverage for capabilities that have **no UI route**: `journal`, `branches`, `monitoring`, `discoveryjobs`, `tracing`, `notifications`, `email`. Verified against `gateway/main.go` route registration — these domains are API-only, so a missing UI spec is not a gap.
- Voice/LiveKit room behavior (`POST /api/token`, mic publish, mute). Requires real LiveKit infrastructure and a media-capable browser; out of scope for this change.
- iOS (`apps/ios`), the Mac/Linux connectors, and PWA manifest assertions.
- Unit and integration coverage of the same flows; this change is e2e only.

## Decisions

### D1 — Extend the existing suite in place

Add specs under the existing `apps/web-ui/tests/e2e/specs/<area>/` directories and reuse `setup`/`bootstrap`/`login`. Do not introduce a second harness, a mocked gateway, or a separate Playwright config.

*Rationale:* the auth and tenant bootstrap is the expensive, hard-won part of this suite; a parallel harness would duplicate it and drift. The legacy `run-e2e.sh` + `mock-memory.mjs` harness stays unwired, as it is today.

*Alternative rejected:* an isolated "coverage" project with its own auth — doubles login cost per run and splits the fixture story.

### D2 — Writers go to `mutations`, readers to `chromium`

Any spec that creates, updates, or deletes state is named `*-ui.spec.ts` and runs in the `mutations` project (`workers: 1`). Read-only interaction assertions (filters, tabs, chart rendering, typeahead against seeded data) stay in `chromium`.

*Rationale:* the `chromium` project runs across all configured workers; putting writers there would allow concurrent mutation of shared bootstrap state.

### D3 — Every state-creating spec self-cleans

A spec that creates an entity deletes it in `afterEach`/`finally`, and where the created resource is enumerable, a trailing guard test asserts none remain — the pattern from `specs/settings/mcp-servers-create-ui.spec.ts`. Scratch entities are named with an `E2E` prefix so the guard can match them.

*Rationale:* `global-teardown.ts` only deletes the bootstrap org when `E2E_RESET=1`. The default reuse mode means an unclean spec accumulates state across runs until it is manually reset — the most likely way this suite becomes flaky over time.

### D4 — Testids only where semantic locators are unstable

Priority order for new locators: `getByRole` → `input[name=]`/`#id` → `data-testid`. Add `data-testid` only when the element has no stable accessible name or form binding: row menus, destructive confirm triggers, dynamically rendered list rows, one-time secret reveal panels, chart containers, and status badges.

*Rationale:* the README already states the "add testid only where unstable" convention, and 8 of the 49 existing specs use `getByTestId`. Blindly testid-ing every element would be churn without benefit. Phase 0 adds the attributes selectively, per the list in `tasks.md`, not wholesale.

### D5 — Harness groundwork + six feature phases, ordered by risk, each independently mergeable

Phase 0 (harness groundwork) → secret lifecycle → authorization → destructive → CRUD → interaction depth → chat. Each phase is a self-contained PR: new specs + the testids those specs need + README/coverage note.

*Rationale:* a single 30-spec PR is unreviewable and will block on one flaky test. Risk ordering means the highest-value coverage lands first even if later phases slip.

### D6 — Keep the mutation project's `setup`-only dependency; fix the docs to match

**Revised during implementation.** `playwright.config.ts:51-55` deliberately declares `mutations: dependencies: ['setup']` — *not* `chromium` — so that running a single mutation spec runs login/bootstrap plus that spec, instead of the whole read surface. That decision is correct and stays. The actual defect was documentation drift: `README.md` described mutations as running "AFTER chromium".

*Rationale:* adding `chromium` as a dependency would multiply the cost of every single mutation run for no isolation benefit — mutation specs already self-clean and the project is serialized (`workers: 1`). Enforcing an ordering the config intentionally avoids would be a regression. The fix is to make the docs describe the real graph.

*Supersedes:* the original proposal asked for the `chromium → mutations` dependency to be added. It is not added.

### D7 — Live-dependency tests stay env-gated and skip, never fail

Tests requiring a live LLM provider, a reachable external MCP server, or a provider catalog stay behind runtime `test.skip(...)` gates, consistent with the existing `scenarios` project and `document-extraction-ui.spec.ts`.

*Rationale:* the suite must be green on a machine without third-party credentials. Skipped-with-reason is already the established contract (there are no static `test.skip`/`test.fixme` markers in the suite today).

### D8 — Chat assertions read the real stream

For `POST /api/chat`, prefer asserting on rendered transcript DOM plus the terminate signal otherwise used by `helpers/chat.ts` (`sendChatMessage` already waits up to 120s for the streamed reply to settle). Where an SSE contract must be verified, observe the stream through the page rather than replacing it with a mock.

*Rationale:* `helpers/chat.ts` exists and works; a parallel mock-stream helper would test a different system than the one that ships. Streaming behavior is verified by the scenario specs against real models and stays gated.

## Risks / Trade-offs

- **Added runtime.** Phases 1-6 add ~30 specs; `mutations` is `workers: 1`, so wall-clock grows roughly linearly. *Mitigation:* keep each spec to one flow, reuse the bootstrap org, avoid per-spec login (shared `storageState`), and do not add live-LLM specs outside `scenarios`.
- **Templ churn.** Adding testids touches `.templ` files and requires `templ generate`. *Mitigation:* attributes are additive and mechanical, and are added **just in time with the consuming spec** rather than in one batch — three anchors so far, all in `api_tokens.templ`. When a `.templ` file does change, that phase runs `templ generate` + `task lint` + the gateway `go build ./...` per `apps/web-ui/gateway/AGENTS.md`.
- **Destructive-spec blast radius.** Object merge, blueprint unapply, migration rollback, and backup delete operate on real state. *Mitigation:* each runs against scratch entities in the bootstrap project created within the spec; blueprint/migration tests use a scratch project where the bootstrap and bundled packs are not required to survive.
- **Mutating specs share one tenant.** The mutation project runs `workers: 1` against the shared bootstrap tenant, so every spec must assert on the entities it created rather than on absolute list contents. *Enforced via D3 and by writing locators that target own-created names; both implementation lanes were green on repeat runs.*
- **Fresh worktree builds.** `*_templ.go` are gitignored, so a new worktree contains no generated templates and the gateway will not compile until `templ generate` runs. Any lane verifying a Go/templ change must generate first. *Mitigation: documented in `tests/e2e/README.md` and in lane instructions.*
- **Selector drift on new testids.** Newly added testids become a contract. *Mitigation:* D4 limits them to elements with no stable semantic alternative, and the README documents the convention so reviewers can push back.

## Migration Plan

No migration or data change. Rollout is per-phase PRs to `main`; each phase is additive and independently revertable. If a phase proves flaky, revert that phase's specs without touching earlier phases. After all phases land, `README.md`'s coverage section reflects the new spec inventory.

## Open Questions

- Should the per-area coverage table live in `tests/e2e/README.md` or in a sibling `COVERAGE.md`? (Current lean: README, since it already documents projects and conventions.)
- Is a CI job for `mutations` in scope for this change, or does the current manual/environment-dependent run stay as-is? (Current lean: out of scope — the suite needs a pre-running session-mode gateway and secrets.)
- Phase 6 streaming assertions: assert on transcript DOM only, or also capture the raw SSE frames for a contract-level check? (Current lean: DOM by default, raw frames only for the ask-user question-card path, where the DOM is insufficient.)
- **Second test identity (blocks Phase 2 tasks 2.1-2.3).** Member role change, member removal, member detail, and invite accept/decline all require a *second* member in the org. The suite has one Zitadel test user, so these routes cannot be covered behaviorally today. Options: provision a second interactive test user (second `storageState`) or have `setup` seed a second member into the bootstrap org through the API. (Current lean: seed a second member in `setup` — no second interactive login required.) Until this is settled, those tasks stay blocked rather than being weakened into page-load assertions.
- Where should the per-area coverage inventory live — `tests/e2e/README.md` (current) or a sibling `COVERAGE.md`? The README is already ~200 lines and now carries conventions, prerequisites and coverage.

## Findings discovered during implementation

Product observations the coverage work surfaced. None are fixed by this change; each needs its own decision and its own PR.

1. **`POST /blueprints/migrate/rollback` restores nothing — confirmed defect.**
   `apps/server/domain/schemas/service.go:808-827` lists objects via `graphSvc.GetRepository().List(...)` and skips every object whose `MigrationArchive` is empty. `apps/server/domain/graph/repository.go:367-371` — `List`'s explicit `Column(...)` projection does **not** include `migration_archive`, so `MigrationArchive` is always empty and every object is skipped by construction.
   Observed live on a scratch project: `303 …migrateMsg=Rolled back to 2.0.0: 0 objects restored`, with the dropped property still dropped. The `restore_type_registry` path is unaffected (it re-installs types and fails loudly when packs are unresolvable) — it is the *data* restoration that silently no-ops.
   Impact: the endpoint reports success while restoring nothing. The e2e assertion for this is already written and holds `test.skip(true, …)`; it auto-reverts to a hard assertion once the projection is fixed.
   Suggested fix: select `migration_archive` in `Repository.List`, or give the rollback path a dedicated query that does — plus a DB-level regression test.

2. **Object merge is not a deterministic endpoint.** `uiObjectMerge` only redirects to a chat session; the fusion itself is agent/LLM work through memory tools. Any assertion about merge *outcomes* belongs in the env-gated live-LLM `scenarios` suite, not in `mutations`.

3. **The gateway exposes no object-delete route.** Object specs cannot clean up through the UI; they authenticate to the memory API with the signed-in user's own session token (from the `memory_session` cookie) plus `X-Project-ID`. Relevant to every future object spec.

4. **The migrations form cannot be exercised as rendered** for a freshly created scratch project: bundled packs ship a single version (no upgrade path) and new objects are stamped with the current `schema_version` (no drift), so `StaleObjects == 0` and the form never renders. `/blueprints/migrate` and `/blueprints/migrate/rollback` are therefore driven as direct requests with the resulting data state asserted on the object page. UI-level migration coverage would need a seeded stale-schema fixture.
