## Why

When `memory-server` is unavailable (a 5xx response, a client-side timeout, or an auth `503
service_unavailable`), the gateway web UI falls through to the **empty onboarding** state — "create your
first org/project" — which is indistinguishable from genuine data loss. A user whose data is perfectly
safe sees the same screen they would see if every org and project had been wiped.

Three routes conflate the two cases:

- `uiRoot` (`GET /`, `org_context.go`) treats a `ListOrgs` error the same as a zero-org success and
  redirects to `/orgs/new`.
- `uiOrg` (`GET /orgs/:id`, `org_context.go`) discards the `ListProjectsIncludingPending` error
  (`captureError`), so a failed projects fetch renders "No projects yet / Create project".
- `resolveDefaultProject` (`auth.go`) treats a `ListProjects` error as "no projects" and redirects to
  `/orgs`.

The backend already emits the failure signal that matters: `503 service_unavailable` ("Token validation
temporarily unavailable"), plus retryable 502/503/504 statuses and transport timeouts
(`memory.go` `memoryHTTPError`, `retryableMemoryStatus`, `isTimeout`).

## What Changes

Split each dispatch so a backend failure surfaces the existing `pageError` unavailable state, while only a
**successful** zero-result response drives the onboarding/create flow:

1. **`uiRoot`** — a `ListOrgs` error renders the page shell's unavailable state (a new
   `RootUnavailablePage` component: "Service temporarily unavailable" + "Your data is safe — Memory is
   temporarily unavailable and will retry automatically."). Only `err == nil && len(orgs) == 0` redirects
   to `/orgs/new`; `err == nil && len(orgs) > 0` still auto-selects the first org.
2. **`uiOrg`** — a `ListProjectsIncludingPending` error sets `orgPageData.LoadErr` so `OrgLandingPage`
   renders `pageError("Organization unavailable", …)` instead of "No projects yet / Create project".
3. **`resolveDefaultProject`** — a `ListProjects` error renders the same unavailable state instead of
   redirecting to `/orgs`; only `len(projects) == 0` redirects.

All three reuse the existing degraded-state precedent (`uiOrgs` sets `data.LoadErr`,
`OrgsPage`/`OrgLandingPage` render `@pageError`), so no new component-convention work is introduced beyond
the one full-page shell for the landing route.

## Capabilities

### New Capabilities

- `web-ui-degraded-backend-state` — the gateway must render an explicit unavailable state on backend
  failure/timeout/`503`, and only render the empty/onboarding state on a successful authenticated
  zero-result response.

## Impact

- **Web UI gateway only.** `org_context.go`, `auth.go`, `ui.templ`, plus unit tests in
  `org_context_test.go` and `account_test.go` and the `fakeMemory` test double in `handlers_test.go`.
- **Verification gate:** `templ generate ./...`, `go generate ./...`, `go build ./...`, `go vet ./...`,
  `go test ./...` from `apps/web-ui/gateway`.
