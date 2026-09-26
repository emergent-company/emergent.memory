## ADDED Requirements

### Requirement: Content-level diff between revisions
The API SHALL return a content diff between two revisions of the same logical document via `GET /api/documents/:id/revisions/diff?from=<version|revisionId>&to=<version|revisionId>`. The diff SHALL be computed over the parsed `content` of both revisions and SHALL include a unified, line-oriented diff with added, removed, and unchanged segments plus counts of added and removed lines. When `from` or `to` is omitted, the API SHALL default `to` to the current revision and `from` to the revision immediately preceding it. Both revisions MUST belong to the same `document_group_id`; a cross-group request SHALL be rejected.

#### Scenario: Diff two parsed revisions
- **WHEN** a client requests a diff between two revisions whose content has been parsed
- **THEN** the API returns a unified line diff and added/removed line counts for that pair

#### Scenario: Default revision pair
- **WHEN** a client requests a diff without specifying revisions and the group has at least two revisions
- **THEN** the API diffs the previous revision against the current revision

#### Scenario: Cross-group diff rejected
- **WHEN** a client requests a diff where one revision does not belong to the target document's group
- **THEN** the API returns a validation error

#### Scenario: Content not yet parsed
- **WHEN** a client requests a diff where one revision's conversion has not completed
- **THEN** the API returns a conflict-style error explaining that content is not yet available, and no partial diff is returned

### Requirement: Entity delta between revisions
The same diff response SHALL include a structured entity delta derived from the revisions' extracted graph objects and relationships. The delta SHALL classify each graph object as `added`, `updated`, or `removed`, matched by `(type, key)` within the project, and SHALL likewise report added and removed relationships. Objects and relationships SHALL be attributed to a revision using extraction provenance (`kb.object_chunks` and the extraction job id), not by re-querying the main graph alone.

#### Scenario: Added entity in the newer revision
- **WHEN** extraction of the target revision produces a graph object whose `(type, key)` is not part of the source revision's extracted set
- **THEN** the delta lists that object under `added`

#### Scenario: Updated entity between revisions
- **WHEN** the same `(type, key)` is extracted from both revisions but with different properties
- **THEN** the delta lists that object under `updated` and includes the changed properties

#### Scenario: Removed entity
- **WHEN** a graph object was produced by the source revision's extraction but its only provenance chunks are absent from the target revision
- **THEN** the delta lists that object under `removed`

#### Scenario: No extraction available for a revision
- **WHEN** a diff is requested for a revision that has no extraction results
- **THEN** the response reports the content diff and an empty entity delta rather than failing
