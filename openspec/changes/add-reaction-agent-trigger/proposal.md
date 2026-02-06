# Change: Add Reaction Agent Trigger

## Why

Currently, agents only support `schedule` and `manual` trigger types. Users cannot configure agents to automatically respond to changes in the knowledge graph (object creation, updates, deletions). This limits the ability to build reactive workflows where agents process new data as it arrives.

Adding a `reaction` trigger type enables event-driven agent execution, allowing use cases like:

- Automatically enriching newly created objects with additional data
- Validating object changes against business rules
- Cascading updates when related objects change
- Automated quality checks on extracted entities

## What Changes

1. **Database Schema Changes:**

   - Add `reaction` to `trigger_type` enum in `kb.agents`
   - Add `reaction_config` JSONB column for reaction-specific settings
   - Add `execution_mode` column (`suggest` | `execute` | `hybrid`)
   - Add `capabilities` JSONB column for capability restrictions
   - Add `actor_type` and `actor_id` columns to `kb.graph_objects` for loop prevention
   - Create `kb.agent_processing_log` table to track per-object processing

2. **Event Emission:**

   - Inject `EventsService` into `GraphService`
   - Emit events on object create/update/delete operations

3. **New Services:**

   - `ReactionDispatcherService` - subscribes to graph events, dispatches to matching agents
   - `BatchedReactionScheduler` - handles debounced/batched execution

4. **Execution Modes:**

   - `execute` - agent directly modifies the graph
   - `suggest` - agent creates tasks for human review
   - `hybrid` - agent can do both based on confidence

5. **New Task Types:**

   - `object_create_suggestion`
   - `object_update_suggestion`
   - `relationship_create_suggestion`
   - `object_delete_suggestion`

6. **Frontend Updates:**
   - Update Agent form to support reaction trigger configuration
   - Display reaction-specific settings (object types, events, concurrency strategy)

## Impact

- **Affected specs:** `agent-infrastructure`
- **Affected code:**
  - `apps/server/src/entities/agent.entity.ts`
  - `apps/server/src/entities/graph-object.entity.ts`
  - `apps/server/src/modules/graph/graph.service.ts`
  - `apps/server/src/modules/agents/` (new services)
  - `apps/server/src/modules/tasks/tasks.service.ts`
  - `apps/admin/src/pages/admin/pages/agents/`
- **Breaking changes:** None (additive changes only)
