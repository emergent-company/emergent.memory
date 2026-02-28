# agent-webhook-hooks Specification

## Purpose
TBD - created by archiving change add-agent-external-triggers. Update Purpose after archive.
## Requirements
### Requirement: Webhook Hook Management

The system SHALL allow project administrators to manage webhook hooks for an agent via the Admin API.

#### Scenario: Create a Webhook Hook

- **WHEN** an admin issues a `POST /api/admin/agents/:id/hooks` request with a label
- **THEN** the system creates a new `AgentWebhookHook` entity, returns a 201 Created with the hook ID, label, and a one-time plaintext token.

#### Scenario: List Webhook Hooks

- **WHEN** an admin issues a `GET /api/admin/agents/:id/hooks` request
- **THEN** the system returns a list of configured webhook hooks for the agent, omitting the tokens.

#### Scenario: Delete a Webhook Hook

- **WHEN** an admin issues a `DELETE /api/admin/agents/:id/hooks/:hookId` request
- **THEN** the system removes the webhook hook and returns a 204 No Content.

### Requirement: Token Security

The system MUST securely store and verify webhook tokens to prevent unauthorized triggering.

#### Scenario: Token Storage

- **WHEN** a webhook hook is created
- **THEN** the system stores only a bcrypt hash of the token in the database, never the plaintext token.

#### Scenario: Token Display

- **WHEN** a webhook hook is successfully created
- **THEN** the plaintext token is returned in the API response exactly once and cannot be retrieved again.

### Requirement: Public Webhook Receiver Endpoint

The system SHALL provide a public endpoint to trigger agent runs via webhook hooks without requiring user session authentication.

#### Scenario: Valid Trigger Request

- **WHEN** a client issues a `POST /api/webhooks/agents/:hookId` request with a valid `Authorization: Bearer <token>` header
- **THEN** the system queues an agent run and returns a 202 Accepted response with the new `run_id`.

#### Scenario: Invalid Token

- **WHEN** a client issues a `POST /api/webhooks/agents/:hookId` request with an incorrect token
- **THEN** the system returns a 401 Unauthorized response.

#### Scenario: Disabled Hook

- **WHEN** a client issues a trigger request using a valid token for a webhook hook that is disabled
- **THEN** the system returns a 403 Forbidden response.

### Requirement: Rate Limiting

The system MUST enforce rate limits on webhook hook invocations to prevent abuse.

#### Scenario: Enforcing the Rate Limit

- **WHEN** a client exceeds the configured requests per minute for a specific webhook hook
- **THEN** the system rejects subsequent requests with a 429 Too Many Requests response until the limit resets.

### Requirement: Payload Context

The system SHALL accept optional context payloads in webhook requests and pass them to the agent run.

#### Scenario: Request with Payload

- **WHEN** a client POSTs to the webhook receiver with a JSON body containing `prompt` and `context` fields
- **THEN** the system initiates the agent run using the provided `prompt` as the user message and includes the `context` metadata.

