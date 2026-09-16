# graph-traversal-pagination Specification

## Purpose
Defines correct, bounded pagination for knowledge-graph traversal: `TraverseGraph` computes `HasNextPage` from the actual result set instead of hard-coding `false`, honors `pageSize` (default 50, cap 1000) with offset/cursor continuation, and `ExpandGraph` reports truncation when its `MaxNodes`/`MaxEdges` bounds are reached. It exists so callers can page through large traversals without re-running the full traversal or silently dropping results.

## Requirements

### Requirement: TraverseGraph returns correct HasNextPage
The `TraverseGraph` endpoint SHALL compute `HasNextPage` from the actual pagination state rather than hard-coding it to `false`. When more results remain beyond the current page, `HasNextPage` MUST be `true`.

#### Scenario: More results remain signals continuation
- **WHEN** `TraverseGraph` is called with `pageSize=50`
- **AND** the traversal yields more than 50 nodes
- **THEN** the response SHALL set `HasNextPage=true`
- **AND** SHALL expose a continuation token or offset to fetch the next page

#### Scenario: Final page signals end
- **WHEN** `TraverseGraph` returns fewer results than `pageSize`
- **THEN** the response SHALL set `HasNextPage=false`

### Requirement: TraverseGraph honors pageSize and offset
`TraverseGraph` SHALL honor the `pageSize` parameter (default 50, cap 1000) and support an offset (or cursor) to retrieve subsequent pages without re-running the full traversal.

#### Scenario: Second page returns next slice
- **WHEN** the first page returns 50 nodes and `HasNextPage=true`
- **AND** a follow-up request is made with the continuation token/offset
- **THEN** the response MUST return the next 50 nodes (non-overlapping with page one)

### Requirement: ExpandGraph pagination is bounded and consistent
`ExpandGraph` SHALL apply its `MaxNodes`/`MaxEdges` bounds consistently and expose a `HasMore` flag (or equivalent) so callers can detect truncation.

#### Scenario: Truncation is observable
- **WHEN** `ExpandGraph` hits the `MaxNodes` cap
- **THEN** the response MUST indicate truncation occurred (e.g., `HasMore=true`)
