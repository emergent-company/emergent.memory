# Design: Reaction Agent Trigger

## Context

The system needs to support event-driven agent execution triggered by graph object changes. This requires:

- Reliable event emission from graph operations
- Efficient dispatching to matching agents
- Loop prevention (agent changes shouldn't trigger the same agent)
- Concurrency control
- Support for both autonomous execution and human-review workflows

## Goals / Non-Goals

**Goals:**

- Enable agents to react to graph object create/update/delete events
- Prevent infinite loops from agent-triggered changes
- Support both immediate execution and suggestion-based workflows
- Track processing state to avoid duplicate processing
- Handle concurrent agent triggers efficiently

**Non-Goals:**

- Complex dependency chains between agents (future work)
- Real-time streaming of agent progress (use existing run logging)
- Cross-project event triggers
- Undo/rollback of agent changes

## Decisions

### 1. Event Emission Strategy

**Decision:** Use existing `EventsService` (EventEmitter2), inject into `GraphService`

**Rationale:** The codebase already has a robust `EventsService` with `emitCreated()`, `emitUpdated()`, `emitDeleted()` methods and `subscribeAll()` for wildcard subscriptions. No need to introduce new infrastructure.

**Implementation:**

```typescript
// In GraphService
this.eventsService.emitCreated({
  entityType: 'graphObject',
  entity: graphObject,
  context: { actorType, actorId, projectId },
});
```

### 2. Actor Tracking for Loop Prevention

**Decision:** Add `actor_type` (`user` | `agent` | `system`) and `actor_id` columns to `kb.graph_objects`

**Rationale:** To prevent infinite loops, we need to know WHO made a change. Agents can then filter out events triggered by themselves or by any agent.

**Schema:**

```sql
ALTER TABLE kb.graph_objects
ADD COLUMN actor_type VARCHAR(20) DEFAULT 'user',
ADD COLUMN actor_id UUID NULL;
```

**Filter Logic:**

- Agents ignore events where `actor_type = 'agent'` AND `actor_id = agent.id` (self-triggered)
- Optional: agents can configure to ignore ALL agent-triggered events

### 3. Processing Log for Idempotency

**Decision:** Create `kb.agent_processing_log` table with index + application-level checks (not unique constraint)

**Rationale:** We need to track which objects an agent has already processed to avoid duplicate processing. Using an index with app-level checks is simpler than unique constraints with complex upsert logic.

**Schema:**

```sql
CREATE TABLE kb.agent_processing_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
  graph_object_id UUID NOT NULL REFERENCES kb.graph_objects(id) ON DELETE CASCADE,
  object_version INTEGER NOT NULL,
  event_type VARCHAR(20) NOT NULL, -- 'created' | 'updated' | 'deleted'
  status VARCHAR(20) NOT NULL DEFAULT 'pending', -- 'pending' | 'processing' | 'completed' | 'failed' | 'abandoned'
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  error_message TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_agent_processing_log_lookup
ON kb.agent_processing_log(agent_id, graph_object_id, object_version, event_type);
```

### 4. Concurrency Strategy

**Decision:** Configurable per-agent: `'skip'` (default) | `'parallel'`

**Rationale:** Keep it simple. Complex strategies like `'wait'` or `'queue'` add significant complexity without clear immediate need.

- `skip`: If agent is already processing this object, skip the new event
- `parallel`: Allow multiple concurrent executions for the same object

### 5. Execution Modes

**Decision:** Three modes: `'suggest'` | `'execute'` | `'hybrid'`

**Rationale:** Different use cases require different levels of automation:

- `suggest`: Agent creates tasks for human review (safer, auditable)
- `execute`: Agent directly modifies graph (efficient, autonomous)
- `hybrid`: Agent can do both based on confidence or operation type

**Capability Restrictions:**

```typescript
interface AgentCapabilities {
  canCreateObjects?: boolean;
  canUpdateObjects?: boolean;
  canDeleteObjects?: boolean;
  canCreateRelationships?: boolean;
  allowedObjectTypes?: string[]; // null = all types
}
```

### 6. Suggestion Handling

**Decision:** Reuse existing `kb.tasks` table with new task types

**Rationale:** The Tasks system already has UI, status tracking, and resolution logic. Adding new task types is minimal effort.

**New Task Types:**

- `object_create_suggestion` - Agent suggests creating a new object
- `object_update_suggestion` - Agent suggests updating an object
- `relationship_create_suggestion` - Agent suggests creating a relationship
- `object_delete_suggestion` - Agent suggests deleting an object

**Task Payload:**

```typescript
interface SuggestionTaskPayload {
  agentId: string;
  agentRunId: string;
  confidence?: number;
  reasoning?: string;
  suggestedChanges: {
    type: 'create' | 'update' | 'delete' | 'createRelationship';
    objectType?: string;
    objectId?: string;
    data?: Record<string, unknown>;
    relationship?: { fromId: string; toId: string; type: string };
  };
}
```

### 7. Stuck Job Handling

**Decision:** 5-minute timeout, mark as `'abandoned'`

**Rationale:** Simple timeout-based recovery. Jobs in `'processing'` state for >5 minutes are marked `'abandoned'` and can be retried.

### 8. Immediate vs Batched Execution

**Decision:** Fire-and-forget (async) for immediate mode

**Rationale:** The graph operation should not wait for agent execution. Agents run asynchronously after the event is emitted.

### 9. Service Architecture

**New Services:**

- `ReactionDispatcherService`: Subscribes to graph events via `subscribeAll()`, finds matching agents, creates processing log entries, dispatches execution
- `BatchedReactionScheduler`: Optional batching/debouncing for high-frequency events (future enhancement)

**Event Subscription Pattern:**

```typescript
@Injectable()
export class ReactionDispatcherService implements OnModuleInit {
  onModuleInit() {
    this.eventsService.subscribeAll(async (event) => {
      if (event.entityType === 'graphObject') {
        await this.handleGraphObjectEvent(event);
      }
    });
  }
}
```

## Agent Configuration Schema

```typescript
interface ReactionConfig {
  objectTypes: string[]; // Which object types to react to (empty = all)
  events: ('created' | 'updated' | 'deleted')[]; // Which events to react to
  concurrencyStrategy: 'skip' | 'parallel';
  ignoreAgentTriggered: boolean; // Ignore events triggered by any agent
  ignoreSelfTriggered: boolean; // Ignore events triggered by this agent (default: true)
}

// On Agent entity:
trigger_type: 'schedule' | 'manual' | 'reaction';
reaction_config: ReactionConfig | null;
execution_mode: 'suggest' | 'execute' | 'hybrid';
capabilities: AgentCapabilities | null;
```

## Risks / Trade-offs

| Risk                                      | Mitigation                                                     |
| ----------------------------------------- | -------------------------------------------------------------- |
| Infinite loops from agent changes         | Actor tracking + self-trigger filtering                        |
| Database bloat from processing log        | Add retention policy (delete entries older than 30 days)       |
| High-frequency events overwhelming agents | Batching/debouncing (future), concurrency limits               |
| Agents making unintended changes          | Capability restrictions, suggest mode for sensitive operations |

## Migration Plan

1. **Phase 1:** Database migrations (additive, non-breaking)
2. **Phase 2:** Add event emission to GraphService
3. **Phase 3:** Implement ReactionDispatcherService
4. **Phase 4:** Add execution mode and capabilities
5. **Phase 5:** Update frontend
6. **Phase 6:** Testing and documentation

**Rollback:** All changes are additive. Disable reaction agents by updating trigger_type back to 'manual'.

## Open Questions

- Should we add rate limiting per agent? (Defer to future iteration)
- Should we support filtering by object properties, not just types? (Defer to future iteration)
- How should we handle agent errors in `execute` mode - retry or fail permanently? (Start with fail, add retry later)
