## Why

Umbrella ("implied") scope semantics were defined twice, each hand-maintained:

- `apps/server/pkg/auth/middleware.go` — `scopeImplies` (consumed by `expandScopes`, which gates REST API-token scopes).
- `apps/server/domain/mcp/service.go` — `scopeImpliesMap` (consumed by `expandScopesSet`, which gates MCP tool listing and calls), whose own comment admitted it *"mirrors auth/middleware.go's scopeImplies but for MCP fine-grained scopes."*

Nothing asserted the two agreed. A scope added to one and not the other silently changed what a token could do. PR #803 pinned the auth-side expansion (`TestRoleScopeUmbrellaExpansionIsPinned`) but the MCP side had no guard, so drift was detectable on one side only (issue #806).

The divergence was strictly narrower on the MCP side: MCP's table omitted several canonical implications and carried its own curated `admin:all` subset. Reconciling it widens what existing `emt_*` API tokens can reach through MCP, so it is a security-relevant behaviour change and must be bounded and explicit.

## What Changes

- **One source of truth.** The canonical umbrella-scope implication relation is exported once from `pkg/auth` as `ScopeImplies`, with a single canonical expansion entry point `auth.ExpandScopes`. `domain/mcp`'s parallel `scopeImpliesMap` is deleted; MCP derives its expansion from `auth.ExpandScopes` instead.
- **MCP projection, not a second table.** MCP is a tool surface, so it honors an implied scope only when that scope can gate an MCP tool — i.e. it is some tool's `RequiredScope`. The vocabulary is derived from the tool catalog (the central static scope map plus the package-level dynamic tool builders), so it cannot drift from the tools it guards. Account/organisation/global-admin scopes (`admin:read`, `admin:write`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`) are never tool `RequiredScope`s, so they are excluded **by construction** rather than by a hand-maintained deny list.
- **Agreement test.** A derivation-identity test asserts the MCP view equals the canonical expansion projected onto the tool vocabulary, plus pinned per-umbrella tool-visible sets, plus an assertion that no expansion reaches an excluded family.
- **Reconciliation (behaviour change).** Tokens holding `data:read` gain MCP access to `schema:read` tools; tokens holding `data:write` gain `schema:write` tools. Expansion stays single-level, so `data:write` does **not** reach `schema:migrate`. All other umbrella results are unchanged. The `admin:all` tool-visible set is unchanged. See `design.md` for the security analysis.
- **No change to #803's pinned role expansion.** The auth map's keys and values are byte-identical; only the identifier is exported. The pinned viewer/user/admin expanded sets (17/29/33) are unchanged.

## Capabilities

### Modified Capabilities

- `oidc-scope-mapping`: the umbrella-scope implication relation is now defined in exactly one place and every consumer derives its view from it; the bounded expansion requirement is extended to the MCP tool-surface projection.

## Impact

- `apps/server/pkg/auth/middleware.go`: `scopeImplies` → exported `ScopeImplies`; new exported `ExpandScopes` wrapper (values unchanged).
- `apps/server/domain/mcp/service.go`: `scopeImpliesMap` deleted; `mcpToolScopeVocabulary` added; `expandScopesSet` now projects `auth.ExpandScopes` onto the tool vocabulary.
- `apps/server/domain/mcp/scope_expansion_test.go` (new): agreement, pinned-set, excluded-family and reconciliation tests.
- No config, schema, migration, or API-surface change. Behaviour change is limited to MCP tool visibility/callability for API tokens carrying `data:read`/`data:write`.
