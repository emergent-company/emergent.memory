## MODIFIED Requirements

### Requirement: A namespace restriction scopes the chunk leg through the object link

A unified search carrying a `namespace` filter SHALL apply that filter to the
document-chunk (text) leg, not only to the graph leg. Because `kb.chunks` has no
namespace of its own, a chunk belongs to a namespace through the extraction link
`kb.object_chunks` → `kb.graph_objects.namespace`, scoped to the same project.
The restriction SHALL be applied inside every chunk query (lexical, vector, and
both legs of hybrid, including the relax/disjoin retries), so the row limit is
applied after the filter and no out-of-namespace chunk can reach the response.

A chunk with no extracted object in the requested namespace SHALL be excluded
(fail-closed). Because graph deletes are versioned (`SoftDelete` inserts a
tombstone HEAD and supersedes the prior HEAD), the predicate SHALL test the
**live HEAD** of the object the chunk was extracted from — the
`supersedes_id IS NULL AND deleted_at IS NULL` row of that object's
`canonical_id` in the same branch — so a soft-deleted object's chunks are
excluded even though the linked version row's own `deleted_at` is still NULL. A
chunk linked to a superseded but still-live version SHALL still count: namespace
is inherited across versions within a branch, and a branch-forked copy (which
carries no namespace of its own) cannot admit a chunk under a namespace it was
never extracted into.

When the caller requests the **default** namespace (the MCP `search-hybrid` /
`search-semantic` tools with no `namespace` argument, which the MCP layer treats
as the null namespace — see its schema-type allow/block lists), the chunk leg
SHALL be scoped to chunks whose extracted object's live HEAD has
`namespace IS NULL`, so chunks extracted into non-null-namespace objects never
leak into a default search. An unfiltered (`namespace: "all"`) search SHALL carry
no chunk namespace predicate and behave exactly as before.

#### Scenario: A restricted namespace never returns an out-of-namespace chunk

- **WHEN** a caller requests unified search with `namespace: "system"` and `include_chunks: true`
- **THEN** every returned chunk belongs to at least one graph object whose live HEAD namespace is `"system"` in the same project
- **AND** no chunk reachable only through another namespace is returned, whether it matched lexically or by embedding similarity

#### Scenario: A chunk with no live object in the namespace is excluded

- **WHEN** a namespace-restricted search would match a chunk whose extracted object has no live (`supersedes_id IS NULL AND deleted_at IS NULL`) HEAD in that namespace — including an object that was soft-deleted
- **THEN** the chunk is not returned

#### Scenario: A chunk linked to a superseded but live version still counts

- **WHEN** a namespace-restricted search matches a chunk whose `kb.object_chunks` link points at a superseded version row and the object's live HEAD is in the requested namespace
- **THEN** the chunk is returned

#### Scenario: A default search scopes chunks to the null namespace

- **WHEN** a caller runs an MCP default search (`namespace` omitted) with `include_chunks: true`
- **THEN** only chunks whose extracted object's live HEAD has `namespace IS NULL` are returned, and chunks extracted into non-null-namespace objects are excluded

#### Scenario: An unrestricted search is unchanged

- **WHEN** a caller requests unified search with `namespace: "all"`
- **THEN** the chunk queries carry no namespace predicate and behave exactly as before
