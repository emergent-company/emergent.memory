## 1. Schema & domain model

- [ ] 1.1 Add migration adding `scope TEXT NOT NULL DEFAULT 'account'`, `requires_action BOOLEAN NOT NULL DEFAULT false`, `event_key TEXT` to `kb.notifications`, plus indices `(user_id, scope, project_id, read)` and `(user_id, requires_action, read)`; backfill `scope='account'`; verify with `task migrate:up` then `task migrate:status`
- [ ] 1.2 Create `kb.notification_preferences(id, user_id, project_id NULL, event_key, channel, enabled, timestamps)` with `UNIQUE(user_id, project_id, event_key, channel)` and FKs (`user_id`→`core.user_profiles`, `project_id`→`kb.projects ON DELETE CASCADE`); verify migration up/down
- [ ] 1.3 Add the new fields + `NotificationPreference` to `apps/server/domain/notifications/entity.go` and verify a unit test asserts Bun mapping/round-trip
- [ ] 1.4 Grep every writer of `kb.notifications` (confirm only `agents/ask_user_tool.go` and `provider/usage_service.go`) and verify no other direct inserts exist before relying on the backfill

## 2. Event taxonomy

- [ ] 2.1 Define the event-key taxonomy (account + project keys, each with scope, default delivery, required/actionable flags, category) in a leaf package to avoid import cycles, and verify a unit test asserts every key has a valid scope and default
- [ ] 2.2 Add a resolver that maps an event key to its effective delivery decision given account-mandatory / required-project / opt-in rules, and verify table-driven unit tests cover account, required, opted-in, and opted-out cases

## 3. Preferences

- [ ] 3.1 Implement preference read/upsert in the repository and verify unit tests cover insert, update, unique-conflict upsert
- [ ] 3.2 Add `Service` methods to list effective preferences for a user (+project) and set a preference, and verify unit tests assert defaults are materialized for every project event key
- [ ] 3.3 Verify a unit test asserts account-scope keys are not suppressible (preference toggle has no effect on delivery)

## 4. Producer API

- [ ] 4.1 Add `Service.Create(ctx, CreateInput)` that resolves scope/taxonomy/preferences, inserts via `Repository.Create`, applies `group_key` coalescing, and returns the created (or nil when suppressed) notification, with unit tests for: account delivered, project opt-in off → not created, required project event → created despite prefs, grouping
- [ ] 4.2 Inject `events.Service` into the notifications module and emit `EntityNotification` on successful create; verify a unit test asserts the event is emitted with the notification id/project
- [ ] 4.3 Migrate `agents/ask_user_tool.go` to `Service.Create` and verify existing agent-question tests still pass
- [ ] 4.4 Migrate `provider/usage_service.go` budget alerts to `Service.Create` (or an event-based hand-off) and verify tests pass and no import cycle is introduced (`go build ./...`)

## 5. New producers

- [ ] 5.1 Emit `project.member.added` / `project.member.removed` (account scope) on project membership changes and verify unit tests
- [ ] 5.2 Emit `user.role.changed` / `user.access.granted|revoked` (account scope) on role/permission changes and verify unit tests
- [ ] 5.3 Emit `invite.received` (account scope, actionable, accept/decline actions) on invitation and verify unit tests
- [ ] 5.4 Emit `task.assigned` and `approval.requested` (project scope, required, actionable, `task_id` set) and verify unit tests

## 6. API & routes

- [ ] 6.1 Add `scope` and `project_id` params to list + counts handlers/repository and verify unit tests cover account-only, project-scoped, and invalid-scope responses
- [ ] 6.2 Add `requires_action` filter and verify unit tests
- [ ] 6.3 Add `GET`/`PUT /api/notifications/preferences` handlers and verify unit tests cover list, upsert, and unknown-key rejection
- [ ] 6.4 Add `POST /api/notifications/:id/unread`, `POST /:id/snooze`/`/unsnooze`, `POST /:id/clear`/`/restore`, `POST /:id/resolve` (action) handlers and verify unit tests per route
- [ ] 6.5 Make `mark-all-read` scope-aware and verify unit tests assert account vs project scoping

## 7. Real-time

- [ ] 7.1 Verify the existing `GET /api/events/stream` carries `entity=notification` events created via `Service.Create` (integration-style unit test or events-service unit test)
- [ ] 7.2 Add a gateway SSE proxy (`/api/notifications/stream`) forwarding the session user's notification events and verify a handler test asserts the stream passes events through with the bearer/project set

## 8. Web UI — bell

- [ ] 8.1 Add the bell to `gateway/ui.templ` navbar (before `@userAccountMenu`) with `ui.Indicator`/`ui.IndicatorBadge` and `data-testid="notifications-bell"`; verify a render test asserts it renders with and without unread count
- [ ] 8.2 Thread the unread count into the shell (`Server.page` → `appShell`) using `GET /api/notifications/counts`; verify render tests cover count present/absent and update shell tests
- [ ] 8.3 Add the "Inbox" nav entry to `sidebarGroups` with the existing role-gating convention; verify render tests

## 9. Web UI — inbox page

- [ ] 9.1 Add `gateway/inbox.templ` page with Account | Project scope switch and tabs (All / Unread / Action required / Snoozed / Cleared); verify render tests for each scope/tab and empty states
- [ ] 9.2 Add unread styling and per-item inline actions (accept/decline, approve/deny, open) wired to the API; verify render tests for actionable vs non-actionable items
- [ ] 9.3 Add the project-inbox opt-in entry pointing at preferences; verify render test
- [ ] 9.4 Wire real-time badge/list updates from the SSE proxy and verify via a DevTools browser check

## 10. Verification

- [ ] 10.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server/` (or `task build` / `task test`) pass
- [ ] 10.2 `templ generate` produces no diff and `task lint` passes
- [ ] 10.3 Restart the dev server, then browser-test: bell shows unread dot, inbox shows account vs project events, unread marking, mark-read, inline accept/decline, preferences suppress a project event, badge updates on a new notification
