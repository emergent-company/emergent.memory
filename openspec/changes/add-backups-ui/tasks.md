## 1. MemoryClient backup methods (TDD)

- [ ] 1.1 Add `Backup` struct and `ListBackups`/`GetBackup`/`CreateBackup`/`DownloadBackup`/`DeleteBackup` methods wrapping `GET /api/v1/organizations/:orgId/backups`, `POST /api/v1/projects/:projectId/backups`, `GET .../:backupId`, `GET .../:backupId/download`, `DELETE .../:backupId`, reusing `do`/`doH`. Verify: `go build ./...` in gateway compiles.
- [ ] 1.2 Resolve `orgId` from the active project (existing current-project lookup) for the org-scoped routes, and treat a 404/not-found on list as "backups unavailable" rather than a hard error. Verify: `go build ./...` compiles.
- [ ] 1.3 Write `httptest` unit tests covering list success, empty list, create 202, get success, download ready/not-ready, delete 204, the 404-degrades-to-unavailable path, and a non-404 error path. Verify: `go test ./...` in gateway passes.

## 2. Backups page handler and routes

- [ ] 2.1 Add `uiBackups` handler, `GET /backups` route, and a Backups sidebar item in `sidebarGroups()`; render the list with best-effort load plus an error state when Memory is unreachable or the feature is gated. Verify: `go build ./...` compiles and `/backups` renders in the browser.
- [ ] 2.2 Add PRG action handlers and routes for `POST /backups` (create), `GET /backups/:id/download` (redirect to presigned URL), and `POST /backups/:id/delete` (confirm + delete), mirroring `uiUploadDocument`/`uiDeleteSkill` (flash toast, redirect). Verify: `go build ./...` compiles.
- [ ] 2.3 Write handler unit tests for create, download (ready + not-ready), delete, and the unreachable/gated-memory error state. Verify: `go test ./...` passes.

## 3. Backups page template

- [ ] 3.1 Create `backups.templ` with the list (status/progress/size/checksums badges), the create form (`includeDeleted`, `includeChat`, `retentionDays`), per-row download/delete actions, and "no backups"/"unavailable"/error states. Verify: `templ generate` succeeds and the page renders.
- [ ] 3.2 Write a `.templ` render test (mirroring `agent_ui_test.go`) asserting the list, the "no backups" empty state, the unavailable state, and the create form. Verify: `go test ./...` passes.

## 4. Build, lint, and manual verification

- [ ] 4.1 Run `templ generate`, `go build ./...`, and `task lint`; fix any issues until clean. Verify: all three commands succeed.
- [ ] 4.2 Manual browser test via DevTools: navigate to `/backups`, create a backup, watch it poll to `ready`, download it, delete it, confirm the empty state, and confirm the unavailable state when Memory has backups disabled. Verify: each flow behaves per spec. (Headless smoke test: `/backups` returns HTTP 200; interactive DevTools pass deferred to the user's local browser.)
