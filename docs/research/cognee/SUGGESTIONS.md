# Cognee → Emergent: Adoption Recommendations

**Analysis Date:** February 11, 2026  
**Purpose:** Actionable recommendations for patterns worth adopting from Cognee into Emergent

---

## Executive Summary

After comprehensive analysis of Cognee's architecture, **6 patterns** are recommended for adoption into Emergent. Prioritized by **impact vs effort**, focusing on patterns that directly enhance Emergent's knowledge graph capabilities without compromising its single-database simplicity.

**Top 3 Quick Wins** (Low effort, high impact):

1. **Access Tracking** → Usage analytics (Low effort: add timestamp column)
2. **Conversation History Cache** → Multi-turn chat context (Low effort: extend existing chat)
3. **Triplet Embedding** → Semantic relationship search (Medium effort: add embedding column)

**Strategic High-Impact** (Medium-High effort): 4. **Pluggable Retrieval Strategies** → Extensible search algorithms (High effort, high flexibility) 5. **Ontology Resolver** → Domain-specific validation (Medium effort, specialized value)

**Avoid**: 6. **Multi-Backend Adapters** → Complexity vs single-database simplicity (Emergent's strength)

---

## 1. Access Tracking (★★★ PRIORITY 1 - Quick Win)

### What It Is

Automatic timestamp updates on graph nodes when accessed via search queries, enabling usage analytics.

### Cognee Implementation

```python
# cognee/modules/retrieval/utils/access_tracking.py
async def update_node_access_timestamps(entity_nodes):
    """Update last_accessed_at for all accessed nodes"""
    updates = []
    for node in entity_nodes:
        updates.append({
            'node_id': node.id,
            'last_accessed_at': datetime.utcnow()
        })

    # Batch update in database
    await graph_db.batch_update_access_times(updates)
```

### Why Adopt This

- **Analytics**: Track which entities are most accessed → prioritize content curation
- **Cache Warmth**: Identify hot nodes for optimization (pre-load, index tuning)
- **User Insights**: Understand what users search for most
- **Content Gaps**: Nodes never accessed = potential quality issues
- **Low Effort**: Single column addition, minimal code change

### Emergent Implementation Plan

**Step 1: Add Column (5 minutes)**

```sql
-- Migration: 20260211_add_access_tracking.sql
ALTER TABLE kb.graph_objects
    ADD COLUMN last_accessed_at TIMESTAMPTZ;

CREATE INDEX idx_graph_objects_last_accessed
    ON kb.graph_objects(last_accessed_at DESC)
    WHERE last_accessed_at IS NOT NULL;
```

**Step 2: Update SearchService (15 minutes)**

```go
// apps/server-go/domain/search/service.go
func (s *Service) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
    // Existing search logic...
    results, err := s.repo.Search(ctx, params)

    // NEW: Track accessed object IDs
    objectIDs := extractObjectIDs(results)
    if len(objectIDs) > 0 {
        go s.updateAccessTimestamps(ctx, objectIDs) // Async, non-blocking
    }

    return results, nil
}

func (s *Service) updateAccessTimestamps(ctx context.Context, objectIDs []uuid.UUID) {
    query := `
        UPDATE kb.graph_objects
        SET last_accessed_at = NOW()
        WHERE id = ANY($1)
    `
    _, err := s.db.Exec(ctx, query, objectIDs)
    if err != nil {
        s.log.Warn("failed to update access timestamps", slog.Any("error", err))
    }
}
```

**Step 3: Analytics Queries (10 minutes)**

```sql
-- Most accessed entities (last 30 days)
SELECT type, key, COUNT(*) as access_count,
       MAX(last_accessed_at) as last_access
FROM kb.graph_objects
WHERE last_accessed_at >= NOW() - INTERVAL '30 days'
GROUP BY type, key
ORDER BY access_count DESC
LIMIT 50;

-- Unaccessed entities (potential quality issues)
SELECT type, key, created_at
FROM kb.graph_objects
WHERE last_accessed_at IS NULL
  AND created_at < NOW() - INTERVAL '7 days'
ORDER BY created_at DESC;
```

### Risks & Mitigation

- **Performance**: Async updates prevent search slowdown
- **Database Load**: Batch updates (1 query per search vs N queries per node)
- **Disk Space**: Timestamp = 8 bytes, negligible for 1M nodes (~8MB)

### Success Metrics

- Zero impact on search latency (async updates)
- Analytics dashboard showing top 100 accessed entities
- Ability to identify unused content within 1 week

### Estimated Effort

- **Development**: 30 minutes (migration + service update + queries)
- **Testing**: 15 minutes (verify async updates don't block search)
- **Documentation**: 15 minutes (analytics query examples)
- **Total**: **1 hour**

---

## 2. Conversation History Cache (★★★ PRIORITY 2 - Quick Win)

### What It Is

Persistent storage of chat conversation context (user queries + AI responses) to enable multi-turn conversations with memory.

### Cognee Implementation

```python
# cognee/modules/retrieval/utils/session_cache.py
async def save_conversation_history(query, context_summary, answer, session_id):
    """Save conversation turn to cache"""
    await redis.lpush(f"session:{session_id}", json.dumps({
        'query': query,
        'context': context_summary,
        'answer': answer,
        'timestamp': datetime.utcnow().isoformat()
    }))
    # Keep last 10 turns only
    await redis.ltrim(f"session:{session_id}", 0, 9)

async def get_conversation_history(session_id):
    """Load last N turns from cache"""
    history = await redis.lrange(f"session:{session_id}", 0, 9)
    return [json.loads(turn) for turn in history]
```

Used in GraphCompletionRetriever:

```python
# Load previous context
conversation_history = await get_conversation_history(session_id)

# Generate response with history
completion = await generate_completion(
    query=query,
    context=context,
    conversation_history=conversation_history,  # ← Enables multi-turn!
    response_model=self.response_model
)

# Save new turn
await save_conversation_history(query, context_summary, completion, session_id)
```

### Why Adopt This

- **Multi-Turn UX**: "What about in Q3?" after "Show revenue trends" understands context
- **Follow-Up Questions**: Users can refine searches without repeating context
- **Context Continuity**: AI remembers previous answers → more coherent conversations
- **Low Effort**: Emergent already has chat service, just needs persistence

### Emergent Implementation Plan

**Step 1: Extend Chat Schema (10 minutes)**

```sql
-- Migration: 20260211_add_conversation_history.sql
ALTER TABLE core.chat_messages
    ADD COLUMN context_summary TEXT,
    ADD COLUMN retrieval_context JSONB;

CREATE INDEX idx_chat_messages_conversation
    ON core.chat_messages(conversation_id, created_at DESC);
```

**Step 2: Update ChatService (20 minutes)**

```go
// apps/server-go/domain/chat/service.go
func (s *Service) SendMessage(ctx context.Context, req SendMessageRequest) (*StreamResponse, error) {
    // NEW: Load conversation history (last 5 turns)
    history, err := s.repo.GetConversationHistory(ctx, req.ConversationID, 5)
    if err != nil {
        s.log.Warn("failed to load conversation history", slog.Any("error", err))
        history = []Message{} // Continue without history
    }

    // Build LLM prompt with history
    prompt := s.buildPromptWithHistory(req.Message, history)

    // Call LLM with enhanced context
    response := s.llmClient.Chat(ctx, prompt)

    // Save message with context summary
    msg := &Message{
        ConversationID: req.ConversationID,
        Content:        response.Content,
        ContextSummary: summarizeContext(history), // For future turns
    }
    s.repo.SaveMessage(ctx, msg)

    return response, nil
}

func (s *Service) buildPromptWithHistory(currentQuery string, history []Message) string {
    prompt := "Previous conversation:\n"
    for _, msg := range history {
        prompt += fmt.Sprintf("User: %s\nAI: %s\n", msg.UserMessage, msg.AIResponse)
    }
    prompt += fmt.Sprintf("\nCurrent question: %s\n", currentQuery)
    return prompt
}
```

**Step 3: Repository Method (15 minutes)**

```go
// apps/server-go/domain/chat/repository.go
func (r *Repository) GetConversationHistory(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error) {
    query := `
        SELECT id, role, content, context_summary, created_at
        FROM core.chat_messages
        WHERE conversation_id = $1
        ORDER BY created_at DESC
        LIMIT $2
    `

    rows, err := r.db.Query(ctx, query, conversationID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []Message
    for rows.Next() {
        var msg Message
        if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &msg.ContextSummary, &msg.CreatedAt); err != nil {
            return nil, err
        }
        messages = append(messages, msg)
    }

    // Reverse to chronological order
    for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
        messages[i], messages[j] = messages[j], messages[i]
    }

    return messages, nil
}
```

### Risks & Mitigation

- **Token Limits**: Limit history to 5-10 turns (configurable)
- **Irrelevant Context**: Summarize long responses before including in history
- **Storage Growth**: Periodically archive old conversations (>30 days)

### Success Metrics

- Users can ask follow-up questions without repeating context
- AI responses reference previous turns (e.g., "As I mentioned earlier...")
- Conversation coherence score increases (user survey/feedback)

### Estimated Effort

- **Development**: 45 minutes (migration + service + repository)
- **Testing**: 30 minutes (multi-turn conversation flows)
- **Documentation**: 15 minutes (conversation history API docs)
- **Total**: **1.5 hours**

---

## 3. Triplet Embedding (★★ PRIORITY 3 - Medium Impact)

### What It Is

Embedding graph relationships (edges) alongside entities (nodes) to enable semantic search on relationships, not just entities.

### Cognee Implementation

```python
# cognee/modules/cognify/config.py
class CognifyConfig:
    triplet_embedding: bool = True  # Enable relationship embeddings

# cognee/tasks/graph/extract_graph_from_data.py
if len(graph_nodes) > 0:
    await add_data_points(
        data_points=graph_nodes,
        custom_edges=graph_edges,
        embed_triplets=embed_triplets  # ← Embeds "Entity1 -[REL]-> Entity2"
    )

# cognee/modules/retrieval/utils/brute_force_triplet_search.py
async def brute_force_triplet_search(query, top_k=5):
    """Search for triplets by semantic similarity"""
    query_embedding = await embeddings.embed(query)

    # Search nodes AND edges
    similar_nodes = await vector_db.search(query_embedding, 'nodes', top_k)
    similar_edges = await vector_db.search(query_embedding, 'edges', top_k)  # ← NEW

    # Combine and rank by distance
    triplets = build_triplets_from_results(similar_nodes, similar_edges)
    return triplets[:top_k]
```

### Why Adopt This

- **Richer Search**: Find relationships like "works for", "founded by", "located in"
- **Query Examples**:
  - "Who founded Tesla?" → Finds `Elon Musk -[founded]-> Tesla` via relationship embedding
  - "Companies in San Francisco" → Finds `-[located_in]-> San Francisco` relationships
- **Better Than Node-Only**: Current approach only finds entities, not how they relate
- **LLM Context**: Include relevant relationships in LLM prompts → better answers

### Emergent Implementation Plan

**Step 1: Add Embedding Column (5 minutes)**

```sql
-- Migration: 20260211_add_triplet_embeddings.sql
ALTER TABLE kb.graph_relationships
    ADD COLUMN embedding vector(768);

CREATE INDEX idx_graph_relationships_embedding
    ON kb.graph_relationships
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
```

**Step 2: Generate Triplet Text (10 minutes)**

```go
// apps/server-go/domain/graph/service.go
func (s *Service) generateTripletText(rel *GraphRelationship) string {
    // Load source and target objects
    source, _ := s.repo.GetByID(ctx, rel.SourceID)
    target, _ := s.repo.GetByID(ctx, rel.TargetID)

    // Build natural language triplet
    // Example: "Elon Musk founded Tesla"
    return fmt.Sprintf("%s %s %s",
        source.Properties["name"],  // "Elon Musk"
        humanizeRelationship(rel.Type),  // "founded"
        target.Properties["name"])  // "Tesla"
}

func humanizeRelationship(relType string) string {
    // WORKS_FOR → "works for"
    return strings.ToLower(strings.ReplaceAll(relType, "_", " "))
}
```

**Step 3: Embed on Creation (15 minutes)**

```go
// apps/server-go/domain/graph/service.go
func (s *Service) CreateRelationship(ctx context.Context, req CreateRelationshipRequest) (*GraphRelationship, error) {
    // Existing validation...

    rel := &GraphRelationship{
        SourceID: req.SourceID,
        TargetID: req.TargetID,
        Type:     req.Type,
    }

    // NEW: Generate and embed triplet
    tripletText := s.generateTripletText(rel)
    embedding, err := s.embeddingClient.Embed(ctx, tripletText)
    if err != nil {
        s.log.Warn("failed to embed triplet", slog.Any("error", err))
        // Continue without embedding (non-critical)
    } else {
        rel.Embedding = embedding
    }

    // Insert with embedding
    return s.repo.CreateRelationship(ctx, rel)
}
```

**Step 4: Update Search (20 minutes)**

```go
// apps/server-go/domain/search/repository.go
func (r *Repository) SearchWithTriplets(ctx context.Context, params SearchParams) ([]*SearchResult, error) {
    // Existing node search...
    nodeResults := r.searchNodes(ctx, params)

    // NEW: Search relationships
    relQuery := `
        SELECT
            r.id,
            r.source_id,
            r.target_id,
            r.type,
            1 - (r.embedding <=> $1) AS similarity,
            src.key AS source_key,
            tgt.key AS target_key
        FROM kb.graph_relationships r
        JOIN kb.graph_objects src ON r.source_id = src.id
        JOIN kb.graph_objects tgt ON r.target_id = tgt.id
        WHERE r.embedding IS NOT NULL
        ORDER BY r.embedding <=> $1
        LIMIT 50
    `

    relResults := r.db.Query(ctx, relQuery, params.EmbeddingVector)

    // Combine node and relationship results
    return mergeResults(nodeResults, relResults), nil
}
```

### Risks & Mitigation

- **Storage**: 768 floats × 4 bytes = 3KB per relationship (manageable)
- **Embedding Cost**: Vertex AI charges per token → batch embed during imports
- **Index Time**: Initial index build takes ~10 minutes for 1M relationships
- **Search Latency**: Hybrid search (nodes + edges) adds ~50-100ms (acceptable)

### Success Metrics

- Can find relationships via natural language queries
- "Founded by" queries return correct entity pairs
- LLM context includes relevant relationships (not just entities)
- Search precision improves by 10-20% (A/B test)

### Estimated Effort

- **Development**: 50 minutes (migration + embedding + search update)
- **Testing**: 30 minutes (relationship search queries, precision tests)
- **Documentation**: 20 minutes (triplet search examples)
- **Total**: **1.7 hours**

---

## 4. Pluggable Retrieval Strategies (★★ PRIORITY 4 - High Impact, High Effort)

### What It Is

Abstract interface for search/retrieval algorithms, enabling easy experimentation with different strategies (graph traversal, temporal, entity-centric, etc.) without changing core search service.

### Cognee Implementation

```python
# cognee/modules/retrieval/base_retriever.py
class BaseRetriever(ABC):
    @abstractmethod
    async def get_retrieved_objects(self, query: str) -> Any:
        """Fetch raw data (nodes, edges, chunks)"""
        pass

    @abstractmethod
    async def get_context_from_objects(self, query: str, objects: Any) -> str:
        """Process raw data into LLM-ready text"""
        pass

    @abstractmethod
    async def get_completion_from_context(self, query: str, objects: Any, context: str) -> List[str]:
        """Generate LLM response with context"""
        pass

    async def get_completion(self, query: str) -> List[str]:
        """Full pipeline: retrieve → contextualize → complete"""
        objects = await self.get_retrieved_objects(query)
        context = await self.get_context_from_objects(query, objects)
        return await self.get_completion_from_context(query, objects, context)

# Implementations:
# - GraphCompletionRetriever (graph triplet search)
# - TemporalRetriever (time-aware graph traversal)
# - EntityCompletionRetriever (entity-centric search)
# - NaturalLanguageRetriever (vector search only)
# ... 15+ total retrievers
```

### Why Adopt This

- **Experimentation**: Try different algorithms without breaking existing search
- **A/B Testing**: Compare retrieval strategies on same queries
- **Domain-Specific**: Medical queries use TemporalRetriever, product queries use GraphCompletionRetriever
- **Extensibility**: Add new strategies without modifying core search service
- **Clean Separation**: Retrieval logic isolated from HTTP/business layers

### Emergent Implementation Plan

**Step 1: Define Retriever Interface (30 minutes)**

```go
// apps/server-go/domain/search/retriever.go
package search

import (
    "context"
    "github.com/google/uuid"
)

// Retriever defines the 3-step retrieval pipeline
type Retriever interface {
    // Step 1: Fetch raw data (nodes, edges, chunks, etc.)
    GetRetrievedObjects(ctx context.Context, query string, projectID uuid.UUID) ([]any, error)

    // Step 2: Process raw data into LLM-ready context string
    GetContextFromObjects(ctx context.Context, query string, objects []any) (string, error)

    // Step 3: Generate LLM response with context
    GetCompletionFromContext(ctx context.Context, query string, objects []any, context string) (string, error)

    // Full pipeline (convenience method)
    GetCompletion(ctx context.Context, query string, projectID uuid.UUID) (string, error)
}

// BaseRetriever provides default GetCompletion implementation
type BaseRetriever struct {
    Retriever
}

func (b *BaseRetriever) GetCompletion(ctx context.Context, query string, projectID uuid.UUID) (string, error) {
    objects, err := b.GetRetrievedObjects(ctx, query, projectID)
    if err != nil {
        return "", err
    }

    context, err := b.GetContextFromObjects(ctx, query, objects)
    if err != nil {
        return "", err
    }

    return b.GetCompletionFromContext(ctx, query, objects, context)
}
```

**Step 2: Implement Hybrid Retriever (Current Logic) (45 minutes)**

```go
// apps/server-go/domain/search/hybrid_retriever.go
package search

import (
    "context"
    "fmt"
    "github.com/google/uuid"
)

type HybridRetriever struct {
    BaseRetriever
    repo   *Repository
    llm    LLMClient
    logger *slog.Logger
}

func NewHybridRetriever(repo *Repository, llm LLMClient, logger *slog.Logger) *HybridRetriever {
    return &HybridRetriever{
        repo:   repo,
        llm:    llm,
        logger: logger,
    }
}

func (h *HybridRetriever) GetRetrievedObjects(ctx context.Context, query string, projectID uuid.UUID) ([]any, error) {
    // Existing search logic (FTS + vector + RRF)
    results, err := h.repo.Search(ctx, SearchParams{
        Query:     query,
        ProjectID: projectID,
        Limit:     50,
    })
    if err != nil {
        return nil, err
    }

    // Convert to []any for interface compatibility
    objects := make([]any, len(results))
    for i, r := range results {
        objects[i] = r
    }
    return objects, nil
}

func (h *HybridRetriever) GetContextFromObjects(ctx context.Context, query string, objects []any) (string, error) {
    // Convert results to LLM context
    context := "Relevant information:\n\n"
    for i, obj := range objects {
        result := obj.(*SearchResult)
        context += fmt.Sprintf("%d. %s (type: %s, score: %.2f)\n%s\n\n",
            i+1, result.Key, result.Type, result.Score, result.ContentSnippet)
    }
    return context, nil
}

func (h *HybridRetriever) GetCompletionFromContext(ctx context.Context, query string, objects []any, context string) (string, error) {
    // Call LLM with context
    prompt := fmt.Sprintf("Context:\n%s\n\nQuestion: %s\n\nAnswer:", context, query)
    return h.llm.Chat(ctx, prompt)
}
```

**Step 3: Implement Graph Traversal Retriever (New Strategy) (1 hour)**

```go
// apps/server-go/domain/search/graph_traversal_retriever.go
package search

import (
    "context"
    "fmt"
    "github.com/google/uuid"
)

// GraphTraversalRetriever explores graph relationships to find connected entities
type GraphTraversalRetriever struct {
    BaseRetriever
    graphRepo *graph.Repository
    llm       LLMClient
    logger    *slog.Logger
}

func (g *GraphTraversalRetriever) GetRetrievedObjects(ctx context.Context, query string, projectID uuid.UUID) ([]any, error) {
    // 1. Find seed entities via vector search
    seeds, err := g.graphRepo.FindSeedEntities(ctx, query, projectID, 5)
    if err != nil {
        return nil, err
    }

    // 2. Traverse graph from seeds (BFS, max 2 hops)
    var allObjects []any
    for _, seed := range seeds {
        neighbors, err := g.graphRepo.GetNeighbors(ctx, seed.ID, 2)
        if err != nil {
            g.logger.Warn("failed to get neighbors", slog.Any("seed", seed.ID), slog.Any("error", err))
            continue
        }

        // Build triplets (seed → relationship → neighbor)
        for _, neighbor := range neighbors {
            triplet := &GraphTriplet{
                Source:       seed,
                Relationship: neighbor.Relationship,
                Target:       neighbor.Node,
            }
            allObjects = append(allObjects, triplet)
        }
    }

    return allObjects, nil
}

func (g *GraphTraversalRetriever) GetContextFromObjects(ctx context.Context, query string, objects []any) (string, error) {
    // Format triplets as natural language
    context := "Knowledge graph connections:\n\n"
    for _, obj := range objects {
        triplet := obj.(*GraphTriplet)
        context += fmt.Sprintf("- %s %s %s\n",
            triplet.Source.Properties["name"],
            humanizeRelationship(triplet.Relationship.Type),
            triplet.Target.Properties["name"])
    }
    return context, nil
}
```

**Step 4: Update SearchService to Use Retriever (20 minutes)**

```go
// apps/server-go/domain/search/service.go
func (s *Service) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
    // Select retriever based on query type or user preference
    retriever := s.selectRetriever(params)

    // Use retriever pipeline
    completion, err := retriever.GetCompletion(ctx, params.Query, params.ProjectID)
    if err != nil {
        return nil, err
    }

    return &SearchResponse{
        Completion: completion,
        // ... other fields
    }, nil
}

func (s *Service) selectRetriever(params SearchParams) Retriever {
    // Strategy selection logic (future: could be LLM-driven)
    if params.Strategy == "graph" {
        return s.graphTraversalRetriever
    }
    return s.hybridRetriever // Default
}
```

**Step 5: Add Retriever Strategy Parameter to API (15 minutes)**

```go
// apps/server-go/domain/search/dto.go
type SearchRequest struct {
    Query    string  `json:"query"`
    Strategy *string `json:"strategy,omitempty"` // "hybrid", "graph", "temporal", etc.
    Limit    int     `json:"limit"`
}
```

### Risks & Mitigation

- **Complexity**: Multiple retriever implementations increase maintenance burden
  - Mitigation: Start with 2-3 core retrievers, add more only when proven valuable
- **Performance**: Different strategies have different costs
  - Mitigation: Add timeout/budget limits per retriever
- **Consistency**: Results may vary significantly between strategies
  - Mitigation: Document use cases for each retriever clearly

### Success Metrics

- Can A/B test retrieval strategies on same queries
- New retrieval algorithms added in <2 hours (vs days of refactoring)
- Domain-specific retrievers improve precision by 15-30%
- Developers can experiment without breaking production search

### Estimated Effort

- **Development**: 3 hours (interface + 2 retrievers + service integration)
- **Testing**: 1 hour (test both retrievers, A/B comparison)
- **Documentation**: 1 hour (retriever architecture, adding new strategies)
- **Total**: **5 hours**

---

## 5. Ontology Resolver (★ PRIORITY 5 - Medium Impact, Specialized Use)

### What It Is

Domain-specific entity validation against predefined ontology schemas (medical, legal, financial, etc.), ensuring extracted entities match expected types and relationships.

### Cognee Implementation

```python
# cognee/modules/ontology/base_ontology_resolver.py
class BaseOntologyResolver(ABC):
    @abstractmethod
    def get_subgraph(self, entity_type: str):
        """Get allowed properties and relationships for entity type"""
        pass

# cognee/tasks/graph/extract_graph_from_data.py
def integrate_chunk_graphs(data_chunks, chunk_graphs, graph_model, ontology_resolver):
    # Validate entities against ontology
    valid_node_ids = set()
    for node in graph.nodes:
        # Check if entity type allowed
        if ontology_resolver.is_valid_entity_type(node.type):
            # Check if properties match schema
            schema = ontology_resolver.get_schema(node.type)
            if validate_properties(node.properties, schema):
                valid_node_ids.add(node.id)

    # Only keep edges between valid nodes
    graph.edges = [e for e in graph.edges
                   if e.source_node_id in valid_node_ids
                   and e.target_node_id in valid_node_ids]
```

Example ontology (medical domain):

```yaml
# medical_ontology.yaml
entity_types:
  Disease:
    properties:
      - name: string (required)
      - icd_code: string (optional)
      - severity: enum[mild, moderate, severe]
    relationships:
      - CAUSES → Symptom
      - TREATED_BY → Medication

  Medication:
    properties:
      - name: string (required)
      - dosage: string (optional)
    relationships:
      - TREATS → Disease
      - CONTRAINDICATED_WITH → Medication
```

### Why Adopt This

- **Domain Quality**: Medical/legal/financial domains have strict validation requirements
- **Consistency**: Prevent type mismatches (e.g., "Person LOCATED_IN Organization" should be rejected)
- **Compliance**: Some industries require schema validation (HIPAA, GDPR, etc.)
- **LLM Guardrails**: Constrain extraction to valid entity types/properties

**Use Cases**:

- Medical: Validate diseases, medications, symptoms per ICD-10/SNOMED CT
- Legal: Validate case citations, statutes, legal entities
- Financial: Validate transactions, accounts, regulatory entities
- HR: Validate job titles, departments, org structure

### Emergent Implementation Plan

**Step 1: Define Ontology Schema Format (30 minutes)**

```yaml
# apps/server-go/config/ontologies/medical.yaml
name: Medical Ontology
version: 1.0
entity_types:
  Disease:
    properties:
      name:
        type: string
        required: true
      icd_code:
        type: string
        pattern: "^[A-Z][0-9]{2}\\.[0-9]$"
      severity:
        type: enum
        values: [mild, moderate, severe]
    relationships:
      - type: CAUSES
        target: Symptom
      - type: TREATED_BY
        target: Medication

  Medication:
    properties:
      name:
        type: string
        required: true
      dosage:
        type: string
    relationships:
      - type: TREATS
        target: Disease
```

**Step 2: Create OntologyResolver Interface (45 minutes)**

```go
// apps/server-go/domain/graph/ontology.go
package graph

import (
    "fmt"
    "regexp"
    "gopkg.in/yaml.v3"
)

type OntologyResolver interface {
    IsValidEntityType(typeName string) bool
    GetSchema(typeName string) (*EntitySchema, error)
    ValidateProperties(typeName string, properties map[string]interface{}) error
    IsValidRelationship(sourceType, relType, targetType string) bool
}

type EntitySchema struct {
    Properties   map[string]PropertySchema `yaml:"properties"`
    Relationships []RelationshipSchema     `yaml:"relationships"`
}

type PropertySchema struct {
    Type     string   `yaml:"type"`      // "string", "int", "enum"
    Required bool     `yaml:"required"`
    Pattern  string   `yaml:"pattern"`   // Regex for validation
    Values   []string `yaml:"values"`    // For enum type
}

type RelationshipSchema struct {
    Type   string `yaml:"type"`
    Target string `yaml:"target"`
}

type YAMLOntologyResolver struct {
    name         string
    entityTypes  map[string]*EntitySchema
}

func NewYAMLOntologyResolver(yamlPath string) (*YAMLOntologyResolver, error) {
    // Load YAML file
    data, err := os.ReadFile(yamlPath)
    if err != nil {
        return nil, fmt.Errorf("read ontology file: %w", err)
    }

    // Parse YAML
    var ontology struct {
        Name        string                      `yaml:"name"`
        EntityTypes map[string]*EntitySchema    `yaml:"entity_types"`
    }
    if err := yaml.Unmarshal(data, &ontology); err != nil {
        return nil, fmt.Errorf("parse ontology: %w", err)
    }

    return &YAMLOntologyResolver{
        name:        ontology.Name,
        entityTypes: ontology.EntityTypes,
    }, nil
}

func (r *YAMLOntologyResolver) IsValidEntityType(typeName string) bool {
    _, exists := r.entityTypes[typeName]
    return exists
}

func (r *YAMLOntologyResolver) ValidateProperties(typeName string, properties map[string]interface{}) error {
    schema, err := r.GetSchema(typeName)
    if err != nil {
        return err
    }

    // Check required properties
    for propName, propSchema := range schema.Properties {
        if propSchema.Required {
            if _, exists := properties[propName]; !exists {
                return fmt.Errorf("missing required property: %s", propName)
            }
        }

        // Validate property value
        if value, exists := properties[propName]; exists {
            if err := r.validatePropertyValue(propName, value, propSchema); err != nil {
                return err
            }
        }
    }

    return nil
}

func (r *YAMLOntologyResolver) validatePropertyValue(name string, value interface{}, schema PropertySchema) error {
    switch schema.Type {
    case "string":
        strVal, ok := value.(string)
        if !ok {
            return fmt.Errorf("property %s must be string", name)
        }
        if schema.Pattern != "" {
            matched, _ := regexp.MatchString(schema.Pattern, strVal)
            if !matched {
                return fmt.Errorf("property %s does not match pattern %s", name, schema.Pattern)
            }
        }
    case "enum":
        strVal, ok := value.(string)
        if !ok {
            return fmt.Errorf("property %s must be string (enum)", name)
        }
        valid := false
        for _, allowed := range schema.Values {
            if strVal == allowed {
                valid = true
                break
            }
        }
        if !valid {
            return fmt.Errorf("property %s has invalid enum value: %s (allowed: %v)", name, strVal, schema.Values)
        }
    }
    return nil
}
```

**Step 3: Integrate with GraphService (30 minutes)**

```go
// apps/server-go/domain/graph/service.go
func (s *Service) Create(ctx context.Context, projectID uuid.UUID, req *CreateGraphObjectRequest) (*GraphObjectResponse, error) {
    // Load project's ontology (if configured)
    ontology, err := s.schemaProvider.GetOntologyResolver(ctx, projectID.String())
    if err != nil {
        s.log.Warn("no ontology configured, skipping validation", slog.Any("error", err))
    } else {
        // Validate entity type
        if !ontology.IsValidEntityType(req.Type) {
            return nil, apperror.ErrBadRequest.WithMessage(
                fmt.Sprintf("invalid entity type: %s (not in ontology)", req.Type))
        }

        // Validate properties
        if err := ontology.ValidateProperties(req.Type, req.Properties); err != nil {
            return nil, apperror.ErrBadRequest.WithMessage(
                fmt.Sprintf("property validation failed: %v", err))
        }
    }

    // Continue with existing creation logic...
}
```

**Step 4: Add Ontology Selection to Project Config (20 minutes)**

```sql
-- Migration: 20260211_add_ontology_config.sql
ALTER TABLE core.projects
    ADD COLUMN ontology_name TEXT,
    ADD COLUMN ontology_version TEXT;

CREATE INDEX idx_projects_ontology
    ON core.projects(ontology_name)
    WHERE ontology_name IS NOT NULL;
```

### Risks & Mitigation

- **Maintenance**: Ontologies need updates as domain knowledge evolves
  - Mitigation: Version ontology files, allow multiple versions per project
- **Performance**: Validation adds 10-50ms per entity creation
  - Mitigation: Cache parsed ontologies in memory, async validation for bulk imports
- **Adoption**: Most projects may not need domain-specific validation
  - Mitigation: Make optional, disable by default, document specific use cases

### Success Metrics

- Medical projects reject invalid ICD codes (100% precision)
- Legal projects only create valid case citation entities
- Extraction quality score increases by 20-40% for domain-specific projects
- Zero schema violations in production (compliance requirement met)

### Estimated Effort

- **Development**: 2 hours (YAML parser + resolver + integration)
- **Testing**: 1 hour (medical ontology validation tests)
- **Documentation**: 1 hour (creating ontologies, examples)
- **Total**: **4 hours**

---

## 6. Multi-Backend Database Adapters (❌ DO NOT ADOPT)

### What It Is

Cognee's interface-based adapter pattern supporting multiple graph databases (Kuzu, Neo4j, Neptune) and vector databases (LanceDB, ChromaDB, PGVector).

### Why NOT Adopt

- **Emergent's Strength**: Single PostgreSQL database is a core architectural decision
- **Simplicity Trade-off**: Multi-backend support adds 3-5x maintenance burden
  - Must maintain 6+ adapters (3 graph × 2 for deprecation)
  - Must test across all combinations
  - Must handle adapter-specific bugs/quirks
- **No User Demand**: Users chose Emergent because it's PostgreSQL-only (simpler ops)
- **Performance**: pgvector + FTS in single database is faster than cross-database joins
- **Cost**: Multiple databases = multiple bills/licenses (Kuzu free, but Neo4j/Neptune expensive)

### If Forced to Support Multiple Databases (Hypothetical)

- **Only consider if**: Users explicitly demand Neo4j for graph analytics OR pgvector hits performance limits
- **Hybrid approach**: Keep PostgreSQL as primary, add read-only Neo4j replication for analytics
- **Don't**: Implement full abstraction layer (high cost, low benefit)

---

## Implementation Roadmap

### Phase 1: Quick Wins (Week 1)

**Goal**: Low-hanging fruit with high impact

1. **Access Tracking** (1 hour)

   - Add `last_accessed_at` column
   - Update SearchService to track
   - Create analytics queries

2. **Conversation History** (1.5 hours)
   - Extend chat schema
   - Update ChatService with history
   - Test multi-turn conversations

**Deliverables**: 2 new features, 2.5 hours dev time

### Phase 2: Enhanced Search (Week 2)

**Goal**: Richer search capabilities

3. **Triplet Embedding** (1.7 hours)
   - Add relationship embeddings
   - Update search to include relationships
   - Test relationship queries

**Deliverables**: Semantic relationship search working

### Phase 3: Advanced Patterns (Month 2)

**Goal**: Strategic flexibility and quality

4. **Pluggable Retrievers** (5 hours)

   - Define Retriever interface
   - Implement 2 retrievers (Hybrid, GraphTraversal)
   - Update SearchService
   - A/B test both strategies

5. **Ontology Resolver** (4 hours)
   - YAML ontology format
   - OntologyResolver implementation
   - Integration with GraphService
   - Medical domain ontology example

**Deliverables**: Extensible search architecture + domain validation

---

## Success Criteria

### Access Tracking

- ✅ Zero search latency impact (async updates)
- ✅ Top 100 accessed entities dashboard live
- ✅ Can identify unused content

### Conversation History

- ✅ Users ask follow-ups without repeating context
- ✅ AI references previous turns naturally
- ✅ Conversation coherence score +20%

### Triplet Embedding

- ✅ "Founded by" queries work correctly
- ✅ Search precision +10-20%
- ✅ LLM context includes relationships

### Pluggable Retrievers

- ✅ New retriever in <2 hours
- ✅ Can A/B test strategies
- ✅ Graph retriever precision +15-30%

### Ontology Resolver

- ✅ Medical projects validate ICD codes
- ✅ Legal projects validate citations
- ✅ Zero schema violations (compliance)

---

## Resource Requirements

### Development Time

- **Phase 1**: 2.5 hours (1 developer)
- **Phase 2**: 1.7 hours (1 developer)
- **Phase 3**: 9 hours (1-2 developers)
- **Total**: 13.2 hours (~2 days)

### Infrastructure

- **Database**: Existing PostgreSQL (no new dependencies)
- **Embedding API**: Existing Vertex AI (triplet embedding)
- **Storage**: +3KB per relationship (negligible)

### Maintenance

- **Access Tracking**: Minimal (passive feature)
- **Conversation History**: Minimal (extends existing chat)
- **Triplet Embedding**: Low (piggybacks on existing embedding pipeline)
- **Pluggable Retrievers**: Medium (new retrievers require testing)
- **Ontology Resolver**: Medium (ontologies need updates)

---

## Risk Assessment

| Pattern              | Risk Level  | Mitigation                             |
| -------------------- | ----------- | -------------------------------------- |
| Access Tracking      | Low         | Async updates, minimal DB load         |
| Conversation History | Low         | Extends proven chat system             |
| Triplet Embedding    | Medium      | Batch embedding, manageable storage    |
| Pluggable Retrievers | Medium-High | Start with 2-3 core retrievers         |
| Ontology Resolver    | Medium      | Optional feature, versioned ontologies |

---

## Alternatives Considered

### Cognee Patterns NOT Recommended

1. **Brute-Force Triplet Search**

   - Cognee: Fetches 100 nodes + 100 edges, scores all combinations, picks top K
   - Problem: O(N²) complexity, slow for large graphs
   - Emergent Alternative: Index-based queries (pgvector + GIN indexes)

2. **SQLite for Relational Data**

   - Cognee: SQLite (dev) → PostgreSQL (prod) creates test/prod disparity
   - Emergent: PostgreSQL everywhere → consistent behavior

3. **Separate Vector/Graph/Relational Databases**
   - Cognee: LanceDB (vector) + Kuzu (graph) + SQLite (relational) = 3 DBs
   - Emergent: PostgreSQL (all 3) = 1 DB, simpler ops, faster joins

---

## Next Steps

### Immediate Actions

1. ✅ **Approve/Reject Priorities**: Review priority 1-3 quick wins with team
2. Create GitHub issues for approved patterns
3. Schedule Phase 1 sprint (Week 1)

### Documentation Needed

- API documentation for new search strategies
- Analytics dashboard designs for access tracking
- Ontology creation guide (for domain-specific projects)

### Open Questions

1. Should retriever strategy selection be:

   - Manual (user chooses in UI)?
   - Automatic (LLM classifies query type)?
   - Hybrid (default + manual override)?

2. Ontology maintenance:
   - Who maintains ontologies (Emergent team vs users)?
   - How often updated (quarterly, on-demand)?
   - Versioning strategy (backward compatible)?

---

## Conclusion

**Recommended Adoption Order**:

1. ✅ **Access Tracking** (1 hour) - Instant value, zero risk
2. ✅ **Conversation History** (1.5 hours) - High user impact
3. ✅ **Triplet Embedding** (1.7 hours) - Better search quality
4. ⏳ **Pluggable Retrievers** (5 hours) - Strategic flexibility (Phase 3)
5. ⏳ **Ontology Resolver** (4 hours) - Specialized use cases (Phase 3)
6. ❌ **Multi-Backend Adapters** (Do NOT adopt) - Conflicts with core architecture

**Total Quick Wins Time**: 4.2 hours (Phase 1+2)  
**Total Strategic Time**: 9 hours (Phase 3)  
**ROI**: High (proven patterns from production system)

**Key Success Factor**: Adopt incrementally, measure impact, iterate based on user feedback. Don't blindly port all 15 Cognee retrievers - start with 2-3 proven ones.
