# Improvement Suggestion: Active Memory Management

**Status:** Proposed
**Priority:** High
**Category:** Architecture
**Proposed:** 2026-03-17
**Proposed by:** AI Agent (Claude)
**Assigned to:** Unassigned

---

## Summary

Upgrade the planned agent memory system from passive (LLM-instructed) to active (system-participates) memory management, adding LLM-assisted dedup/merge, proactive core memory injection, memory reflection, and confidence decay.

---

## Current State

The existing `docs/features/agent-memory-design.md` describes a v1 passive memory system:

- `save_memory` uses a fixed **0.85 cosine similarity threshold** for dedup — binary pass/fail, no nuance
- `recall_memories` is **LLM-initiated only** — the agent must follow system prompt instructions to call it; if it doesn't, no memories are injected
- Memories **accumulate indefinitely** — no decay, no lifecycle, no quality management
- No **reflection** — no mechanism to synthesize higher-level patterns from clusters of specific memories
- No **observability** — no metrics to tune thresholds or measure recall quality

This is functional for simple cases but degrades as memory count grows and fails when:
- The LLM doesn't follow recall instructions at session start
- Two memories are logically contradictory but semantically similar (both pass the threshold and coexist)
- Two memories are semantically distant but logically complementary (neither is merged)
- Stale memories from months ago contaminate the agent's context

---

## Proposed Improvement

Extend the base memory design with five active management capabilities, ordered by ROI:

### 1. LLM-Assisted Merge in `save_memory`

**Current**: `if cosine_distance(new, existing) > 0.85 → supersede`

**Proposed**:
1. Retrieve top-3 semantically similar memories
2. Call LLM with: `(new_content, [similar_memory_1, similar_memory_2, ...])`
3. LLM returns structured decision:
   ```json
   {"action": "ADD" | "UPDATE" | "DELETE_OLD" | "NOOP", "target_id": "...", "merged_content": "..."}
   ```
4. Apply mutation

Handles cases cosine similarity cannot:
- **Contradiction**: "User prefers tabs" + "User prefers spaces" → DELETE old, ADD new (corrected)
- **Merge**: Two memories about TypeScript preferences → UPDATE with unified content
- **Superset**: New memory contains everything in old + more → UPDATE to replace
- **Nuance**: New memory adds important caveat to old → UPDATE to refine

**Research basis**: mem0 (arXiv:2504.19413) shows +26% accuracy over threshold-based approaches.

### 2. Core Memory Block (Proactive Injection)

Add a `core_memory` tier alongside the existing archival tier:

```
core_memory     ← top-N high-priority memories injected into EVERY session system prompt
archival_memory ← existing: searched on-demand via recall_memories tool call
```

Core memory selection criteria (configurable):
- Memories with `priority > 0.8` (explicitly promoted by user or agent)
- Memories with `use_count > threshold` (most-used)
- Memories with `category = instruction` (always-apply rules)

A new MCP tool `promote_to_core` allows the agent or user to explicitly promote a memory to the core tier.

**Research basis**: Letta/MemGPT's `memory_blocks` — always-in-context named blocks eliminate reliance on LLM recall compliance.

### 3. Memory Reflection Job

A scheduled job (using existing `domain/scheduler`) that:
1. Clusters Memory objects by embedding similarity (threshold: 0.75, min cluster size: 3)
2. For each cluster: calls LLM to synthesize a `MemoryContext` summary
3. Creates or updates the `MemoryContext` object with the synthesis
4. Links cluster members via `BELONGS_TO_CONTEXT` relationships
5. Sets `source = 'inferred'` and `confidence = 0.7` on the synthesized context

Example: 5 memories about TypeScript style → synthesized `MemoryContext("TypeScript Conventions")` with a concise summary usable in recall.

**Research basis**: Memory survey (arXiv:2404.13501) multi-granularity storage; mem0 memory compression.

### 4. Confidence Decay

A scheduled job that runs weekly:
1. For every Memory not recalled in N days (configurable, default: 30):
   - `confidence *= 0.95`
2. Memories below `confidence < 0.3`:
   - Set `needs_review = true` (existing field on GraphObjects)
3. Memories below `confidence < 0.1` and older than 90 days:
   - Auto-archive (soft-delete) unless `scope = 'instruction'`

**Research basis**: Memory survey forgetting mechanisms; prevents stale context contamination.

### 5. Memory Analytics Endpoint

New endpoint: `GET /api/memory/analytics`

Metrics to expose:
- `recall_rate`: % of recall calls that returned ≥1 memory
- `hit_rate`: % of recalled memories used in final response (requires response feedback signal)
- `memory_age_distribution`: histogram of memory ages
- `top_recalled`: most-used memories (by `use_count`)
- `stale_memories`: count of memories not recalled in 30/60/90 days
- `confidence_distribution`: histogram of confidence scores

**Research basis**: NVIDIA NeMo Agent Toolkit observability; required to tune thresholds data-driven.

---

## Benefits

- **User Benefits**: Agent reliably applies known preferences even without explicit recall instruction; context is always relevant and current; stale/wrong memories are eventually corrected or decayed
- **Developer Benefits**: Observable memory system with metrics; dedup behavior is explainable (LLM reasoning vs. opaque threshold); tunable via analytics
- **System Benefits**: Memory quality improves over time; token usage stays bounded (decay + compression); recall precision improves (LLM merge removes contradictions)
- **Business Benefits**: Differentiates from mem0/Letta by combining graph relationships, hybrid search, AND active management in one system

---

## Implementation Approach

### Phase 1: LLM-Assisted Merge (1-2 days)
1. Add `memoryMergeDecision` LLM call in `executeSaveMemory` (after semantic search, before write)
2. Define merge prompt template with structured output schema
3. Apply decision: ADD / UPDATE (supersede) / DELETE_OLD+ADD / NOOP
4. Unit tests for each decision branch

**Affected**: `domain/mcp/service.go` (`executeSaveMemory`)

### Phase 2: Core Memory Tier (2 days)
1. Add `memory_tier` property to Memory schema: `core` | `archival` (default: `archival`)
2. Add `promote_to_core` MCP tool
3. Add `coreMemoryInjector` called at chat session start — queries `memory_tier=core` memories for current user, prepends to system prompt
4. Update `memory_guidelines` MCP prompt to document core tier

**Affected**: `domain/mcp/service.go`, `domain/chat/service.go`, `agent-memory` schema pack

### Phase 3: Reflection Job (2-3 days)
1. Add `MemoryReflectionJob` to `domain/scheduler`
2. Implement clustering via pgvector: `SELECT ... ORDER BY embedding <=> $base LIMIT N WHERE cosine_distance < 0.25`
3. Implement synthesis LLM call
4. Create/update `MemoryContext` objects and relationships
5. Make schedule configurable (default: daily, run during off-peak hours)

**Affected**: `domain/scheduler`, `domain/mcp/service.go` (or new `domain/memory/service.go`)

### Phase 4: Confidence Decay (1 day)
1. Add `MemoryDecayJob` to `domain/scheduler`
2. Batch-update `confidence` for non-recalled memories
3. Apply `needs_review` flag below threshold
4. Auto-archive below hard floor (with configurable opt-out per scope)

**Affected**: `domain/scheduler`, `domain/graph/service.go`

### Phase 5: Analytics (1 day)
1. Add `GET /api/memory/analytics` handler
2. Query `kb.graph_objects` for Memory-type objects with aggregations
3. Add to API documentation

**Affected**: `domain/mcp/handler.go` or new `domain/memory/handler.go`

**Estimated Effort:** Large (7-9 days total across all phases)

---

## Alternatives Considered

### Alternative 1: Keep threshold-based dedup, skip LLM merge

- Pro: Simpler, no extra LLM call
- Con: Cannot handle contradictions or merges — known failure mode
- Why not chosen: The LLM merge is the highest-ROI change per mem0's research

### Alternative 2: Use a separate mem0 service

- Pro: Proven production system with managed infra
- Con: Additional service to deploy; loses graph relationships, hybrid search, project context integration; extra infrastructure cost
- Why not chosen: Emergent's value-add IS the graph integration — we should build on it, not replace it

### Alternative 3: Implement only core memory (skip reflection/decay)

- A valid reduced scope for v1. Phases 1+2 alone would address the most common failure mode (LLM non-compliance with recall)

---

## Risks & Considerations

- **Breaking Changes**: No — additive to existing `agent-memory-design.md` design. Existing `save_memory` behavior is preserved with the merge step added.
- **Performance Impact**: Negative (1 extra LLM call per `save_memory` for the merge decision). Mitigated: only triggers when cosine similarity > 0.70 (threshold to even run the merge call).
- **LLM Cost**: Each merge decision call is a small prompt (~500 tokens). At scale, could add up. Mitigation: cache merge decisions for identical content pairs (TTL 1h).
- **Security Impact**: Neutral — merge LLM call uses the same model/credentials as extraction pipeline.
- **Dependencies**: Requires `agent-memory-design.md` base implementation to exist first. This is an extension, not a replacement.
- **Migration Required**: No — schema fields already accommodate all proposed additions.

---

## Success Metrics

- Dedup quality: % of contradictory memory pairs resolved (measured via test suite with known contradictions)
- Core memory compliance: % of sessions where core memories appear in context (should be 100%)
- Recall precision: % of recalled memories rated "useful" by LLM in response generation
- Memory staleness: % of memories with age > 60 days and use_count = 0 (should trend down with decay)
- Memory compression ratio: average MemoryContext:Memory ratio (target: 1 context per 3-5 memories)

---

## Testing Strategy

- [x] Unit tests: LLM merge decision for each action type (ADD, UPDATE, DELETE_OLD, NOOP)
- [x] Unit tests: Core memory selection and injection into system prompt
- [x] Integration tests: Full save_memory → merge decision → graph write flow
- [x] Integration tests: Reflection job clustering and synthesis
- [x] Integration tests: Decay job confidence updates and archival
- [ ] E2E tests: Multi-session flow with core memory persistence across sessions
- [ ] Performance tests: save_memory latency with and without merge call
- [ ] Security review: Ensure merge LLM call cannot be prompted to leak other users' memories

---

## Related Items

- Related to `docs/features/agent-memory-design.md` (base design, prerequisite)
- Depends on `agent-memory` template pack implementation
- Related to improvement #006 (chat-context-management)
- Research: `docs/features/active-memory-management/research/active-memory-management-research.md`

---

## References

- mem0: https://github.com/mem0ai/mem0
- mem0 paper (arXiv:2504.19413): https://arxiv.org/abs/2504.19413
- Letta/MemGPT: https://github.com/letta-ai/letta
- MemGPT paper (arXiv:2310.08560): https://arxiv.org/abs/2310.08560
- Memory survey (arXiv:2404.13501): https://arxiv.org/abs/2404.13501
- Research doc: `docs/features/active-memory-management/research/active-memory-management-research.md`

---

## Notes

This improvement is designed to be implemented incrementally — each phase is independently valuable. If only one phase is shipped, Phase 1 (LLM-assisted merge) or Phase 2 (core memory block) should be prioritized. Phase 2 addresses the highest-impact reliability gap (LLM recall compliance), Phase 1 addresses the highest-impact correctness gap (contradiction/merge handling).

The reflection and decay phases (3+4) are more speculative and should be validated with real usage data from Phases 1+2 before committing to implementation.

---

**Last Updated:** 2026-03-17 by AI Agent (Claude)
