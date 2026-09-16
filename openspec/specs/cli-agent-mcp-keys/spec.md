# cli-agent-mcp-keys Specification

## Purpose
Lets an operator manage an agent's own MCP endpoint from the `memory` CLI: inspect or enable the endpoint, create, list, revoke and rotate its labeled keys, and read its sessions — so an agent can be handed to an external MCP client without driving the admin API or the web UI by hand.

## Requirements

### Requirement: Agent MCP endpoint commands live under the agents group

The CLI SHALL expose the agent MCP endpoint lifecycle as a `mcp-endpoint`
subgroup of the existing `memory agents` command, distinct from the
`agents mcp-servers` registry subgroup. The subgroup SHALL provide `show`,
`create`, and `revoke` for the endpoint, `create`, `list`, `revoke`, and
`rotate` for its keys, and `sessions` for its sessions.

#### Scenario: Command tree is registered

- **WHEN** an operator runs `memory agents mcp-endpoint --help`
- **THEN** the output lists `create`, `revoke`, `show`, `keys`, and `sessions`
- **THEN** `memory agents mcp-endpoint keys --help` lists `create`, `list`, `revoke`, and `rotate`

#### Scenario: Registry subgroup is untouched

- **WHEN** an operator runs `memory agents mcp-servers list`
- **THEN** behaviour is identical to before this change and no endpoint command is added to that subgroup

### Requirement: Agent reference resolves by id, name, or slug

Every endpoint, key, and session command that takes an agent SHALL accept a
runtime agent id, a runtime agent name, or an agent-definition name or slug. A
UUID SHALL be passed through unchanged. A name or slug SHALL be matched
case-insensitively against the project's runtime agents and then its agent
definitions, and SHALL resolve to an id the agent MCP endpoint API accepts.

#### Scenario: Agent resolved by id

- **WHEN** the agent argument is a UUID
- **THEN** the command uses it directly without a lookup

#### Scenario: Agent resolved by name

- **WHEN** the agent argument matches a runtime agent's name, ignoring case
- **THEN** the command uses that agent's id

#### Scenario: Agent resolved by definition slug

- **WHEN** the agent argument matches an agent definition's slug (lowercase, non-alphanumerics replaced by hyphens)
- **THEN** the command uses that definition's id

#### Scenario: Unknown agent is rejected

- **WHEN** the agent argument matches no runtime agent and no definition
- **THEN** the command exits non-zero with a message listing the available agent names and sends no request

#### Scenario: Ambiguous agent is rejected

- **WHEN** the agent argument matches more than one agent
- **THEN** the command exits non-zero with a message naming the matches and sends no request

#### Scenario: Interactive picker when omitted

- **WHEN** the agent argument is omitted and stdin is a terminal
- **THEN** the command prompts with the existing agent picker

#### Scenario: Omitted argument is non-interactive

- **WHEN** the agent argument is omitted and stdin is not a terminal
- **THEN** the command exits non-zero with a message that the agent is required

### Requirement: Endpoint show reports the agent's endpoint

`memory agents mcp-endpoint show` SHALL read the agent's active MCP endpoint and
print its id, status, agent id, MCP URL, and creation time. When the agent has no
active endpoint it SHALL exit non-zero with a message that no endpoint exists.

#### Scenario: Endpoint exists

- **WHEN** the agent has an active endpoint
- **THEN** the output contains the endpoint id, the status `active`, and the MCP URL

#### Scenario: No endpoint

- **WHEN** the agent has no active endpoint
- **THEN** the command exits non-zero and reports that the agent has no MCP endpoint

#### Scenario: JSON output

- **WHEN** `--json` is passed
- **THEN** stdout is the endpoint DTO as valid JSON

### Requirement: Endpoint create and revoke manage the single endpoint

`memory agents mcp-endpoint create` SHALL create the agent's active endpoint and
print its id and MCP URL. `memory agents mcp-endpoint revoke` SHALL revoke the
agent's endpoint and its remaining keys. Revoke SHALL be destructive and SHALL
require confirmation.

#### Scenario: Create succeeds

- **WHEN** the agent has no active endpoint
- **THEN** the endpoint is created and the command prints its id and MCP URL

#### Scenario: Duplicate create is explained

- **WHEN** the agent already has an active endpoint
- **THEN** the command exits non-zero and explains that an endpoint already exists, and does not create a second one

#### Scenario: Revoke requires confirmation

- **WHEN** revoke is run non-interactively without `--yes`
- **THEN** the command exits non-zero without revoking
- **WHEN** revoke is run with `--yes`, or confirmed at an interactive prompt
- **THEN** the endpoint is revoked and the command reports success

### Requirement: Keys are created with a label and the secret is shown exactly once

`memory agents mcp-endpoint keys create` SHALL require a label, create the key,
and print the raw secret exactly once together with a warning that it will not be
shown again and the MCP URL to paste into a client. Create and rotate SHALL be
the only paths that print a secret.

#### Scenario: Create requires a label

- **WHEN** `keys create` is run without `--label`
- **THEN** the command exits non-zero before sending any request

#### Scenario: Secret is shown exactly once

- **WHEN** `keys create` succeeds
- **THEN** the command output contains the raw token exactly once, a warning that it will not be shown again, and the MCP URL

#### Scenario: Duplicate label is explained

- **WHEN** the endpoint already has an active key with the same label, ignoring case
- **THEN** the command exits non-zero and reports that the label already exists

#### Scenario: Rotate shows the new secret once

- **WHEN** `keys rotate` succeeds
- **THEN** the command prints the new raw token exactly once with the same warning and no old secret

### Requirement: Key list reports lifecycle without secrets

`memory agents mcp-endpoint keys list` SHALL list the endpoint's keys with each
key's label, status, creation time, and last-used time, and MUST NOT print a raw
secret.

#### Scenario: Keys listed

- **WHEN** the endpoint has keys
- **THEN** the output shows each key's label and status with no raw token value

#### Scenario: JSON output

- **WHEN** `--json` is passed
- **THEN** stdout is the key list DTO as valid JSON and contains no raw secret

### Requirement: Key revoke and rotate are confirmed destructive operations

`memory agents mcp-endpoint keys revoke` and `keys rotate` SHALL take the key id
directly, resolve the project from project context, and SHALL require
confirmation because revoke invalidates a credential and rotate invalidates the
previous secret.

#### Scenario: Key revoke requires confirmation

- **WHEN** `keys revoke <key-id>` is run non-interactively without `--yes`
- **THEN** the command exits non-zero without revoking the key

#### Scenario: Key rotate requires confirmation

- **WHEN** `keys rotate <key-id>` is run non-interactively without `--yes`
- **THEN** the command exits non-zero without rotating the key

#### Scenario: Key id is required

- **WHEN** `keys revoke` or `keys rotate` is run without a key id
- **THEN** the command exits non-zero with a usage error

### Requirement: Sessions are listed with an optional status filter

`memory agents mcp-endpoint sessions` SHALL list the endpoint's sessions, most
recently active first, showing each session's id, owning key label, status, turn
count, total steps, and timestamps. It SHALL accept `--status` with one of
`active`, `running`, `interrupted`, or `expired`, and SHALL reject an
unrecognized value without calling the server.

#### Scenario: Sessions listed

- **WHEN** the endpoint has sessions
- **THEN** each row shows the session id, key label, status, turn count, and total steps

#### Scenario: Status filter is applied

- **WHEN** `--status active` is passed
- **THEN** the request carries the status filter and only matching sessions are shown

#### Scenario: Invalid status rejected

- **WHEN** `--status` is an unrecognized value
- **THEN** the command exits non-zero before sending any request

#### Scenario: JSON output

- **WHEN** `--json` is passed
- **THEN** stdout is the session list DTO as valid JSON

### Requirement: Server errors are mapped readably

The commands SHALL translate the endpoint API's error responses into actionable
messages: a 409 for an existing active endpoint, a 409 for a duplicate key label,
a 404 for an endpoint or key that does not exist in the project, and a 403 for a
caller who is not a project admin.

#### Scenario: Duplicate key label

- **WHEN** the server rejects a key create with 409 for a duplicate label
- **THEN** the command reports that an active key with that label already exists

#### Scenario: Endpoint already exists

- **WHEN** the server rejects an endpoint create with 409
- **THEN** the command reports that an active endpoint already exists for the agent

#### Scenario: Not found

- **WHEN** the server responds 404
- **THEN** the command reports that the endpoint or key was not found

#### Scenario: Not an admin

- **WHEN** the server responds 403
- **THEN** the command reports that project admin permission is required

### Requirement: The SDK exposes the endpoint, key, and session routes

The SDK `mcp` client SHALL expose methods for all eight agent MCP routes —
endpoint create/get, endpoint revoke, key create/list/revoke/rotate, and session
list — taking the project id explicitly and returning decoded DTOs, so the CLI
does not hand-roll HTTP.

#### Scenario: SDK methods map to routes

- **WHEN** a caller uses the SDK key-rotate method
- **THEN** it issues `POST /api/projects/{projectId}/agent-mcp-keys/{id}/rotate` and decodes the one-time-secret response

#### Scenario: SDK surfaces API errors

- **WHEN** the API responds with an error status
- **THEN** the SDK returns a typed error carrying the status code and server message
