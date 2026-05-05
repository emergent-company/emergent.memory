## ADDED Requirements

### Requirement: GraphEventSink interface in domain/graph
`domain/graph` SHALL define a `GraphEventSink` interface with methods covering all graph mutation event types currently logged directly to `*journal.Service`. `graph.Service` SHALL hold a `GraphEventSink` field (not `*journal.Service`) and call it for all graph mutation events.

#### Scenario: graph.Service has no direct journal import
- **WHEN** the codebase is compiled after migration
- **THEN** `domain/graph` does not import `domain/journal`

#### Scenario: Graph mutation events are dispatched via interface
- **WHEN** a graph object is created, updated, or deleted
- **THEN** `graph.Service` calls the corresponding `GraphEventSink` method

### Requirement: NoopEventSink provided as default
`domain/graph` SHALL provide a `NoopEventSink` struct implementing `GraphEventSink` with no-op method bodies. When journal is not configured, `graph.Service` SHALL be initialized with `NoopEventSink{}` so nil-guard checks are unnecessary.

#### Scenario: Graph service operates without journal
- **WHEN** the server starts with the journal domain disabled
- **THEN** graph mutations succeed without errors, and no nil-guard checks are needed in graph service methods

#### Scenario: No nil checks on event sink in graph.Service
- **WHEN** the codebase is compiled after migration
- **THEN** `graph/service.go` contains no `if s.journal != nil` or equivalent nil-guard pattern

### Requirement: journal.Service implements GraphEventSink
`domain/journal`'s service SHALL implement the `GraphEventSink` interface. When journal is enabled, it SHALL be provided to `graph.Service` via fx injection, replacing `NoopEventSink`.

#### Scenario: Journal receives graph events when enabled
- **WHEN** the server runs with the journal domain included
- **THEN** graph mutations are logged to the journal via the `GraphEventSink` interface methods

#### Scenario: Journal absence does not break graph operations
- **WHEN** the server runs without the journal domain
- **THEN** graph mutations complete successfully and the `NoopEventSink` silently discards events
