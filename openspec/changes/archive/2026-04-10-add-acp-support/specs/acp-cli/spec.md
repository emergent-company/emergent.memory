## ADDED Requirements

### Requirement: CLI command group `memory acp`
The CLI SHALL provide a top-level `memory acp` command group containing subcommands for ACP operations. The command group SHALL use the Go SDK client at `pkg/sdk/acp/client.go` to make HTTP calls to `/acp/v1/` endpoints on the configured Memory server.

#### Scenario: Running `memory acp` with no subcommand shows help
- **WHEN** a user runs `memory acp`
- **THEN** the CLI prints the available subcommands: `ping`, `agents`, `runs`, `sessions`

### Requirement: `memory acp ping`
The CLI SHALL provide `memory acp ping` that sends `GET /acp/v1/ping` to the configured server and displays the result. This command SHALL NOT require authentication.

#### Scenario: Ping succeeds against a running server
- **WHEN** a user runs `memory acp ping` and the server is reachable
- **THEN** the CLI prints a success message (e.g., "ACP endpoint is reachable")

#### Scenario: Ping fails against unreachable server
- **WHEN** a user runs `memory acp ping` and the server is not reachable
- **THEN** the CLI prints an error message with connection details

### Requirement: `memory acp agents list`
The CLI SHALL provide `memory acp agents list` that calls `GET /acp/v1/agents` and displays a table of externally-visible agents. The table SHALL include columns: `NAME`, `DESCRIPTION`, `VERSION`, `SUCCESS RATE`.

#### Scenario: List agents displays table
- **WHEN** a user runs `memory acp agents list` with a valid config
- **THEN** the CLI prints a formatted table of external agents

#### Scenario: List agents with no external agents
- **WHEN** a user runs `memory acp agents list` and no agents have `visibility = 'external'`
- **THEN** the CLI prints "No externally-visible agents found"

#### Scenario: List agents with --json flag
- **WHEN** a user runs `memory acp agents list --json`
- **THEN** the CLI outputs the raw JSON array response

### Requirement: `memory acp agents get <name>`
The CLI SHALL provide `memory acp agents get <name>` that calls `GET /acp/v1/agents/:name` and displays the full agent manifest in a readable format.

#### Scenario: Get agent displays manifest
- **WHEN** a user runs `memory acp agents get my-agent`
- **THEN** the CLI prints the agent's full manifest including name, description, capabilities, input/output modes, and status metrics

#### Scenario: Get non-existent agent shows error
- **WHEN** a user runs `memory acp agents get nonexistent`
- **THEN** the CLI prints "Agent 'nonexistent' not found" and exits with code 1

#### Scenario: Get agent with --json flag
- **WHEN** a user runs `memory acp agents get my-agent --json`
- **THEN** the CLI outputs the raw JSON manifest

### Requirement: `memory acp runs create <agent-name>`
The CLI SHALL provide `memory acp runs create <agent-name>` that creates a new run via `POST /acp/v1/agents/:name/runs`. The command SHALL accept `--message` (required, text input), `--mode` (optional, default `sync`; values: `sync`, `async`, `stream`), and `--session` (optional session ID).

#### Scenario: Create sync run with message
- **WHEN** a user runs `memory acp runs create my-agent --message "Summarize the project"`
- **THEN** the CLI blocks until the run completes, then prints the agent's output text

#### Scenario: Create async run
- **WHEN** a user runs `memory acp runs create my-agent --message "Process data" --mode async`
- **THEN** the CLI prints the run ID and status `submitted`, then exits immediately

#### Scenario: Create stream run
- **WHEN** a user runs `memory acp runs create my-agent --message "Explain" --mode stream`
- **THEN** the CLI streams the agent's text output to stdout in real-time as SSE events arrive

#### Scenario: Sync run that pauses for input
- **WHEN** a sync run returns `status: "input-required"` with an `await_request`
- **THEN** the CLI prints the question and prompts the user for input interactively, then calls resume automatically

#### Scenario: Create run with session
- **WHEN** a user runs `memory acp runs create my-agent --message "Hello" --session <sessionId>`
- **THEN** the run is linked to the specified session

#### Scenario: Create run with missing message flag
- **WHEN** a user runs `memory acp runs create my-agent` without `--message`
- **THEN** the CLI prints an error: "--message flag is required"

### Requirement: `memory acp runs get <agent-name> <run-id>`
The CLI SHALL provide `memory acp runs get <agent-name> <run-id>` that calls `GET /acp/v1/agents/:name/runs/:runId` and displays the run state.

#### Scenario: Get a completed run
- **WHEN** a user runs `memory acp runs get my-agent <runId>` for a completed run
- **THEN** the CLI prints the run status, output messages, and timing information

#### Scenario: Get a paused run
- **WHEN** a user runs `memory acp runs get my-agent <runId>` for a paused run
- **THEN** the CLI prints the run status `input-required` and the pending question

#### Scenario: Get run with --json flag
- **WHEN** a user runs `memory acp runs get my-agent <runId> --json`
- **THEN** the CLI outputs the raw JSON run object

### Requirement: `memory acp runs cancel <agent-name> <run-id>`
The CLI SHALL provide `memory acp runs cancel <agent-name> <run-id>` that calls `DELETE /acp/v1/agents/:name/runs/:runId` to cancel the run.

#### Scenario: Cancel a running run
- **WHEN** a user runs `memory acp runs cancel my-agent <runId>` for an active run
- **THEN** the CLI prints "Run <runId> cancellation requested (status: cancelling)"

#### Scenario: Cancel an already completed run
- **WHEN** a user runs `memory acp runs cancel my-agent <runId>` for a completed run
- **THEN** the CLI prints "Cannot cancel run <runId>: run has already completed" and exits with code 1

### Requirement: `memory acp runs resume <agent-name> <run-id>`
The CLI SHALL provide `memory acp runs resume <agent-name> <run-id>` that calls `POST /acp/v1/agents/:name/runs/:runId/resume`. The command SHALL accept `--message` (required, the human response), and `--mode` (optional, default `sync`).

#### Scenario: Resume a paused run
- **WHEN** a user runs `memory acp runs resume my-agent <runId> --message "Yes, proceed"`
- **THEN** the CLI blocks until the resumed run completes and prints the output

#### Scenario: Resume a non-paused run
- **WHEN** a user runs `memory acp runs resume my-agent <runId> --message "data"` for a run not in `input-required` status
- **THEN** the CLI prints "Cannot resume run <runId>: run is not awaiting input" and exits with code 1

#### Scenario: Resume with stream mode
- **WHEN** a user runs `memory acp runs resume my-agent <runId> --message "Go ahead" --mode stream`
- **THEN** the CLI streams the resumed run's output in real-time

### Requirement: `memory acp sessions create`
The CLI SHALL provide `memory acp sessions create` that calls `POST /acp/v1/sessions`. The command SHALL accept an optional `--agent` flag to scope the session to a specific agent.

#### Scenario: Create unscoped session
- **WHEN** a user runs `memory acp sessions create`
- **THEN** the CLI prints the new session ID

#### Scenario: Create agent-scoped session
- **WHEN** a user runs `memory acp sessions create --agent my-agent`
- **THEN** the CLI prints the new session ID scoped to `my-agent`

### Requirement: `memory acp sessions get <session-id>`
The CLI SHALL provide `memory acp sessions get <session-id>` that calls `GET /acp/v1/sessions/:sessionId` and displays the session details including run history.

#### Scenario: Get session with history
- **WHEN** a user runs `memory acp sessions get <sessionId>` for a session with 3 runs
- **THEN** the CLI prints the session details and a table of run summaries

#### Scenario: Get session with --json flag
- **WHEN** a user runs `memory acp sessions get <sessionId> --json`
- **THEN** the CLI outputs the raw JSON session object

### Requirement: Go SDK client at `pkg/sdk/acp/`
The CLI commands SHALL use a Go SDK client at `pkg/sdk/acp/client.go` that provides typed methods for all ACP endpoints: `Ping()`, `ListAgents()`, `GetAgent(name)`, `CreateRun(agentName, req)`, `GetRun(agentName, runId)`, `CancelRun(agentName, runId)`, `ResumeRun(agentName, runId, req)`, `GetRunEvents(agentName, runId)`, `CreateSession(req)`, `GetSession(sessionId)`. The client SHALL be initialized from the same config sources as the existing SDK (`~/.memory/config.yaml` or env vars).

#### Scenario: SDK client uses configured server URL
- **WHEN** a user's `~/.memory/config.yaml` has `server: http://localhost:3012`
- **THEN** the ACP SDK client makes requests to `http://localhost:3012/acp/v1/`

#### Scenario: SDK client attaches auth token
- **WHEN** the SDK client makes a request to a protected endpoint
- **THEN** the request includes the `Authorization: Bearer emt_*` header from config

#### Scenario: SDK client returns typed errors
- **WHEN** the server returns HTTP 404
- **THEN** the SDK client returns a typed `NotFoundError` that the CLI can handle for user-friendly messages
