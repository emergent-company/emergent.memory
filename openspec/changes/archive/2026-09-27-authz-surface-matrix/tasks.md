# Implementation tasks

This change is **documentation-only** (no production code, no spec delta). The tasks are the audit → consolidation → check-in steps.

## 1. Consolidate the four audits

- [x] 1.1 Re-check every finding at current `origin/main` and mark each `fixed #N` / `in-flight #N` / `open` / `stale`/`refuted` — verify by reading `matrix.md` §2/§3 rows each cite a current guard chain
- [x] 1.2 Resolve the two report conflicts (bare-`admin` blast radius; per-agent MCP endpoint trust) and record the judgment in `matrix.md` §4
- [x] 1.3 Write `matrix.md` with one row per surface, collapsed OK rows, expanded open mismatches — verify counts match the Summary table

## 2. Maintenance story

- [x] 2.1 Write `design.md` with the audit method, re-run procedure, and the registry-tie invariant — verify it references `authz-abstraction` #991 `B2`/`B7`
- [x] 2.2 Add the open-finding → abstraction mapping table — verify every `matrix.md` §2 row appears exactly once

## 3. Verification

- [x] 3.1 `openspec validate authz-surface-matrix --strict` passes
- [x] 3.2 `openspec validate --all --strict` passes (report counts)
