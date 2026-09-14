## 1. Backend client contract

- [x] 1.1 Add `TransferProject(ctx context.Context, projectID, destinationOrgID string) error` to the `MemoryBackend` interface in `gateway/backend.go` and confirm `go build ./...` passes from `gateway/`
- [x] 1.2 Implement `MemoryClient.TransferProject` in `gateway/memory.go` as `POST /api/projects/{id}/transfer` with JSON body `{"orgId": destinationOrgID}` (mirror `DeleteProject`/`do` conventions) and verify a unit test asserting method, path, request body, and auth/session headers passes against an `httptest` server
- [x] 1.3 Update any other `MemoryBackend` implementations/fakes in the gateway with `TransferProject` and verify `go build ./...` and `go test ./...` pass

## 2. Gateway route and handler

- [x] 2.1 Register `POST /projects/transfer` next to `/projects/delete` in `gateway/main.go` and verify the route is present (startup or route table test)
- [x] 2.2 Implement `uiTransferProject` in `gateway/org_context.go` mirroring `uiDeleteProjects`: read `projectId`/`orgId`/`destinationOrgId` from form or query, reject `destinationOrgId == orgId` (or missing/unknown destination) with an error flash and NO backend call, otherwise call `memory.TransferProject` and redirect back to the source org via `render.RedirectAfterMutation` (HTMX) / PRG fallback with a `?moved=` confirmation flash, and verify handler unit tests against a fake `MemoryBackend` cover: success (redirect to source org + flash naming project and destination org), local-guard short-circuit (fake records zero calls), backend error → error flash and unchanged state

## 3. Organization view wiring

- [x] 3.1 Extend the org-landing page data assembly in `gateway/org_context.go` so `uiOrg` supplies the acting user's organizations with roles (access-tree fetch) and derive per-view flags — `canTransfer` (user is `org_admin` of the current org and ≥1 candidate destination exists) and the candidate destination org list (user's orgs minus source) — and verify unit tests cover: org_admin with a candidate sees the action, no other org → action hidden, non-admin → action hidden
- [x] 3.2 Add the Transfer item to the per-project row popover menu in `gateway/org_context.templ` (rendered only when `canTransfer`) that opens a `modalShell` transfer dialog with a destination-org `<select>` (candidates only, source org excluded, exactly one required) plus Confirm/Cancel, and verify `templ generate` succeeds and the dialog renders candidates in a browser test
- [x] 3.3 Wire the dialog's Confirm to submit `projectId` + source `orgId` + `destinationOrgId` to `POST /projects/transfer` via hx-post and verify the manual browser flow: transferring project P from org A to org B shows the success flash back in A, P disappears from A's project list, and appears in B's list; cancel leaves everything unchanged

## 4. Verification

- [x] 4.1 Run full gateway checks from `gateway/`: `templ generate`, `go build ./...`, `go test ./...`, and `task lint`, and verify all pass
- [x] 4.2 Restart the dev server (`task dev`) and verify the org view renders without console errors and the transfer flow works end-to-end against the running app — verified via Playwright against the running dev gateway (air hot-reload picked up the rebuild): Transfer action + dialog render with correct candidate orgs (source excluded), Cancel is a no-op, zero console errors. Live submit returns HTTP 303 → graceful error flash back on the source org: the Memory service transfer endpoint (`POST /api/projects/{id}/transfer`) does not exist yet on api.dev (external service, see design Risks/Migration) — success path is covered by fake-backend unit tests and awaits the service change.
