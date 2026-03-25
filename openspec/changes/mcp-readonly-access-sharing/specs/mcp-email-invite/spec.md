## ADDED Requirements

### Requirement: Admin can send an MCP invite email to one or more addresses
When `emails` is provided in the `POST /api/projects/{projectId}/mcp/share` request body, the system SHALL send an MCP invite email to each address. The email MUST be dispatched via the existing email queue (`emailSvc.Enqueue`) and MUST NOT block the HTTP response. The HTTP response is returned immediately after token creation; email delivery is asynchronous.

#### Scenario: Email sent when addresses provided
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` with `"emails": ["alice@example.com"]`
- **THEN** the system enqueues one email to `alice@example.com` using the `mcp-invite` template
- **THEN** the HTTP response is returned without waiting for email delivery

#### Scenario: Multiple emails sent in one request
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` with `"emails": ["alice@example.com", "bob@example.com"]`
- **THEN** the system enqueues two separate emails, one to each address

#### Scenario: No email sent when emails field is absent
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` without an `emails` field
- **THEN** no email is enqueued and the token is still created successfully

#### Scenario: Invalid email address rejected
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` with `"emails": ["not-an-email"]`
- **THEN** the system returns HTTP 422 Unprocessable Entity with a validation error
- **THEN** no token is created and no email is sent

### Requirement: MCP invite email contains the endpoint, API key, and setup guide
The `mcp-invite` email template SHALL include all information a recipient needs to configure an AI agent to use the MCP server. The email MUST contain: the project name, the MCP endpoint URL, the API key, the sender's name, and step-by-step setup instructions for at least Claude Desktop and Cursor.

#### Scenario: Email contains required fields
- **WHEN** an MCP invite email is rendered
- **THEN** the email body contains the project name, MCP URL, API key, and sender name
- **THEN** the email body contains configuration instructions for Claude Desktop
- **THEN** the email body contains configuration instructions for Cursor

#### Scenario: API key is displayed in a code block
- **WHEN** the MCP invite email is rendered as HTML
- **THEN** the API key is wrapped in a `<code>` or monospace element to distinguish it from prose

#### Scenario: Email subject identifies the project
- **WHEN** an MCP invite email is sent for project "Acme Knowledge Base"
- **THEN** the email subject contains the project name (e.g., "MCP Access — Acme Knowledge Base")

### Requirement: MCP invite email uses a dedicated Handlebars template
The system SHALL use a new template `mcp-invite.hbs` (following the existing Handlebars/MJML pattern) for MCP invite emails. This template MUST be separate from `project-invitation.hbs` and MUST NOT require the recipient to create a Memory account.

#### Scenario: Template renders without errors for valid context
- **WHEN** the `mcp-invite` template is rendered with `projectName`, `mcpUrl`, `apiKey`, `projectId`, `senderName`, and `snippets`
- **THEN** the rendered HTML contains no template errors and all variables are substituted

#### Scenario: Email does not prompt recipient to create an account
- **WHEN** the MCP invite email is rendered
- **THEN** the email body does not contain links to sign up or create a Memory account
- **THEN** the email body does not reference the Memory CLI install flow
