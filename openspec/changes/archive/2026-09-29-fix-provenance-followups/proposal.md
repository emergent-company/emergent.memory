## Why

A post-merge review of `agent-object-provenance` found no blockers, but several correctness gaps in the shipped provenance work:

- The `created` provenance subquery matches on `(actor_type, actor_id, version=1, deleted_at)` with no project/branch predicate. A `system`-actor query (NULL `actor_id`) materializes every system-authored version-1 row deployment-wide, not just this project's.
- Merge `conflict` and `similar` paths (and the relationship similarity-merge path) create new versions without `ActorType`/`ActorID`, silently downgrading them to `user`+NULL — unlike the `added`/`fast_forward` paths that already carry the source actor.
- `actor_type` is not validated at the API boundary, and `provenance` without `actor_type` silently no-ops.
- A delegated run whose definition is unresolvable inherits the PARENT agent's actor instead of falling back.
- iOS derives the gateway base URL by stripping `/api/token` from the token endpoint instead of using the dedicated `apiBaseURL`.

## What Changes

- Scope the `created` provenance subquery to the same `project_id` (and branch) as the outer query, keeping the partial-index-friendly shape.
- Carry the source row's actor on merge `conflict`, `similar`, and relationship similarity-merge versions.
- Validate `actor_type` ∈ {`user`, `agent`, `system`} and reject `provenance` without `actor_type` with a 400.
- Give `auth.WithActor` an explicit "clear" form (empty actor type) and always stamp the actor at the run boundary, so an unresolvable delegated run clears the inherited parent actor.
- iOS object browser uses `config.apiBaseURL` directly.

## Capabilities

### Modified Capabilities

- `object-provenance`: validation rules (`actor_type` enum, `provenance` requires `actor_type`), project/branch scoping of the `created` filter, merge conflict/similar actor preservation, and the delegated-run actor-clear semantics.

## Impact

- Server (`apps/server/domain/graph`, `apps/server/pkg/auth`, `apps/server/domain/agents`): filter scoping, validation, merge-path actor stamping, and context clear semantics.
- iOS (`apps/ios`): object browser base-URL derivation.
- No migration, no wire-shape change beyond new 400 responses for previously-accepted invalid inputs.
