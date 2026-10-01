## Why

The web console has no unified place to see what happened and what needs the user's attention. Notifications exist server-side (`kb.notifications`, `apps/server/domain/notifications`) but there is no producer API, no account-vs-project separation, no opt-in controls, and no UI. Users miss membership changes, permission changes, invitations, approvals, and project activity.

This change designs and adds an **Inbox subsystem** — a ClickUp-style inbox that is the single surface for both global account events and per-project activity, with a bell + unread indicator in the console.

## What Changes

- Add an explicit **scope** to notifications (`account` | `project`) and expose **two inboxes**: Account (global, mandatory) and Project (per-project activity, opt-in).
- Add a documented **event taxonomy** with stable event keys and per-event default delivery.
- Add **notification preferences** (per user, per project, per event key, per channel) driving opt-in delivery for project events, while account/security/action-required events stay mandatory.
- Add a central **producer API** (`notifications.Service.Create`) and migrate the two ad-hoc inserts onto it, so scope, taxonomy, preferences, and grouping are applied consistently.
- Add **actionable notifications** (`requires_action` + action state) for invitations, approvals, mentions, and agent questions, with inline resolve.
- Add **real-time** delivery of new notifications and unread badge updates.
- Add the **web UI**: a bell with an unread dot/count in the console header linking to the inbox, and an inbox page with an Account | Project scope switch, tabs, unread styling, and inline actions.

## Capabilities

### New Capabilities
- `notifications`: per-user inbox with account/project scopes, event taxonomy, opt-in preferences, actionable items, a producer API, and real-time delivery.
- `web-inbox`: the console bell + unread indicator and the inbox page (scope switch, tabs, unread marking, inline actions).

### Modified Capabilities
<!-- none -->

## Impact

- `apps/server/domain/notifications/`: entity fields, repository, service `Create`, routes (scope/preferences), SSE emit; fx module wiring.
- `apps/server/migrations/`: `kb.notifications` (`scope`, `requires_action`, `event_key`) + new `kb.notification_preferences`.
- Producer domains: `projects`, `orgs`/`useraccess`/`invites`, `tasks`, and migration of `agents/ask_user_tool.go` + `provider/usage_service.go` onto the central create path.
- `apps/web-ui/gateway/`: bell in `ui.templ` navbar, new inbox page + handler + route, Memory client methods, SSE proxy.
- Supersedes the notifications portion of `openspec/changes/add-notifications-tasks/` (that change stays for the background-task monitor; close/archive it once this lands).
