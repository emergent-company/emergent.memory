## Context

Issue #806: the umbrella-scope implication relation existed twice — `pkg/auth.scopeImplies` and `domain/mcp.scopeImpliesMap` — with nothing asserting they agreed. #803 pinned the auth side (`TestRoleScopeUmbrellaExpansionIsPinned`: viewer 17 / user 29 / admin 33 expanded scopes) but left the MCP side unguarded.

## Goals / Non-Goals

- **Goal:** make implication drift structurally impossible; reconcile the MCP divergence with an explicit, bounded security analysis; keep #803's pinned role expansion unchanged.
- **Non-Goal:** change the auth implication relation itself, `GetAllScopes()`, API-token scope validation, or the REST authorization surface. Non-Goal: transitive/multi-level expansion (see below).

## Decisions

### Single source of truth lives in `pkg/auth`

`pkg/auth` imports no `domain/*` package, and `domain/mcp` already imports `pkg/auth`, so exporting the canonical relation from `pkg/auth` and consuming it from `domain/mcp` is acyclic. The reverse direction would create a cycle. `scopeImplies` is therefore exported as `ScopeImplies` (keys and values byte-identical) and a single canonical expansion entry point `ExpandScopes` is added. `domain/mcp`'s `scopeImpliesMap` is deleted.

### A consumer derives its view by projection — it does not re-list implications

`pkg/auth` targets the REST/OIDC surface and includes scopes (`org:*`, `account:*`, `admin:read`, `mcp:admin`) that MCP has no meaning for. The MCP surface's consumer vocabulary is precisely "scopes that can gate a tool (some tool's `RequiredScope`)". So MCP derives its view as:

```
expandScopesSet(scopes) = {explicit scopes} ∪ { s ∈ auth.ExpandScopes(scopes) : s ∈ mcpToolScopeVocabulary }
```

`mcpToolScopeVocabulary` is itself derived from the tool catalog — the central static `toolRequiredScope` map plus the ordered package-level builder list (`dynamicToolBuilders`) that `GetToolDefinitions` also consumes. The implication relation is single-sourced structurally; the vocabulary is derived from the same builder list the catalog uses, and `TestMCPToolScopeVocabularyCoversCatalog` asserts the derived vocabulary contains every required scope in the full catalog (including handler-provided agent/registry tools), so a gap fails the build rather than silently narrowing. No hand-maintained boundary list is involved: the account/organisation/global-admin families are excluded **because none of them is ever a tool `RequiredScope`**, so a future canonical widening that is irrelevant to MCP cannot leak into the MCP view, and a widening that *is* MCP-relevant is caught by the pinned test.

Explicitly held scopes are always retained; only *implied* scopes are projected. This preserves current behaviour for tokens that hold a scope directly.

### Single-level expansion is preserved

Both the canonical `expandScopes` and MCP's `expandScopesSet` are single-level (one pass over input scopes, not a transitive closure). This is load-bearing for the security analysis: `data:write` implies `schema:write`, but — because `schema:write` in the *implied* position is not itself expanded — `data:write` does **not** reach `schema:migrate`. The refactor deliberately preserves this; it does not "improve" it into a closure.

### #803's pinned sets are untouched

The auth map's keys and values are unchanged; only the Go identifier is exported and a wrapper added. `TestRoleScopeUmbrellaExpansionIsPinned` (viewer 17 / user 29 / admin 33) still passes unchanged. This is a de-duplication, not an entitlement change, on the auth side.

## Security analysis — what widens and why it is bounded

**What widens (MCP tool access for existing `emt_*` tokens):**

- A token holding `data:read` now satisfies `schema:read`, unlocking the `schema:read` tools (`schema-list`, `schema-get`, `schema-history`, `schema-icon-list`, `schema-list-available`, `schema-list-installed`, and `schema:read` blueprint/domain read tools).
- A token holding `data:write` now satisfies `schema:write`, unlocking the `schema:write` tools (`schema-create`, `schema-delete`, `schema-assign`, `schema-assignment-update`, `schema-uninstall`, and `schema:write` blueprint/domain write tools).
- Expansion remains single-level: `data:write` does **not** reach `schema:migrate` tools.
- No other umbrella's tool-visible result changes. `admin:all`'s tool-visible set is unchanged (its canonical implication set already contains every tool scope; the extra canonical entries are REST scopes that are not tool scopes).

**Bounded to which families:** both additions stay in the project data/schema plane — precisely the families the token already holds. `schema:read` and `schema:write` are not `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, or `account:*`.

**No new privilege relative to the platform:** the REST layer already grants these exact implications — `pkg/auth.ScopeImplies` maps `data:read → schema:read` and `data:write → schema:write`, consumed by `RequireAPITokenScopes`. MCP was the outlier, strictly narrower than REST. This change removes the anomaly and restores parity. If the REST `data:write → schema:write` grant is itself judged too broad, that is a pre-existing #736 decision, tracked separately, not introduced here.

**`admin:all`:** tool-visible set unchanged. The forbidden REST-only scopes it canonically implies (`admin:read`, `admin:write`, `mcp:admin`, `org:*`, `project:invite:create`) cannot reach the MCP view because none is a tool `RequiredScope`; the excluded-family test asserts this.

**Defense in depth:** the MCP call paths still apply the share-instance tool allowlist (`InstanceDeniesTool`) after the scope check, so share tokens remain bounded regardless of scope widening.

## Risks / Trade-offs

- **Behaviour change for existing tokens:** `data:read`/`data:write` API tokens can now see and call schema read/write MCP tools. This is intentional, bounded, and called out in the PR body for operators.
- **Vocabulary completeness:** the vocabulary is derived from the shared `dynamicToolBuilders` list plus the static scope map, so a change to those cannot drift. Handler-provided tools (agent/registry) are outside that list; `TestMCPToolScopeVocabularyCoversCatalog` asserts every tool in `GetToolDefinitions()` — including handler-provided ones — has its `RequiredScope` present in the vocabulary, failing closed-safe (a gap would make a tool unreachable via umbrella, never widen).
- **Related drift surface (not fixed here):** `domain/agents/toolgroups` has a hand-maintained dynamic-scope list in its test. Once a fully catalog-derived vocabulary exists, that test could consume it. Out of scope; a follow-up issue is appropriate.

## Migration Plan

None. No schema, config, or API change. Resolution is per request and never cached.
