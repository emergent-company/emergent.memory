# agent-type-scoping-followups

## 1. Spec artifacts

- [x] 1.1 Write `openspec/changes/agent-type-scoping-followups/proposal.md`
- [x] 1.2 Write `openspec/changes/agent-type-scoping-followups/tasks.md`
- [x] 1.3 Write `openspec/changes/agent-type-scoping-followups/.openspec.yaml`
- [x] 1.4 Write delta spec for `agent-object-type-scoping` (MODIFIED + ADDED requirements)
- [x] 1.5 Validate with `openspec validate agent-type-scoping-followups --strict`

## 2. Item A — writes are scoped

- [x] 2.1 `entity-create`: reject an object type outside the object allowlist (clear error, no excluded type name echoed)
- [x] 2.2 `entity-create`: skip inline relationships whose relationship type is excluded
- [x] 2.3 `relationship-create`: reject a relationship type outside the relationship allowlist
- [x] 2.4 `entity-update`: gate on the object's current type (lookup helper; fail-open on lookup error)
- [x] 2.5 `relationship-update`: gate on the relationship's current type (lookup helper)
- [x] 2.6 Empty/nil scope unchanged (all types)

## 3. Item B — search-knowledge threads the scope

- [x] 3.1 `executeQueryKnowledge` serializes the caller scope into the `/query` request body
- [x] 3.2 `/query` handler accepts `object_types` / `relationship_types` and attaches them to the internal run context
- [x] 3.3 Scoped call no longer fails closed; internal run enforces the caller scope (narrow-only)
- [x] 3.4 Harden the executor to `NarrowTypeScope(inherited, declared)` instead of replacing, so an inherited scope is never widened by a definition allowlist (incl. a future internal-agent allowlist)

## 4. Item C — relationship-list pagination metadata

- [x] 4.1 Add `RelationshipListParams.Types` and apply `type IN (?)` in count + list BEFORE pagination
- [x] 4.2 `executeListRelationships` sets `params.Types` from the relationship allowlist; reject an explicit excluded `type`
- [x] 4.3 `total` / `has_more` reflect the filtered set (remove post-filter override)

## 5. Item D — graph-traverse dangling edges

- [x] 5.1 Drop edges whose endpoints fall outside the object allowlist (object axis restricted), incl. object-only scopes
- [x] 5.2 Unrestricted paths byte-identical

## 6. Item E — entity-history is type-scoped

- [x] 6.1 Select the object type and reject history for an object outside the object allowlist (no excluded type name)

## 7. Tests

- [x] 7.1 Writes: excluded object/relationship create rejected; allowed create succeeds; empty scope creates unchanged; excluded update rejected
- [x] 7.2 `relationship-list`: `total`/`has_more` correct for the filtered set with an excluded type seeded ahead of allowed ones
- [x] 7.3 `graph-traverse`: object-scope (incl. object-only) drops edges referencing excluded object ids
- [x] 7.4 `entity-history`: rejected for excluded type; allowed type returns history
- [x] 7.5 `search-knowledge`: scope threaded into the loopback body; unscoped omits the fields; `/query` attaches the scope to the run context
- [x] 7.6 Mutation checks: each enforcement removed in turn makes its test fail (restored after)

## 8. Verification

- [x] 8.1 `go build ./...` + `go vet ./...`
- [x] 8.2 `go test ./domain/agents/... ./domain/mcp/... ./domain/graph/... ./domain/chat/... -count=1`
- [x] 8.3 `golangci-lint run --new-from-rev=origin/main` on touched packages → 0 new issues
- [x] 8.4 `openspec validate agent-type-scoping-followups --strict`
- [x] 8.5 No migration added
