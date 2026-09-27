## 1. Server: exclude projection

- [ ] 1.1 Add `ExcludeFields []string` to `ListParams` in `domain/graph/repository.go`.
- [ ] 1.2 Parse `exclude_fields` (comma-separated) in `Handler.ListObjects` and set `params.ExcludeFields`.
- [ ] 1.3 In the list service, apply projection when `Fields` or `ExcludeFields` is set, passing both to `GraphExpandProjection`.
- [ ] 1.4 Unit/integration coverage for: exclude drops the key; include+exclude compose; no parameter is unchanged.

## 2. Gateway: drop the document body on the list

- [ ] 2.1 `MemoryClient.ListGraphObjects` sends `exclude_fields=content`.
- [ ] 2.2 Confirm an object list page response no longer contains `properties.content`.

## 3. Verification

- [ ] 3.1 `go build ./...`, `go vet`, `go test ./domain/graph/...` (server).
- [ ] 3.2 `go build ./...` (gateway) and `task lint` where available.
- [ ] 3.3 Measure `?exclude_fields=content` on dev for `type=EUDirective` and confirm the multi-MB payload is gone.
