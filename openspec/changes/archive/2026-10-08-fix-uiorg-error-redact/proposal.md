## Why

Follow-up to #1645. That change redacted the tenancy-load surfaces through `userFacingMemoryError`, but
scoped only the `ListOrgs`, `ListProjects`, and `ListProjectsIncludingPending` call sites. The single-org
load (`GetOrg`, via `resolveOrg`) still passes the raw backend error to `pageError`, so a memory outage on
`GET /orgs/:id` leaks internal detail — transport strings such as
`dial tcp 172.17.0.1:8080: connect: connection refused` and the `memory 503 service_unavailable:` prefix —
straight into the "Organization unavailable" page.

The capability is introduced by the still-open change `web-ui-unavailable-not-empty` as
`web-ui-degraded-backend-state` (the issue's shorthand "`web-ui-unavailable-not-empty` capability" is that
change's directory name, not the capability name; no `openspec/specs/web-ui-unavailable-not-empty` spec
exists). `openspec validate --strict` passes; because that introducing change is not yet archived, the
MODIFIED delta carries an INFO that archive must run after `web-ui-unavailable-not-empty` is archived (it
is merged — #1645 — and remains an archive follow-up).

## What Changes

- Redact the `uiOrg` org-load failure (`resolveOrg`/`GetOrg`) with `userFacingError`, the error-typed
  wrapper over `userFacingMemoryError`.
- Audit the sibling tenancy "unavailable" renders in `org_context.go` (grep `pageError(`) and redact the
  remaining raw backend errors: the org sub-pages' `resolveOrg` failures (members, invite, settings,
  settings/general, settings/danger-zone) and the org-scoped `ListOrgMembers` / `ListOrgToolSettings`
  failures.
- Add `userFacingError(err error) error` to `memory.go` (wraps `errors.New(userFacingMemoryError(err))`)
  so the sanitizer is applied as an `error` at each `pageError` surface.
- Add a gateway regression test (`TestUIOrgOrgLoadFailureRedacted`) proving the org-load failure page
  carries the generic message and none of the internal detail; it fails if the redaction is bypassed.

Out of scope, noted rather than silently widened: non-tenancy single-resource pages (`agent.go`,
`documents.go`, `runs.go`, `objects.go`, `schema.go`, `skills.go`, `api_tokens_handlers.go`, the
`mcp_*` handlers, `project_settings`) also pass raw backend errors to `pageError`, but the
`web-ui-degraded-backend-state` capability is scoped to org/project tenancy loads.

## Capabilities

### Modified Capabilities

- `web-ui-degraded-backend-state` — extend "Unavailable surfaces never expose internal error detail" to
  the single-org (`GetOrg`) load behind `GET /orgs/:id` and its org sub-pages.

## Impact

- `apps/web-ui/gateway/org_context.go` — single-org (`GetOrg`) load plus the org sub-page and
  org-scoped list unavailable renders.
- `apps/web-ui/gateway/org_members_ui.go` — the org list (`/orgs`) access-tree failure.
- `apps/web-ui/gateway/settings_handlers.go` — the project settings / budget pages' `GetCurrentProject`
  failure.
- `apps/web-ui/gateway/memory.go` — the `userFacingError` wrapper.
- `apps/web-ui/gateway/org_context_test.go`, `apps/web-ui/gateway/handlers_test.go` — regression tests and
  the fake's injectable access-tree error.
