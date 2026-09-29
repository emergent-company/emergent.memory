## Context

See `proposal.md` — Why. `kb.notifications` and `apps/server/domain/notifications/` already implement the hard parts of an inbox: per-user rows with `read`/`read_at`, `dismissed`, `cleared_at`, `snoozed_until`, `importance` (`important`/`other`) and tabs (`all`/`important`/`other`/`snoozed`/`cleared`), an `actions` jsonb column, `action_url`/`action_label`/`action_status*`, `group_key` coalescing, `source_type`/`source_id`, `related_resource_type`/`related_resource_id`, `task_id`, `severity`, and `category`. Routes (`apps/server/domain/notifications/routes.go`) already expose list (tab/category/unread/search), counts, stats, mark-read, dismiss, and mark-all-read.

The gaps this change fills:

- **No scope model.** A single user-scoped list mixes global account events with project activity. There is no way to present two inboxes.
- **No producer API.** Only two ad-hoc direct inserts create rows (`agents/ask_user_tool.go`, `provider/usage_service.go`); there is no `Create` on the service, and `provider` deliberately avoided importing `notifications` to dodge a circular dependency.
- **No preferences/opt-in.** Nothing suppresses noisy project events or lets a user opt into them.
- **No real-time emit on create.** The `events` bus has an `EntityNotification` type (`apps/server/domain/events/types.go`) but only the agent-question path emits it.
- **No UI.** No bell, badge, or inbox page exists in `apps/web-ui/gateway/`.

`events.Service` is an in-memory fan-out with no outbox/durability — safe to use as a real-time poke only; `kb.notifications` remains the durable store.

## Goals / Non-Goals

**Goals:**

- Two inboxes driven by an explicit `scope` (`account` | `project`).
- A stable event taxonomy with documented per-event default delivery.
- Opt-in delivery for project activity, mandatory delivery for account/security/action-required events.
- One producer entry point that applies scope, taxonomy, preferences, grouping, and emits a real-time event.
- Actionable notifications with inline resolve for invitations, approvals, mentions, and agent questions.
- A bell with an unread indicator and an inbox page with a scope switch and tabs.

**Non-Goals:**

- Sophisticated filtering beyond scope + existing tabs (explicitly deferred by the requester).
- Email/desktop/mobile channel delivery (the preference model reserves the channel dimension; only `in_app` is delivered now).
- Digests and quiet hours.
- Replacing the background-task monitor (remains in `add-notifications-tasks`).
- Org-scoped inbox views or an `organization_id` column on notifications.

## Decisions

### D1 — Extend `kb.notifications`, do not fork

Build on the existing table and domain. It already carries read/dismiss/clear/snooze/importance/actions/grouping. A parallel "inbox" table would duplicate all of it.

- **Rejected:** a new `kb.inbox_events` table — duplicates semantics and splits the read/unread source of truth.

### D2 — Explicit `scope` enum, not derived from `project_id`

Add `scope TEXT NOT NULL DEFAULT 'account'` (`account` | `project`). Scope cannot be derived from `project_id` because "added to a project" is an **account** event that still references a project for its link/context.

- **Migration:** backfill existing rows to `account` (the two current producers are account/budget-ish); new project-activity producers set `scope='project'`.
- **Rejected:** derive scope as `project_id IS NULL ? account : project` — misclassifies cross-cutting membership events and blocks account events from linking to an entity.

### D3 — Inbox selection is a list filter, orthogonal to importance tabs

Scope answers "which inbox"; the existing tabs answer "how important / lifecycle state". `GET /api/notifications` gains `scope` and (for project scope) `project_id` params, alongside the existing `tab`/`category`/`unread_only`/`search`.

- **Rejected:** encoding scope as extra tab values — conflates two independent axes and breaks existing tab counts.

### D4 — Two-tier delivery: mandatory account vs opt-in project

- **Account scope is mandatory.** Membership, permissions, security, invites, announcements are always delivered; preferences do not suppress them (channel-level opt-out is a later concern).
- **Project scope is opt-in.** Default off, except a small **required** set of action events (assigned, mention, approval request, agent question, comment reply) which are always delivered.
- Rule of thumb encoded in the taxonomy: **needs the user to act → required + `requires_action`; FYI about a change → opt-in, default off.**

This directly answers the requester's "avoid cluttering the inbox with unnecessary information for project events".

### D5 — Actionable items are explicit (`requires_action`)

Add `requires_action BOOLEAN NOT NULL DEFAULT false`. Drives an "Action required" view/tab and inline controls, reusing `actions` jsonb and `action_status`/`action_status_at`/`action_status_by`. Existing `task_id` and `kb.agent_questions.notification_id` remain the jump/resolve links.

### D6 — Preferences table

New `kb.notification_preferences(id, user_id, project_id NULL, event_key, channel, enabled)` with `UNIQUE(user_id, project_id, event_key, channel)`. `project_id NULL` = account-scope default (mostly informational; account delivery is mandatory). Resolution at create time: account keys ignore prefs; required project keys ignore prefs; other project keys require an enabled `in_app` row, otherwise the notification is not created.

- **Rejected:** a JSON blob on `kb.project_memberships` — harder to query/validate, and couples notification prefs to membership lifecycle.

### D7 — Central `Service.Create` with fan-out

`notifications.Service.Create(ctx, CreateInput)` resolves scope/taxonomy/preferences, inserts via `Repository.Create`, coalesces on `group_key`, and emits `EntityNotification` through the injected `events.Service`. Migrate `agents/ask_user_tool.go` and `provider/usage_service.go` onto it.

- **Circular-dependency note:** `provider` avoided importing `notifications` on purpose. Resolve by keeping the taxonomy/preferences types in a leaf package if needed, or by having `provider` publish a domain event that a notifications consumer maps — decide during implementation; do not introduce an import cycle.

### D8 — Real-time is a poke, storage is durable

`Create` emits an event on the existing in-memory bus; the console subscribes via a gateway-proxied SSE stream (browser `EventSource` cannot send `Authorization`). Because the bus is non-durable, the client reconciles counts from `GET /api/notifications/counts` on load/project switch/reconnect, so a missed event never corrupts the badge.

### D9 — Web UI follows existing shell patterns

Bell slots into `layout.Navbar` in `gateway/ui.templ` immediately before `@userAccountMenu`, using `ui.Indicator`/`ui.IndicatorBadge` for the dot/count, `data-testid="notifications-bell"`, linking to `/inbox`. The inbox page is a new templ page + handler + route using `layout.Container` + `pageHeader` + `ui.EmptyState` + `components.ListRow`, with a scope switch (Account | Project) and tabs (All / Unread / Action required / Snoozed / Cleared).

## Risks / Trade-offs

- **[Scope backfill on a live table]** Existing rows default to `account`; if a past producer intended project scope, it will be mis-scoped → Mitigation: the only current producers are agent questions and budget alerts (both account-appropriate); verify no other writer before migrating.
- **[Non-durable event bus]** Badge can miss events across reconnect → Mitigation: reconcile counts on load/switch/reconnect; acceptable until a durable outbox exists.
- **[Import cycle]** `provider` → `notifications` was avoided deliberately → Mitigation: leaf types or event-based hand-off (D7); check with `go build` immediately.
- **[No org column]** Account scope covers org events without `organization_id`, but a future org-filtered view needs one → Mitigation: non-goal for now; additive later.
- **[Tab counts vs badge counts]** Adding scope to an existing tabbed endpoint can desync counts → Mitigation: counts endpoint takes the same scope filter; unit-test both.
- **[Two overlapping changes]** `add-notifications-tasks` also specifications notifications → Mitigation: this change supersedes its notifications portion; close/archive it once this lands.

## Migration Plan

1. Additive migration: `scope`, `requires_action`, `event_key` on `kb.notifications`; create `kb.notification_preferences`; backfill `scope='account'`. Reversible (drop columns/table).
2. Add `Service.Create` + preferences types/endpoints; wire `events.Service` into the notifications module.
3. Migrate existing producers; add new producers (membership, permissions, invites, tasks).
4. Add the UI behind existing session middleware (bell hidden for anonymous sessions; nav entry follows the existing role-gating convention).
5. Rollback: remove nav entry + routes + the new columns; existing list/read/dismiss behavior is unaffected.

## Open Questions

- Does any producer other than `agents/ask_user_tool.go` and `provider/usage_service.go` write `kb.notifications` today? Confirm exhaustively before the backfill.
- Final home for the taxonomy types to avoid the `provider` import cycle (leaf package vs event consumer).
- Whether `mark-all-read` should be additive (current) or scope-aware in the UI (client-side scoping vs server param).
- Exact event keys where the taxonomy table is illustrative (approval/mention sources may not all exist yet).
