# Tasks — harden-mcp-registry-resilience

## 1. Upstream timeout

- [x] 1.1 Bump `defaultRegistryTimeout` from 15s to 45s with a comment
  explaining why (slow/degraded upstream; stays under the gateway's 60s client
  timeout; retries disabled separately).

## 2. In-memory browse cache

- [x] 2.1 Add `cache.go` in `apps/server/domain/mcpregistry`: a thread-safe,
  short-TTL cache keyed by full request identity, caching successful results
  only, with an injectable clock (`now func() time.Time`) and TTL for
  deterministic tests.
- [x] 2.2 Wire the cache into `Service.SearchRegistry` (search key:
  query+limit+cursor) and `Service.GetRegistryServer` (lookup key: name).

## 3. Tests (TDD)

- [x] 3.1 Cache hit avoids a second upstream call (search + lookup).
- [x] 3.2 Cache miss calls upstream; distinct request identities produce
  distinct cache entries.
- [x] 3.3 TTL expiry re-fetches (injectable clock).
- [x] 3.4 Errors are never cached (transient upstream failure is not sticky).
- [x] 3.5 Search vs lookup keys are distinct.

## 4. Specs + verify gate

- [x] 4.1 `openspec validate harden-mcp-registry-resilience` passes.
- [x] 4.2 `apps/server`: `go build ./... && go test ./domain/mcpregistry/...`.
