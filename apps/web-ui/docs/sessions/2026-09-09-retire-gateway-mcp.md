# 2026-09-09 — Retire MCP from the gateway memory client

## Goal

Continuation of the 2026-09-08 web-UI performance session. The user asked whether the
blueprints page's slow schema call was MCP-gated and whether a naked REST API should exist
("Are we missing a naked API for the blueprints that would allow it to bypass the MCP?"),
then: "create a pull request to the memory repository…", and finally to handle the
remaining backlog items.

## Outcome

Done. MCP is now **entirely retired from the Go gateway's memory client** — the gateway
talks REST only; MCP remains agent-internal on the memory backend.

- Memory REST schema-catalog endpoint added via **PR emergent-company/emergent.memory #389**
  (merged, deployed, verified live).
- Gateway `ListAllSchemas` → REST (`a61e693`).
- Gateway `ListMemories` → REST + removed ~320 lines of dead MCP client machinery (`7dcc31e`).
- Closed the three 2026-09-08 session follow-ups (title-sync, multipart upload, godaisy-css).
- Shipped the lean `godaisy-css` CLI to go-daisy (`eff4bb8`).

## Decisions

- **Blueprint/schema list needed a naked REST endpoint** — 3 of 4 blueprints-page calls
  were already REST; only `ListAllSchemas` went through MCP `schema-list` because the memory
  backend had no REST list endpoint (`GET /api/schemas` → 404). Confirmed the user's
  hypothesis, then built the missing endpoint.
- **Endpoint is a 1:1 REST mirror of the MCP tool** (same SQL semantics, row shape, envelope)
  — lets the gateway swap transport without parser changes and removes semantic risk.
- **Project-scoped route under the existing group** (`GET /api/schemas/projects/:projectId`)
  — reuses the domain's auth (`RequireProjectScope`) instead of inventing new scoping for a
  global list; matches how the gateway's session reaches memory (project context).
- **`ListMemories` needed no memory PR** — the graph objects REST endpoint
  (`GET /api/graph/objects/search`, no type filter) reads the same store as MCP
  `entity-query`, so it was a pure gateway flip. Verified live (same 2 memories before/after).
- **Removed the dead MCP session machinery** rather than leaving it — after both flips,
  `mcpToolCall`/`mcpSessionID`/`mcpInitialize` had zero callers (~320 lines + struct fields +
  `sync` import).
- **`godaisy-css` lean CLI over import-graph codegen** — the Phase-3 "walk the consumer's Go
  imports" idea was over-engineered; a registry-backed CLI that unions modules for an
  explicit package set is what actually prevents drift.
- **Title-sync needed no work** — `f189e69` (parallel session) already prefixed partials with
  a `<title>`; verified live, closed the task.

## Changes

### emergent.memory (backend) — PR #389
- `apps/server/domain/schemas/{entity,repository,service,handler,routes}.go` — added
  `GET /api/schemas/projects/:projectId?search&limit&offset` → `{project_id, schemas, total,
  limit, offset}`; `Repository.ListSchemaPacks` mirrors the MCP `schema-list` query.
  Merged as `d618cea5` (deployed to `api.dev.emergent-company.ai`; reviewer refined the row
  type to emit `visibility`/`org_id` verbatim like the MCP tool).

### gateway (alfred)
- `a61e693` — `memory.go`: `ListAllSchemas` → `GET /api/schemas/projects/<projectID>?limit=100`;
  rewrote `TestListAllSchemas` for REST; MCP-cache tests moved onto `ListMemories`.
- `7dcc31e` — `memory.go`: `ListMemories` → `GET /api/graph/objects/search?limit=100`;
  deleted `mcpToolCall`, `mcpSessionID`/`mcpClearSession`/`mcpSessionKey`, `mcpInitialize`,
  the `mcpMu`/`mcpSid` fields, and the `sync` import. `memory_test.go`: `TestListMemories`
  rewritten for REST; removed obsolete MCP/empty-content tests.
- `88286ab`, `5e3b017` — docs/tasks closures.

### go-daisy
- `eff4bb8` — `components/css/registry.go` (+`ModulesFor`/`CSSFilesFor`), `cmd/godaisy-css`,
  `components/css/modules_test.go`.

## Verification

- emergent.memory: `go build ./...` + `go vet ./domain/schemas/...` + `go test
  ./domain/schemas/` pass (DB-backed behavior runs in CI — no local Postgres). PR #389 CI all
  green. Deployed endpoint curl-checked (HTTP 200, correct envelope).
- gateway: `go build ./...`, `go vet`, `golangci-lint` (0 issues), `go test ./...` pass.
- go-daisy: `go build ./...`, `go test ./components/css/` pass; CLI emits the 43-module union
  for the gateway's 6 packages.
- Live browser: blueprints page renders (installed + available catalogs) via the new REST
  path; agent "test" memories page identical (2 memories) before/after the `ListMemories`
  flip; hx-boost multipart upload verified with a synthetic `File` (no full navigation,
  file listed).

## Open questions / follow-ups

- **Leftover test document** `hx-boost-upload-test.txt` (id `d7fad8bc-a80a-4846-883b-2aa238cddab1`,
  project f131d865, 1 chunk) in the dev tenant — the gateway has **no document-delete UI/API**
  path, so it could not be removed from the UI. → task `documents-delete-ui`.
- go-daisy now ships `custom.css` + registry + `godaisy-css`; the gateway's daisyUI `exclude`
  list can be regenerated/audited with the new CLI (not yet wired into the gateway build).

## Tasks

- [documents-delete-ui](../tasks/documents-delete-ui.md) — gateway/UI has no way to delete a document (surfaced by upload-verification cleanup)
