# Improvement Suggestion: Expose sandbox reconciliation as a metric with an orphan alarm

**Status:** Proposed
**Priority:** Medium
**Category:** Architecture / Observability
**Proposed:** 2026-09-21
**Proposed by:** AI Agent (deferred open question from the PR #691 design)
**Assigned to:** Unassigned

---

## Summary

Publish the orphan-reconciliation outcome (reconciled / skipped / failed, plus the labelled container and volume counts) as metrics and alarm on sustained orphan growth, instead of relying on log lines only.

---

## Current State

PR #691 (`fix/warm-pool-orphan-reaping`) made the reconciler log a per-pass summary and a line per destroyed resource:

```text
reconciliation complete  reconciled=N skipped=N failed=N grace_period=15m0s
reconciliation destroyed orphan container  container_id=... reason=ownerless
```

Those lines are only visible if someone reads the logs, and nothing detects drift. The leak that motivated the fix accumulated 184 containers and 191 volumes over 11 days across 11 image versions with no alarm — it was found operationally, not from telemetry. There is also an existing `WORKSPACE_ALERT_THRESHOLD_PCT` notion for resource usage that orphan counts do not participate in.

---

## Proposed Improvement

- Emit a counter for reconciled/skipped/failed resources per pass and a gauge for labelled sandbox containers and volumes on the host, from the reconciler.
- Add an alarm when the number of ownerless containers or volumes does not converge toward the configured pool target over successive passes (e.g. non-zero reconciled count on N consecutive cycles, or labelled volume count above pool target + active workspaces).
- Surface the same numbers in the sandbox health/status surface so `memory` CLI or the admin UI can show "sandbox orphans: N" without shelling into the host.

---

## Benefits

- **User Benefits:** leaks are caught before hosts run out of disk (the PR #691 incident reached 56 GB of `/var/lib/docker`).
- **Developer Benefits:** no need to grep three different log files to answer "is reconciliation working?".
- **System Benefits:** makes the fix's core invariant — container count bounded by the pool target — continuously verifiable rather than assumed.
- **Business Benefits:** avoids unscheduled host cleanup and the production-recovery work the initial leak required.

---

## Implementation Approach

1. Add counters/gauges to the reconciler pass (`ReconcileResult` already carries the three counts).
2. Wire them into the existing metrics/observability surface (tracing is opt-in; metrics should follow the same opt-in posture).
3. Define the alarm threshold in config alongside the existing `WORKSPACE_*` knobs, defaulting to off.
4. Expose the current counts on the sandbox health surface.

**Affected Components:**

- `apps/server/domain/sandbox/reconcile.go`
- `apps/server/domain/sandbox/cleanup.go`
- `apps/server/internal/config/config.go`
- sandbox health/status surface

**Estimated Effort:** Small

---

## Alternatives Considered

### Alternative 1: Log-only with a documented grep

- Description: keep the current log lines and document the exact `grep` in `OPERATIONS.md`.
- Pros: zero code.
- Cons: pull-based, nobody watches logs for absence of an event; the original leak was invisible this way.
- Why not chosen: it is the status quo that failed.

### Alternative 2: Alert on absolute host disk usage

- Description: reuse existing host-level disk alarms.
- Pros: no domain code.
- Cons: fires late, after the leak has consumed resources, and cannot distinguish sandbox orphans from unrelated disk growth.
- Why not chosen: less precise and later than counting orphans directly.

---

## Risks & Considerations

- **Breaking Changes:** No.
- **Performance Impact:** Neutral — counts come from the pass that already enumerates resources.
- **Security Impact:** Neutral.
- **Dependencies:** Requires the metrics/observability surface chosen by the platform.
- **Migration Required:** No.

---

## Success Metrics

- Metric 1: an orphan leak is detectable within one cleanup interval instead of days.
- Metric 2: labelled container/volume counts are queryable without host shell access.

---

## Testing Strategy

- [x] Unit tests (reconciler counts already covered by `reconcile_test.go`)
- [ ] Integration tests (metric emission per pass)
- [ ] E2E tests
- [ ] Performance tests
- [ ] Security review
- [ ] User acceptance testing

---

## Related Items

- Addresses the `Open Questions` item in the archived change `openspec/changes/archive/2026-09-21-fix-warm-pool-orphan-reaping/design.md`.
- Related to bug `docs/bugs/056-snapshot-and-mcp-extra-volumes-never-swept.md` (a leak class that would be visible as non-converging counts).

---

## References

- PR #691 — `fix(sandbox): reclaim orphaned warm-pool containers and volumes`
- `docs/agent-sandbox/OPERATIONS.md` — reconciliation knobs and verification commands

---

## Notes

PR #691 deliberately shipped logging as the minimum bar; this suggestion is the follow-up the design deferred to review.

---

**Last Updated:** 2026-09-21 by AI Agent
