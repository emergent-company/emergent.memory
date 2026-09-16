## Context

The archived change `add-playwright-e2e-suite` established the harness: a `setup` project (real Zitadel OIDC login → API bootstrap of org+project → storage state), a `chromium` read/use project, a serial `mutations` project for UI wizard specs, and `globalTeardown` that deletes the bootstrap org. It is green at 13 specs.

The gateway (Echo + templ + HTMX + go-daisy) renders everything server-side; there is no client-side route table to enumerate, so coverage is driven by the gateway route list and `pageTitle` labels. Selectors currently mix roles/text with a handful of manual `data-testid` attributes (`project-switcher`, `chat-input`, `session-list`, `schedule-run-row`, `usage-session-chart`, `usage-token-chart`, `account-menu`).

## Goals / Non-Goals

**Goals:**

- Add a stable `data-testid` convention and anchor the shell + every page root and primary action.
- Reach 100% route coverage: every page renders under the bootstrap tenant (smoke), plus UI-driven mutations for the main entities.
- Keep the suite deterministic (no live-LLM/flaky gates in the green path).

**Non-Goals:**

- No product behavior change; test IDs are the only DOM addition.
- Deep/flaky flows (chat streaming, sandbox run, document extraction, provider live test) stay out of the green gate until dev memory has a synced catalog and a seed agent.

## Decisions

### Test-ID convention

```
data-testid="page-<route>"        main content root, e.g. page-agents, page-settings-providers
data-testid="<domain>-<thing>"    lists/rows/panels, e.g. agents-list, agents-row
data-testid="<domain>-<action>"   controls, e.g. agent-create, token-save, org-delete
data-testid="stat-<metric>"       stat cards, e.g. stat-agents
```

- Anchors are **unconditional** (ship in prod): harmless, standard practice, no build plumbing. Dev-only gating was considered and rejected as complexity without payoff.
- Mechanics: go-daisy components already expose `Attrs templ.Attributes` (`ui.Button`, `ui.CardRaw`, `nav.Link`, `form.Field`, `layout.SidebarItem`); for raw HTML just add `data-testid="..."`. No new library code.

### Coverage tiers

| Tier | Definition | Gate |
|---|---|---|
| **Smoke** | every route renders under session: `page-*` visible + title matches | `npx playwright test` green |
| **Mutation** | UI create/update/delete for a main entity; asserts list/detail reflects it | green |
| **Deep/flaky** | chat stream, sandbox run, extraction, provider test | excluded from green gate; documented, added only when infra ready |

### Seed strategy

The `setup` bootstrap already creates a clean org+project. For detail-page specs that need a target entity (agent detail, schedule detail, skill detail, token detail, document detail, object detail), a **mutation spec creates the entity via UI first** (or a shared `beforeAll` helper), then the smoke spec asserts the detail page renders. Mutations run in the serial `mutations` project so they never race the parallel read surface. Entities created inside the bootstrap org are cascade-deleted by teardown; org-level entities (org tool-settings, member invite) self-clean.

## Coverage matrix (target)

### Smoke — read/use surface (add ~24 specs)

| Group | Routes |
|---|---|
| Agents | `/agents`, `/agents/:id`, `/agents/:id/settings`, `/agents/:id/sandbox`, `/agents/:id/sessions` |
| Chat | `/chat` (empty + session rail when a conversation exists) |
| Documents | `/documents`, `/documents/:id` |
| Objects | `/objects`, `/objects/new`, `/objects/:id`, `/objects/search` |
| Schema | `/schema`, `/schema/object-types/:name` |
| Blueprints | `/blueprints`, `/blueprints/:id`, `/blueprints/migrations` |
| Skills | `/skills`, `/skills/new`, `/skills/:id` |
| Schedules | `/schedules`, `/schedules/new`, `/schedules/:id`, `/runs/:runId` |
| Sessions | `/sessions`, `/sessions/:id` |
| Backups / Usage | `/backups`, `/usage` |
| Settings | `/settings`, `/settings/assistant`, `/settings/overrides`, `/settings/voice`, `/settings/devices`, `/settings/approvals`, `/settings/tokens` |
| Org | `/orgs/:id` (landing), `/orgs/:id/members`, `/orgs/:id/tool-settings` |
| Members / Profile | `/members/new`, `/profile/invitations`, `/profile/tokens` |

### Mutation — UI flows (add ~8 specs)

agent create/update · skill create · schedule create · token create · object create · document upload · member invite · org delete (danger zone)

## Risks

- **Detail-page specs need an existing entity** — mitigated by a mutation-before-assert pattern in the serial project.
- **HTMX partial swaps don't update `<title>`** — smoke specs assert `page-*` visibility + URL, not title, after in-page nav (same caveat as the archived suite).
- **Test-ID drift** — convention lives in the README; new pages must carry `page-*`, enforced by a lint-ish grep in CI later (out of scope now).
