## 0. Harness groundwork

> **Revised during implementation.** Helpers and per-control `data-testid` anchors are added **just in time, with the spec that needs them** — the batch `data-testid` sweep across 13 templ areas originally planned here was dropped: without a consuming spec there is no way to tell which locators are genuinely unstable, and it would touch many `.templ` files concurrently with in-flight lanes. Both Phase 1/2 lanes needed only three anchors in total.

- [x] 0.1 `helpers/tokens.ts` — created with the Phase 1 token specs (create token, read the one-time secret panel, find a token row, revoke)
- [x] 0.2 `helpers/toast.ts` / `helpers/dialogs.ts` — **dropped from Phase 0**: investigation showed the preference is to assert resulting state rather than transient toasts (`toast.templ` + `app.js` flash stash), and neither implementation lane needed a dialog helper. Add only if a later phase's spec genuinely needs one.
- [x] 0.3 `helpers/mcp.ts` — deferred with Phase 1.3/1.4 (see the blocker there)
- [x] 0.4 Scratch-project helper — **not needed**: the restore lane seeded its own scratch org/project through the existing bootstrap helpers (`helpers/bootstrap.ts`, unchanged)
- [x] 0.5 Per-control `data-testid` anchors — now per-phase, alongside the spec that uses them. Added so far: `token-row`, `token-revoked-badge`, `token-secret-panel` in `api_tokens.templ` (row menu / one-time reveal panel had no stable semantic anchor)
- [x] 0.6 Verify templ changes when they happen: `PATH="/root/go/bin:$PATH" templ generate -f <file>.templ`, then from `apps/web-ui/gateway` run `go build ./...`, `go test ./...` and `task lint` — the full pipeline required by `apps/web-ui/gateway/AGENTS.md` and by this change's harness spec ("Templ changes are verified through the standard pipeline"). This verification applies to every phase that touches templates, not just Phase 0. Note: `*_templ.go` are gitignored, so a fresh worktree must generate before it can build.
- [x] 0.7 Correct the project-order documentation instead of changing the config: keep `mutations: dependencies: ['setup']` (deliberate — makes a single mutation spec cheap) and fix `README.md`, which described mutations as running after chromium (D6, revised)
- [x] 0.8 Update `apps/web-ui/tests/e2e/README.md`: landed — the project-order description now matches `playwright.config.ts` (mutations depend on `setup` only), the fresh-worktree `templ generate` prerequisite is documented in Notes, and the Coverage section carries the new Phase 1 token-lifecycle entries plus the Phase 3 additions (backup lifecycle, document delete, project restore, project-settings autosave, project overrides)
- [x] 0.9 Baseline verified: the mutation project ran green (setup + new specs) before further specs landed

## 1. Phase 1 — Secret lifecycle

- [x] 1.1 `specs/settings/project-token-lifecycle-ui.spec.ts`: create → edit scopes (`POST /settings/tokens/:tokenId/scopes`) → regenerate → revoke; asserts the one-time secret, the changed scope set, and the revoked state. Includes a self-cleanup guard test.
- [x] 1.2 `specs/account/profile-token-lifecycle-ui.spec.ts`: `/profile/tokens/new` → `/:tokenId/edit` → `/:tokenId/scopes` → `/:tokenId/regenerate` → `/:tokenId/revoke`, plus a self-cleanup guard test.
- [x] 1.3 `specs/settings/mcp-share-lifecycle-ui.spec.ts`: list → new → edit/update → rotate → revoke; self-cleanup guard. The blocker is stale — the two lanes landed via #482/#487 and the templates are stable; the spec creates a share (one-time key reveal, no redirect), asserts the list row, edit/update (`?updated=1`), rotate (second one-time key), revoke (`?revoked=1`), and self-cleans via `DELETE /api/mcp-shares/:id`. Skips with a stated reason when the backend tool catalog is empty (create requires ≥1 tool).
- [x] 1.4 ~~`specs/agents/agent-mcp-share-ui.spec.ts`~~ **obsolete** — `agent_mcp_shares.templ` is gone and #501 (`make project share instances tools-only, drop agent allowlist`) removed the per-agent share surface. Superseded by agent MCP *endpoints*, already covered by `agent-mcp-keys-ui.spec.ts`.
- [x] 1.5 Phase 1 verify: `npx playwright test specs/settings/project-token-lifecycle-ui.spec.ts specs/account/profile-token-lifecycle-ui.spec.ts --project=mutations` → setup + 4 tests (project lifecycle, project no-live-token guard, account lifecycle, account no-live-token guard), green on repeat; no tokens left behind (guard tests assert this).

## 2. Phase 2 — Authorization

- [ ] 2.1 `specs/organizations/member-role-ui.spec.ts`: change role; assert self-change and equal-role requests are rejected (`org_members_ui.go`). **Blocked — needs a second identity.** Role change requires a second member in the org; the suite has one Zitadel test user. Resolve by provisioning a second test user in `setup` (see Open Questions in `design.md`).
- [ ] 2.2 `specs/organizations/member-remove-ui.spec.ts`: remove member via UI. **Blocked — same second-identity requirement.**
- [ ] 2.3 `specs/organizations/member-detail-ui.spec.ts`: `/members/:userId` renders identity + role state. **Blocked — needs a second member to have a detail page to open.**
- [x] 2.4 `specs/organizations/invite-revoke-ui.spec.ts`: creates a pending invite for a unique address via `/members/new`, asserts the pending row, revokes it (`POST /invites/:id/revoke`) and asserts it is no longer pending in both the DOM and `/api/invites`. The **accept/decline** surfaces remain uncovered — they need the invitee identity.
- [ ] 2.5 Phase 2 verify: run the mutation project once the second-identity question is settled.

## 3. Phase 3 — Destructive operations

- [x] 3.1 `specs/objects/object-merge-ui.spec.ts`: **the premise was wrong — there is no merge endpoint.** `GET /objects/:id/merge?with=<dst>` (`uiObjectMerge`, `objects.go:478`) resolves source/target/agent and 303-redirects to `/chat?agent=…&prompt=<mergeInstruction>`; fusion is performed by an LLM agent through memory tools, so it is non-deterministic. The spec asserts the route's complete deterministic contract (303 to `/chat` with an agent and a prompt naming both objects by key+id, graph left untouched) plus the missing/unknown-target guards. The literal "losing object gone, relationships carried" assertion belongs in the env-gated live-LLM `scenarios` suite, not here.
- [x] 3.2 `specs/objects/object-relationships-ui.spec.ts`: drives the Connect-object dialog (Task→Person `assigned_to`), then asserts the edge appears in the Relationships section of **both** objects.
- [x] 3.3 `specs/objects/object-search-ui.spec.ts`: asserts `/objects/search` filters to the created object (and returns nothing for a nonsense query), that the object surfaces in the `#connect-dst` datalist, and that the returned id opens the object page. (The UI consumer is a `<datalist>`, so there is no href to click — link-through is asserted by navigating from the returned id.)
- [x] 3.4 `specs/schema/blueprint-lifecycle-ui.spec.ts`: installs bundled `agent-notes` on a scratch project via the gallery form, asserts its types appear in `/api/blueprints/compiled-types` and in the `select[name=type]` of `/objects/new`, unapplies it, and asserts the types and applied state are gone. Plus a no-leftovers guard.
- [x] 3.5 `specs/schema/blueprint-migration-rollback-ui.spec.ts`: creates an object carrying an undeclared `legacy_field`, previews and force-executes `POST /blueprints/migrate`, asserts the stored property is dropped, then drives `POST /blueprints/migrate/rollback`. **This task proves migration *apply* and the rollback route's response contract — not rollback data restoration.** The restoration assertion is skipped (runtime `test.skip`) because rollback restores zero objects: `Repository.List` (`apps/server/domain/graph/repository.go`) projects an explicit column set that omits `migration_archive`, so `RollbackObject` sees no archive and skips every object. **Tracked as GitHub issue #513.** The spec asserts the redirect/`migrateMsg` and auto-reverts to the hard restoration assertion once that is fixed. Plus a no-leftovers guard.
- [x] 3.6 `specs/backups/backups-lifecycle-ui.spec.ts`: creates a backup via the real form, waits up to 120s for `ready` (skips with a reason otherwise), opens the detail, asserts the Download affordance and the route's 302 `Location`, deletes it and asserts it is gone from the list. **Known limitation:** the download click itself is not simulated — the gateway 302s to a presigned off-origin object-storage URL, so the spec asserts the rendered link/href and verifies the 302 through the authenticated request context instead.
- [x] 3.7 `specs/projects/project-restore-ui.spec.ts`: schedules a scratch project for deletion from its row menu, asserts the pending state, restores it via `POST /projects/restore`, and asserts it returns to the active list. Self-cleans the scratch org.
- [x] 3.8 `specs/documents/document-delete-ui.spec.ts`: uploads a uniquely-named document, opens its detail, deletes it (accepting the `hx-confirm` dialog), then asserts it is gone from the list and that the detail/chunk view renders "Document unavailable".
- [x] 3.9 Phase 3 verify: `flock /tmp/memory-e2e.lock npx playwright test <the Phase 3 specs> --project=mutations` → **9 passed, 1 skipped** (the skip is 3.5's rollback assertion). Cleanup audits (each spec's `finally` delete, plus the scratch-project guard tests) confirm zero scratch projects, objects, edges or agents remain in **live listings and search results**. Graph deletes are **soft** — object and relationship rows are retained in the archive and stay restorable via `POST /api/graph/objects/:id/restore` and `POST /api/graph/relationships/:id/restore` — so "left behind" here means absent from live state, not absent from the database.

## 4. Phase 4 — Agent & skill CRUD, org admin

- [ ] 4.1 `specs/agents/agent-edit-ui.spec.ts`: `POST /agents/:id/update` persists name/model/tools and is reflected on detail
- [x] 4.2 `specs/agents/agent-delete-ui.spec.ts`: delete via modal (list row action → `delete-confirm-modal` → `DELETE /api/agents/:id` → row gone). Activate/deactivate (`/api/agents/:id/activate|deactivate`) has **no UI surface** — a JSON-API-only pair, so there is nothing to drive through the browser; noted here rather than left silently open.
- [ ] 4.3 `specs/agents/agent-memories-ui.spec.ts`: `/agents/:id/memories` renders and paginates memories
- [x] 4.4 `specs/agents/agent-sandbox-update-ui.spec.ts`: `POST /agents/:id/sandbox/update` persists sandbox settings (enable switch, base image, fixed repo source + URL/branch, tool allowlist, cpu/memory/disk, setup commands, env vars) across a full reload; plus a fixed-source-without-URL rejection guard.
- [x] 4.4a `specs/agents/agent-tool-groups-ui.spec.ts`: `POST /agents/:id/settings/tools` — the default approval policy round-trip always runs; the per-group approval-policy + enable-switch persistence (PR #568/#578 group-level enable + approval policy) probes for the server capability taxonomy and skips with a stated reason when the backend reports no `ToolGroups`.
- [x] 4.5 `specs/skills-schedules/skill-edit-delete-ui.spec.ts`: `POST /skills/:id/update` (description/content persist across reload) + `/skills/:id/delete` (gone from the list); self-cleaning.
- [x] 4.6 `specs/organizations/org-rename-ui.spec.ts`: `POST /orgs/:id/rename` + `/orgs/:id/settings/general` reflects the new name (`?renamed=1`, persisted across reload). Scratch org + bootstrap reactivation cleanup.
- [x] 4.7 `specs/organizations/org-tool-settings-ui.spec.ts`: `POST /orgs/:id/tool-settings/:toolName` (+ `/delete`) persists and clears tool settings — seeds an override (enabled=true) via the toggle route (a fresh org has none to render), then toggles Disable and deletes through the UI, asserting resulting state each step. Scratch org + bootstrap reactivation cleanup.
- [ ] 4.8 Phase 4 verify: `task e2e:test -- --project=mutations`

## 5. Phase 5 — Interaction depth on render-only pages

- [x] 5.1 `specs/settings/project-settings-autosave-ui.spec.ts`: drives the `project_info` textarea (`POST /settings/project/project_info`, `hx-trigger="change delay:400ms"`, `hx-swap="none"`) and the `editor_agent` select (`POST /settings/editor`), asserting the `HX-Trigger` `memory-toast` event plus the persisted value after a full reload. Both values are captured and restored in cleanup. `dedup_threshold` and `budget_usd` were rejected as not symmetrically restorable through the UI (empty input means "leave unchanged"), which would strand tenant state.
- [x] 5.2 `scenarios/approvals-respond.spec.ts`: `POST /settings/approvals/:questionId/respond` clears the pending item. **Moved to the env-gated `scenarios` project** (was planned as a mutations spec) — a tool approval only exists once a live run pauses on a "ask"-policy tool, and there is no deterministic seed API. The spec drives a gated tool call end-to-end and approves the pending item from `/settings/approvals`, polling a sibling tab (reload) until the Approve control appears while the chat SSE stream stays alive; skips on model non-call / env rejection, hard-fails on page/selector regressions. Reject and Cancel share the same `respond`/`cancel` routes but remain **uncovered by UI e2e** — tracked as a follow-up (no deterministic seed for their paths); this scenario exercises the primary respond path once.
- [ ] 5.3 `specs/settings/devices-revoke-ui.spec.ts`: `POST /settings/devices/:key/revoke`
- [ ] 5.4 `specs/settings/voice-save-ui.spec.ts`: `POST /settings/voice`, `/voice/:key`, `/voice/group/:group` persist field/group edits
- [ ] 5.5 `specs/settings/provider-connection-test-ui.spec.ts`: `POST /settings/providers/test`, `/check-url`, `/:provider/test`, `/:provider/remove`
- [x] 5.6 `specs/settings/settings-overrides-ui.spec.ts`: creates then deletes a scratch `E2E override …` (`POST /settings/overrides`, `/:agentName/delete`), asserting both states across reloads, plus a no-leftovers guard test. No scratch agent is needed — overrides are name-keyed project settings and memory performs no agent-existence check.
- [x] 5.7 `specs/settings/settings-editor-remember-ui.spec.ts`: `POST /settings/remember/agent` — the remember-agent select autosaves inline (HX-Trigger toast) and persists across reload, then restores the prior value. The `dedup` sibling is deliberately not exercised: empty input means "leave unchanged", so a set value is not symmetrically restorable (same reason 5.1 rejected `dedup_threshold`).
- [ ] 5.8 `specs/settings/mcp-nodes-ui.spec.ts`: `/settings/mcp-nodes` lists nodes and `POST /settings/mcp-nodes/remove` works
- [ ] 5.9 `specs/sessions/usage-charts-ui.spec.ts`: assert `stat-total-tokens`/`stat-estimated-cost`/`stat-sessions`/`stat-month-spend` render and `usage-token-chart`/`usage-session-chart` draw from `#usage-timeseries`
- [ ] 5.10 `specs/sessions/session-detail-ui.spec.ts`: `/sessions/:id` renders timeline, run grouping and tool-call details
- [ ] 5.11 `specs/skills-schedules/schedule-lifecycle-ui.spec.ts`: `/schedules/:id` detail, `/:id/update`, `/:id/trigger`, `/:id/toggle`, `/:id/delete`, and `/runs/:runId` run detail
- [ ] 5.12 Phase 5 verify: `task e2e:test -- --project=mutations`

## 6. Phase 6 — Chat & sidepanel

- [ ] 6.1 `specs/sessions/chat-streaming-ui.spec.ts`: streamed turn renders incrementally and terminates; assert transcript DOM stages (env-gated for live model)
- [ ] 6.2 `specs/sessions/chat-conversation-events-ui.spec.ts`: conversation rail refreshes via `GET /api/conversations/:id/events` and `/partial/chat-rail`
- [ ] 6.3 `specs/sessions/chat-question-cards-ui.spec.ts`: ask-user question card respond (`POST /api/chat/questions/:id/respond`) and cancel, asserting on raw SSE frames where the DOM is insufficient
- [ ] 6.4 `specs/shell/assistant-sidepanel-ui.spec.ts`: sidepanel opens, sends a turn via `POST /api/chat`, and renders the reply without leaving the page
- [ ] 6.5 Phase 6 verify: `task e2e:test -- --project=chromium --project=mutations --project=scenarios`

## 7. Close-out

- [ ] 7.1 Update the coverage table in `tests/e2e/README.md` to the post-change spec inventory
- [ ] 7.2 Assert no mutating route from `gateway/main.go` lacks at least one interaction spec; record the audit result in the change notes
- [ ] 7.3 Run the full suite (`task e2e:test`) green against a session-mode gateway; capture the report
- [ ] 7.4 `cd apps/web-ui/gateway && go build ./... && golangci-lint run` clean for any templ/testid changes
- [ ] 7.5 Open PR per phase (base `main`); final PR summarises which routes moved from render-only to behavioral coverage
