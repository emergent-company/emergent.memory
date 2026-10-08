## 1. Redact the single-org load failure (`web-ui-degraded-backend-state`)

- [ ] 1.1 Add `userFacingError(err error) error` to `memory.go`, wrapping `userFacingMemoryError` for `pageError` surfaces.
- [ ] 1.2 In `uiOrg` (`org_context.go`), wrap the `resolveOrg`/`GetOrg` failure in `userFacingError` so the "Organization unavailable" page never renders the raw backend error (#1654).
- [ ] 1.3 Redact the sibling org-tenancy unavailable renders in `org_context.go`: the org sub-page `resolveOrg` failures plus the `ListOrgMembers`/`ListOrgToolSettings` failures.
- [ ] 1.4 Unit test (TDD): `TestUIOrgOrgLoadFailureRedacted` asserts the org-load failure page shows the generic message and never leaks host/IP/port/`dial tcp`/`service_unavailable`/`memory 5xx`; it fails if the redaction is bypassed.

## 2. Verification

- [ ] 2.1 `go build ./...`, `go vet ./...`, `go test ./...` from `apps/web-ui/gateway`.
- [ ] 2.2 `openspec validate fix-uiorg-error-redact --strict`.
