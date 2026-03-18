## ADDED Requirements

### Requirement: Benchmark runner measures baseline before implementation
A benchmark test suite SHALL be defined and executed against the passive system (before active memory management is implemented) to record baseline metrics for all improvement dimensions.

#### Scenario: Baseline run executes before implementation begins
- **WHEN** the benchmark test is run with build tag `benchmark` against the unmodified passive system
- **THEN** it SHALL produce a JSON results file at `testdata/memory_benchmark/results/YYYY-MM-DD_baseline.json`
- **THEN** the file SHALL contain measurements for all 8 metrics: M1 through M8
- **THEN** no assertions against targets SHALL run (baseline run is measurement-only)

#### Scenario: Post-implementation run asserts improvement targets
- **WHEN** the benchmark test is run after full implementation
- **THEN** it SHALL compare results against the stored baseline
- **THEN** it SHALL assert each metric meets or exceeds its target (defined in `benchmark-design.md` §7)
- **THEN** it SHALL fail the test if any regression gate is violated

### Requirement: Test corpus is fixed and reproducible
The benchmark test corpus (memory set, query set, contradiction pairs) SHALL be deterministic and version-controlled so baseline and post-implementation runs are comparable.

#### Scenario: Same corpus used for baseline and post-implementation
- **WHEN** the benchmark is run at any point in time
- **THEN** the same 50 synthetic memories, 40 queries, 20 contradiction pairs, and 10 temporal pairs SHALL be seeded
- **THEN** memory embeddings SHALL be computed fresh (not stored) to avoid embedding model drift

#### Scenario: Corpus covers all 6 memory categories
- **WHEN** the corpus is seeded
- **THEN** it SHALL contain memories for each category: `preference`, `pattern`, `correction`, `fact`, `instruction`, `convention`
- **THEN** it SHALL contain memories with both recent and stale `event_time` values for temporal testing

### Requirement: M1 core memory hit rate is measured
The benchmark SHALL measure what fraction of chat sessions include core memories in the system prompt without an explicit `recall_memories` tool call.

#### Scenario: M1 measured as 0% on passive system
- **WHEN** M1 is measured against the passive system (no core injection)
- **THEN** the result SHALL be 0.0 (no sessions inject core memories automatically)

#### Scenario: M1 measured as 100% after core memory implementation
- **WHEN** M1 is measured after core memory tier implementation
- **AND** the test user has ≥ 1 core-tier memory
- **THEN** the result SHALL be 1.0 (all sessions inject core memories)

### Requirement: M2 dedup precision is measured against fixed contradiction pairs
The benchmark SHALL evaluate the dedup/merge decision against 20 hand-crafted test pairs with known ground-truth resolution actions.

#### Scenario: M2 baseline captures threshold-only dedup behavior
- **WHEN** M2 is measured against the passive system (threshold-based dedup)
- **THEN** it SHALL record the fraction of 20 test cases where the threshold decision matches ground truth
- **THEN** the expected baseline SHALL be approximately 0.40 (threshold handles NOOP but fails on contradictions)

#### Scenario: M2 post-implementation captures LLM merge quality
- **WHEN** M2 is measured after LLM merge implementation
- **THEN** it SHALL call the LLM merge decision for each test pair
- **THEN** it SHALL compare the LLM's decision against the ground truth action
- **THEN** the target SHALL be ≥ 0.85

### Requirement: M3 recall precision is measured with LLM-as-judge relevance rating
The benchmark SHALL measure recall precision@5 and precision@10 using an LLM to judge relevance of each returned memory for each query.

#### Scenario: LLM judge rates each returned memory as relevant or not
- **WHEN** M3 is measured for a given query
- **THEN** the top-5 and top-10 recalled memories SHALL each be rated by an LLM judge (0=not relevant, 1=relevant)
- **THEN** precision@K = sum(relevance scores) / K

#### Scenario: M3 measured across all 4 intent types separately
- **WHEN** M3 is measured
- **THEN** precision SHALL be reported separately for PREFERENCE, CHRONOLOGICAL, FACTUAL, and INSTRUCTIONAL query sets
- **THEN** the aggregate and per-type scores SHALL both be recorded

### Requirement: M4 recency ranking is measured with paired temporal memories
The benchmark SHALL measure whether newer memories rank above older ones when both have identical semantic similarity to a query.

#### Scenario: Recency ranking measured with age-only difference
- **WHEN** M4 is measured
- **THEN** for each of 10 test pairs, both memories SHALL have cosine similarity within 0.02 of each other to the test query
- **THEN** the benchmark SHALL record whether the memory with smaller `age_days` ranks higher
- **THEN** the baseline (no score decay) SHALL be approximately 0.50

### Requirement: M7 and M8 latency benchmarks capture p50 and p95
The benchmark SHALL record latency percentiles for both `recall_memories` and `save_memory` under realistic load conditions.

#### Scenario: M7 recall latency measured with 100 calls and 200-memory corpus
- **WHEN** M7 is measured
- **THEN** 100 `recall_memories` calls SHALL be made against a corpus of 200 memories per user
- **THEN** p50 and p95 wall-clock latency SHALL be recorded in milliseconds
- **THEN** calls SHALL be made sequentially (not concurrent) to measure single-request latency

#### Scenario: M8 save latency measured separately for fast and merge paths
- **WHEN** M8 is measured
- **THEN** 50 calls with no similar existing memory (fast path) SHALL be timed separately from
- **THEN** 50 calls with a known similar memory (merge path triggers) SHALL be timed separately
- **THEN** both p50 and p95 SHALL be recorded for each path
