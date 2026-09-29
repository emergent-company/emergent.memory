<!-- openspec:archive-hold: staged across 4 PRs (server, CLI, web-ui, iOS); archive after the final PR -->

## 1. Server foundation (PR1)

### 1.1 Actor constants + context plumbing

- [x] 1.1.1 Add `ActorAgent = "agent"` constant to `domain/graph/events.go` alongside the existing `ActorUser`/`ActorSystem` constants
- [x] 1.1.2 Add `auth.WithActor(ctx, actorType, actorID)` and `auth.ActorFromContext(ctx)` in `pkg/auth/context.go` beside `namespaceCtxKey`, following the existing typed-key pattern
- [x] 1.1.3 Add unit tests for `ActorAgent` constant value and for `WithActor`/`ActorFromContext` round-trip and absent-actor (nil) behavior

### 1.2 Read actor from context in graph mutators

- [x] 1.2.1 In every graph object/relationship mutator, replace the hardcoded `"user"` actor with `ActorFromContext(ctx)` (defaulting to `user` + current user only when no actor is stamped)
- [x] 1.2.2 Add unit tests asserting each mutator writes `user` actor from context when unstamped, and writes the stamped `agent`/`system` actor when `WithActor` was applied

### 1.3 Stamp actor at the agent run boundary + per-agent MCP endpoint

- [x] 1.3.1 Stamp `auth.WithActor(ctx, "agent", <kb.agents UUID>)` at the agent run boundary so the writing agent's id is in context for the whole run
- [x] 1.3.2 Stamp `auth.WithActor(ctx, "agent", endpoint.AgentID)` in the per-agent MCP endpoint path so MCP tool calls attribute to that endpoint's agent
- [x] 1.3.3 Add unit tests: a nested/delegated run re-stamps the sub-agent's id and its writes attribute to the sub-agent, not the parent

### 1.4 MCP write paths read actor from context

- [x] 1.4.1 In `executeBatchCreateEntities`, pass a nil (context-derived) actor to `CreateOrUpdate` instead of a hardcoded `user`
- [x] 1.4.2 In `executeUpdateEntity`, pass a nil (context-derived) actor to `Patch` instead of a hardcoded `user`
- [x] 1.4.3 Update the MCP delete and restore paths to read actor from context instead of hardcoding `user`
- [x] 1.4.4 Add unit tests covering each MCP write path writing the context actor (agent vs user vs system)

### 1.5 Relationship provenance migration + attribution

- [x] 1.5.1 Add a Goose migration adding nullable `actor_type`/`actor_id` columns to `kb.graph_relationships`
- [x] 1.5.2 Attribute `actor_type`/`actor_id` on relationship create, update, and bulk relationship mutators (read from context)
- [x] 1.5.3 Add unit tests asserting relationship rows carry the stamped actor on create/update/bulk

### 1.6 Repo actor handling for soft delete/restore/bulk

- [x] 1.6.1 Update `SoftDelete`, `SoftDeleteOnBranch`, `Restore`, `BulkUpdateStatus`, and `BulkActionByFilter` to write the deleting/restoring/bulk actor into the tombstone/restored row
- [x] 1.6.2 Add unit tests asserting each of these paths records the correct actor

### 1.7 Merge / fast-forward clone provenance fix

- [x] 1.7.1 Fix `graph.Service.applyMerge` (around `service.go:3985-3998`) to carry the source row's actor into the cloned rows instead of building fresh actor-less rows
- [x] 1.7.2 Fix the fast-forward clone path to carry the source row's actor
- [x] 1.7.3 Add unit tests asserting merged/cloned objects and relationships retain the source row's actor

### 1.8 Object provenance filter (ListParams + buildObjectBaseQueryWith)

- [x] 1.8.1 Add `ActorType`, `ActorID`, and `Provenance` (`created`|`updated`|`any`) fields to `graph.ListParams`
- [x] 1.8.2 Implement the filter in `buildObjectBaseQueryWith` with created (earliest surviving version authored by actor) / updated (HEAD row authored by actor) / any semantics, always keyed on the `(actor_type, actor_id)` pair
- [x] 1.8.3 Add unit tests for each mode: created-only, updated-only, any, and the actor-pair (never actor_id alone) rule

### 1.9 DTO + SDK actor fields

- [x] 1.9.1 Populate actor fields on `GraphObjectResponse` and the relationship response DTO from the stored columns
- [x] 1.9.2 Add `ActorType`/`ActorID`/`Provenance` fields to SDK `ListObjectsOptions` (`pkg/sdk/graph/client.go`) and thread them to the list endpoint
- [x] 1.9.3 Add unit tests for the DTO actor field population and SDK option serialization

## 2. CLI (PR2)

### 2.1 Object list actor provenance flags

- [x] 2.1.1 Add `--actor-type`, `--actor-id`, and `--provenance created|updated|any` flags to `memory graph objects list`, wired to the SDK `ListObjectsOptions` fields
- [x] 2.1.2 Add unit tests for flag parsing, an agent-scoped listing (actor-type=agent + actor-id), and rejection of an invalid `--provenance` value

## 3. Web UI (PR3)

### 3.1 Agent-scoped object browse view

- [x] 3.1.1 Add an agent-provenance filter (Created by / Updated by modes) to the object list, reusing the existing browse/detail components for an agent-scoped view
- [x] 3.1.2 Replace the agent dashboard "memories browser" link with a link to objects created/updated by that agent
- [x] 3.1.3 Remove the memories subpage list/search/detail (the three memory-object requirements)
- [x] 3.1.4 Add unit tests (gateway templ/handler) for the provenance filter rendering and the agent-scoped link

## 4. iOS (PR4)

### 4.1 Agent object browser

- [ ] 4.1.1 Add a native SwiftUI object browser scoped to an agent's provenance (list objects created/updated by the agent, search, view object detail, gate on agent availability, fresh data), replacing the memory browser
- [ ] 4.1.2 Remove the memories browser views and their entry point
- [ ] 4.1.3 Add unit tests for the object list/search/detail view models and the agent-availability gate

## 5. Archive

- [ ] 5.1 After the final PR merges and CI is green, run `openspec archive agent-object-provenance --yes` to sync delta specs into `openspec/specs/`
