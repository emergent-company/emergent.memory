# Bug Report: Snapshot and MCP extra volumes are excluded from orphan reconciliation

**Status:** Open
**Severity:** Medium
**Component:** Sandbox / Workspace lifecycle (`apps/server/domain/sandbox`)
**Discovered:** 2026-09-21
**Discovered by:** AI Agent (deferred finding from the PR #691 independent review)
**Assigned to:** Unassigned

---

## Summary

The orphan-reconciliation volume sweep deliberately skips volumes labelled `workspace.from_snapshot` or `workspace.parent`, so a restored-from-snapshot workspace volume and every persistent MCP server's extra mount volume can never be reclaimed once their container is gone.

---

## Description

- **What is happening (actual behaviour):** `isPrimaryWorkspaceVolume` (`apps/server/domain/sandbox/gvisor_resources.go`) returns `false` when the labels contain `workspace.parent`, `workspace.source` or `workspace.from_snapshot`. The reconciler then records the volume as `not_primary` and skips it forever. Two real volume classes fall into this set:
  - the **primary** volume of a workspace created via `CreateFromSnapshot` (carries `workspace.from_snapshot`, see `gvisor_provider.go`),
  - **extra mount volumes** created for hosted/persistent MCP servers (carry `workspace.parent`, see `gvisor_provider.go`), which `GVisorProvider.Destroy` does not remove either.
- **What should happen (expected behaviour):** once the owning container no longer exists and no active workspace references it, the volume should be reclaimed — either by the reconciler learning to enumerate these classes with their own ownership/`refs` checks, or by removing them on the container destroy path.
- **When/how the issue occurs:** any out-of-band container removal (crash, `docker rm`, provider-side eviction) leaves a `from_snapshot` volume behind permanently; MCP extra volumes leak even on a clean teardown, because only the primary `workspace.volume` is removed by `Destroy`.

The exclusion itself is the *fail-safe* direction (spare rather than destroy), which is why it was accepted in PR #691 — this report tracks the resulting gap, not a regression.

---

## Reproduction Steps

1. Create a workspace from a snapshot (`CreateFromSnapshot`), so its primary volume carries `workspace.from_snapshot`.
2. Remove the container out-of-band: `docker rm -f <container>`.
3. Wait past the reconcile grace window and let a reconciliation pass run (or invoke it via the cleanup cycle).
4. Observed behaviour: the volume is not destroyed and the log records `reason=not_primary`; `docker volume ls --filter label=memory.workspace=true` still lists it.
5. Host a persistent MCP server (extra volume with `workspace.parent`), then remove the server through the normal path and observe the extra volume still present.

---

## Logs / Evidence

```text
reconciliation skipped volume  volume=memory-workspace-<id> reason=not_primary
```

**Log Location:** `apps/server/logs/server/` (`component=sandbox-reconcile`)
**Timestamp:** any reconciliation cycle

---

## Impact

- **User Impact:** none directly (workspaces still function; disk pressure only).
- **System Impact:** unbounded volume accumulation on hosts that use snapshots or host MCP servers. The PR #691 incident was 191 zero-byte leaked volumes and 56 GB of `/var/lib/docker`; this class is not covered by that fix.
- **Frequency:** every snapshot restore whose container is removed out-of-band, and every MCP server teardown with extra volumes.
- **Workaround:** manual `docker volume rm` using the label filters documented in `docs/agent-sandbox/OPERATIONS.md`.

---

## Root Cause Analysis

`isPrimaryWorkspaceVolume` is a positive-allowlist filter written to make the sweep conservative: anything it cannot prove is a primary workspace volume is spared. Because the label set cannot distinguish "extra volume of a live workspace" from "extra volume of a dead workspace", the safe choice was to spare all of them. Reclaiming them requires resolving the parent container's liveness for those classes.

**Related Files:**

- `apps/server/domain/sandbox/gvisor_resources.go` — `isPrimaryWorkspaceVolume` (exclusions), `ListSandboxVolumes` (sweep input)
- `apps/server/domain/sandbox/reconcile.go` — volume sweep (`not_primary` skip) and `protectedVolumes`
- `apps/server/domain/sandbox/gvisor_provider.go` — `CreateFromSnapshot` (`workspace.from_snapshot`), MCP extra mounts (`workspace.parent`), `Destroy` (removes only the primary volume)

---

## Proposed Solution

Extend the container enumeration to publish its full set of attached volume names (inspect `Mounts` per container, or add a labelled `workspace.extra_volumes` value at create time), then treat any enumerated volume whose parent container is absent and which is not referenced by an active workspace row as reclaimable. Keep the fail-safe ordering: only reclaim a non-primary volume when its parent label points at a container ID that is confirmed absent, never when the parent is unknown.

**Changes Required:**

1. Record extra volume names on the container (label) or read them from `ContainerInspect`.
2. Build a `protectedVolumes` set from all live containers, not just the primary `workspace.volume`.
3. Sweep any labelled volume not in that set and not referenced by an active workspace row.
4. Remove MCP extra volumes in `GVisorProvider.Destroy` as well.

**Testing Plan:**

- [ ] Unit test: `from_snapshot` volume whose parent container is absent is destroyed
- [ ] Unit test: extra volume whose parent container is present is spared
- [ ] Unit test: MCP extra volume removed on `Destroy`
- [ ] Docker-backed test: out-of-band container removal then reconcile reclaims all attached volumes

---

## Related Issues

- Follow-up to PR #691 (`fix/warm-pool-orphan-reaping`).
- Related: the persistent-MCP reclamation policy tracked by PR #690.

---

## Notes

Found during the independent architecture review of PR #691 (`@oracle` verdict: MINOR/NIT-8, accepted as a deliberate fail-safe exclusion rather than fixed in that PR).

---

**Last Updated:** 2026-09-21 by AI Agent
