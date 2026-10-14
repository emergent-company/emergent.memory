## 1. Validator

- [x] 1.1 Validate relationship `sourceType`/`targetType` endpoints against the union of all loaded packs' object types (`buildKnownObjectTypes(packs)`), not a single pack. Verify: `go test ./internal/blueprints/...`.
- [x] 1.2 Keep a hard error when an endpoint matches no pack in the blueprint. Verify: the "unknown target still fails red" case in `validator_test.go`.
- [x] 1.3 Install preflight additionally accepts endpoints owned by the project (installed packs + built-ins), gated by `HasUnknownRelationshipEndpoints`/`PreflightWithProjectTypes` so the compiled-types read happens only when an endpoint is unresolved. Verify: `TestInstallPreflight_ProjectOwnedEndpoint`, `TestHasUnknownRelationshipEndpoints`.
- [x] 1.4 Preserve the #1586 guarantee: a blueprint whose endpoints resolve locally (including a malformed-model blueprint) still aborts with zero requests, and every abort remains zero-mutation. Verify: `TestRunBlueprintsInstall_BadModelAbortsBeforeAnyApply`.

## 2. Docs

- [x] 2.1 Update the `memory blueprints validate` help text and both embedded CLI-reference copies (`.agents/skills/` and `apps/cli/internal/skillsfs/skills/`), noting the offline validate is blueprint-only.

## 3. Verification

- [x] 3.1 `go build ./... && go vet ./...` and `go test ./internal/blueprints/... ./internal/cmd/...` in `apps/cli`.
- [x] 3.2 `gofmt -l` clean on changed `.go` files.
- [x] 3.3 `TestRunBlueprintsInstall_ConsultsProjectTypesForUnknownEndpoint` proves the read happens for an unresolved endpoint and zero mutations occur on abort.
