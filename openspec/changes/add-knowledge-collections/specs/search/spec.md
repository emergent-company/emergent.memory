## ADDED Requirements

### Requirement: Unified search filters by collection

The unified search SHALL accept an optional list of collection ids on the request. When collection ids are present, each leg (graph, text/chunk, relationship) SHALL be restricted to items that belong to **every** listed collection (AND semantics) within the project. The relationship leg SHALL be restricted to relationships whose `src_id` OR `dst_id` is in the collection's canonical set (canonical-to-canonical, since `src_id`/`dst_id` store `canonical_id`). Head resolution SHALL apply to the graph-object leg only.

#### Scenario: Collection filter narrows all legs

- **WHEN** a unified search runs with one or more collection ids
- **THEN** graph, text/chunk, and relationship legs SHALL each return only items that are members of all specified collections

#### Scenario: Relationship leg uses canonical membership

- **WHEN** a unified search runs with a collection that contains graph objects by canonical id
- **THEN** the relationship leg SHALL return only relationships whose `src_id` OR `dst_id` is in the collection's canonical set (canonical-to-canonical)

#### Scenario: No collections behaves as today

- **WHEN** a unified search runs with no collection ids
- **THEN** each leg SHALL filter by project id only, unchanged from current behaviour

#### Scenario: Collection from another project yields nothing

- **WHEN** a search requests collection ids that belong to a different project
- **THEN** the request SHALL return no results for those collections rather than leaking cross-project items (read-filter, not a write rejection)

### Requirement: Multiple collections are AND-combined

When more than one collection id is specified, an item SHALL match only if it is a member of every listed collection.

#### Scenario: AND across collections

- **WHEN** a search requests collections A and B
- **THEN** only items present in both A and B SHALL be returned

#### Scenario: Empty collection contributes nothing

- **WHEN** one of the requested collections has no members
- **THEN** the combined result SHALL be empty
