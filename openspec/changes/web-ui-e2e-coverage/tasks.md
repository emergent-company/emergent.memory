## 0. Harness groundwork

- [ ] 0.1 Add `helpers/toast.ts` with `expectFlashToast(page, text?)` (settles the `flashToast` render in `toast.templ` + `app.js` stash/flush)
- [ ] 0.2 Add `helpers/dialogs.ts` with `confirmDialog(page, action)` and `openRowMenu(page, rowLocator)` for destructive/row-menu flows
- [ ] 0.3 Add `helpers/tokens.ts` with `createProjectToken`, `createProfileToken`, `readOneTimeSecret` (one-time reveal panel)
- [ ] 0.4 Add `helpers/mcp.ts` with `registerMCPServer`, `createMCPShare`, `revokeShare`, `rotateShare` built on the existing bootstrap/auth helpers
- [ ] 0.5 Extend `helpers/bootstrap.ts` with `createScratchProject(name)` + `deleteScratchProject(id)` for destructive phases
- [ ] 0.6 Add selective `data-testid` attributes (D4 list) to: `agents*.templ`, `documents.templ`, `objects.templ`, `schema*.templ`, `blueprints.templ`, `skills*.templ`, `backups.templ`, `project_settings.templ`, `api_tokens.templ`, `mcp_servers*.templ`, `mcp_shares*.templ`, `org_members_ui.templ`, `sessions.templ` — row menus, destructive triggers, status badges, secret-reveal panels, list rows only
- [ ] 0.7 Run `templ generate`; `go build ./...` and `task lint` from `apps/web-ui/gateway`
- [ ] 0.8 Encode `chromium → mutations → scenarios` ordering via explicit `dependencies` in `tests/e2e/playwright.config.ts` (match README, D6)
- [ ] 0.9 Document the testid/locator convention and the phase coverage table in `apps/web-ui/tests/e2e/README.md`
- [ ] 0.10 Verify baseline: `cd apps/web-ui && task e2e:test -- --project=chromium` green before any new spec lands

## 1. Phase 1 — Secret lifecycle

- [ ] 1.1 `specs/settings/project-token-lifecycle-ui.spec.ts`: create → edit scopes (`POST /settings/tokens/:id/scopes`) → regenerate → revoke; assert one-time secret shown once
- [ ] 1.2 `specs/account/profile-token-lifecycle-ui.spec.ts`: `/profile/tokens/new` → `/:id/edit` → `/:id/scopes` → `/:id/regenerate` → `/:id/revoke`
- [ ] 1.3 `specs/settings/mcp-share-lifecycle-ui.spec.ts`: list → new → edit/update → rotate (old token rejected) → revoke; self-cleanup guard
- [ ] 1.4 `specs/agents/agent-mcp-share-ui.spec.ts`: create/revoke/rotate per-agent share from the agent detail surface
- [ ] 1.5 Phase 1 verify: `task e2e:test -- --project=mutations`, no orphaned tokens/shares left behind

## 2. Phase 2 — Authorization

- [ ] 2.1 `specs/organizations/member-role-ui.spec.ts`: change role; assert self-change and equal-role requests are rejected (`org_members_ui.go:346,388,608`)
- [ ] 2.2 `specs/organizations/member-remove-ui.spec.ts`: remove member via UI, assert removal from list and loss of access
- [ ] 2.3 `specs/organizations/member-detail-ui.spec.ts`: `/members/:userId` renders identity + role state
- [ ] 2.4 `specs/organizations/invite-lifecycle-ui.spec.ts`: revoke (`POST /invites/:id/revoke`), decline, and accept (`/invites/:id/accept`) surfaces
- [ ] 2.5 Phase 2 verify: `task e2e:test -- --project=mutations`

## 3. Phase 3 — Destructive operations

- [ ] 3.1 `specs/objects/object-merge-ui.spec.ts`: `GET /objects/:id/merge` + merge action; assert losing object gone, relationships carried over
- [ ] 3.2 `specs/objects/object-relationships-ui.spec.ts`: `POST /objects/:id/relationships`; assert edge visible on both objects
- [ ] 3.3 `specs/objects/object-search-ui.spec.ts`: `/objects/search` typeahead filters and result navigation
- [ ] 3.4 `specs/schema/blueprint-lifecycle-ui.spec.ts`: `POST /blueprints/enable` then `POST /blueprints/:id/unapply`; assert types removed
- [ ] 3.5 `specs/schema/blueprint-migration-rollback-ui.spec.ts`: `POST /blueprints/migrate` then `POST /blueprints/migrate/rollback`; assert schema returns to pre-migration state
- [ ] 3.6 `specs/backups/backups-crud-ui.spec.ts`: `POST /backups` → detail `ready` → `/:id/download` → `/:id/delete`
- [ ] 3.7 `specs/projects/project-restore-ui.spec.ts`: `POST /projects/restore` after a scheduled purge; assert project restored to the active list
- [ ] 3.8 `specs/documents/document-delete-ui.spec.ts`: `POST /documents/:id/delete`; assert removal from list and chunk viewer no longer reachable
- [ ] 3.9 Phase 3 verify: `task e2e:test -- --project=mutations`; confirm scratch projects cleaned up

## 4. Phase 4 — Agent & skill CRUD, org admin

- [ ] 4.1 `specs/agents/agent-edit-ui.spec.ts`: `POST /agents/:id/update` persists name/model/tools and is reflected on detail
- [ ] 4.2 `specs/agents/agent-delete-ui.spec.ts`: delete via modal + activate/deactivate (`/api/agents/:id/activate|deactivate`); assert list/status transitions
- [ ] 4.3 `specs/agents/agent-memories-ui.spec.ts`: `/agents/:id/memories` renders and paginates memories
- [ ] 4.4 `specs/agents/agent-sandbox-update-ui.spec.ts`: `POST /agents/:id/sandbox/update` persists sandbox settings
- [ ] 4.5 `specs/skills-schedules/skill-edit-delete-ui.spec.ts`: `POST /skills/:id/update` + `/skills/:id/delete`
- [ ] 4.6 `specs/organizations/org-rename-ui.spec.ts`: `POST /orgs/:id/rename` + `/orgs/:id/settings/general` reflects the new name
- [ ] 4.7 `specs/organizations/org-tool-settings-ui.spec.ts`: `POST /orgs/:id/tool-settings/:toolName` (+ `/delete`) persists and clears tool settings
- [ ] 4.8 Phase 4 verify: `task e2e:test -- --project=mutations`

## 5. Phase 5 — Interaction depth on render-only pages

- [ ] 5.1 `specs/settings/project-settings-autosave-ui.spec.ts`: assert `hx-post` + `hx-trigger="change delay:400ms"` + `hx-swap="none"` autosave shows the toast and persists after reload
- [ ] 5.2 `specs/settings/approvals-respond-ui.spec.ts`: `POST /settings/approvals/:id/respond` and `/cancel` clear the pending item
- [ ] 5.3 `specs/settings/devices-revoke-ui.spec.ts`: `POST /settings/devices/:key/revoke`
- [ ] 5.4 `specs/settings/voice-save-ui.spec.ts`: `POST /settings/voice`, `/voice/:key`, `/voice/group/:group` persist field/group edits
- [ ] 5.5 `specs/settings/provider-connection-test-ui.spec.ts`: `POST /settings/providers/test`, `/check-url`, `/:provider/test`, `/:provider/remove`
- [ ] 5.6 `specs/settings/settings-overrides-ui.spec.ts`: project override create (`POST /settings/overrides`) + `/:agentName/delete`
- [ ] 5.7 `specs/settings/settings-editor-remember-ui.spec.ts`: `POST /settings/editor` and `/settings/remember/:field` persist
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
