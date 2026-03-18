# Active Memory Management — Research Report

**Date:** 2026-03-17
**Status:** Complete
**Related feature:** `docs/features/agent-memory-design.md`

---

## 1. Overview

This document surveys open-source projects and academic publications on **active memory management** for AI agents — where the system participates in deciding what gets stored, merged, promoted, or discarded rather than passively accumulating everything it encounters.

The existing `agent-memory-design.md` describes a passive approach: the LLM is instructed when to call `save_memory` / `recall_memories`, and dedup is handled by a fixed cosine similarity threshold. Active memory management replaces or augments this with LLM-in-the-loop decision making, multi-tier promotion, proactive reflection, and decay.

---

## 2. Sources Surveyed

| Source | Type | Key Contribution |
|---|---|---|
| mem0 (github.com/mem0ai/mem0) | Open source | LLM-assisted dedup/merge, scalable production memory |
| Letta / MemGPT (github.com/letta-ai/letta) | Open source | Virtual context management, memory tiers, agentic memory tools |
| MemGPT paper (arXiv:2310.08560) | Academic | OS-inspired hierarchical memory, interrupt-based control flow |
| Memory Survey (arXiv:2404.13501) | Academic | Systematic taxonomy of memory mechanisms in LLM agents |
| mem0 paper (arXiv:2504.19413) | Academic | Production-ready scalable long-term memory |
| NVIDIA NemoClaw / NeMo Agent Toolkit | Open source | Agent observability, profiling, optimization for memory pipelines |

---

## 3. mem0 — LLM-Assisted Memory Operations

### Architecture

mem0 positions itself as a drop-in "memory layer" for any LLM application. Its core insight is that **distilled, semantically compressed memories** dramatically outperform passing full conversation history:

- **+26% accuracy** vs OpenAI Memory (LOCOMO benchmark)
- **91% faster** than full-context approaches
- **90% fewer tokens** than full-context
- ~2% additional improvement with the graph store variant

### Memory Type Classification (`mem0/configs/enums.py`)

```python
class MemoryType(Enum):
    SEMANTIC   = "semantic_memory"    # General factual knowledge: preferences, profile, relationships
    EPISODIC   = "episodic_memory"    # Specific past events and interactions (default)
    PROCEDURAL = "procedural_memory"  # Agent execution traces (verbatim step-by-step actions + results)
```

Memory is scoped by session identifiers: `user_id`, `agent_id`, `run_id` (at least one required).

### Storage Architecture — Three Stores

**Primary: Vector store** (Qdrant default; also FAISS, Pinecone, Weaviate, Redis):
- Each item: `{id: UUID, data: str, hash: MD5, created_at, updated_at, user_id, agent_id, run_id, actor_id, role}`
- Optional reranker pass (`RerankerFactory`)

**Secondary: Graph store** (Neo4j via `langchain_neo4j`):
- Entity nodes and relationship edges: `source -- RELATIONSHIP -- destination`
- Indexed on `user_id` and composite `(name, user_id)`
- BM25 reranking on `(source, relationship, destination)` triples
- Vector and graph writes executed **concurrently** via `ThreadPoolExecutor`

**Tertiary: SQLite** (`SQLiteManager`) — audit log of all memory operations

### LLM-in-the-Loop Dedup Pipeline — Two Stages

**Stage 1 — Fact Extraction** (`USER_MEMORY_EXTRACTION_PROMPT` / `AGENT_MEMORY_EXTRACTION_PROMPT`):
- Extracts atomic facts from conversation: `{"facts": ["Name is John", "Is a software engineer"]}`
- User facts sourced from user messages only; agent facts from assistant messages only
- Response format: `json_object`

**Stage 2 — Memory Decision** (`DEFAULT_UPDATE_MEMORY_PROMPT`):
1. For each extracted fact, embed and vector-search → top-5 most similar existing memories
2. Deduplicate retrieved memories by ID
3. **Map UUIDs to integers** to prevent LLM hallucination of fake IDs
4. Single LLM call with all retrieved memories + all new facts
5. LLM emits: `{"memory": [{id, text, event, old_memory?}]}`

Decision events:
- `ADD` — new fact, create with new UUID
- `UPDATE` — rewrite existing memory with merged/refined content, preserve original ID, record `old_memory` for history
- `DELETE` — contradicting/superseded fact, remove from vector store
- `NONE` — already present or semantically equivalent, no change

**Merge rule** (from source prompt): "if the memory contains 'User likes to play cricket' and the retrieved fact is 'Loves to play cricket with friends', UPDATE (more specific). But if the memory contains 'Likes cheese pizza' and the retrieved fact is 'Loves cheese pizza', do NOT update (same information)."

### Graph Dedup (separate prompts)

- `UPDATE_GRAPH_PROMPT`: identifies contradicting relationships (same source+destination, differing relationship type) and updates them
- `DELETE_RELATIONS_SYSTEM_PROMPT`: deletes only when new info is more recent/accurate or directly contradicts; does NOT delete when the same relationship type could have a different valid destination
- Entity extraction → `EXTRACT_ENTITIES_TOOL` → relationship establishment → `RELATIONS_TOOL` → delete contradicted edges → add new triples

### Memory Decay / Forgetting

No explicit decay. Forgetting is LLM-directed via `DELETE` events when new facts contradict stored ones. The graph deletion prompt includes: *"Temporal Awareness: If timestamps are available, consider the recency of information when making updates."*

### Proactive vs. Reactive

**Reactive only** in the OSS library — `memory.add()` is called explicitly after each conversation turn. No background consolidation process in open source (may exist in hosted platform).

---

## 4. Letta / MemGPT — Virtual Context Management

### Core Concept

Letta (formerly MemGPT) treats the LLM's context window as a CPU register file and applies OS-style virtual memory principles:

```
Context window  ←→  CPU L1/L2 cache (fast, small, always visible)
Recall storage  ←→  RAM (medium speed, conversation history DB)
Archival storage ←→  Disk (slow, unlimited, semantic vector store)
```

The agent **actively manages paging** between tiers using tool calls — promoting memories from archival to context when needed, evicting stale content when context fills up.

### Memory Tiers (3 tiers)

**Tier 1 — Main Context Window (in-context, always visible)**:
- Core memory blocks: named, size-limited free-text slots (`persona`, `human`, user-defined)
- Recent conversation messages (truncated as needed via summarization)

**Tier 2 — Recall Storage (out-of-context, searchable)**:
- Full conversation history persisted to DB
- Hybrid search: text matching + semantic similarity
- Filterable by role, date range, free-text

**Tier 3 — Archival Memory (out-of-context, infinite, semantic)**:
- Long-term facts, summaries, project notes
- Vector-embedded, ranked by semantic similarity
- Supports tags with `any` / `all` match modes

### Exact Tool Function Signatures (from `letta/functions/function_sets/base.py`)

```python
core_memory_append(agent_state, label: str, content: str) -> str
core_memory_replace(agent_state, label: str, old_content: str, new_content: str) -> str
memory_rethink(agent_state, label: str, new_memory: str) -> str   # full block rewrite
memory_finish_edits(agent_state) -> None
archival_memory_insert(self: Agent, content: str, tags: Optional[list[str]]) -> str
archival_memory_search(self: Agent, query: str, tags, tag_match_mode, top_k, start_datetime, end_datetime) -> str
conversation_search(self: Agent, query, roles, limit, start_date, end_date) -> str
```

### Control Flow: Interrupts and Heartbeats

Two interrupt types in the original system:
- **User interrupt**: triggered when a user sends a message
- **Heartbeat interrupt**: allows the agent to chain tool calls without waiting for the user — after calling a tool, the agent requests a heartbeat to continue reasoning before yielding control

To yield control: end response without calling a tool. To continue: call another tool. This is core to supporting multi-step memory management within a single agent turn.

### Context Overflow: Summarization (not naive FIFO truncation)

When context pressure grows, Letta uses LLM-driven summarization:
- **Sliding window** (`SLIDING_PROMPT`): evicts oldest messages, generates a structured ~300-word summary preserving: high-level goals, what happened, important details, errors/fixes, and **lookup hints** for future `conversation_search`
- **Full-context** (`ALL_PROMPT`): comprehensive summary including current state and next step

### Sleep-Time Memory Consolidation (key proactive feature)

A background `Letta-Sleeptime-Memory` agent runs **between conversations** (not during) to consolidate and update core memory blocks:

1. Receives recent conversation history
2. Reads current core memory blocks
3. Iteratively calls `memory_replace`, `memory_insert`, `memory_rethink` to update blocks
4. Calls `memory_finish_edits` when done

Prompt instructions: *"Not every observation warrants a memory edit, be selective in your memory editing, but also aim to have high recall."* And: *"do not contain redundant and outdated information."* Uses absolute dates, not relative ("today").

This is mem0's proactive gap addressed: Letta's sleep-time agent is the real mechanism for proactive consolidation. Mem0 OSS has no equivalent.

### Memory Blocks in API

```python
agent = client.agents.create(
    memory_blocks=[
        {"label": "human",   "value": "Name: Alice. Preferences: ..."},
        {"label": "persona",  "value": "I am a coding assistant. I know Alice prefers..."},
    ]
)
```

---

## 5. MemGPT Paper (arXiv:2310.08560) — Theoretical Foundation

**Abstract excerpt**: *"Large language models (LLMs) have revolutionized AI, but are constrained by limited context windows, hindering their utility in tasks like extended conversations and document analysis. To enable using context beyond limited context windows, we propose virtual context management, a technique drawing inspiration from hierarchical memory systems in traditional operating systems that provide the appearance of large memory resources through data movement between fast and slow memory."*

### Key Principles

1. **Hierarchical memory**: Fast (in-context) + slow (external storage) with explicit paging
2. **Interrupt-based control flow**: Agent can interrupt mid-reasoning to execute memory operations before continuing
3. **Transparent to the LLM**: The LLM sees an apparently unlimited context; the system manages the illusion
4. **Two domains demonstrated**: Document analysis (documents > context window) and multi-session chat

---

## 6. Memory Survey (arXiv:2404.13501) — Systematic Taxonomy

### Memory Type Taxonomy

```
┌─────────────────────────────────────────────────────────────┐
│                    LLM Agent Memory                          │
├─────────────────┬──────────────────┬────────────────────────┤
│  Sensory Memory  │  Short-Term /    │  Long-Term Memory      │
│  (immediate      │  Working Memory  │  (persistent, external)│
│  perceptual      │  (in-context)    │                        │
│  input)          │                  │                        │
└─────────────────┴──────────────────┴────────────────────────┘
```

### Storage Modalities

| Modality | Where | Speed | Capacity | Updatable |
|---|---|---|---|---|
| In-context | LLM context window | Instant | Very limited | Via rewriting |
| In-weights | Model parameters | Very slow (training) | Large | Only via fine-tuning |
| In-cache | KV cache | Fast | Session-limited | No |
| In-external | Vector DB / graph / SQL | Medium | Unlimited | Yes |

### Operations Taxonomy

The survey identifies four fundamental memory operations:

1. **Write** (storage): When and how to persist new information
2. **Read** (retrieval): How to surface relevant stored information
3. **Reflect** (distillation): Summarizing or synthesizing stored memories
4. **Forget** (pruning): Removing or decaying stale/irrelevant memories

### Retrieval Strategies

- **Exact match**: Key-based lookup
- **Semantic search**: Vector similarity (cosine, dot product)
- **Temporal**: Recency-weighted retrieval
- **Frequency**: Usage-count-weighted retrieval
- **Hybrid**: Combining multiple signals (most production systems)

### Forgetting Mechanisms

- **LRU (Least Recently Used)**: Discard memories not accessed in N days
- **Confidence decay**: Reduce confidence score over time for non-recalled memories
- **Explicit deletion**: Agent or user manually removes
- **Supersession**: New memory replaces old one (version chain)
- **Threshold pruning**: Remove memories below minimum confidence

### Multi-Granularity Storage

The survey notes that best-performing systems store memories at **multiple levels of abstraction**:
- Raw events (high detail, low compression)
- Distilled summaries (low detail, high compression)
- Pattern/convention abstractions (generalized rules from specific events)

---

## 6b. EvoAgent (arXiv:2406.14228) — Not a Memory Paper

This paper is about automatically extending agents to multi-agent systems via evolutionary algorithms (mutation, crossover, selection on agent populations). It does not address memory management and is not relevant to this research area.

---

## 7. mem0 Paper (arXiv:2504.19413) — Production Insights

### Scalability Approach

The mem0 paper describes how to maintain memory quality at scale (thousands of memories per user):

1. **Selective extraction**: Not every conversation turn deserves a memory — use LLM to decide what's worth storing
2. **Hierarchical dedup**: First check for exact/near-exact duplicates (hash), then semantic similarity, then LLM judgment
3. **Memory compression**: Periodically consolidate related memories into higher-level abstractions
4. **User feedback loop**: Track which recalled memories were "useful" (used in generation) vs "ignored"

### Metrics from Production

- Memory recall rate: % of relevant memories surfaced vs total available
- Precision: % of recalled memories that were actually useful
- Memory density: Information per stored memory (higher = better compression)
- Recall latency: End-to-end time from query to injected memories

---

## 8. NVIDIA NeMo Agent Toolkit — Observability Insights

The NeMo toolkit provides profiling infrastructure relevant to memory system optimization:

- **Per-step latency tracking**: Identifies whether memory recall operations are adding latency
- **Token-level profiling**: Measures how much of the context window memories occupy
- **RL fine-tuning hooks**: Could train a model to make better save/recall decisions based on outcome signals
- **Prompt optimizer**: Auto-tunes memory extraction and dedup prompts for accuracy

Key insight: Memory systems need **observability infrastructure** to close the optimization loop — without knowing which memories were useful, threshold and decay parameters are set by intuition rather than data.

---

## 9. Cross-Cutting Comparison (from Deep Research)

### Memory Taxonomy Comparison

| Dimension | MemGPT/Letta | Mem0 | Survey |
|---|---|---|---|
| Primary taxonomy | Tier-based (core/recall/archival) | Type-based (semantic/episodic/procedural) | Source/form/operation |
| Working memory | Core blocks (in-context, size-limited) | None | In-trial memory |
| Long-term | Archival (vector, infinite) | Vector store + graph store | Cross-trial memory |
| Episodic | Recall storage (conversation history) | Episodic memory type | Experience memory |
| Semantic | No explicit type | Semantic memory type | Knowledge abstraction |
| Procedural | Sleep-time execution traces | Explicit PROCEDURAL type (verbatim traces) | Behavioral patterns |

### Dedup / Merge Comparison

| System | Approach |
|---|---|
| **Mem0** | Two-stage LLM pipeline: extract facts → vector-search top-5 similar → single LLM call emits ADD/UPDATE/DELETE/NONE per memory. UUID-to-integer mapping prevents hallucination. Merge rule: keep more specific/informative version. |
| **Mem0 Graph** | Separate `UPDATE_GRAPH_PROMPT` and `DELETE_RELATIONS_SYSTEM_PROMPT` for relationship-level dedup. Temporal awareness: recency determines which version wins. |
| **Letta** | Agent self-directed via `memory_rethink` / `core_memory_replace`. Sleep-time agent explicitly consolidates between sessions. No automated pipeline during conversations. |

### Proactive vs. Reactive

| Mode | Letta | Mem0 OSS |
|---|---|---|
| Reactive | Agent writes to archival during conversation | `memory.add()` called after each turn |
| **Proactive** | **Sleep-time background agent consolidates core memory between conversations** | Not present (hosted platform only) |

The sleep-time agent is Letta's most distinctive contribution and has no equivalent in mem0 OSS. It directly maps to the "memory reflection job" concept in this feature.

### What Neither System Has

- Explicit confidence decay / LRU-based forgetting (both rely on LLM-directed deletion for forgetting)
- Recency-weighted retrieval scoring applied at query time (neither ranks recent memories higher in search results)
- Bi-temporal tracking (`event_time` + `ingestion_time`)
- Access-frequency tracking for decay decisions
- A graph knowledge base integrated with agent memory (mem0's graph is for entity relationships, not project knowledge)
- Intent-aware retrieval (query type classification before search execution)

---

## 9b. 2026 Architectural Blueprint — Additional Concepts

These concepts extend the SOTA analysis above with three ideas not present in mem0 or Letta OSS.

### Bi-Temporal Memory Tracking

Track two independent timestamps per memory:

| Field | Meaning | Use |
|---|---|---|
| `event_time` | When the described event actually happened ("User moved to London") | Temporal queries: "what did I know about X last month?" |
| `ingestion_time` | When the agent learned it | Staleness: how long ago was this fact captured? |

This distinction matters for contradiction resolution: a memory with `event_time=2024-01` describing an old state is not the same as a memory with `event_time=2026-03` describing the current state. Neither should simply overwrite the other — the agent needs to understand *when* each was true.

**Current gap in our design**: We only track `created_at` (ingestion time). We have no `event_time` field, so temporal contradiction resolution is impossible.

### Retrieval Score Decay (Applied at Query Time)

Instead of (or in addition to) a batch confidence decay job, apply an exponential decay multiplier to the retrieval relevance score at query time:

$$S_{final} = S_{semantic} \cdot e^{-\lambda(t_{now} - t_{created})}$$

Where:
- `S_semantic` = cosine similarity score from vector search (0–1)
- `λ` = decay constant, controlling half-life (e.g., `λ=0.01` → half-life ~70 days; `λ=0.001` → ~700 days)
- `(t_now - t_created)` = age of memory in days

This is **fundamentally different** from the batch decay job:
- Batch decay modifies stored `confidence` permanently (irreversible without recall)
- Score decay is computed at retrieval time — it's a query-time re-ranking that doesn't modify stored data
- Allows per-category `λ` values: `correction` memories could decay slowly (λ=0.001), `fact` memories faster (λ=0.01)

**Relationship to existing design**: The batch decay job and score-based decay are complementary. Batch decay manages long-term storage quality; score decay ensures recently relevant memories rank higher in every retrieval, even before the batch job runs.

**Reference**: Zep's Graphiti framework; Generative Agents paper (arXiv:2304.03442) uses a similar recency score component in their weighted retrieval formula.

### Intent-Aware Retrieval

Before executing a vector search, classify the query's intent to select the optimal retrieval strategy:

```
Query: "How did I solve that memory leak last month?"
  ↓
Intent Classifier:
  → type: CHRONOLOGICAL + ANALYTICAL
  → entities: ["memory leak", "bug", "solution"]
  → time_filter: last 30 days
  → strategy: keyword match on entities + date-range filter + semantic search
```

Query intent types:

| Type | Description | Strategy |
|---|---|---|
| `PREFERENCE` | "What does the user like/prefer?" | Semantic search, filter `category=preference` |
| `CHRONOLOGICAL` | "What happened last week/month?" | Date-range filter + keyword |
| `FACTUAL` | "What is the current value of X?" | Keyword-first, high-recency weight |
| `ANALYTICAL` | "How/why did something happen?" | Full hybrid, include related contexts |
| `INSTRUCTIONAL` | "How should I do X?" | Filter `category=instruction,convention` first |

**Current gap**: `recall_memories` today uses a single hybrid search strategy for all query types. Intent-aware retrieval would produce better precision for temporal and factual queries where vector similarity alone is noisy.

**Implementation note**: Intent classification can be a cheap LLM call (or a small classifier model) that runs before the main search. The retrieval plan then drives query construction.

---

## 10. Synthesis: Active vs Passive Memory Management

### Passive Memory (current `agent-memory-design.md`)

```
User message → LLM decides to call save_memory → Threshold-based dedup → Store
User task → LLM decides to call recall_memories → Hybrid search → Inject
```

**Limitations:**
- LLM compliance: relies on LLM following system prompt instructions
- Binary dedup: 0.85 threshold catches exact duplicates but misses contradictions/merges
- No proactive injection: context from past sessions only available if LLM remembers to ask
- No decay: memories accumulate indefinitely with no quality management
- No reflection: no mechanism to synthesize higher-level patterns from specific memories

### Active Memory Management

```
Any interaction → System evaluates what to extract → LLM decides ADD/UPDATE/DELETE/NOOP
                ↓
Session start → System injects top-N core memories automatically (no LLM call needed)
                ↓
Scheduled job → LLM reflects on memory clusters → Synthesizes patterns → Creates summaries
                ↓
Decay job     → Confidence decays for non-recalled memories → Low-confidence flagged for review
```

**Key differences:**
1. **LLM-in-the-loop dedup**: Not just threshold, but semantic reasoning about memory relationships
2. **Proactive injection**: Core memories always present, not dependent on LLM asking
3. **Reflection**: System generates new memories by synthesizing existing ones
4. **Decay**: Memories have a lifecycle, not just an accumulation pattern

---

## 10. Implementation Recommendations for Emergent

Based on this research, the following additions to the base `agent-memory-design.md` are recommended:

### Priority 1: LLM-Assisted Merge in `save_memory`

Replace the fixed 0.85 cosine threshold with:
1. Semantic search (top-3 similar memories)
2. LLM merge call with structured output: `{action: ADD|UPDATE|DELETE|NOOP, target_id?, new_content?}`
3. Apply mutation

**Why**: Handles contradictions, merges, and nuance that distance alone cannot. mem0's research shows this is the highest-ROI change.

### Priority 2: Core Memory Block (Proactive Injection)

Add a `core_memory` tier: top-N highest-priority memories injected into every session's system prompt automatically, without requiring an LLM tool call.

**Why**: Eliminates the largest reliability gap — LLM compliance with recall instructions.

### Priority 3: Reflection Job

A scheduled job that:
1. Clusters related Memory objects (by embedding similarity)
2. Calls LLM to synthesize a `MemoryContext` summary from the cluster
3. Creates `BELONGS_TO_CONTEXT` relationships

**Why**: Reduces token usage for recall and creates higher-value compressed insights.

### Priority 4: Confidence Decay

Scheduled job: reduce `confidence` by a multiplier (e.g., 0.95/week) for memories not recalled. Flag memories below 0.3 as `needs_review`.

**Why**: Prevents stale memories from dominating recall and polluting the agent's context.

### Priority 5: Memory Analytics

Track: recall rate, hit rate (recalled + used in response), miss rate (recalled but empty result), memory age distribution.

**Why**: Required to tune thresholds, decay rates, and core memory selection data-driven rather than by intuition.

---

## 11. OSS Ecosystem Survey (Round 2)

### 11.1 Microsoft GraphRAG

GraphRAG is not an agent memory system but its query architecture contains two highly novel ideas applicable to memory retrieval.

**Data structures:**
- `Entity` table: named entities with types, descriptions, embeddings
- `Relationship` table: typed directed edges with float `weight`
- `Community` table: hierarchical clusters indexed by `(level, cluster_id, parent_cluster)` — built using the **Leiden algorithm** (hierarchical graph clustering, not k-means)
- `CommunityReport`: LLM-generated structured summaries per community: `{title, summary, findings: [{summary, explanation}], rating: float}`

**Key novel ideas:**

**LLM-as-judge BFS community selection**: To answer a query, GraphRAG performs BFS traversal of the community hierarchy. At each level, it rates each community's relevance using an LLM judge (0-10 integer score, majority vote over `num_repeats` calls). Communities at or above a `threshold` are included; their children are enqueued. Up to 8 concurrent coroutines. This is **semantic routing through a pre-computed ontology** — fundamentally different from flat vector search.

**DRIFT Search**: A novel hybrid search mode. A **Primer** step generates intermediate answers and follow-up sub-queries from the original query against global community context. Then runs local entity-graph search per sub-query and merges via a `QueryState` accumulator. This is dynamic iterative deepening — the query expands as it discovers relevant context.

**Relevance to Emergent**: GraphRAG's community hierarchy is structurally identical to our `Memory → MemoryContext` two-level hierarchy. The LLM-as-judge BFS approach could be applied to memory context retrieval: instead of flat vector search across all Memory objects, traverse `MemoryContext` nodes first (LLM rates relevance), then pull in constituent memories from selected contexts. This is a higher-quality alternative to the current "cluster then retrieve" approach.

**Entity extraction gleaning loop**: Uses `CONTINUE_PROMPT` + `LOOP_PROMPT (Y/N)` to re-run extraction up to `max_gleanings` times, squeezing additional entities from each chunk. This prevents single-pass extraction misses.

---

### 11.2 OpenAI Agents Python SDK — Server-Delegated Compaction

The SDK's `OpenAIResponsesCompactionSession` introduces a novel compaction pattern:

1. After each conversation turn, check if compaction _would_ be needed (deferred check)
2. Mark `_deferred_response_id` but do NOT compact yet
3. On the next turn start, trigger actual compaction via `openai.responses.compact()` — a **server-side API** that summarizes conversation history without requiring a local LLM call
4. Replace the entire session history with the compacted output

**Incremental candidate tracking**: New items are appended to a `candidates` list on each `add_items()`. After compaction, the list resets to the compacted output's candidates. This avoids O(n) re-scans of full history to determine compaction need.

**Three compaction modes**: `previous_response_id` (server uses stored history), `input` (sends local items), `auto` (uses stored if available).

**Relevance to Emergent**: The deferred compaction pattern maps to our reflection job — instead of triggering synthesis immediately when a cluster forms, defer the LLM call to a low-traffic window and batch multiple clusters. The server-delegation idea (offload summarization to a lightweight model endpoint) maps to our summarizer model routing.

---

### 11.3 ChatDev 2.0 — Multi-Agent Blackboard + Multimodal Memory

**Three memory types:**

1. **BlackboardMemory** — shared append-log. `MemoryItem`: `{id, content_summary, metadata, embedding, timestamp, input_snapshot, output_snapshot}`. `retrieve()` returns last `top_k` by recency (no semantic search). Novel: `metadata` includes `attachment_overview()` — list of `{role, attachment_id, mime_type, name, size}` for multimodal block fidelity.

2. **SimpleMemory** — FAISS cosine similarity with **MD5 hash dedup**. Strips instruction prefixes before indexing (`_extract_key_content()` regex removes "Agent Role:", "You are...", etc). Max 3 sentences, 500 chars.

3. **FileMemory** — FAISS over files/dirs. **Incremental re-indexing**: stores per-file SHA hash; on load, only re-indexes changed or new files. Sentence-boundary chunking, 500 chars/50 char overlap.

**Relevance to Emergent**:
- MD5 hash dedup (exact match fast-path before semantic search) is a cheap pre-filter worth adding to `save_memory` — avoids the LLM merge call entirely for identical content
- The blackboard pattern maps directly to our planned cross-agent memory sharing scope
- Multimodal `MemoryContentSnapshot` is relevant if Emergent's extraction pipeline expands to handle image/file attachments in memory items

---

### 11.4 Letta / MemGPT (Deeper — Production Platform Details)

From the production codebase (beyond the README):

**Multi-block unified-diff patches** (`memory_apply_patch`): Accepts a superset of unified diff syntax with `*** Add Block:`, `*** Delete Block:`, `*** Update Block:`, `*** Move to:` headers. Allows the agent to restructure its entire core memory layout — create, delete, rename, and reorganize named blocks — in a single atomic tool call.

**Self-compaction**: `self_compact_all` mode uses the agent's own LLM (same cached key structure for KV cache compatibility) to summarize its history. No separate summarizer model needed. Protected messages: the last N messages kept verbatim (configurable `partial_evict_summarizer_percentage`); cutoff walked forward to first assistant message to avoid role alternation violations.

**Lightweight model routing**: Summarization defaults to a cheap model per provider (Haiku for Anthropic, GPT-5-mini for OpenAI, Gemini-2.5-flash for Google). Per-agent override supported.

**Tool-rule-enforced sleeptime sequences**: Sleeptime agents enforce tool call ordering via `ToolRulesSolver`:
1. `store_memories` (must run first — `InitToolRule`)
2. `rethink_user_memory` (can repeat — `ContinueToolRule`)
3. `finish_rethinking_memory` (must run last — `TerminalToolRule`)

**Tag-based archival with junction table**: `PassageTag` junction table for efficient `DISTINCT` tag queries at scale, alongside JSON column for flexibility.

**Relevance to Emergent**: The `ToolRulesSolver` pattern maps to our reflection job's LLM call sequence — enforce that the synthesis always starts with memory retrieval, then synthesis, then write. The lightweight model routing is directly applicable: our reflection job should use a cheap model (Haiku/Flash), not the full agent model.

---

### 11.5 Supermemory — Benchmark Leader

Supermemory claims #1 on three major AI memory benchmarks (LongMemEval, LoCoMo, ConvoMem) as of March 2026. The implementation is partially proprietary (server-side), but the OSS SDK reveals architecture:

**Memory graph**: Nodes + relationships organized into "spaces" (namespaces via `containerTags`). Visual graph explorer in the UI.

**Dual-mode user profile API**: Separate endpoint returning:
- Stable long-term facts (rarely changes)
- Recent activity context
Combined in < 50ms. Pre-materialized, not computed per-query.

**AST-aware code chunking**: Code files chunked at syntactic boundaries (function/class definitions) rather than character/sentence splits. This preserves semantic coherence — a function body is never split mid-implementation.

**Chunk-level filtering**: `chunkThreshold: 0.6` on search — chunks below similarity threshold are discarded before document-level reranking.

**Real-time connector webhooks**: Google Drive, Gmail, Notion, OneDrive, GitHub — automatic re-ingestion when source documents change.

**Relevance to Emergent**:
- The dual-mode user profile (stable facts + recent activity, sub-100ms) maps precisely to our Core Memory tier (stable, always injected) + Archival (on-demand). The sub-50ms latency claim validates that our synchronous core memory injection is feasible at scale.
- AST-aware chunking is directly applicable to extraction from code files in the knowledge graph
- Chunk-level threshold filtering (`chunkThreshold`) is worth adding to `recall_memories` to discard low-similarity chunks before returning

---

### 11.6 System Comparison Table (Complete)

| System | Storage | Dedup/Merge | Decay/Forget | Tiers | Proactive | Most Novel Idea |
|---|---|---|---|---|---|---|
| **mem0** | Vector DB + Neo4j + SQLite | Two-stage LLM (ADD/UPDATE/DELETE/NONE) | LLM-directed DELETE | None | None (OSS) | UUID→int mapping; dual-store concurrent writes |
| **Letta** | Core blocks + PG vector + history DB | Agent self-directed | None explicit | 3 | Sleeptime agent | Multi-block diff patches; tool-rule sequencing |
| **GraphRAG** | Entity/community/report hierarchy | LLM entity description merge | None (static) | Hierarchy | None | LLM-rated BFS traversal; DRIFT search |
| **OpenAI Agents** | SQLite append-log | None | None | None | None | Server-delegated compaction; deferred trigger |
| **ChatDev 2.0** | Blackboard + FAISS + file FAISS | MD5 hash + cosine dedup | `max_items` truncation | Per-type | None | Multi-agent blackboard; multimodal metadata |
| **Supermemory** | Memory graph + user profiles | Temporal contradiction resolution | Automatic expiration | Stable + recent | Unknown | AST-aware chunking; dual-mode profile sub-50ms |
| **LiteLLM** | Redis vector cache | Semantic similarity dedup | TTL expiration | L1+L2 | None | Semantic LLM response caching |

---

### 11.7 Top Novel Ideas to Consider for Emergent

**Adopt now (low effort, high value):**
1. **MD5 hash fast-path dedup** (ChatDev): Before the LLM merge call, check `MD5(content)` against a hash index. Skip the LLM entirely for exact duplicates.
2. **Lightweight model for reflection/decay jobs** (Letta): Use Haiku/Flash, not the full agent model, for synthesis and summarization in scheduled jobs.
3. **Chunk-level threshold filter** (Supermemory): In `recall_memories`, discard chunks with `S_semantic < 0.6` before returning — reduces noise in low-quality recall results.

**Consider for v2:**
4. **LLM-rated community traversal** (GraphRAG): When `MemoryContext` objects exist, use LLM rating to select relevant contexts before pulling constituent memories — better than flat vector search across all memories.
5. **Tool-rule-enforced reflection sequence** (Letta): Enforce `retrieve_memories → synthesize → write_context` order in the reflection job via a state machine.
6. **Dual-mode profile materialization** (Supermemory): Pre-compute a user's top-N core memories at write time (on every `save_memory` to core tier) so session-start injection is a cache read, not a graph query.

**Future / Phase 3:**
7. **Multi-agent blackboard** (ChatDev): Agent-scoped memories readable by other agents within the same project — maps to our planned cross-agent scope.
8. **AST-aware code chunking** (Supermemory): Route `.go`, `.ts`, `.py` files through an AST-aware chunker in the extraction pipeline.

---

## 12. References

- mem0 GitHub: https://github.com/mem0ai/mem0
- Letta GitHub: https://github.com/letta-ai/letta
- MemGPT Paper: https://arxiv.org/abs/2310.08560
- Memory Survey: https://arxiv.org/abs/2404.13501
- mem0 Paper: https://arxiv.org/abs/2504.19413
- NVIDIA NemoClaw: https://github.com/NVIDIA/NemoClaw
- NVIDIA OpenShell: https://github.com/NVIDIA/OpenShell
- NVIDIA NeMo Agent Toolkit: https://github.com/NVIDIA/NeMo-Agent-Toolkit
- Microsoft GraphRAG: https://github.com/microsoft/graphrag
- OpenAI Agents Python SDK: https://github.com/openai/openai-agents-python
- ChatDev 2.0: https://github.com/OpenBMB/ChatDev
- Supermemory: https://github.com/supermemoryai/supermemory
- LiteLLM: https://github.com/BerriAI/litellm
