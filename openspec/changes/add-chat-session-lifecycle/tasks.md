## 1. Persistence and entity

- [x] 1.1 Add a Goose migration adding `is_archived boolean NOT NULL DEFAULT false` and `archived_at timestamptz NULL` to `kb.chat_conversations`, with a reversible down step; verify `task migrate:up` then `task migrate:status` shows the migration applied and a down/up cycle succeeds.
- [x] 1.2 Add `IsArchived bool` and `ArchivedAt *time.Time` to the `Conversation` model (`apps/server/domain/chat/entity.go`) and extend `ListConversationsParams` with an `IncludeArchived bool`; verify `go build ./...` in `apps/server` and that the model round-trips (unit test asserting persisted values are read back).

## 2. Repository and list filtering (unit-tested)

- [x] 2.1 Add repository accessors to set and clear the archive state for a conversation, applying the `(owner_user_id = ? OR is_private = false)` predicate and project scope, returning not-found on no match; verify unit test: owner archive/unarchive succeeds, foreign and unknown ids return not-found.
- [x] 2.2 Apply the default-exclude filter to `ListConversations` (`WHERE is_archived = false`) unless `IncludeArchived` is set, preserving `updated_at DESC` ordering and the existing count; verify unit test asserting default excludes archived, include-archived returns both ordered by recency, and `Total` matches the filtered set.
- [x] 2.3 Add unit test asserting an archived conversation is still returned by the by-id / history accessors (archive is non-destructive, D5); verify the test fails if archive were implemented as a soft-delete filter on reads.
- [x] 2.4 Extend the delete unit tests to cover the access predicate (D8): owner deletes, a non-owner member deletes a non-private conversation, a foreign private id returns not-found, and messages are removed with the conversation; verify each case asserts the expected row count and message visibility.

## 3. Service, handler, and routes

- [x] 3.1 Add `ArchiveConversation` / `UnarchiveConversation` service methods (idempotent — setting an already-set state succeeds) and verify unit tests cover success, idempotency, and 404 propagation.
- [x] 3.2 Add `ArchiveConversation` / `UnarchiveConversation` handlers and register `POST /api/chat/:id/archive` and `POST /api/chat/:id/unarchive` under the `chat:use` group (not the `chat:admin` subgroup); verify handler unit tests assert 404 for foreign ids and 200 for owner, and that neither route requires `chat:admin`.
- [x] 3.3 Wire `includeArchived` from the list query parameter into `ListConversationsParams` and regenerate swagger; verify `go build ./...` and a handler test asserting the parameter is forwarded and defaults to false.
- [x] 3.4 Move `DELETE /api/chat/:id` from the `chat:admin` subgroup to the `chat:use` group (D8) and confirm the handler still returns not-found for foreign ids; verify a route test asserts a non-admin caller reaches the handler and that the delete handler test returns 404 for a foreign conversation.

## 4. Gateway client and call-site audit

- [x] 4.1 Extend the gateway memory client `ListConversations` to accept the include-archived option and add `ArchiveConversation` / `UnarchiveConversation` / `DeleteConversation` methods hitting the corresponding routes; verify gateway unit tests assert the request path, method, and query parameter.
- [x] 4.2 Audit every gateway `ListConversations` call site and set the parameter deliberately — default (exclude) for the rail, agent dashboard "recent chats", and the Sessions trace page; `includeArchived=true` for the usage/session-series statistics (`usage.go`) so archive does not rewrite historical counts; verify a unit test asserting the usage series requests include-archived and a list surface does not.

## 5. Web UI — rail filter and per-row actions

- [x] 5.1 Add an "Include archived" control to the chat rail filter row and have it drive the rail list query; verify a gateway render test asserts the control is present with its expected `data-testid`/`aria-label` and that archived rows are absent when off and present when on.
- [x] 5.2 Add a per-row action menu to active session rows with an Archive action posting to the archive route, and to archived rows with an Unarchive action posting to the unarchive route, refreshing the rail on success; verify gateway render test asserts the menu and correct action per row state, and an update test asserts the route is called.
- [x] 5.3 Visually distinguish archived rows (badge/muted treatment) using existing badge/density conventions; verify render test asserts the archived marker's test id is present only on archived rows.
- [x] 5.4 Confirm archiving the currently-open conversation leaves it open (D5/ spec scenario); verify a gateway test rendering the rail with the open conversation archived still shows the workspace and its messages.
- [x] 5.5 Add a Delete action to the per-row action menu for every session state, posting to the delete route only after a confirmation dialog (reuse the existing confirm-dialog component) and refreshing the rail on success; verify render tests assert Delete is present for active and archived rows, and an interaction test asserts no delete request is sent when the confirmation is dismissed.
- [x] 5.6 Handle deleting the currently-open conversation: the workspace SHALL not render the deleted transcript or an error page but return to a ready-to-start state (D5 / spec scenario); verify a gateway test rendering the workspace after the open conversation is deleted shows the empty/new-session state.

## 6. Verification

- [x] 6.1 Run server suites: `task test` and `task test:integration`; fix any regressions in chat domain tests.
- [x] 6.2 Run gateway suites: `go build ./...`, `go test ./...`, and `task lint` from `apps/web-ui/gateway`; ensure no lint findings.
- [ ] 6.3 Manual rail smoke (`task dev` in `apps/web-ui`): archive an active session (disappears), enable include-archived (reappears marked), unarchive (returns to active), delete a session (confirmation shown; confirming removes it, dismissing keeps it); record the outcome.

## 7. Optimistic + bulk rail actions (#1382, #1385)

- [x] 7.1 Make archive/unarchive optimistic in `chat.js`: hide an archived row instantly, restore it on failure, and reconcile with the server list on settle; verify with the hermetic js-dom spec `chat-session-optimistic-archive.spec.ts`.
- [x] 7.2 Add bulk session selection to the rail: a selection-mode toggle, per-row labelled checkboxes, select-all with an indeterminate state, a live selected count, and a bulk archive action reusing the per-session archive route; verify the markup contract test and `chat-session-bulk-archive.spec.ts`.
- [x] 7.3 Keep the selection controls accessible (accessible names, `aria-pressed` on the toggle, `aria-live` selector count, keyboard-operable checkboxes) and re-apply selection state after every rail refresh.
- [x] 7.4 Keep the optimistic pending-archive hide distinct from the agent/origin filter's `hidden` class (a filter change or a failed rollback must not reveal a still-hidden row), and re-sync selection state when `#chat-root` is swapped; verify with the bulk js-dom regression cases and `openspec validate --strict`.
