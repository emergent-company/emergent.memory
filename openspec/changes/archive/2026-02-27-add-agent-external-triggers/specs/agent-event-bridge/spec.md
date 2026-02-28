## ADDED Requirements

### Requirement: Event Bridge Initialization

The system SHALL wire the `TriggerService` to the `events.Service` pub/sub system on application startup to enable reaction triggers.

#### Scenario: Subscribing to Graph Object Events

- **WHEN** the server starts and the agents module is initialized
- **THEN** the `TriggerService` subscribes globally to the `events.Service` to listen for graph object events (`created`, `updated`, `deleted`).

### Requirement: Event Dispatching to Reaction Agents

The system MUST dispatch relevant events from the event bus to configured reaction agents.

#### Scenario: Dispatching a Graph Object Event

- **WHEN** an event matching a reaction agent's `ReactionConfig.ObjectTypes` and `Events` is emitted by the `events.Service`
- **THEN** the `TriggerService` evaluates the event and queues an agent run if the agent is enabled.

### Requirement: Loop Prevention

The system SHALL ignore events triggered by other agents or the system itself to prevent infinite execution loops.

#### Scenario: Ignoring Agent-Triggered Events

- **WHEN** the `events.Service` emits an event where the `ActorContext.ActorType` is `agent`
- **THEN** the event bridge filters out the event and does not pass it to the `TriggerService.HandleEvent()`.

#### Scenario: Processing User-Triggered Events

- **WHEN** the `events.Service` emits an event where the `ActorContext.ActorType` is `user`
- **THEN** the event bridge passes the event to the `TriggerService.HandleEvent()`.
