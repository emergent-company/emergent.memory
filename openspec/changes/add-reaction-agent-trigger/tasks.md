# Tasks: Add Reaction Agent Trigger

## 1. Database Migrations

- [x] 1.1 Create migration to add `reaction` to `trigger_type` enum
- [x] 1.2 Create migration to add `reaction_config`, `execution_mode`, `capabilities` columns to `kb.agents`
- [x] 1.3 Create migration to add `actor_type`, `actor_id` columns to `kb.graph_objects`
- [x] 1.4 Create migration to create `kb.agent_processing_log` table
- [x] 1.5 Update Agent entity with new columns and types
- [x] 1.6 Update GraphObject entity with actor columns
- [x] 1.7 Create AgentProcessingLog entity

## 2. Event Emission

- [x] 2.1 Inject `EventsService` into `GraphService`
- [x] 2.2 Emit `created` event in `createObject()` method
- [x] 2.3 Emit `updated` event in `patchObject()` method
- [x] 2.4 Emit `deleted` event in `deleteObject()` method
- [x] 2.5 Include actor context in all emitted events

## 3. Agent Processing Log

- [x] 3.1 Create `AgentProcessingLog` entity
- [x] 3.2 Create `AgentProcessingLogService`
- [x] 3.3 Add methods for creating, updating, and querying processing log entries
- [x] 3.4 Implement stuck job detection (5-minute timeout)

## 4. Reaction Dispatcher Service

- [x] 4.1 Create `ReactionDispatcherService` class
- [x] 4.2 Implement `onModuleInit` to subscribe to graph events
- [x] 4.3 Implement `findMatchingAgents()` to query agents by reaction config
- [x] 4.4 Implement `shouldProcess()` to check processing log and concurrency
- [x] 4.5 Implement `dispatch()` to create processing log entry and trigger execution

## 5. Agent Execution with Capabilities

- [x] 5.1 Update `AgentSchedulerService.executeAgent()` to support reaction triggers
- [x] 5.2 Implement capability checking before graph operations
- [x] 5.3 Implement `suggest` mode - create task instead of executing
- [x] 5.4 Implement `execute` mode - directly perform graph operations
- [x] 5.5 Implement `hybrid` mode - choose based on confidence/operation type

## 6. Suggestion Tasks

- [x] 6.1 Add new task types to `TaskType` enum (via SuggestionService)
- [x] 6.2 Update `TasksService.resolve()` to handle suggestion tasks
- [x] 6.3 Implement `approveSuggestion()` to apply suggested changes
- [x] 6.4 Implement `rejectSuggestion()` to mark task as rejected

## 7. Frontend Updates

- [x] 7.1 Update Agent form to show reaction trigger options
- [x] 7.2 Add reaction config fields (object types, events, concurrency)
- [x] 7.3 Add execution mode dropdown
- [x] 7.4 Add capabilities configuration UI
- [x] 7.5 Update agent list to show reaction triggers appropriately

## 8. Testing

- [x] 8.1 Unit tests for `ReactionDispatcherService`
- [x] 8.2 Unit tests for processing log logic
- [x] 8.3 Unit tests for capability checking
- [x] 8.4 Unit tests for `SuggestionService`
- [x] 8.5 Unit tests for `SuggestionTaskHandlerService`
- [ ] 8.6 E2E tests for reaction agent flow (optional)
- [ ] 8.7 E2E tests for suggestion task approval/rejection (optional)

## 9. Documentation

- [x] 9.1 Update Agent entity AGENT.md with new columns
- [x] 9.2 Document reaction trigger configuration
- [x] 9.3 Document execution modes and capabilities
