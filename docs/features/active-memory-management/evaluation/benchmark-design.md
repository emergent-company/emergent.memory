# Active Memory Management — Evaluation & Benchmark Design

**Date:** 2026-03-18
**Status:** Pre-implementation baseline definition
**Purpose:** Define what "better" means before we build, so we can prove improvement after.

---

## 1. What We're Improving

The active memory management change targets six concrete failure modes of the passive system. Each maps to a measurable metric:

| Failure Mode | Root Cause | Metric to Fix |
|---|---|---|
| Agent ignores past preferences | No guaranteed recall injection | **Core Memory Hit Rate** |
| Contradictory memories coexist | Fixed cosine threshold can't detect contradiction | **Dedup Precision** |
| Relevant memories not surfaced | Flat hybrid search, no query-type awareness | **Recall Precision@K** |
| Stale memories dominate context | No decay, old facts rank equally with new | **Recency Ranking Score** |
| Token budget wasted on related memories | No compression/summarization | **Token Efficiency** |
| Temporal facts resolved incorrectly | No event_time, only ingestion time | **Temporal Accuracy** |

---

## 2. Metrics Definitions

### M1 — Core Memory Hit Rate
**What it measures:** % of chat sessions where core memories appear in the system prompt without the LLM calling `recall_memories`.

```
hit_rate = sessions_with_core_injection / total_sessions_with_core_memories
```

**Baseline (passive system):** 0% (no injection mechanism exists)
**Target:** 100% (deterministic — synchronous injection at session start)
**Test type:** Unit / integration

---

### M2 — Dedup Precision
**What it measures:** Given a set of test memory pairs with known correct resolution (ADD, UPDATE, DELETE_OLD, NOOP), % of cases where the system makes the correct decision.

```
precision = correct_decisions / total_test_cases
```

**Test cases (20 hand-crafted pairs):**
- 5 × clear contradiction ("User in NY" vs "User in London") → DELETE_OLD
- 5 × additive update ("User likes Python" vs "User likes Python, especially type hints") → UPDATE
- 5 × genuinely distinct ("User uses tabs" vs "User prefers dark mode") → ADD
- 5 × redundant restatement ("User prefers functional style" vs "User likes functional programming") → NOOP

**Baseline (passive system — threshold only):** ~40% (threshold dedup handles NOOP correctly but always fails on contradiction and merge)
**Target:** ≥ 85%
**Test type:** Unit with fixed test vectors

---

### M3 — Recall Precision@K (K=5, K=10)
**What it measures:** Given a query, what fraction of the top-K recalled memories are actually relevant? Uses LLM-as-judge to rate relevance (0/1 binary per memory).

```
precision@K = relevant_memories_in_top_K / K
```

**Test scenarios:**
- 10 × preference queries ("what are the user's TypeScript preferences?")
- 10 × chronological queries ("what did we discuss last week about auth?")
- 10 × factual queries ("what is the user's preferred test framework?")
- 10 × instructional queries ("how should I handle errors?")

Fixed memory corpus of 50 memories per test user (mix of relevant and distractors).

**Baseline:** Run against current hybrid search (no intent-aware, no score decay)
**Target:** +15% improvement on chronological queries; +10% on factual queries
**Test type:** Integration with LLM judge

---

### M4 — Recency Ranking Score
**What it measures:** For queries with both old and recent versions of the same fact, does the recent version rank higher?

```
recency_score = cases_where_recent_ranks_above_old / total_temporal_pairs
```

**Test cases (10 pairs):**
- Same fact, two versions: recent (30 days old) vs stale (365 days old)
- Semantic similarity identical; only age differs
- Measure rank position of each in recall results

**Baseline (passive system):** ~50% (random, no temporal weighting)
**Target:** ≥ 90%
**Test type:** Unit (deterministic given fixed embeddings and timestamps)

---

### M5 — Token Efficiency
**What it measures:** Average tokens consumed when injecting memory context into a prompt, before vs after reflection compression.

```
efficiency = tokens_with_active_management / tokens_with_passive_system
```

**Measurement conditions:**
- Fixed set of 20 memories across 4 topics (5 memories per topic)
- Passive: all 20 memories recalled and injected (worst case)
- Active: reflection job synthesizes 4 MemoryContext summaries; recall returns 4 summaries (best case)
- Measure token counts of injected text

**Baseline:** N memories × avg_memory_tokens (full expansion)
**Target:** ≤ 40% of baseline tokens for memory clusters (≥ 60% reduction after reflection)
**Test type:** Integration (requires reflection job to have run)

---

### M6 — Temporal Accuracy
**What it measures:** When two memories describe the same fact at different points in time, does the system correctly resolve to the more recent event?

```
temporal_accuracy = correct_temporal_resolutions / total_temporal_test_cases
```

**Test cases (10 pairs with known event_times):**
- "User lives in New York" (event_time: 2023-06) vs "User moved to London" (event_time: 2026-01)
- "User prefers React" (event_time: 2024-01) vs "User switched to Vue" (event_time: 2025-06)
- etc.

**Baseline (passive system):** ~50% (no event_time, arbitrary resolution)
**Target:** ≥ 90%
**Test type:** Integration

---

### M7 — End-to-End Recall Latency (p50, p95)
**What it measures:** Wall-clock time from `recall_memories` call to response, under realistic load.

**Measurement conditions:**
- 100 recall calls with corpus of 200 memories per user
- Measure: p50, p95, p99 latency
- Compare: passive (pure hybrid search) vs active (hybrid + score decay + optional intent classification)

**Baseline:** Measure passive system
**Target:** p95 regression ≤ 50ms for score decay alone; ≤ 150ms when intent classification enabled
**Test type:** Performance / load test

---

### M8 — save_memory Latency with LLM Merge (p50, p95)
**What it measures:** Overhead introduced by the LLM merge decision call in `save_memory`.

**Measurement conditions:**
- 50 save calls where similar memory exists (merge call triggers)
- 50 save calls where no similar memory exists (fast path)
- Measure both paths separately

**Baseline:** Passive `save_memory` latency (no merge call)
**Target:** Fast path regression ≤ 5ms; merge path overhead ≤ 600ms p95 (including LLM call)
**Test type:** Performance

---

## 3. Reference Benchmarks (External)

To situate our improvements relative to the broader field:

| Benchmark | What It Tests | Leaders (2026) |
|---|---|---|
| **LOCOMO** | Long-context memory recall accuracy | mem0 (+26% vs OpenAI Memory) |
| **LongMemEval** | Long-horizon fact tracking with temporal updates | Supermemory (#1) |
| **LoCoMo** | Conversational memory across 300+ turn sessions | Supermemory (#1) |
| **ConvoMem** | Contradiction detection and resolution | Supermemory (#1) |

We will NOT run these benchmarks directly (they require specific datasets and evaluation harnesses). Instead, our M2 (Dedup Precision) and M6 (Temporal Accuracy) are local proxies for LongMemEval and ConvoMem respectively.

---

## 4. Test Data Design

### 4.1 Synthetic Memory Corpus

A fixed set of 50 memories per test user, covering all 6 categories:

```
Preferences (10):
  - "User prefers TypeScript over JavaScript"
  - "User uses single quotes in all code"
  - "User prefers functional programming style"
  - "User dislikes ORMs, prefers raw SQL"
  - ...

Patterns (8):
  - "User consistently writes tests before implementation"
  - "User always requests code review before merging"
  ...

Corrections (5):
  - "User requires apperror.Error for all API errors — do NOT use plain errors"
  - ...

Facts (10):
  - "Project uses PostgreSQL 16 with pgvector"
  - "API runs on port 8080 in development"
  ...

Instructions (7):
  - "Always run golangci-lint before suggesting code changes"
  - ...

Conventions (10):
  - "Database service pattern: never access DB directly from handlers"
  - ...
```

### 4.2 Query Set (40 queries, 10 per intent type)

```
PREFERENCE queries (10):
  - "What coding style does the user prefer?"
  - "What database does the user prefer?"
  ...

CHRONOLOGICAL queries (10, require event_time in memories):
  - "What framework was the user using before switching?"
  - "What did we discuss last month about the auth system?"
  ...

FACTUAL queries (10):
  - "What port does the API run on?"
  - "What database version is the project using?"
  ...

INSTRUCTIONAL queries (10):
  - "How should I handle errors in this codebase?"
  - "What linting checks should I run?"
  ...
```

### 4.3 Contradiction Test Pairs (20 pairs)

Pre-built pairs with known ground-truth resolution action:

```go
type ContradictionTestCase struct {
    MemoryA     string
    MemoryB     string  // newer
    EventTimeA  time.Time
    EventTimeB  time.Time
    GroundTruth MergeAction  // ADD | UPDATE | DELETE_OLD_ADD_NEW | NOOP
    Rationale   string
}
```

---

## 5. Measurement Plan

### Phase 0: Baseline (BEFORE implementation)
Run all metrics against the current passive system. Record as baseline.

```
baseline_run:
  date: <before implementation starts>
  M1_hit_rate: 0% (expected — no injection exists)
  M2_dedup_precision: measure with threshold-only dedup
  M3_recall_precision@5: measure with current hybrid search
  M3_recall_precision@10: measure
  M4_recency_ranking: measure (expected ~50%)
  M5_token_efficiency: N/A (no reflection yet, record raw token count)
  M6_temporal_accuracy: measure (expected ~50%)
  M7_latency_p50: measure
  M7_latency_p95: measure
  M8_save_latency_fast_path: measure
```

### Phase 1: Post-Core-Memory + LLM-Merge (after phases 1+2)
Re-run M1, M2, M3, M7, M8.

### Phase 2: Post-Temporal + Score Decay (after phases 3+4)
Re-run M4, M6, M7.

### Phase 3: Post-Reflection (after phase 5)
Re-run M3, M5.

### Phase 4: Full Post-Implementation
Re-run all metrics. Generate comparison report.

---

## 6. Evaluation Infrastructure

### 6.1 Test Fixture: `domain/mcp/testdata/memory_benchmark/`

```
testdata/memory_benchmark/
├── corpus.json          # 50 synthetic memories with all properties
├── queries.json         # 40 queries with intent labels and relevant_memory_ids
├── contradiction_pairs.json  # 20 contradiction test cases with ground truth
└── temporal_pairs.json  # 10 temporal test cases with event_times
```

### 6.2 Benchmark Runner: `domain/mcp/memory_benchmark_test.go`

A Go test file (build tag `//go:build benchmark`) that:
1. Seeds the test corpus into a test project
2. Runs each metric measurement
3. Outputs a JSON report to `testdata/memory_benchmark/results/YYYY-MM-DD.json`
4. Compares against the most recent baseline and asserts targets are met (post-implementation only)

```go
//go:build benchmark

func TestMemoryBenchmark(t *testing.T) {
    // Setup
    ctx, svc := setupBenchmarkContext(t)
    seedCorpus(t, ctx, svc, "testdata/memory_benchmark/corpus.json")

    // M1: Core Memory Hit Rate
    t.Run("M1_CoreMemoryHitRate", testCoreMemoryHitRate)

    // M2: Dedup Precision
    t.Run("M2_DedupPrecision", testDedupPrecision)

    // M3: Recall Precision@K
    t.Run("M3_RecallPrecision", testRecallPrecision)

    // M4: Recency Ranking
    t.Run("M4_RecencyRanking", testRecencyRanking)

    // M5: Token Efficiency
    t.Run("M5_TokenEfficiency", testTokenEfficiency)

    // M6: Temporal Accuracy
    t.Run("M6_TemporalAccuracy", testTemporalAccuracy)

    // M7: Recall Latency
    t.Run("M7_RecallLatency", testRecallLatency)

    // M8: SaveMemory Latency
    t.Run("M8_SaveMemoryLatency", testSaveMemoryLatency)
}
```

### 6.3 Results Format

```json
{
  "run_date": "2026-03-18",
  "phase": "baseline",
  "git_commit": "...",
  "metrics": {
    "M1_core_hit_rate": 0.0,
    "M2_dedup_precision": 0.42,
    "M3_recall_precision_at_5": 0.61,
    "M3_recall_precision_at_10": 0.54,
    "M4_recency_ranking": 0.51,
    "M5_token_efficiency": 1.0,
    "M6_temporal_accuracy": 0.50,
    "M7_latency_p50_ms": 45,
    "M7_latency_p95_ms": 120,
    "M8_save_fast_path_p95_ms": 30,
    "M8_save_merge_path_p95_ms": null
  },
  "targets": {
    "M1": 1.0,
    "M2": 0.85,
    "M3_at_5": "baseline + 0.10",
    "M4": 0.90,
    "M5": 0.40,
    "M6": 0.90,
    "M7_p95": "baseline + 50ms",
    "M8_merge_p95": 600
  }
}
```

---

## 7. Success Criteria (Gate for Shipping)

All of the following must pass before the feature is considered complete:

| Metric | Gate |
|---|---|
| M1 Core Hit Rate | = 100% |
| M2 Dedup Precision | ≥ 85% |
| M3 Recall Precision@5 | ≥ baseline + 10% |
| M4 Recency Ranking | ≥ 90% |
| M5 Token Efficiency | ≤ 40% of baseline |
| M6 Temporal Accuracy | ≥ 90% |
| M7 Recall p95 Latency | ≤ baseline + 50ms (score decay only) |
| M8 Save merge p95 | ≤ 600ms |

**Regression gates** (must not worsen):
- M7 fast-path recall latency: ≤ baseline + 10ms
- M8 save fast-path latency: ≤ baseline + 5ms (MD5 hash check overhead)

---

## 8. What We Are NOT Measuring (and Why)

| Metric | Reason for exclusion |
|---|---|
| User satisfaction / subjective quality | No user study infrastructure; defer to qualitative feedback |
| Memory accuracy on LOCOMO/LongMemEval | Requires external dataset download and evaluation harness; our proxy metrics (M2, M6) are sufficient for v1 |
| Reflection job quality | Too subjective (LLM synthesis quality); covered indirectly by M5 and M3 |
| Cost per session | Requires cost attribution per LLM call; deferred to observability dashboard |
| Cross-user isolation correctness | Covered by existing RLS tests; not a memory-specific metric |
