# Fix provenance followups

## 1. Server

### 1.1 Scope the `created` provenance subquery to project + branch

- [ ] 1.1.1 Add `project_id` (and branch) predicates to the `created` subquery in `buildObjectBaseQueryWith`, mirroring the outer query's scope
- [ ] 1.1.2 Unit test: render the query and assert the subquery carries the project/branch predicates
- [ ] 1.1.3 DB test: a `system`-authored version=1 object in another project does not leak into this project's `created` results

### 1.2 Validate `actor_type` and `provenance`-requires-`actor_type`

- [ ] 1.2.1 Reject `actor_type` outside `user`/`agent`/`system` with a 400 in `parseActorProvenance`
- [ ] 1.2.2 Reject `provenance` without `actor_type` with a 400 in `parseActorProvenance`
- [ ] 1.2.3 Unit test: both 400 paths

### 1.3 Carry the source actor on merge conflict/similar paths

- [ ] 1.3.1 Add `ActorType`/`ActorID` to the merge `conflict` object version
- [ ] 1.3.2 Add `ActorType`/`ActorID` to the merge `similar` (absorb) object version
- [ ] 1.3.3 Add `ActorType`/`ActorID` to the relationship similarity-merge version
- [ ] 1.3.4 DB test: a conflict-resolved merge version carries the source row's actor

### 1.4 Clear inherited actor on unresolvable delegated runs

- [ ] 1.4.1 Give `auth.WithActor` a "clear" form (empty actor type) and make `ActorFromContext` treat it as no actor
- [ ] 1.4.2 Always stamp the actor at the run boundary (resolvable → agent+id, unresolvable → cleared)
- [ ] 1.4.3 Unit test: `WithActor(ctx, "", nil)` clears a parent actor, and `actorFromContext` falls back to user

## 2. iOS

### 2.1 Use `apiBaseURL` in the object browser

- [ ] 2.1.1 Derive the gateway base URL from `config.apiBaseURL` instead of stripping `/api/token`
- [ ] 2.1.2 Update the error message to reference `apiBaseURL`

## 3. Archive

- [ ] 3.1 After the PR merges, run `openspec archive fix-provenance-followups --yes`
