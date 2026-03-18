# Agent Notes — Prior Art Research

**Date:** 2026-03-18
**Purpose:** Survey graph-based agent memory systems to inform and validate the `agent-notes` design.

---

## Summary of Findings

The agent-notes design (separate `Note` nodes with their own embeddings, attached via `ANNOTATES` relationships, with dual entity-anchored + semantic retrieval) has **no direct precedent** in the systems surveyed. Every existing system conflates observation storage with entity storage to some degree. The key differentiators of agent-notes relative to the field:

| Design property | agent-notes | Field state |
|---|---|---|
| Observations as separate typed nodes | Yes — `Note` type | Partial: Zep has episodic nodes, but these are raw conversation chunks not structured observations |
| Observations have their own independent embeddings | Yes — `content` field only | No system explicitly isolates observation embeddings from entity embeddings |
| Entity embeddings are never polluted by observations | Yes — enforced by schema | No system enforces this; most systems embed entities with accumulated facts |
| Universal annotation layer on any schema | Yes — `ANNOTATES` is schema-agnostic | Not found; all systems own or assume a specific graph schema |
| Dual retrieval: entity-anchored + semantic | Yes — explicit two-path design | Partial: Zep and mem0 do hybrid retrieval but not separated by observation type |
| NoteCluster: aggregated summaries for prompt injection | Yes | Closest: Zep community subgraph; A-MEM Zettelkasten note linking |
| Notes created only by agent tool calls, never extracted | Yes — enforced by schema | No system makes this distinction |

---

## 1. MemGPT / Letta

**Repository:** https://github.com/letta-ai/letta
**Paper:** MemGPT: Towards LLMs as Operating Systems (2023)
**Blog:** https://www.letta.com/blog/agent-memory

### Graph structure
No. MemGPT/Letta does not use a knowledge graph as its memory store. It uses an OS-inspired memory hierarchy: in-context **core memory** (key-value blocks, ~2K tokens), **recall memory** (conversation history in a relational store, searchable via keyword), and **archival memory** (vector store, unlimited depth). Memory blocks are explicit string slots (e.g. `human`, `persona`) that the agent can read/write.

### How memories attach to entities
They do not. Memory is flat — it is stored in named memory blocks or as archival documents. There is no entity identity, no relationships between memories. A community discussion (#2118 and #2119) in the Letta GitHub repo asks about replacing the vector store archival memory with a knowledge graph, but this is user request, not implemented behavior.

### Separate embedding spaces
No. Archival memory is a monolithic vector store. All documents are embedded uniformly. No distinction between entity properties and agent observations.

### Entity-anchored retrieval
No. Retrieval is purely semantic (cosine similarity over archival embeddings) or keyword-based over recall memory. There is no entity graph to traverse.

### Contradiction/dedup handling
None in the base system. The LLM agent is responsible for managing its own core memory blocks (it can overwrite or append), but there is no automatic contradiction detection.

### Relevance to agent-notes
Low. MemGPT/Letta is the closest thing to "agent controls its own memory" but is flat, not graph-based. The agent-notes design solves the problem MemGPT does not address: structured entity-level annotation with isolated retrieval signals.

---

## 2. mem0

**Repository:** https://github.com/mem0ai/mem0
**Docs:** https://docs.mem0.ai/open-source/features/graph-memory
**Paper:** Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory (arXiv:2504.19413, 2025)
**DeepWiki:** https://deepwiki.com/mem0ai/mem0/4.1-graph-memory-overview

### Graph structure
Yes — optional. Mem0 has two storage layers that can be used independently or together: a **vector store** (default) and a **graph store** (mem0ᵍ). The graph uses a Bolt-compatible backend: Neo4j, Memgraph, Amazon Neptune, or Kuzu.

Graph structure: directed labeled graph. Nodes = entities (with type, embedding vector, and creation timestamp). Edges = labeled relationships between entities, e.g. `Alice --works_at--> Google`.

### How memories attach to entities
Graph memories are extracted as subject-predicate-object triples from every memory write. Entities become nodes; relationships become edges. There is no "observation node" — the observation itself becomes an edge (or modifies an existing edge). Example: "Alice prefers async communication" → edge `Alice --prefers_communication_style--> async`.

Entity nodes carry an embedding of the entity name/type. Relationship triples also have embeddings stored separately in the vector store for semantic search.

### Separate embedding spaces
Partial. Entity nodes have embeddings (of entity name). Relationship edges/triples have embeddings stored in the vector database. However, observations are encoded **as edge labels** — not as separate nodes. The entity node's embedding is of the entity itself; observations live as edges. This is the closest any system comes to separation, but it is still not the same as agent-notes: in mem0 there is no `Note` node type — observations are edge properties, not first-class objects.

### Entity-anchored retrieval
Yes. The entity-centric retrieval method: (1) extract key entities from the query, (2) locate corresponding nodes via semantic similarity, (3) explore incoming/outgoing relationships from anchor nodes, (4) return a subgraph. This is similar in spirit to the entity-anchored path in agent-notes.

### Contradiction/dedup handling
LLM-powered conflict resolution. On each write: (1) extract triplets, (2) compare against existing overlapping edges for the same entity pair, (3) LLM resolver decides: ADD, UPDATE (merge relationship label), DELETE (mark invalid), or NOOP. Invalidated edges are soft-deleted (marked invalid) rather than removed, enabling temporal reasoning. Operationally: same as Zep's approach but without explicit bitemporal timestamps on edges.

### Novel retrieval techniques
Dual mode: vector similarity + BM25 reranking over relationship triplets. Graph operations run in parallel with vector operations using `ThreadPoolExecutor`.

### Relevance to agent-notes
Medium-high. mem0's graph layer is architecturally similar but inverted: observations are edges, not nodes. Agent-notes makes observations first-class nodes with their own embeddings. The entity-centric retrieval is directly analogous to entity-anchored retrieval in agent-notes.

---

## 3. Zep / Graphiti

**Repository:** https://github.com/getzep/graphiti
**Paper:** Zep: A Temporal Knowledge Graph Architecture for Agent Memory (arXiv:2501.13956, January 2025)
**PDF:** https://blog.getzep.com/content/files/2025/01/ZEP__USING_KNOWLEDGE_GRAPHS_TO_POWER_LLM_AGENT_MEMORY_2025011700.pdf
**Neo4j blog:** https://neo4j.com/blog/developer/graphiti-knowledge-graph-memory/

### Graph structure
Yes — the most sophisticated graph memory system in the field. Zep uses a temporally-aware dynamic knowledge graph G = (N, E, φ), with **three hierarchical subgraphs**:

1. **Episodic subgraph**: raw conversation messages or JSON events as nodes, timestamped with original event time. These are the ground truth corpus — never modified. Each episode node has an original text embedding.

2. **Semantic entity subgraph**: entities and facts extracted from episodes. Each `EntityNode` has: `name`, `name_embedding` (1024D), `summary`, `labels` (type classification). Facts between entities are `EntityEdge` objects with: `fact` (natural language), `fact_embedding`, and four temporal dimensions (see below).

3. **Community subgraph**: clusters of highly-connected entities summarized by LLM. Community reports contain executive overview of key entities and relationships. Built using label propagation (not Leiden).

### How memories attach to entities
Facts/observations are stored as `EntityEdge` objects connecting two `EntityNode`s. Episodic edges link episode nodes to the entity nodes they mentioned. This creates a **provenance chain**: raw episode → extracted entities → derived facts, all bidirectionally traversable.

There is no separate "observation node" type — observations are edges (like mem0), not nodes. The episodic subgraph stores raw events as nodes, but these are not structured agent observations; they are unprocessed source material.

### Separate embedding spaces
Partial separation:
- Entity nodes embed their `name` field only (`name_embedding`)
- Fact edges embed the `fact` text field only (`fact_embedding`)
- Episode nodes embed their raw content

Entity name embeddings and fact/observation embeddings are stored in separate fields and conceptually separate spaces. However, entity node summaries accumulate across all extracted facts — so the entity node summary blends all observations over time, even if the embedding is only of the name.

### Entity-anchored retrieval
Implicit. Zep's hybrid retrieval combines: (1) semantic embedding similarity on `EntityNode.name_embedding` and `EntityEdge.fact_embedding`, (2) BM25 keyword search, (3) direct graph traversal from matched entity nodes. P95 latency: 300ms. No explicit "entity-in-scope → traverse ANNOTATES → get notes" path, but the graph traversal component functions analogously.

### Contradiction/dedup handling (key strength)
Most sophisticated in the field:

- **Entity deduplication**: three-tier strategy — exact match → fuzzy similarity → LLM reasoning. Merges duplicate entity nodes.
- **Edge deduplication**: hybrid search constrained to edges between the same entity pair, then LLM decides to keep/merge/replace.
- **Contradiction detection**: LLM compares new edges against semantically related existing edges between the same entity pair.
- **Temporal invalidation**: bitemporal model. When a contradiction is found, the old edge's `t_invalid` is set to the new edge's `t_valid`. Old facts are preserved (non-lossy) but marked as no longer valid.
- **Bitemporal tracking**: every `EntityEdge` carries four timestamps: `t_created` (ingestion time when created), `t_expired` (ingestion time when invalidated), `t_valid` (event time when fact became true), `t_invalid` (event time when fact stopped being true). Enables: "what was true on date X?" vs "what did we know on date X?".

### Novel retrieval techniques
- Bi-temporal queries: can ask "what was true at event time T?" independent of ingestion order
- Community summaries for global/thematic search (analogous to NoteCluster but auto-generated, not agent-created)
- Non-lossy episodic store: can always re-derive or verify facts from source episodes

### Relevance to agent-notes
High. Zep/Graphiti is the most complete prior art. Key differences from agent-notes:
1. Zep has no "observation node" type — facts are edges, not nodes
2. Zep does not support annotating an arbitrary external schema — it owns its own graph schema
3. Zep has no concept of a "Note" created by agent tool call vs. extracted automatically
4. Zep's episodic nodes are raw text chunks, not structured observations
5. The bitemporal `event_time`/`ingestion_time` distinction in agent-notes is directly informed by Zep's model

---

## 4. Microsoft GraphRAG

**Repository:** https://github.com/microsoft/graphrag
**Docs:** https://microsoft.github.io/graphrag/
**Research blog:** https://www.microsoft.com/en-us/research/blog/graphrag-unlocking-llm-discovery-on-narrative-private-data/

### Graph structure
Yes. GraphRAG constructs a knowledge graph from a **document corpus** (not from agent observations). Pipeline: text documents → chunks → entity/relationship extraction → community detection (Leiden algorithm) → LLM-generated community reports.

Nodes: entities (person, organization, location, event, concept) extracted from source text.
Edges: labeled subject-predicate-object relationships between entities.
Community reports: LLM-generated summaries of each community cluster — containing executive overviews, key entities, relationships, and claims.

### How memories attach to entities
GraphRAG does not attach agent observations to entities. It is a **read/query** system over a pre-indexed corpus, not a write/observe system. Entities accumulate descriptions from all source text mentions (merged into an entity description), but this is automated extraction, not agent annotation.

Claims extraction (optional) does add a third node type — claims/covariate nodes — which are fact-like statements associated with entities. These are the closest to observation nodes but are extracted from documents, not written by agents.

### Separate embedding spaces
No distinction between entity embeddings and observation embeddings. Entity descriptions are LLM-generated summaries that blend all extracted facts. Community reports have embeddings but these are summary-level, not entity-level.

GraphRAG supports multiple vector indexes in Neo4j (e.g., separate indexes for text chunks vs. community embeddings), but this is a storage optimization, not an epistemological distinction between entity properties and agent observations.

### Entity-anchored retrieval
Yes (Local Search mode). GraphRAG's local search: (1) identify entities in query, (2) retrieve entity node + neighbors + relationships, (3) optionally include community summary. This is entity-anchored graph traversal.

Global Search: queries community report summaries without entity anchoring.

Dynamic community selection (2025 improvement): selects relevant community reports dynamically per query rather than using all communities.

### Contradiction/dedup handling
Entity deduplication via LLM during extraction phase. No temporal tracking or fact invalidation — GraphRAG is designed for static corpora, not evolving knowledge. Updates require re-indexing.

### Relevance to agent-notes
Low-medium. GraphRAG is primarily a read-time RAG system, not an agent memory system. It does not write observations. The community report concept (LLM-generated thematic summaries) is the closest analog to `NoteCluster` — both are LLM-generated compressed summaries of related content for prompt injection. Key difference: community reports are auto-generated from extracted facts; NoteClusters aggregate hand-written agent observations.

---

## 5. Cognee

**Repository:** https://github.com/topoteretes/cognee
**Website:** https://www.cognee.ai
**Blog:** https://memgraph.com/blog/from-rag-to-graphs-cognee-ai-memory

### Graph structure
Yes — graph-vector hybrid. Cognee uses a unified storage architecture with three stores: **relational** (documents, chunks, provenance), **vector** (embeddings for semantic similarity), and **graph** (entities and relationships).

Core abstraction: `DataPoint` — the building block for both nodes and relationships in the knowledge graph. Every node has a corresponding embedding, so semantic similarity and graph traversal are always available in tandem.

### How memories attach to entities
Cognee processes documents into text chunks, passes them to an LLM to generate a graph representation (subject-relation-object triples), and stores the resulting nodes and edges. There is no explicit "observation node" or agent annotation mechanism — all memory content is extracted from documents.

Two memory tiers:
- **Session memory**: short-term working memory, loads relevant embeddings and graph fragments into runtime context
- **Permanent memory**: long-term knowledge artifacts (user data, interaction traces, external documents, derived relationships)

Memory Fragment Projection: a personalization feature that constructs a per-user sub-graph view by projecting the full graph down to fragments relevant to a specific user context.

### Separate embedding spaces
No explicit separation. Cognee's principle is "every node in the graph has a corresponding embedding" — entity nodes and chunk nodes both have embeddings. The graph and vector stores are kept in sync. No distinction between entity property embeddings and observation embeddings.

### Entity-anchored retrieval
Yes. Cognee supports multiple search types: (1) graph completion search (traversal), (2) similarity search (vector), (3) insights search (merging both). Entity-anchored traversal is part of the graph completion path.

### Contradiction/dedup handling
Temporal cognification (a Cognee feature): transforms ingested text into an event-based knowledge graph with timestamps and intervals, connecting events to entities through explicit temporal relationships. Contradiction handling not prominently described.

### Relevance to agent-notes
Low-medium. Cognee is primarily a document-ingestion knowledge graph, not an agent observation system. Memory Fragment Projection is the most relevant concept — personalized sub-graph views are analogous to entity-scoped note retrieval, but in Cognee this is about viewing existing entity data, not storing separate agent observations.

---

## 6. AriGraph

**Paper:** AriGraph: Learning Knowledge Graph World Models with Episodic Memory for LLM Agents (arXiv:2407.04363, 2024/2025; IJCAI 2025)
**Repository:** https://github.com/AIRI-Institute/AriGraph

### Graph structure
Yes — dual semantic + episodic memory graph. The agent builds a knowledge graph from parsed environmental observations. Semantic memory = subject-relation-object triples (the KG). Episodic memory = episodic vertices (per-step observations) linked to the semantic triples they contributed to.

### How memories attach to entities
Agent parses each observation step into triples (object1, relation, object2) → semantic KG. Episodic vertices represent individual observations, connected via episodic edges to every semantic triple extracted from that observation. This creates the same bidirectional provenance chain as Zep.

### Separate embedding spaces
Not described. AriGraph is focused on interactive text-based games (TextWorld); embedding strategies for retrieval are not the primary concern.

### Entity-anchored retrieval
Yes — the agent retrieves relevant graph context by identifying relevant entities and traversing the local neighborhood.

### Contradiction/dedup handling
Graph updates are additive in interactive environments. Contradictions (e.g., object location changes) are handled by replacing old triples with new ones from the latest observation.

### Relevance to agent-notes
Medium. AriGraph is the clearest prior art for **episodic observation nodes linked to semantic entity nodes** — the closest structural analog to Note → ANNOTATES → Entity. The main difference: AriGraph's episodic nodes are raw observation texts from a game environment, not structured typed observations created by agent tool calls. AriGraph does not generalize to annotating an external arbitrary schema.

---

## 7. MAGMA

**Paper:** MAGMA: A Multi-Graph based Agentic Memory Architecture for AI Agents (arXiv:2601.03236, January 2026)
**HuggingFace:** https://huggingface.co/papers/2601.03236

### Graph structure
Yes — four parallel graphs representing orthogonal relational views of the same memory items:

1. **Semantic graph**: conceptual similarity between memories
2. **Temporal graph**: chronological sequence
3. **Causal graph**: cause-effect relationships (directed edges)
4. **Entity graph**: tracks people, places, things across time (solves object permanence)

### How memories attach to entities
The entity graph tracks entity identity across memories. When a memory mentions an entity, it is linked into the entity graph. This is entity-anchored at the graph level, not at the schema level.

### Separate embedding spaces
The multi-graph structure inherently separates different relational views. Semantic graph uses similarity-based embeddings; other graphs use structural relationships. Not explicitly about separating entity embeddings from observation embeddings.

### Entity-anchored retrieval
Yes. Policy-guided traversal: the retrieval policy selects which graph(s) to traverse based on query type (causal query → causal graph, temporal query → temporal graph, entity query → entity graph).

### Contradiction/dedup handling
Not the focus of the paper. The causal graph implicitly handles temporal ordering of facts.

### Novel retrieval techniques
Query-adaptive graph selection: the system chooses which relational view to traverse based on the query type. Achieves 45.5% higher reasoning accuracy on long-context benchmarks, 95% reduction in token consumption, and 40% faster query latency vs. prior methods.

### Relevance to agent-notes
Medium. The entity graph in MAGMA solves a similar problem to entity-anchored retrieval in agent-notes (tracking observations per entity across time). MAGMA's multi-graph design is more complex than agent-notes needs, but the core insight — that different retrieval goals need different relational structures — validates agent-notes' dual-path design.

---

## 8. A-MEM

**Paper:** A-MEM: Agentic Memory for LLM Agents (arXiv:2502.12110, February 2025)
**Repository:** https://github.com/agiresearch/A-mem

### Graph structure
Yes — a Zettelkasten-inspired linked note network. Each memory item is stored as a structured "note" with: contextual description, keywords, tags, and dynamic links to related notes. Notes are organized into **boxes** (clusters of interlinked notes about similar themes).

### How memories attach to entities
A-MEM does not attach memories to external entities. Notes exist as free-standing units in a network. They are linked to each other (via similarity and LLM-assessed connections) but not to typed entities in an external schema. There is no ANNOTATES-style relationship to an existing graph.

### Separate embedding spaces
Embedding similarity is used for link generation between notes. No separation between entity embeddings and observation embeddings (there are no entity nodes — everything is a note).

### Entity-anchored retrieval
No. Retrieval is note-to-note — similarity search over note embeddings plus traversal of note links.

### Contradiction/dedup handling
Memory evolution: when a new note is linked to an existing note, the existing note's contextual description can be updated to reflect the new connection. No explicit contradiction detection — updates are additive.

### Novel retrieval techniques
Zettelkasten-style emergent knowledge structure: as more notes accumulate, the network develops thematic clusters that emerge from pairwise linking rather than explicit categorization. Notes are grouped into boxes = clusters.

### Relevance to agent-notes
Medium-high. A-MEM's note structure is the closest precedent for agent-notes' `Note` type — specifically: (1) each memory is a first-class object (not an edge or property), (2) notes have their own embeddings, (3) boxes (clusters of related notes) are directly analogous to `NoteCluster`. Key differences: A-MEM notes are not attached to external entities via typed relationships; A-MEM does not distinguish "entity embedding" from "note embedding" because it has no entity graph.

---

## 9. Neo4j Labs Agent Memory

**Repository:** https://github.com/neo4j-labs/agent-memory
**Blog:** https://neo4j.com/blog/developer/meet-lennys-memory-building-context-graphs-for-ai-agents/
**Article:** https://medium.com/neo4j/modeling-agent-memory-d3b6bc3bb9c4

### Graph structure
Yes — graph-native, backed by Neo4j. Three memory types in a single graph:

1. **Short-Term Memory**: conversation history (messages, turns)
2. **Long-Term Memory**: extracted entity knowledge (facts, preferences) — uses the POLE+O data model (Person, Object, Location, Event, Organization + subtypes)
3. **Reasoning Memory**: decision traces, tool usage logs, provenance — "without it, you can't explain decisions, learn from experience, or debug unexpected behavior"

### How memories attach to entities
Entity extraction pipeline (spaCy + GLiNER2 + LLM) extracts entities from conversation turns. Extracted entities become graph nodes. Facts and preferences are stored as relationships between entity nodes or as properties on entity nodes. Reasoning traces are stored as separate Reasoning nodes linked to the relevant conversation turn and entities.

The `StreamingTraceRecorder` can record tool calls and add observations during agent execution — these become Reasoning nodes in the graph.

### Separate embedding spaces
Entity nodes have embeddings for semantic retrieval. Reasoning/observation nodes are stored as separate nodes (not merged into entity properties). This is the closest existing system to the agent-notes pattern: observations are separate nodes linked to entity nodes.

However, there is no explicit enforcement that entity embeddings are never polluted by observations — the POLE+O schema can store facts as entity properties too.

### Entity-anchored retrieval
Yes. The knowledge graph enables traversal from entity nodes to related facts, preferences, and reasoning traces. Framework integrations (LangChain, LlamaIndex, etc.) provide retrieval APIs.

### Contradiction/dedup handling
Multi-stage entity extraction with configurable merge strategies. Wikipedia enrichment for entity disambiguation. No detailed contradiction detection described.

### Relevance to agent-notes
High. The Neo4j Labs agent-memory is the most structurally similar existing implementation to agent-notes:
- Separate Reasoning nodes for agent observations (analogous to Note nodes)
- Entity nodes with their own embeddings
- Observations linked to entities (analogous to ANNOTATES)
- Three memory tiers (short-term, long-term, reasoning)

Key differences: not a universal annotation layer; tied to the POLE+O schema; reasoning nodes record decision traces rather than typed observations (preference, correction, instruction, etc.); no NoteCluster aggregation.

---

## 10. MemoryOS (BAI-LAB)

**Paper:** Memory OS of AI Agent (arXiv:2506.06326; EMNLP 2025 Oral)
**Repository:** https://github.com/BAI-LAB/MemoryOS

### Graph structure
Hierarchical storage with three levels: short-term memory (recent context), mid-term memory (processed patterns), long-term personal memory (persistent user model). Graph relationships are not the primary organizing principle — the architecture is more OS-inspired (like MemGPT) than graph-inspired.

### Relevance to agent-notes
Low. OS metaphor without graph structure. Not entity-level.

---

## 11. MemOS (MemTensor)

**Repository:** https://github.com/MemTensor/MemOS
**Paper:** MemOS: A Memory OS for AI System (arXiv:2507.03724, July 2025)

### Graph structure
Yes — GraphMemory component uses a networked knowledge representation. Unified Memory API structures memory as a graph, described as "inspectable and editable by design, not a black-box embedding store." Introduces MemCube: a unified abstraction encapsulating plaintext, activation, and parameter memories. Uses a tree-structured hierarchy with graph-style cross-links.

### Relevance to agent-notes
Low-medium. MemOS is primarily about memory management as a system resource (scheduling, orchestration). The graph is incidental to the OS abstraction.

---

## 12. MAGMA is distinct from Microsoft Magma

Note: arXiv:2601.03236 (MAGMA: Multi-Graph Agentic Memory) is a different paper from arXiv:2502.13130 (Magma: Foundation Model for Multimodal AI Agents by Microsoft/CVPR 2025). The latter is about action grounding in multimodal agents, not memory architecture.

---

## 13. HippoRAG

**Paper:** HippoRAG: Neurobiologically Inspired Long-Term Memory for Large Language Models (arXiv:2405.14831; NeurIPS 2024)
**Repository:** https://github.com/OSU-NLP-Group/HippoRAG

### Graph structure
Yes. Inspired by hippocampal indexing theory. LLM extracts named entities from documents; entities become nodes in a knowledge graph (the "hippocampal index"). Passages and phrases become nodes linked to entities.

### Retrieval mechanism
(1) Extract query entities, (2) run Personalized PageRank from query entity nodes across the graph, (3) return passages ranked by PPR score. HippoRAG 2 uses a dual-node graph (passage nodes + phrase nodes) with enhanced PPR and LLM-based triple filtering.

### Relevance to agent-notes
Low for observations, high for retrieval. HippoRAG's entity-anchored PPR traversal is an interesting retrieval model for note recall — instead of simple graph traversal from entity-in-scope, PPR spreads relevance across the neighborhood. This could be a future enhancement to entity-anchored retrieval in agent-notes.

---

## 14. Memoria

**Paper:** Memoria: A Scalable Agentic Memory Framework for Personalized Conversational AI (arXiv:2512.12686, December 2024)
**IEEE Xplore:** https://ieeexplore.ieee.org/document/11330332/

### Architecture
Two components: (1) dynamic session-level summarization (rolling summaries of conversation), (2) weighted knowledge graph for user modeling. User traits, preferences, and behavioral patterns are stored as entities and relationships in the KG. Weights on edges decay over time.

### Contradiction/dedup handling
Exponential Weighted Average for conflict resolution — newer facts receive higher weight; old conflicting facts decay. Achieves 87.1% accuracy, 38.7% inference latency reduction, and token compression from 115K to <400 tokens.

### Relevance to agent-notes
Medium. The weighted KG user model (preferences as weighted edges) is similar to agent-notes' confidence scoring on Note objects. The session summarization → injection pattern parallels NoteCluster → prompt injection.

---

## 15. PersonalAI (Comparison Paper)

**Paper:** PersonalAI: A Systematic Comparison of Knowledge Graph Storage and Retrieval Approaches for Personalized LLM Agents (arXiv:2506.17001, 2025)

### What it is
A benchmark and comparison study building on AriGraph architecture. Evaluates different graph designs (standard edges vs. hyper-edges) and retrieval strategies (A*, water-circle traversal, beam search, hybrid) across TriviaQA, HotpotQA, and DiaASQ benchmarks. DiaASQ extended with temporal annotations and contradictory statements.

### Relevance to agent-notes
High for retrieval strategy selection. Key findings: different retrieval strategies are optimal for different query types. The DiaASQ temporal contradiction extension directly validates agent-notes' `event_time` field for temporal fact ordering.

---

## 16. ProMem (Beyond Static Summarization)

**Paper:** Beyond Static Summarization: Proactive Memory Extraction for LLM Agents (arXiv:2601.04463, January 2025)

### Architecture
ProMem uses a recurrent feedback loop for memory extraction: agent self-questions dialogue history to actively probe for missing information, then verifies consistency. Addresses two problems with static summarization: (1) blind feed-forward extraction misses details, (2) no feedback loop for verification.

### Relevance to agent-notes
Medium. The proactive/iterative extraction approach is relevant to how `save_note` should work when called by an agent — not a single extraction pass but an iterative refinement. The consistency verification loop is analogous to the dedup check in `save_note` (cosine similarity ≥ 0.70 → LLM merge call).

---

## 17. Graph-Based Agent Memory Survey

**Paper:** Graph-based Agent Memory: Taxonomy, Techniques, and Applications (arXiv:2602.05665, February 2026)
**Repository:** https://github.com/DEEP-PolyU/Awesome-GraphMemory

### What it is
Comprehensive survey classifying graph-based agent memory systems. Taxonomy covers: short-term vs. long-term, knowledge vs. experience memory, non-structural vs. structural. Four key technique categories: memory extraction, storage organization, retrieval, and evolution (update/decay).

### Key finding relevant to agent-notes
No surveyed system explicitly separates entity embeddings from observation embeddings. The survey taxonomy does not include "annotation layer on external schema" as a category — this is a genuine gap in the literature.

---

## Key Gaps in Existing Literature

1. **No system treats observations as first-class typed nodes with their own embeddings independent of entity embeddings.** Zep and mem0 come closest but store facts as edges, not nodes.

2. **No system is designed as a universal annotation layer that can annotate entities in an arbitrary external schema.** All systems own their schema or assume a specific entity model (POLE+O, Zep's entity/episodic nodes, etc.).

3. **No system enforces that entity embeddings are never modified by agent observations.** This is the core innovation of agent-notes — it is an architectural invariant, not a feature.

4. **No system distinguishes "Notes created only by agent tool calls" from "facts extracted automatically from text."** Most systems conflate agent observations with document extraction.

5. **NoteCluster-style aggregation exists in partial form** — GraphRAG community reports (auto-generated from extracted facts), A-MEM boxes (emergent from note linking), Zep community subgraph (label propagation). But none are explicitly "LLM-synthesised summaries of agent-written observations for prompt injection."

---

## Design Validations from Prior Art

The following design choices in agent-notes are validated by prior art:

- **`event_time` field for temporal ordering** → Validated by Zep's bitemporal model (event time T vs. ingestion time T′) and PersonalAI's temporal annotation extension to DiaASQ
- **`superseded_by` relationship instead of deletion** → Validated by Zep's non-lossy invalidation (soft delete with `t_invalid`) and mem0's mark-as-invalid approach
- **Cosine similarity ≥ 0.70 → LLM merge call** → Validated by Zep's three-tier dedup (exact match → fuzzy similarity → LLM) and mem0's LLM resolver (ADD/UPDATE/DELETE/NOOP)
- **Entity-anchored retrieval as primary path** → Validated by mem0's entity-centric retrieval, Zep's graph traversal, MAGMA's entity graph, Neo4j agent-memory's POLE+O traversal
- **Semantic search as secondary path** → Validated universally; all systems use semantic similarity as a retrieval component
- **`tier=core` notes injected without retrieval** → Validated by MemGPT's core memory blocks (always in context without needing retrieval)
- **NoteCluster for prompt injection** → Validated by Zep's community reports, GraphRAG's community summaries, A-MEM's note boxes — LLM-compressed thematic summaries reduce token cost while preserving semantics
- **Confidence decay on stale notes** → Validated by Memoria's Exponential Weighted Average decay on edge weights

---

## Sources

- [Intro to Letta | Letta Docs](https://docs.letta.com/concepts/memgpt/)
- [Agent Memory: How to Build Agents that Learn and Remember | Letta](https://www.letta.com/blog/agent-memory)
- [MemGPT with Knowledge Graphs Discussion](https://github.com/letta-ai/letta/discussions/2118)
- [Graph Memory - Mem0](https://docs.mem0.ai/open-source/features/graph-memory)
- [Graph Memory Overview | mem0ai/mem0 | DeepWiki](https://deepwiki.com/mem0ai/mem0/4.1-graph-memory-overview)
- [Graph Memory for LLM Agents with mem0-falkordb](https://www.falkordb.com/blog/graph-memory-llm-agents-mem0-falkordb/)
- [Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory (arXiv:2504.19413)](https://arxiv.org/html/2504.19413v1)
- [Zep: A Temporal Knowledge Graph Architecture for Agent Memory (arXiv:2501.13956)](https://arxiv.org/abs/2501.13956)
- [Zep paper HTML](https://arxiv.org/html/2501.13956v1)
- [Graphiti: Knowledge Graph Memory for an Agentic World - Neo4j](https://neo4j.com/blog/developer/graphiti-knowledge-graph-memory/)
- [GitHub - getzep/graphiti](https://github.com/getzep/graphiti)
- [getzep/graphiti | DeepWiki](https://deepwiki.com/getzep/graphiti)
- [Welcome - GraphRAG](https://microsoft.github.io/graphrag/)
- [GitHub - microsoft/graphrag](https://github.com/microsoft/graphrag)
- [GraphRAG: Unlocking LLM discovery on narrative private data](https://www.microsoft.com/en-us/research/blog/graphrag-unlocking-llm-discovery-on-narrative-private-data/)
- [GitHub - topoteretes/cognee](https://github.com/topoteretes/cognee)
- [From RAG to Graphs: How Cognee is Building Self-Improving AI Memory](https://memgraph.com/blog/from-rag-to-graphs-cognee-ai-memory)
- [Cognee - AI Memory Architecture](https://www.cognee.ai/blog/fundamentals/how-cognee-builds-ai-memory)
- [Cognee - Memory Fragment Projection](https://www.cognee.ai/blog/deep-dives/memory-fragment-projection-from-graph-databases)
- [AriGraph: Learning Knowledge Graph World Models with Episodic Memory (arXiv:2407.04363)](https://arxiv.org/abs/2407.04363)
- [AriGraph IJCAI 2025](https://www.ijcai.org/proceedings/2025/0002.pdf)
- [MAGMA: A Multi-Graph based Agentic Memory Architecture (arXiv:2601.03236)](https://arxiv.org/abs/2601.03236)
- [A-MEM: Agentic Memory for LLM Agents (arXiv:2502.12110)](https://arxiv.org/abs/2502.12110)
- [GitHub - agiresearch/A-mem](https://github.com/agiresearch/A-mem)
- [GitHub - neo4j-labs/agent-memory](https://github.com/neo4j-labs/agent-memory)
- [Meet Lenny's Memory: Building Context Graphs for AI Agents - Neo4j](https://neo4j.com/blog/developer/meet-lennys-memory-building-context-graphs-for-ai-agents/)
- [Modeling Agent Memory - Neo4j](https://medium.com/neo4j/modeling-agent-memory-d3b6bc3bb9c4)
- [HippoRAG: Neurobiologically Inspired Long-Term Memory (arXiv:2405.14831)](https://arxiv.org/abs/2405.14831)
- [GitHub - OSU-NLP-Group/HippoRAG](https://github.com/OSU-NLP-Group/HippoRAG)
- [Memoria: A Scalable Agentic Memory Framework (arXiv:2512.12686)](https://arxiv.org/abs/2512.12686)
- [PersonalAI: A Systematic Comparison of KG Storage and Retrieval (arXiv:2506.17001)](https://arxiv.org/abs/2506.17001)
- [Beyond Static Summarization: Proactive Memory Extraction (arXiv:2601.04463)](https://arxiv.org/abs/2601.04463)
- [Graph-based Agent Memory: Taxonomy, Techniques, and Applications (arXiv:2602.05665)](https://arxiv.org/abs/2602.05665)
- [GitHub - DEEP-PolyU/Awesome-GraphMemory](https://github.com/DEEP-PolyU/Awesome-GraphMemory)
- [MemoryOS (BAI-LAB) - EMNLP 2025 Oral (arXiv:2506.06326)](https://arxiv.org/abs/2506.06326)
- [GitHub - BAI-LAB/MemoryOS](https://github.com/BAI-LAB/MemoryOS)
- [MemOS: A Memory OS for AI System (arXiv:2507.03724)](https://arxiv.org/pdf/2507.03724)
- [Property Graph Index Guide For LLM Knowledge Graphs | LlamaIndex](https://www.llamaindex.ai/blog/introducing-the-property-graph-index-a-powerful-new-way-to-build-knowledge-graphs-with-llms)
- [Membase | Unibase Docs](https://openos-labs.gitbook.io/unibase-docs/membase)
- [Temporal Knowledge-Graph Memory in a Partially Observable Environment (arXiv:2408.05861)](https://arxiv.org/abs/2408.05861)
