## Why

The Memory Web UI (`apps/web-ui/gateway`, Go templ + HTMX) exposes 251 registered routes across 18 feature areas, but the Playwright suite in `apps/web-ui/tests/e2e` covers only a fraction of that surface. An audit of route registration (`gateway/main.go:83-377`) against the 49 spec files / 91 tests shows:

- **34 of 91 tests are render/title-only** — they call `expectAppPage(page, title)` and assert nothing about behavior. An entire routes (`/settings/*` × 7, `/skills`, `/schedules`, `/schema`, `/blueprints`, `/members`, `/profile/*`, `/sessions`, `/backups`, `/usage`, `/objects` list, `/agents` list, `/documents` list) is asserted only to load.
- **Zero coverage for several high-risk mutation families**: API token lifecycle (scopes/regenerate/revoke, both project and profile scope), MCP shares (create/update/revoke/rotate), member role changes and removals, invite revoke/accept/decline, object merge and relationship creation, blueprint enable/unapply and migration rollback, backup create/download/delete, agent and skill update/delete, project restore, org rename and org tool-settings, approval respond/cancel, schedule update/delete/trigger/toggle and run detail, session/run detail.
- **Chat streaming is unasserted** — the `POST /api/chat` SSE stream, conversation live events, ask-user question cards, and the assistant sidepanel (`sidepanel.js`) have no e2e proof.
- **Feature testids exist in only 7 templ files** (`ui.templ`, `chat.templ`, `schedules.templ`, `usage.templ`, `account_menu.templ`, `sidepanel.templ`, `auth_ui.templ`). Every other area must be targeted with `#id`/`input[name=]` locators, which are brittle and break on markup refactors.
- The documented `chromium → mutations → scenarios` project ordering is described in `tests/e2e/README.md` but **not encoded** in `playwright.config.ts`, so the read-surface and mutation projects currently run without a declared order.

The consequence is that regressions in secret lifecycle, authorization changes, and destructive graph/schema operations can ship undetected — precisely the flows where a silent regression is most expensive.

## What Changes

Six independently mergeable phases, ordered by risk:

- **Phase 0 — Harness**: shared helpers and per-control `data-testid` anchors added **just in time, with the spec that needs them** (see the revision note in `design.md` D6 and `tasks.md` §0); plus a README correction so the documented project order matches the config's deliberate `mutations: dependencies: ['setup']`.
- **Phase 1 — Secret lifecycle** (highest risk): project API tokens (`edit`/`scopes`/`regenerate`/`revoke`), profile API tokens (full family), MCP share lifecycle (list/new/edit/update/revoke/rotate + one-time token reveal), per-agent MCP shares.
- **Phase 2 — Authorization**: member role change (including the self-change and equal-role guards), member removal, `/members/:userId` detail, invite revoke/accept/decline.
- **Phase 3 — Destructive operations**: object merge, relationship creation, object search typeahead, blueprint enable/unapply, blueprint migration apply + rollback, backup create/download/delete, project restore, document delete.
- **Phase 4 — Agent & skill CRUD**: agent update, agent delete + activate/deactivate, agent memories, agent sandbox update, skill update/delete, org rename, org `settings/general`, org tool-settings update/delete.
- **Phase 5 — Interaction depth on render-only pages**: project-settings inline autosave (the 69 `hx-` attributes in `project_settings.templ`), approvals respond/cancel, device key revoke, voice settings save, provider test-connection/check-URL/remove, project overrides CRUD, settings editor and `remember/:field`, usage charts, schedule detail/update/delete/trigger/toggle + `/runs/:runId`, `/sessions/:id` detail, MCP node removal.
- **Phase 6 — Chat**: SSE streaming assertions against the real stream, conversation live events, ask-user question card respond/cancel, assistant sidepanel turn.

Every new spec lands in the `mutations` project (`*-ui.spec.ts`, `workers: 1`) unless it is a read-only assertion, and each spec that creates persistent state carries a self-cleanup guard (the pattern already established by `specs/settings/mcp-servers-create-ui.spec.ts`).

## Capabilities

### New Capabilities

- `web-ui-e2e-coverage`: behavioral end-to-end coverage requirements for every mutating Web UI route, the render-only routes that must gain interaction assertions, and the chat/SSE surfaces.
- `web-ui-e2e-harness`: suite-level conventions that make the coverage maintainable — locator/testid policy, helper inventory, project ordering and execution, fixture isolation and self-cleanup, and env-gating for live-dependency tests.

### Modified Capabilities

<!-- No production behavior changes. Testids are markup additions in existing templ
     files and do not alter any spec'd capability's observable behavior. -->

## Impact

- `apps/web-ui/tests/e2e/specs/**` — ~30 new spec files (phases 1-6)
- `apps/web-ui/tests/e2e/helpers/**` — new `toast.ts`, `dialogs.ts`, `tokens.ts`, `mcp.ts`; extensions to `bootstrap.ts`
- `apps/web-ui/tests/e2e/playwright.config.ts` — encode `chromium → mutations` ordering
- `apps/web-ui/tests/e2e/README.md` — testid convention and phase/coverage documentation
- `apps/web-ui/gateway/**/*.templ` + generated `*_templ.go` — additive `data-testid` attributes in agents, documents, objects, schema, blueprints, skills, backups, project settings, api tokens, MCP servers/shares, orgs/members/invites/profile, sessions templates (run `templ generate`)
- No production behavior change, no API change, no migration, no breaking change
- The suite still requires an already-running gateway in session mode (`AUTH_MODE=session`, Zitadel) at `E2E_BASE_URL`; tests do not start a server
