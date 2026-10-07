## 1. Landing route splits failure from zero-org (`web-ui-degraded-backend-state`)

- [ ] 1.1 Add `RootUnavailablePage(err error)` to `ui.templ`: a shell `layout.Container` wrapping the shared `pageError` with a "Your data is safe — Memory is temporarily unavailable and will retry automatically." reassurance line.
- [ ] 1.2 In `uiRoot` (`org_context.go`), when `ListOrgs` returns an error, render `RootUnavailablePage(err)` via `s.page`; keep the `/o/<id>` redirect for `err == nil && len(orgs) > 0` and the `/orgs/new` redirect only for `err == nil && len(orgs) == 0`.
- [ ] 1.3 Unit test (TDD): a backend `ListOrgs` failure (e.g. `memoryHTTPError{Status:503, Code:"service_unavailable"}`) renders a 200 with "Service temporarily unavailable" and "Your data is safe", and does **not** redirect to `/orgs/new` nor render the onboarding copy; `err == nil, len == 0` yields `/orgs/new`; `err == nil, len > 0` yields `/o/<id>`.

## 2. Org landing surfaces a projects-fetch failure (`web-ui-degraded-backend-state`)

- [ ] 2.1 In `uiOrg` (`org_context.go`), stop discarding the `ListProjectsIncludingPending` error: on error, render `OrgLandingPage(orgPageData{Org: org, LoadErr: err})` so the page shows `pageError("Organization unavailable", …)`.
- [ ] 2.2 Unit test (TDD): a `ListProjectsIncludingPending` failure renders "Organization unavailable" and does **not** render "Create project" / "Create the first project in this organization.".

## 3. Default-project middleware splits failure from no-projects (`web-ui-degraded-backend-state`)

- [ ] 3.1 In `resolveDefaultProject` (`auth.go`), when `ListProjects` returns an error, render `RootUnavailablePage(err)` via `s.page` instead of redirecting to `/orgs`; keep the `/orgs` redirect only for `len(projects) == 0`.
- [ ] 3.2 Unit test (TDD): a `ListProjects` failure renders "Service temporarily unavailable" (200, no redirect); the existing no-projects case still redirects to `/orgs`.

## 4. Verification

- [ ] 4.1 `templ generate ./...`, `go generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...` from `apps/web-ui/gateway`.
