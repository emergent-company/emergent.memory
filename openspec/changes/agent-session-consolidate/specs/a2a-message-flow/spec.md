## MODIFIED Requirements

### Requirement: Context continuity

`Task.contextId` SHALL map to the existing session record (`kb.sessions`). When a client sends a message without `contextId`, the server SHALL create a context lazily; when `contextId` is supplied, the server SHALL link the task to it.

#### Scenario: Context is created lazily

- **WHEN** a client sends a message without `contextId`
- **THEN** the response task has a non-empty `contextId`

#### Scenario: Supplied context is honoured

- **WHEN** a client sends a message with a valid `contextId`
- **THEN** the created task's `contextId` equals the supplied value

#### Scenario: Unknown context is rejected

- **WHEN** a client sends a message with a `contextId` that does not exist in the project
- **THEN** the server responds with HTTP 400
