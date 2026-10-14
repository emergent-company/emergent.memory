## Why

The org- and project-scoped "Data source OAuth clients" pages (added in
`add-datasource-oauth-app-setup`) are reachable only through the context-aware "no OAuth client
configured" alert on a failed connect. There is no way to discover them proactively: the project and
org settings rails have no "Data source OAuth clients" entry, and the Data Sources page offers no
shortcut to where a project's Google app is configured. A team that wants to set up its OAuth app
*before* its first failed connect has to know the exact URL.

## What Changes

- **Project settings rail**: add a "Data source OAuth clients" entry (scoped `href
  /settings/oauth-clients`) between "Connected devices" and "Budget". The entry is shown to
  `project_admin` only and hidden (not disabled) for everyone else.
- **Org settings rail**: add a "Data source OAuth clients" entry (`/orgs/:id/settings/oauth-clients`)
  after "Tools", gated on `org_admin`.
- **OAuth client pages in-rail**: both the project and org "Data source OAuth clients" pages render
  inside their settings rail with the OAuth-clients entry marked active, so the page is visually
  anchored in the same settings hub as every other settings screen.
- **Data Sources page**: add an "OAuth clients" shortcut (outline button) in the page header, shown
  to `project_admin` only, linking to `/settings/oauth-clients`.

Non-admin reachability is unchanged: the OAuth-clients endpoints remain `project_admin` /
`org_admin`-gated and still 404 for non-admins — the rail entries are hidden, not a route change.

## Capabilities

### Modified Capabilities

- `web-data-sources`: discoverable OAuth-client settings navigation (project + org rails), in-rail
  rendering of the OAuth-client pages, and the Data Sources page shortcut.

## Impact

- `apps/web-ui/gateway/**` (`settings_handlers.go`, `project_settings.templ`, `org_context.templ`,
  `org_members_ui.{go,templ}`, `datasources_oauth.templ`, `datasources.templ`, `datasources_ui.go`)
  plus gateway render tests.
- `docs/site/user-guide/datasources.md` — document the new discoverability paths.
- No server, API, SDK, or schema changes.
