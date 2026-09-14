## Context

See proposal.md — Why. Current state on `main` (verified against live tree, Sep 2026):

- Batch tools (`entity-create` ~service.go:3875, `relationship-create` ~4050) build ad-hoc maps: `{ok, created, success(int, deprecated), failed, total, results[]({success bool, entity|error, index}), similar*, message}`. Message set unconditionally on both paths. remember/forget return plain-text `ToolResult`s with `run_id` buried in prose (service.go ~5250-5400); sync path decodes structured REST body `{run_id, status, summary, document_id}` but only interpolates it into text; `DocumentID` never returned.
- search-hybrid/search-semantic already return slim entities via `slimEntity`/`slimSearchResponse` (`response_contract.go:117/265`) with a `verbose` opt-in. entity-query returns `{projectId, entities[], pagination}` from typed structs (`entity.go:346-373`). No envelope re-verbosification needed — only wrapping.
- In-process consumers: `agents/toolpool.go:743-793` `convertToolResult` (preserves JSON maps as-is, injects `ok:true` when absent, wraps text as `{ok,result}`); `agents/remember_status.go:315-425` `parseEntityCreate/parseEntityUpdate/parseRelationshipCreate/parseEntityDelete/parseRelationshipDelete` parse the CURRENT top-level shapes (`results[]`, per-item bool `success`, fallback top-level `entity`+`success`). Agent `remember-status` aggregates these stored outputs. If producers change without consumers, counts silently zero while fixtures (encoding old shape) keep tests green.
- Dead code: `mcp/entity.go:588-634` legacy result structs (CreateEntityResult/CreatedEntity/CreateRelationshipResult/CreatedRelationship) — nothing constructs them.
- Partial fixes already on main: `ok` bool + `created` count on batch tools (db2178721-era), `run_id` in sync prose, loopback HTTP errors surface missing scopes via `mcpHTTPError` (#342). External consumer (Alfred chat UI chips) lives in another repo.
- No openspec main spec governs MCP result envelopes (`mcp-tool-naming-convention` covers names only).

## Goals / Non-Goals

**Goals:**
- One uniform `{ok, error?, data, meta?}` envelope (compact JSON text block) across the #319 tool set + envelope siblings, produced through a single shared helper so future tools inherit it.
- Kill the int `success` ambiguity: top-level int `success` dropped, per-item renamed `success`→`ok`.
- `message` = human prose only; status readable from `ok`/`error` alone.
- remember/forget return structured JSON with `run_id` (and known completion data) — no prose parsing.
- In-process consumers updated in the same change; regression test pins old-shape ⇒ zero counts so the silent-zero trap cannot recur.
- Slim read responses preserved (already default).

**Non-Goals:**
- Migrating all remaining structured-result MCP tools (Phase 2 follow-up).
- Changing REST/chat remember/forget endpoints (already structured `{run_id,status,summary,document_id}`).
- Changing MCP tool names, input schemas, or `remember-status` tool definition.
- Coordinating/editing the external Alfred repo (separate follow-up + release note).
- Flattening `data` into the top level for agent prompting (models handle nested JSON; flattening risks key collisions and breaks round-trip).

## Decisions

### D1. Shared envelope helper in mcp domain, nested `{ok, error?, data, meta?}`, compact JSON
New helper (e.g. in `response_contract.go` or new `envelope.go`): `envelopeResult(ok bool, data map[string]any, meta map[string]any, errMsg string) *ToolResult` serializing via the existing compact marshal path (`wrapResultCompact`; indented `wrapResult` only where a tool currently uses it — prefer compact to save LLM tokens). `error` omitted when `ok`; `meta` omitted when empty. Rationale: matches the issue author's proposal, makes generic classification trivial, and the existing `wrapResult`/`wrapResultCompact` split means tools already choose verbosity — envelope helper takes a verbosity param. Rejected: flat additive (keeps top-level collisions, defeats uniform classification); strict-nested-only with no compat surface (needless churn — current keys move under `data` wholesale).

### D2. Scope: #319 tools + envelope siblings now; registry test; Phase 2 later
Migrate in THIS change: `entity-create`, `entity-update`, `entity-delete`, `relationship-create`, `relationship-delete`, `search-hybrid`, `search-semantic`, `entity-query`, `remember`, `forget`. The fragile lockstep (`remember_status.go`) touches exactly the 5 mutating tools; search/query already slim. Sweeping 30+ tools multiplies fixture/prompt/e2e churn for zero functional gain. New tools MUST use the helper — enforced by a registry test (tool → envelope expectation list for Phase-1 tools), not a lint rule (not cleanly expressible).

### D3. Bool cleanup: drop top-level int `success`, rename per-item `results[].success` → `ok`
`success` int removed outright (no grace — `created`/`failed`/`total` + `ok` already exist). Per-item bool renamed to `ok`. Rationale: single status vocabulary; churn is one parser + fixtures already bundled in lockstep. Top-level `message` stays as human summary only.

### D4. remember/forget → structured JSON envelope on both paths
Sync + async both return the envelope with `data` decoded from the full REST body, not just `run_id`: async `data.{run_id, status?, document_id?}`; sync remember `data.{run_id, status, summary, document_id}`; sync forget `data.{run_id, status, summary}` (REST returns no `document_id` for forget — verify at implementation). Prose moved to `meta`/`data.message` if desired, but never the carrier. Fix forget async copy ("see what was created" → "see what was removed"). Rationale: Alfred chip + callers act on run_id programmatically; prose regex is the exact failure mode #319 reports.

### D5. Consumer lockstep, same change
Update `agents/remember_status.go` five `parse*` funcs to read `data.results[].ok` / `data.entity` / `meta` counts. Delete the single-entity fallback branch in `parseEntityCreate` (batch form always yields `data.results[]`; the fallback becomes dead). Update fixtures in `remember_status_test.go` to the new envelope AND add one negative fixture with the OLD top-level shape asserting zero counts (pins the trap). Update `agents/repository.go` prompt text (~576) and `personal_kb_agent.go` references to field positions (`pagination` under `data`, `version` present). toolpool `convertToolResult`: NO code change — nested envelope passes through, `ok` already present (no injection). Add one passthrough test asserting `data`/`meta`/`ok` survive normalization intact.

### D6. Delete dead legacy structs
`mcp/entity.go:588-634` (`CreateEntityResult`, `CreatedEntity`, `CreateRelationshipResult`, `CreatedRelationship`) deleted. One coordinated breaking release (envelope restructure is already breaking for external consumers, so a deprecated int `success` buys zero incremental compatibility). Release note + Alfred repo follow-up.

### D7. Docs + parity surfaces
Update: swagger annotations on chat/agents handlers if they document affected shapes; `apps/server/domain/mcp/README.md`; `docs/site/` MCP pages (tracked in git — do not skip); verify CLI (`apps/cli`) parses none of these shapes (expected no-op). `mcpHTTPError` already improves error surfacing (#342) — no change.

## Risks / Trade-offs

- [Silent zero-count regression in agent `remember-status`] → Consumers + fixtures change in the same commit as producers; negative regression fixture pins old-shape ⇒ zero; `remember_status_test.go` updated deliberately, not left stale.
- [Breaking change for external Alfred consumer] → Single coordinated breaking release + release note + separate-repo follow-up issue; fields preserved under `data` minimize rework.
- [Parallel session actively committing on this repo] → Plan written against verified anchor lines that survived rebases (batch envelope ~3875, remember sync ~5290, parsers 315-425); implementers re-grep anchors at apply time rather than trusting line numbers.
- [Per-item rename `success`→`ok` ripples through prompt text/examples] → Grep for `results[].success`/`"success"` readers (`remember_status.go`, tests, prompt strings) in the same change; doc surfaces in D7.
- [`meta` field name collision — tool args use `meta` SSE events (query_knowledge)] → Envelope `meta` is a result field, not a tool arg; no collision in practice, but confirm `query_tools.go:163` handling is arg-side only (Phase 2 tool anyway).
- [Depth/verbosity change for agent context] → Envelope adds one nesting level; compact serialization keeps token cost near parity; verified models handle nested JSON (D1 note).

## Migration Plan

1. Land producer + consumer + tests + docs as ONE commit (lockstep requirement).
2. Release note: breaking — MCP tool results now `{ok,error,data,meta}`; old top-level fields under `data`; per-item status `ok`; remember/forget results now JSON.
3. File Alfred-repo follow-up to update chip classifier (reads `ok`/`error` at top level — actually a simplification).
4. Phase 2 change: migrate remaining structured-result tools through the same helper.

## Open Questions

- Exact REST forget sync body fields (does it return `summary` only, no `document_id`?) — resolved at implementation by reading `chat/handler.go` forget handlers; does not change envelope shape or spec.
- Whether entity-query should also accept a `verbose` flag for parity with search tools, or stay as-is — defer to Phase 2; current Entity struct fields are acceptable slim output and spec only forbids verbose internals by default.
