## Purpose

Defines how a branch merge that enables similarity uses relationship embeddings to detect a
staged relationship that is equivalent to one already present on the target, so it is
absorbed as a new version with merged properties instead of being inserted as a duplicate.

## ADDED Requirements

### Requirement: Relationship embedding is read from the live column

The relationship embedding lookup SHALL read `kb.graph_relationships.embedding` (added in
migration 00011, with `embedding_updated_at` in migration 00013). It MUST NOT reference a
non-existent `embedding_v2` column on that table.

#### Scenario: Embedded relationship returns its vector

- **WHEN** a relationship row has a vector stored in `kb.graph_relationships.embedding`
- **THEN** `GetBranchRelationshipEmbedding` MUST return that vector without error

#### Scenario: Un-embedded relationship is not an error

- **WHEN** a relationship row has a NULL `embedding`
- **THEN** the lookup MUST return `nil, nil` rather than an error

### Requirement: Similar relationships are detected by remapped endpoints

When similarity is enabled for a merge, a staged relationship classified as added SHALL have
its endpoints remapped onto any absorbed target objects and SHALL be probed against existing
target relationships with the same `(src_id, dst_id)` and cosine distance within the
similarity threshold.

#### Scenario: Equivalent staged relationship is found

- **WHEN** a staged relationship's remapped endpoints match an existing target relationship
- **AND** the cosine distance between their embeddings is within the threshold
- **THEN** the existing target relationship MUST be identified as the similarity match

### Requirement: A matched relationship is absorbed, not duplicated

When a staged relationship matches an existing target relationship, the merge SHALL create a
new version of the target relationship with properties merged per the conflict policy,
instead of inserting a new relationship row.

#### Scenario: Matched relationship is enriched in place

- **WHEN** a staged relationship matches an existing target relationship
- **AND** the staged relationship carries a property key absent from the target
- **THEN** the target relationship MUST receive a new version enriched with that key
- **AND** no additional live relationship MUST be created for that source/target pair

#### Scenario: Different-type or below-threshold relationship is still added

- **WHEN** no target relationship is within the similarity threshold of the staged
  relationship
- **THEN** the staged relationship MUST follow the existing add path
