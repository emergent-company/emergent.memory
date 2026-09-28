## Purpose

Defines an intra-run memoisation of read-only MCP tool results so an agent run that repeats an identical search/getter executes the underlying query once, without ever serving stale, cross-tenant, or error/timeout-shaped data.

## ADDED Requirements

### Requirement: Cache scope is exactly one agent run

A tool-result cache SHALL be created fresh for each agent run and carried only by that run's context. It SHALL NOT be stored as package-global state, on a long-lived service, or in any cross-request store, and it SHALL NOT be inherited by a nested/child run.

#### Scenario: Two runs never share cached reads
- **WHEN** two agent runs (or a run and its child run) each call the same read-only tool with identical arguments and project
- **THEN** each run SHALL execute the underlying query at least once
- **THEN** no run SHALL observe a result cached by a different run

#### Scenario: Non-run caller is unaffected
- **WHEN** an MCP tool is invoked through an HTTP transport (no run context)
- **THEN** the call SHALL be dispatched exactly as before and no result SHALL be cached

### Requirement: Only explicitly read-only tools are cacheable

Only tool names on an explicit read-only allowlist SHALL be eligible for caching. The allowlist SHALL contain only direct reads with no write side effect and no nested agent run. Tools not on the allowlist SHALL be executed as today.

#### Scenario: Identical read-only call executes once
- **WHEN** the same read-only tool is called twice in one run with byte-identical canonical arguments for the same project
- **THEN** the underlying query SHALL execute once
- **THEN** the second call SHALL return the first call's result

#### Scenario: Different arguments miss
- **WHEN** the same read-only tool is called with different arguments in one run
- **THEN** each distinct canonical argument set SHALL execute its own underlying query

#### Scenario: Different project misses
- **WHEN** the same read-only tool with identical arguments is called for two different projects within one cache
- **THEN** each project SHALL execute its own query and SHALL NOT receive the other project's result

#### Scenario: Mutating tool is never served from cache
- **WHEN** a mutating tool (e.g. `entity-create`) is called
- **THEN** it SHALL always execute and SHALL never return a cached result

### Requirement: A mutation invalidates the cache for the rest of the run

Before dispatching any tool that is not on the read-only allowlist, the run's cache SHALL be cleared. A later read-only call SHALL therefore re-execute rather than serve a result computed before the mutation.

#### Scenario: Read after write re-executes
- **WHEN** a run calls a cacheable read, then a mutating tool, then the same read again
- **THEN** the read after the mutation SHALL execute the underlying query again
- **THEN** it SHALL NOT return the pre-mutation cached result

#### Scenario: Failed mutation still invalidates
- **WHEN** a non-allowlisted tool is dispatched and fails
- **THEN** the cache SHALL still have been cleared before dispatch

### Requirement: Errors, cancellation, and incomplete results are never cached

A result SHALL be cached only when it carries no error, the run context is still live, and the payload does not mark itself incomplete. In particular, a result produced while the run context was cancelled/expired, a result with `isError`/`ok:false`, and a result with `truncated:true` SHALL NOT be cached, so a recoverable retry cannot be masked.

#### Scenario: Timeout-shaped success is not cached
- **WHEN** a tool returns a success-shaped but truncated/empty payload (the #1187 timeout shape)
- **THEN** the payload SHALL NOT be stored
- **THEN** a subsequent identical call SHALL re-execute the underlying query

#### Scenario: Error result is not cached
- **WHEN** a read-only tool returns a Go error or an error-shaped result
- **THEN** nothing SHALL be stored for that call

### Requirement: Cache memory is bounded

The cache SHALL be bounded by both a maximum number of entries and a maximum total byte size, evicting oldest entries first. A single result larger than the byte bound SHALL NOT be stored.

#### Scenario: Bounds hold under many distinct reads
- **WHEN** a run issues more distinct read-only calls than the entry bound
- **THEN** the number of stored entries SHALL NOT exceed the bound and the cache SHALL remain usable

### Requirement: Authorization is enforced on every call and misses are semantically unchanged

Authority checks (share-instance allowlist, superadmin resolution, tool authority) SHALL run on every call, including cache hits. On a cache miss the tool SHALL execute exactly as before, and the returned result SHALL be indistinguishable from an uncached call.

#### Scenario: Cache hit does not skip authorization
- **WHEN** a caller lacking authority attempts a tool
- **THEN** the authority check SHALL still refuse the call before any cache lookup
