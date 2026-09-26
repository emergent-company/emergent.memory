## Why

Four read-only authorization audits (auth core, MCP/agent surfaces, server domains A–L, server domains M–Z) produced a large body of findings at four *different* commit SHAs, several of which had already been fixed or were in flight by the time the last report landed. There is no single durable artifact that maps every entrypoint to the authority actually gating it, so the audit results would rot immediately without a checked-in reconciliation.

This change publishes that reconciliation as one maintained **surface→authority matrix**: every entrypoint (`method + path` or in-process dispatch), the guard chain that actually protects it at the audited SHA, the resource tier, the *correct* authority, and a verdict. It also records which findings were stale/refuted versus genuinely open, and which fixed PR closed which gap.

## What Changes

- `openspec/changes/authz-surface-matrix/matrix.md` — the artifact. One row per surface, grouped by domain, with columns: surface · guard chain (current) · resource tier · correct authority · verdict · mechanism (1–8) · status (`fixed #N` / `in-flight #N` / `open` / `stale`/`refuted`). Uncontroversial OK rows are collapsed; every open mismatch is expanded.
- `openspec/changes/authz-surface-matrix/design.md` — the audit method (how it was performed, how to re-run it) and the maintenance story: how this matrix stays honest over time, tied to the `authorization-enforcement` CI coverage guard proposed in `authz-abstraction` (#991). Includes a mapping from every open finding to the abstraction component that would prevent it.
- `openspec/changes/authz-surface-matrix/proposal.md` — this file.

No production code changes. This is a documentation-only change: the matrix and its maintenance story. `skip_specs` is set because no capability's observable behavior is altered by this change.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

<!-- none — documentation-only change; no requirement changes. -->

## Impact

- New files under `openspec/changes/authz-surface-matrix/` only.
- References (read, not modified): `openspec/specs/scope-authority/spec.md`, the `authz-abstraction` change (#991), and the four audit reports under `/tmp/opencode/` (not committed).
- No code, spec, schema, or API impact.
