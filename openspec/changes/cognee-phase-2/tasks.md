## 1. Database Migration

- [ ] 1.1 Create migration file: `apps/server-go/migrations/20260212_add_relationship_embeddings.sql`
- [ ] 1.2 Add `embedding vector(768)` column to `kb.graph_relationships` (nullable initially)
- [ ] 1.3 Add SQL comment documenting nullable rationale (backfill support)
- [ ] 1.4 Apply migration to dev environment and verify schema change
- [ ] 1.5 Test rollback scenario (DROP COLUMN)

## 2. Triplet Text Generation

- [ ] 2.1 Add `humanizeRelationType()` helper in `graph.Service` (WORKS_FOR → "works for")
- [ ] 2.2 Add `getDisplayName()` helper with fallback to object key when name missing
- [ ] 2.3 Add `generateTripletText()` method combining source + relation + target
- [ ] 2.4 Add unit tests for humanization edge cases (empty string, special chars, unicode)
- [ ] 2.5 Add unit tests for name fallback logic (nil name, empty name, missing property)

## 3. Embedding Service Integration

- [ ] 3.1 Update `GraphRelationship` entity in `entity.go` to include `Embedding pgvector.Vector` field
- [ ] 3.2 Tag embedding field with `bun:"embedding,type:vector(768),nullzero"`
- [ ] 3.3 Inject embedding service into `graph.Service` constructor
- [ ] 3.4 Add `embedTripletText()` method that calls Vertex AI embedding service
- [ ] 3.5 Add retry logic with exponential backoff for embedding API calls (handle 429 rate limits)
- [ ] 3.6 Add unit tests for embedding service errors (mock API failure scenarios)

## 4. Relationship Creation Enhancement

- [ ] 4.1 Update `CreateRelationship()` to generate triplet text before DB insert
- [ ] 4.2 Update `CreateRelationship()` to call embedding service synchronously
- [ ] 4.3 Update `CreateRelationship()` to store embedding in relationship record
- [ ] 4.4 Add transaction rollback logic if embedding generation fails
- [ ] 4.5 Add integration test: verify embedding is NOT NULL for new relationships
- [ ] 4.6 Add integration test: verify transaction rollback on embedding failure
- [ ] 4.7 Add performance test: verify p95 latency < 300ms for relationship creation

## 5. Database Index Creation

- [ ] 5.1 Create separate migration: `20260212_add_relationship_embedding_index.sql`
- [ ] 5.2 Add ivfflat index with CREATE INDEX CONCURRENTLY (lists=100, vector_cosine_ops)
- [ ] 5.3 Document expected index build time in migration comments (10 min per 1M rows)
- [ ] 5.4 Add verification query to check index usage (EXPLAIN ANALYZE)
- [ ] 5.5 Test index creation on staging environment with production-like data volume

## 6. Relationship Vector Search

- [ ] 6.1 Add `searchRelationships()` method to `search.Repository`
- [ ] 6.2 Implement vector similarity query filtering `WHERE embedding IS NOT NULL`
- [ ] 6.3 Add configurable result limit parameter (default: 50)
- [ ] 6.4 Return triplet text in search results alongside relationship metadata
- [ ] 6.5 Add unit tests for null embedding filtering
- [ ] 6.6 Add integration test: verify index is used (check query plan)
- [ ] 6.7 Add integration test: verify results ranked by cosine similarity

## 7. RRF Merging Implementation

- [ ] 7.1 Extract existing RRF logic into reusable `reciprocalRankFusion()` helper
- [ ] 7.2 Update helper to support merging 2+ result sets (not just FTS + vector)
- [ ] 7.3 Update `Search()` method to call both `searchNodes()` and `searchRelationships()` in parallel
- [ ] 7.4 Apply RRF with k=60 to merge node and edge results
- [ ] 7.5 Add unit tests for RRF edge cases (empty node results, empty edge results, both empty)
- [ ] 7.6 Add integration test: verify parallel execution (not sequential)
- [ ] 7.7 Add performance test: verify p95 latency increase < 100ms

## 8. Search Response Format Update

- [ ] 8.1 Update search response DTO to include optional `Relationships []RelationshipResult` field
- [ ] 8.2 Add `RelationshipResult` struct with triplet_text, source_id, target_id, relation_type fields
- [ ] 8.3 Update search controller to populate relationships array when results exist
- [ ] 8.4 Add API documentation for new response format (OpenAPI/Swagger)
- [ ] 8.5 Add integration test: verify backward compatibility (old clients ignore relationships field)
- [ ] 8.6 Add integration test: verify new clients receive both objects and relationships

## 9. LLM Context Enhancement

- [ ] 9.1 Update LLM prompt builder to accept relationships array parameter
- [ ] 9.2 Format relationship triplets as "Relationship: {triplet_text}" in context
- [ ] 9.3 Add relationships section to prompt template after objects section
- [ ] 9.4 Add unit tests for prompt formatting with mixed results (objects + relationships)
- [ ] 9.5 Add integration test: verify LLM receives relationship context in prompts

## 10. Backfill Script (Optional)

- [ ] 10.1 Create backfill command: `apps/server-go/cmd/backfill-embeddings/main.go`
- [ ] 10.2 Query relationships WHERE embedding IS NULL in batches of 100
- [ ] 10.3 Generate triplet text for each relationship
- [ ] 10.4 Call embedding service with batch delay (100ms between batches)
- [ ] 10.5 Update relationships with embeddings in database
- [ ] 10.6 Add progress tracking (processed / total counts)
- [ ] 10.7 Add error handling and retry logic for embedding failures
- [ ] 10.8 Add dry-run mode for testing without database updates
- [ ] 10.9 Document backfill usage in operations guide

## 11. Monitoring and Observability

- [ ] 11.1 Add metrics for relationship creation latency (p50, p95, p99)
- [ ] 11.2 Add metrics for embedding service call duration
- [ ] 11.3 Add metrics for search latency breakdown (node query, edge query, RRF merge)
- [ ] 11.4 Add counter for relationships with null embeddings (monitor backfill progress)
- [ ] 11.5 Add counter for embedding service errors (rate limits, timeouts, etc.)
- [ ] 11.6 Add Cloud Monitoring alert for Vertex AI quota consumption
- [ ] 11.7 Add dashboard showing relationship embedding adoption rate over time

## 12. Documentation

- [ ] 12.1 Update API documentation with relationship embedding feature
- [ ] 12.2 Document triplet text format and generation rules
- [ ] 12.3 Document search response format changes (relationships array)
- [ ] 12.4 Create operations guide for index creation and backfill
- [ ] 12.5 Document monitoring metrics and alerts
- [ ] 12.6 Add migration guide for existing deployments
- [ ] 12.7 Update architecture diagrams showing embedding flow

## 13. Testing and Validation

- [ ] 13.1 Add end-to-end test: create relationship → verify embedding exists → search finds it
- [ ] 13.2 Add end-to-end test: search returns mixed objects and relationships
- [ ] 13.3 Add end-to-end test: LLM receives relationship context in prompt
- [ ] 13.4 Add load test: verify performance under concurrent relationship creation
- [ ] 13.5 Add load test: verify search performance with 1M+ relationships indexed
- [ ] 13.6 Verify backward compatibility: existing API clients work without changes
- [ ] 13.7 Run full regression test suite and verify no breaking changes
