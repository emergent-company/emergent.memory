## Purpose

Stamp an `embedding_space` (resolved model id + dimension) at graph-object and relationship writes so the
write path can detect an embedding-model change and re-embed rather than comparing vectors across spaces,
while keeping the fixed 768 dimension.

## ADDED Requirements

### Requirement: Stamp embedding_space and re-embed on model change

The graph-object (and relationship) write path SHALL stamp an `embedding_space` — the resolved model id plus
the embedding dimension — alongside the vector at write time. When the project's embedding model changes, the
write path SHALL re-embed the object rather than compare the previous vector (produced under a different
model) against the new model's space. The dimension SHALL remain fixed at 768 (`EmbeddingDimension`).

#### Scenario: Model change re-embeds rather than compares across spaces

- **WHEN** a project's embedding model changes and a write occurs
- **THEN** the object is re-embedded under the new model, not compared against a vector produced by the old model

#### Scenario: embedding_space records model id and dimension

- **WHEN** an embedding is written
- **THEN** the stored `embedding_space` records the resolved model id and the 768 dimension

#### Scenario: Dimension stays fixed at 768

- **WHEN** embeddings are generated
- **THEN** the dimension is 768; a dimension change is out of scope for this change
