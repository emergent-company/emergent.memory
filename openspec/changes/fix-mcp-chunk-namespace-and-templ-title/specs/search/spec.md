## ADDED Requirements

### Requirement: A namespace restriction scopes the chunk leg through the object link

A unified search carrying a `namespace` filter SHALL apply that filter to the
document-chunk (text) leg, not only to the graph leg. Because `kb.chunks` has no
namespace of its own, a chunk belongs to a namespace through the extraction link
`kb.object_chunks` → `kb.graph_objects.namespace`, scoped to the same project.
The restriction SHALL be applied inside every chunk query (lexical, vector, and
both legs of hybrid, including the relax/disjoin retries), so the row limit is
applied after the filter and no out-of-namespace chunk can reach the response.

A chunk with no extracted object in the requested namespace SHALL be excluded
(fail-closed), and a soft-deleted object SHALL NOT count as an extraction into
the namespace. The default namespace and `all` SHALL be unaffected. Branch and
version scoping SHALL NOT be applied: namespace is inherited across versions
within a branch, so a chunk extracted into a superseded or namespace-carrying
branch version of an object in the requested namespace still belongs to it, and a
branch-forked copy (which carries no namespace of its own) cannot admit a chunk
under a namespace it was never extracted into.

#### Scenario: A restricted namespace never returns an out-of-namespace chunk

- **WHEN** a caller requests unified search with `namespace: "system"` and `include_chunks: true`
- **THEN** every returned chunk belongs to at least one graph object whose namespace is `"system"` in the same project
- **AND** no chunk reachable only through another namespace is returned, whether it matched lexically or by embedding similarity

#### Scenario: A chunk with no live object in the namespace is excluded

- **WHEN** a namespace-restricted search would match a chunk that no live object in that namespace was extracted from
- **THEN** the chunk is not returned

#### Scenario: An unrestricted search is unchanged

- **WHEN** a caller requests unified search without a namespace, or with `namespace: "all"`
- **THEN** the chunk queries carry no namespace predicate and behave exactly as before
