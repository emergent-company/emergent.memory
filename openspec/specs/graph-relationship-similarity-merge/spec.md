# graph-relationship-similarity-merge Specification

## Purpose
Defines how a branch merge that enables similarity uses relationship embeddings to detect a
staged relationship that is equivalent to one already present on the target, so it is
absorbed as a new version with merged properties instead of being inserted as a duplicate.

## Requirements

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

### Requirement: Similar relationships are detected by type and remapped endpoints

When similarity is enabled for a merge, a staged relationship classified as added SHALL have
its endpoints remapped onto any absorbed target objects and SHALL be probed against existing
target relationships with the same relationship `type` AND the same `(src_id, dst_id)`, with a
cosine distance within the similarity threshold. A relationship of a different `type` on the
same endpoints MUST NOT be treated as a match.

#### Scenario: Equivalent staged relationship is found

- **WHEN** a staged relationship's remapped endpoints match an existing target relationship of
  the same `type`
- **AND** the cosine distance between their embeddings is within the threshold
- **THEN** the existing target relationship MUST be identified as the similarity match

#### Scenario: Same-endpoint relationship of a different type is not a match

- **WHEN** a staged relationship's remapped endpoints match an existing target relationship but
  its `type` differs
- **THEN** the staged relationship MUST NOT be identified as a similarity match

### Requirement: A matched relationship is absorbed, not duplicated

When a staged relationship matches an existing target relationship, the merge SHALL create a
new version of the target relationship with properties merged per the conflict policy,
instead of inserting a new relationship row. The new version SHALL preserve the existing
relationship's `label` and `weight`.

#### Scenario: Matched relationship is enriched in place

- **WHEN** a staged relationship matches an existing target relationship
- **AND** the staged relationship carries a property key absent from the target
- **THEN** the target relationship MUST receive a new version enriched with that key
- **AND** no additional live relationship MUST be created for that source/target pair
- **AND** the new version MUST retain the target relationship's existing `label` and `weight`

#### Scenario: Different-type or below-threshold relationship is still added

- **WHEN** no target relationship of the same `type` and `(src_id, dst_id)` is within the
  similarity threshold of the staged relationship (including a same-endpoint relationship of a
  different `type`)
- **THEN** the staged relationship MUST follow the existing add path
