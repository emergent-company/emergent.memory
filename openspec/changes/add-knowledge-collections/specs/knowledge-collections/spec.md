## ADDED Requirements

### Requirement: Collections are created, updated, and deleted

A user SHALL be able to create a named collection within a project, update its name and description, and delete it. Deleting a collection SHALL also remove its membership items. Collections SHALL be project-scoped.

#### Scenario: Create a collection

- **WHEN** a user creates a collection with a name in their project
- **THEN** the collection is persisted with the project id and the creating user can see it

#### Scenario: Update a collection

- **WHEN** a user updates a collection's name or description
- **THEN** the change is persisted and visible to other members of the project

#### Scenario: Delete a collection removes its items

- **WHEN** a collection is deleted
- **THEN** the collection and all its `collection_items` rows SHALL be removed

#### Scenario: Names are project-scoped

- **WHEN** two projects each create a collection with the same name
- **THEN** both collections coexist independently within their own projects

### Requirement: Collection items reference documents or graph objects

A collection item SHALL reference exactly one of: a `document_id` or a graph `canonical_id`. A `source` item type SHALL NOT exist in v1. The collection SHALL be an addressable subgraph filter over these references.

#### Scenario: Add a document item

- **WHEN** a user adds a document to a collection
- **THEN** a collection item of type `document` referencing that document id is created

#### Scenario: Add a graph object item

- **WHEN** a user adds a graph object by canonical id to a collection
- **THEN** a collection item of type `canonical` referencing that canonical id is created

#### Scenario: Duplicate membership is idempotent

- **WHEN** the same item is added to a collection twice
- **THEN** no duplicate membership row is created

### Requirement: Items are added to and removed from collections

A user SHALL be able to add and remove items from a collection. Removal SHALL delete the membership row without deleting the underlying document or graph object.

#### Scenario: Remove an item

- **WHEN** a user removes an item from a collection
- **THEN** the membership row is deleted and the underlying referenced object is unchanged

### Requirement: Membership is project-scoped

Collection items SHALL only reference objects belonging to the same project as the collection. Cross-project membership SHALL be rejected at **write time**: adding an item whose object belongs to a different project SHALL fail. (This is distinct from search filtering, where naming a cross-project collection id simply returns no results at read time.)

#### Scenario: Cross-project item rejected at write

- **WHEN** a caller attempts to add an item from a different project to a collection
- **THEN** the operation is rejected (write-time), and no membership row is created

### Requirement: Canonical id items resolve to the graph head

When a collection item references a graph `canonical_id`, a search or retrieval filtered by that collection SHALL resolve the canonical id to its current head (per `graph-head-resolution`) so the collection tracks the live object. Head resolution SHALL follow the current head of the stored `canonical_id` but SHALL NOT follow a merge/rename into a **new** `canonical_id`.

#### Scenario: Canonical item resolves to head

- **WHEN** a collection contains a canonical id whose head has advanced
- **THEN** search/retrieval filtered by that collection SHALL match the current head object

#### Scenario: Merge to a new canonical id is not followed

- **WHEN** a stored canonical id is merged into a fresh canonical id
- **THEN** the collection SHALL NOT automatically gain membership of the new canonical id

### Requirement: Relationship membership is endpoint-based

A relationship SHALL belong to a collection when either of its endpoints (`src_id` OR `dst_id`) is in the collection's canonical set. This applies to search/retrieval filtered by a collection.

#### Scenario: Relationship matches by either endpoint

- **WHEN** a collection contains a canonical id that is the `src_id` or `dst_id` of a relationship
- **THEN** the relationship SHALL be returned by search/retrieval filtered by that collection

### Requirement: Empty or dangling collections yield no results

A collection with no members SHALL contribute no results to a search, and a search filtered to an empty collection SHALL return no results. A collection whose items all resolve to soft-deleted or dangling objects SHALL behave the same as an empty collection (no results, not an error).

#### Scenario: Empty collection search is empty

- **WHEN** a search is filtered to a collection that has no members
- **THEN** the search returns no results

#### Scenario: All-dangling collection behaves as empty

- **WHEN** every item in a collection resolves to a soft-deleted or dangling object
- **THEN** the collection contributes no results and no error is raised

### Requirement: Collections are reused across search and agent scoping

The same collection primitive SHALL be usable to scope unified search, agent knowledge scoping, and chat retrieval, without each surface maintaining its own membership store.

#### Scenario: Agent scoped to a collection

- **WHEN** an agent's knowledge scope references a collection
- **THEN** retrieval for that agent SHALL be restricted to the collection's members

#### Scenario: Chat retrieval scoped to a collection

- **WHEN** a chat retrieval request references a collection
- **THEN** retrieval SHALL be restricted to the collection's members
