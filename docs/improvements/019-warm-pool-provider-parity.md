# Improvement Suggestion: Ownership, enumeration and leases for Firecracker and E2B sandboxes

**Status:** Proposed
**Priority:** Medium
**Category:** Architecture
**Proposed:** 2026-09-21
**Proposed by:** AI Agent (deferred finding from the PR #691 independent review)
**Assigned to:** Unassigned

---

## Summary

Give the Firecracker and E2B providers the same durable ownership labels, label-scoped resource enumeration and container-liveness leases the Docker/gVisor provider now has, so the warm pool and orphan reconciliation can work on hosts where gVisor is not the selected provider.

---

## Current State

PR #691 made orphan reclamation label-driven and lease-based, but implemented it only for Docker/gVisor:

- `Reconciler.resourceManager` resolves `ProviderGVisor` only;
- `SandboxResourceManager` / `ContainerHeartbeater` are implemented by `GVisorProvider` only;
- the reconciler's `memory.workspace` / `memory.owner` / `memory.owner.heartbeat` labels are Docker labels, which Firecracker/E2B containers do not carry.

As a consequence, `WarmPool.createWarmContainer` was deliberately restricted to the gVisor provider (design D9 of the archived change): the previous `SelectProvider(..., "auto")` call resolves to Firecracker first on a KVM-enabled host, which would once again pre-boot containers that are never enumerated, never kept alive by a lease, and never reaped. So on such hosts the warm pool now refuses to start instead of leaking — correct, but it means warm starts are unavailable wherever gVisor is not the provider of record.

---

## Proposed Improvement

Define a provider-agnostic ownership/lease contract and implement it for every provider that advertises warm-pool support:

- a durable ownership identity that survives process death (Firecracker/E2B equivalent of a label — provider metadata, VM tag, or a record the provider API can enumerate);
- enumeration of a provider's sandbox resources for the reconciler (interface parity with `SandboxResourceManager`);
- per-container liveness leases (parity with `ContainerHeartbeater`), so a live peer is distinguishable from a dead predecessor;
- destroy paths that reclaim the whole resource set for a workspace (container/VM plus every attached volume), matching D9's guarantee.

Then relax the D9 restriction so the warm pool can select any provider that satisfies the contract.

---

## Benefits

- **User Benefits:** warm starts remain available on non-Docker hosts.
- **Developer Benefits:** one reconciliation contract instead of a silently gVisor-only feature; the invariant "no sandbox outlives its owner" is testable per provider.
- **System Benefits:** removes the class of leak the PR #691 fix does not cover (non-Docker providers on KVM hosts).
- **Business Benefits:** no need to choose between warm-start latency and leak safety when deploying on KVM.

---

## Implementation Approach

1. Extract the ownership/lease/enumeration surface into a provider-neutral interface (`SandboxResourceManager`, `ContainerHeartbeater` are already close).
2. Implement it for Firecracker (VM metadata/tags + provider-side enumeration) and E2B (sandbox metadata + listing).
3. Make reconciliation resolve whichever provider implements the interface (not a hardcoded `ProviderGVisor`).
4. Re-allow warm-pool selection across providers that satisfy the contract, and extend the contract tests per provider.

**Affected Components:**

- `apps/server/domain/sandbox/orchestrator.go` (provider resolution)
- `apps/server/domain/sandbox/reconcile.go` (currently `ProviderGVisor`-only resolution)
- `apps/server/domain/sandbox/warm_pool.go` (D9 restriction)
- `apps/server/domain/sandbox/firecracker_provider.go`, `e2b_provider.go`
- `apps/server/domain/sandbox/gvisor_resources.go` (interface definitions)

**Estimated Effort:** Large

---

## Alternatives Considered

### Alternative 1: Keep the warm pool gVisor-only (status quo after PR #691)

- Description: accept that warm starts are unavailable on hosts where another provider is selected.
- Pros: no work; leak safety is preserved by refusing to pre-boot unreconcilable containers.
- Cons: loses warm-start performance on KVM hosts and leaves the provider asymmetry undocumented outside the design file.
- Why not chosen: it is a safety-preserving stopgap, not a permanent product position.

### Alternative 2: DB rows for warm-pool containers instead of provider labels

- Description: track warm containers in the database so reaping can be DB-driven for all providers.
- Pros: provider-agnostic.
- Cons: the reconciler still needs to enumerate live provider resources to find containers with *no* row (the original defect); rows also skew `CountActive`/concurrency limits.
- Why not chosen: does not remove the need for provider enumeration.

---

## Risks & Considerations

- **Breaking Changes:** No (additive interfaces; the D9 restriction relaxes only once a provider implements the contract).
- **Performance Impact:** Neutral.
- **Security Impact:** Neutral; the lease/ownership surface must not expose provider credentials.
- **Dependencies:** Requires provider APIs that can store and enumerate ownership metadata.
- **Migration Required:** No.

---

## Success Metrics

- Metric 1: the warm pool starts successfully on a KVM host with a non-gVisor provider selected.
- Metric 2: crash-leftover containers on that host are reclaimed within one cleanup cycle.

---

## Testing Strategy

- [ ] Unit tests (contract tests per provider, mirroring `gvisor_resources_test.go`)
- [ ] Integration tests (provider API-backed enumeration)
- [ ] E2E tests (unelegant restart then startup reconciliation reclaims the predecessor's pool)
- [ ] Performance tests
- [ ] Security review
- [ ] User acceptance testing

---

## Related Items

- Related to PR #691 (`fix/warm-pool-orphan-reaping`, design D9) and its archived change.
- Related to bug `docs/bugs/056-snapshot-and-mcp-extra-volumes-never-swept.md` (same "volume classes not swept" theme on the gVisor side).

---

## References

- `openspec/changes/archive/2026-09-21-fix-warm-pool-orphan-reaping/design.md` — D9 (provider restriction)
- `openspec/specs/agent-sandbox-lifecycle/spec.md` — reconciled-provider requirement

---

## Notes

Raised by the independent architecture review of PR #691. It was addressed in that PR by restricting the pool rather than by implementing parity, because implementing provider-side enumeration/leases was out of scope and untestable there.

---

**Last Updated:** 2026-09-21 by AI Agent
