## 1. Server DTOs

- [x] 1.1 Add `BulkUpsertObjectsRequest`/`BulkUpsertObjectsResponse`/`BulkUpsertObjectResult` DTOs to `apps/server/domain/graph/dto.go`, with a `Created bool` on the result and a `validate:"required,min=1,max=100"` items cap. Verify: unit test asserts the cap (`TestBulkUpsertMaxItems_Constant`) and the response shape (`success`/`failed`/`results` with `index`/`success`/`created`/`object`/`error`).
- [x] 1.2 Add `BulkUpsertRelationshipsRequest`/`BulkUpsertRelationshipsResponse`/`BulkUpsertRelationshipResult` DTOs with a `Created bool` on the result and the same 100-item cap. Verify: unit tests assert the cap and per-item `created`/`relationship` shape.

## 2. Repository

- [x] 2.1 Add `ListParams.Keys []string` and a `key IN (...)` clause to `buildObjectBaseQuery` alongside the existing single `Key` filter. Verify: DB test asserts `List` returns heads for those keys only, and single `Key` is unaffected.
- [x] 2.2 Add `FindHeadsByTypeAndKeys` returning the HEAD versions for a set of `(type, key)` pairs in one query, `nil` for absent keys (not an error). Verify: DB test asserts multi-key resolution, HEAD-only/type/project scoping, and that absent keys are omitted. (Note: currently exercised only by its test — the service bulk path upserts per item via `CreateOrUpdate`.)

## 3. Server service + handler + routes

- [x] 3.1 Add `Service.BulkUpsertObjects`: for each item create-or-update by `(type, key)` via the existing `CreateOrUpdate`, set `created` accordingly, and isolate per-item errors. Verify: service DB test asserts create-vs-update, the `created` flag, the result `index`, and one-bad-item isolation.
- [x] 3.2 Add `Service.BulkUpsertRelationships` with idempotent `(type, src, dst)` upsert and per-item isolation. Verify: service DB test asserts idempotency, the `created` flag, result `index`, and isolation.
- [x] 3.3 Add `Handler.BulkUpsertObjects`/`Handler.BulkUpsertRelationships`, parse the request body, and return the bulk response. Verify: binding enforced by the DTO `validate` tags; per-item missing-`key` handling asserted in the service DB test.
- [x] 3.4 Register `POST /objects/bulk-upsert` and `POST /relationships/bulk-upsert` in `routes.go` and declare them in `route-authority.yaml`. Verify: `go run ./cmd/route-authority-guard` reports all routes declared with matching tiers.
- [x] 3.5 Parse the new `keys` query param in `ListObjects` into `ListParams.Keys`. Verify: SDK `TestGraphListObjectsKeys` pins `keys=a,b` serialization and repository `TestListMultiKeySearchIntegration` asserts the `key IN (...)` filter; single `?key=a` is unchanged.

## 4. SDK

- [x] 4.1 Confirm `BulkUpsertObjects`/`BulkUpsertRelationships` SDK methods and request/response types exist and hit the new endpoints; align field names with the server DTOs. Verify: SDK client test asserts the correct URL/body for each.
- [x] 4.2 Confirm `ListObjectsOptions.Keys` emits a `keys` query param; add a test if missing. Verify: SDK client test asserts `keys=a,b,c` is serialized.

## 5. CLI seeder refactor

- [x] 5.1 Refactor `Seeder.applyObjects` to resolve all non-empty keys in a batch via one `ListObjects` call with `Keys` (falling back to exact single-key lookups for keys that cannot round-trip the comma-joined param), then insert new objects via `BulkCreateObjects` and update existing ones via `BulkUpsertObjects` under `--upgrade`, preserving the existing created/updated/skipped/failed counters and dry-run output. Verify: seeder test asserts the batched lookup, the bulk create/upsert split, and correct counters.
- [x] 5.2 Refactor `Seeder.applyRelationships` to use `BulkUpsertRelationships`, preserving per-item error reporting and `RelsCreated`/`RelsFailed` counters. Verify: seeder test asserts idempotent bulk-upsert and correct counters.
- [x] 5.3 Forward `assignee` and `status` on seed objects through the bulk-upsert item shape. Verify: seeder test asserts both fields are sent and round-trip.

## 6. Verification

- [x] 6.1 Run `go build ./...` from `apps/server/` and `apps/cli/`; verify both succeed.
- [x] 6.2 Run the graph DB test suites for the new repository/service paths; verify all pass.
- [x] 6.3 Run SDK client tests (`apps/server/pkg/sdk/graph`); verify all pass.
- [x] 6.4 Run CLI seeder tests (`apps/cli/internal/blueprints`); verify all pass.
- [x] 6.5 Run `PATH="/root/go/bin:$PATH" task lint` (or the lint-ratchet); verify no new issues.
- [x] 6.6 Run `openspec validate graph-bulk-upsert --strict`; verify it passes. (`openspec validate --specs --strict` still reports 4 pre-existing, unrelated spec failures on `main` — `cli-object-provenance`, `object-provenance`, `ios-agent-object-browser`, `web-chat-header`.)
