## Why

The account inbox lists notifications across every project the user belongs
to, but each row shows only *when · category · source* — nothing says which
project an event originated from. A membership change for "Acme" and one for
"Globex" are indistinguishable at a glance. There is also no way to narrow the
account inbox to one project, so triaging a busy account inbox means scanning
every project's noise. Meanwhile the sidebar exposes an "Inbox" link under a
project context (odd, since the account inbox is cross-project) while the
topbar bell — the actual entry point — uses a bell glyph that no longer
matches the inbox concept.

## What Changes

- Account-inbox rows attribute their originating project with a small chip
  (folder icon + project name) whenever the notification carries a
  `projectId` and the name resolves.
- The Account inbox gains a **project filter** ("All projects" + the caller's
  projects) that narrows the list and counts. The Project scope keeps its
  single-project narrowing and does **not** show the filter (or repeat the
  project label — the scope switch already names it).
- The sidebar "Inbox" entry is removed (the inbox is reached from the topbar).
- The topbar entry's bell glyph is swapped for an inbox glyph; its anchors
  (`data-notifications-bell`, `data-notifications-badge`,
  `data-testid="notifications-bell"`) are unchanged.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `web-inbox`: account-inbox row attribution, the account project filter, the
  removed sidebar entry, and the inbox topbar glyph.

## Impact

- `apps/web-ui/gateway/inbox.go` — `inboxView` gains `ProjectNames`,
  `ProjectFilter`, `ProjectOptions`; `uiInbox` fetches projects and validates
  `?project=` (fail closed); `inboxProjectFilter` passes the explicit account
  filter through.
- `apps/web-ui/gateway/inbox_view.go` — `notificationProjectLabel` pure helper.
- `apps/web-ui/gateway/inbox.templ` — thread the view through the row, render
  the chip, and render the account-scope project filter.
- `apps/web-ui/gateway/ui.go` — drop the sidebar Inbox item.
- `apps/web-ui/gateway/notifications_bell.templ` — inbox glyph.
- `apps/web-ui/gateway/webui/static/js/inbox.js` — `refreshList()` preserves
  the `project` param.
- Tests: `inbox_ui_test.go` (sidebar, chip, filter), `inbox_view_test.go`
  (label helper).
