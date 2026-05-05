## ADDED Requirements

### Requirement: Named interfaces replace all setter-injection wiring
The codebase SHALL NOT use post-construction setter methods (`SetXxx()`) to wire cross-domain dependencies. Each such dependency SHALL be expressed as a named interface defined in the *receiving* package and injected via fx at startup.

#### Scenario: No SetXxx methods exist for cross-domain wiring
- **WHEN** the codebase is compiled after migration
- **THEN** none of the following setter methods exist: `mcp.Service.SetAgentToolHandler`, `mcp.Service.SetEmbeddingControlHandler`, `mcp.Service.SetGraphObjectPatcher`, `mcp.Service.SetSessionTitleHandler`, `mcpregistry.Service.SetToolPoolInvalidator`, `orgs.Service.SetToolPoolInvalidator`, `mcprelay.Service.OnChange`

#### Scenario: Interface wired at startup via fx
- **WHEN** the server starts
- **THEN** all cross-domain interface dependencies are provided through `fx.Provide` or `fx.Invoke` in `main.go` with no deferred setter calls

### Requirement: AgentToolDispatcher interface in domain/mcp
`domain/mcp` SHALL define an `AgentToolDispatcher` interface specifying the method(s) previously called via `SetAgentToolHandler`. `domain/agents` SHALL implement this interface, and `domain/mcp` SHALL receive it as a constructor parameter.

#### Scenario: MCP dispatches agent tool call via interface
- **WHEN** an MCP session needs to dispatch a tool call to the agents subsystem
- **THEN** it calls the `AgentToolDispatcher` interface method without importing `domain/agents`

### Requirement: EmbeddingWorkerController interface in domain/mcp
`domain/mcp` SHALL define an `EmbeddingWorkerController` interface for the embedding control capabilities previously injected via `SetEmbeddingControlHandler`. `domain/extraction` SHALL implement it.

#### Scenario: MCP controls embedding workers via interface
- **WHEN** MCP needs to pause or resume embedding workers
- **THEN** it calls through `EmbeddingWorkerController` without importing `domain/extraction`

### Requirement: ToolPoolInvalidator interface in domain/mcpregistry
`domain/mcpregistry` SHALL define a `ToolPoolInvalidator` interface for cache invalidation previously injected via `SetToolPoolInvalidator`. `domain/agents` SHALL implement it.

#### Scenario: Registry invalidates tool pool via interface
- **WHEN** a tool registry change requires pool invalidation
- **THEN** `mcpregistry` calls the `ToolPoolInvalidator` interface without importing `domain/agents`

### Requirement: OrgToolPoolInvalidator interface in domain/orgs
`domain/orgs` SHALL define an `OrgToolPoolInvalidator` interface for the org-level pool invalidation previously injected via `SetToolPoolInvalidator`. `domain/agents` SHALL implement it.

#### Scenario: Org change triggers pool invalidation via interface
- **WHEN** an org-level change requires tool pool invalidation
- **THEN** `domain/orgs` calls through `OrgToolPoolInvalidator` without importing `domain/agents`

### Requirement: SessionChangeHandler interface in domain/mcprelay
`domain/mcprelay` SHALL define a `SessionChangeHandler` interface for session state changes previously injected via `OnChange`. `domain/agents` SHALL implement it.

#### Scenario: MCP relay notifies agents of session change via interface
- **WHEN** an MCP relay session changes state
- **THEN** `mcprelay` calls through `SessionChangeHandler` without importing `domain/agents`
