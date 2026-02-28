# cli-tool Specification

## Purpose
TBD - created by archiving change unify-emergent-clt. Update Purpose after archive.
## Requirements
### Requirement: Authentication and Credential Management

The CLI SHALL authenticate users via OAuth 2.0 Password Grant flow and manage credentials securely.

#### Scenario: Initial credential setup

- **WHEN** user runs `emergent-cli config set-credentials --email user@example.com`
- **THEN** CLI prompts for password securely (no echo)
- **AND** credentials are stored in `~/.emergent/credentials.json` with 0600 permissions
- **AND** CLI confirms "Credentials saved successfully"

#### Scenario: Automatic token acquisition

- **WHEN** user runs any command requiring authentication
- **AND** no valid token exists in cache
- **THEN** CLI obtains JWT token using stored credentials
- **AND** caches token with expiry timestamp
- **AND** executes the requested command

#### Scenario: Token refresh before expiry

- **WHEN** cached token expires within 5 minutes
- **AND** user runs an authenticated command
- **THEN** CLI refreshes token automatically
- **AND** updates token cache
- **AND** executes command without user intervention

#### Scenario: Environment variable authentication

- **WHEN** `EMERGENT_EMAIL` and `EMERGENT_PASSWORD` environment variables are set
- **AND** no credentials file exists
- **THEN** CLI uses environment variables for authentication
- **AND** does not require `config set-credentials` command

#### Scenario: Invalid credentials error

- **WHEN** user runs authenticated command
- **AND** credentials are invalid or expired
- **THEN** CLI displays "Authentication failed. Run 'emergent-cli config set-credentials' to update."
- **AND** exits with non-zero status code

---

### Requirement: Configuration Management

The CLI SHALL manage server configuration and user preferences via config commands.

#### Scenario: Set server URL

- **WHEN** user runs `emergent-cli config set-server --url https://api.example.com`
- **THEN** server URL is saved to `~/.emergent/config.yaml`
- **AND** CLI confirms "Server URL updated"

#### Scenario: Show current configuration

- **WHEN** user runs `emergent-cli config show`
- **THEN** CLI displays current configuration including server URL, default org/project, and output format
- **AND** masks sensitive values (passwords, tokens)

#### Scenario: Set default organization and project

- **WHEN** user runs `emergent-cli config set-defaults --org org_123 --project proj_456`
- **THEN** defaults are saved to config file
- **AND** subsequent commands use these defaults when `--org` and `--project` flags are omitted

#### Scenario: Configuration precedence

- **WHEN** user provides `--server` flag on command line
- **AND** config file has different server URL
- **THEN** command-line flag takes precedence over config file

---

### Requirement: Document Operations

The CLI SHALL support CRUD operations for documents in the knowledge base.

#### Scenario: List documents

- **WHEN** user runs `emergent-cli documents list`
- **THEN** CLI displays table of documents with ID, title, status, and created date
- **AND** respects `--org` and `--project` context

#### Scenario: List documents with JSON output

- **WHEN** user runs `emergent-cli documents list --output json`
- **THEN** CLI outputs JSON array of document objects
- **AND** output is valid JSON suitable for `jq` processing

#### Scenario: Create document from file

- **WHEN** user runs `emergent-cli documents create --file /path/to/doc.pdf`
- **THEN** CLI uploads file to server
- **AND** displays progress indicator for large files
- **AND** outputs created document ID and status

#### Scenario: Delete document with confirmation

- **WHEN** user runs `emergent-cli documents delete doc-123`
- **THEN** CLI prompts "Are you sure you want to delete 'Document Title'? (y/N)"
- **AND** deletes document only if user confirms

#### Scenario: Delete document with force flag

- **WHEN** user runs `emergent-cli documents delete doc-123 --force`
- **THEN** CLI deletes document without confirmation prompt

---

### Requirement: Output Formatting

The CLI SHALL support multiple output formats for different use cases.

#### Scenario: Table output (default)

- **WHEN** user runs list command without `--output` flag
- **THEN** CLI displays results in formatted ASCII table
- **AND** columns are properly aligned
- **AND** long text is truncated with ellipsis

#### Scenario: JSON output for scripting

- **WHEN** user runs command with `--output json`
- **THEN** CLI outputs valid JSON
- **AND** includes all fields (not truncated)
- **AND** can be piped to `jq` for processing

#### Scenario: YAML output

- **WHEN** user runs command with `--output yaml`
- **THEN** CLI outputs valid YAML
- **AND** maintains proper indentation

#### Scenario: CSV output for spreadsheets

- **WHEN** user runs command with `--output csv`
- **THEN** CLI outputs valid CSV with header row
- **AND** properly escapes commas and quotes in values

---

### Requirement: Error Handling

The CLI SHALL provide clear, actionable error messages for all failure scenarios.

#### Scenario: Network error

- **WHEN** CLI cannot connect to server
- **THEN** displays "Error: Unable to connect to server at <URL>. Check your network connection and server URL."
- **AND** exits with non-zero status code

#### Scenario: Authentication error

- **WHEN** API returns 401 Unauthorized
- **THEN** displays "Error: Authentication failed. Run 'emergent-cli config set-credentials' to update."
- **AND** includes suggestion to check credentials

#### Scenario: Permission error

- **WHEN** API returns 403 Forbidden
- **THEN** displays "Error: Permission denied. You don't have access to this resource."
- **AND** includes the specific resource that was denied

#### Scenario: Resource not found

- **WHEN** API returns 404 Not Found
- **THEN** displays "Error: <Resource type> '<ID>' not found."
- **AND** suggests checking the ID or listing available resources

#### Scenario: Validation error

- **WHEN** user provides invalid input
- **THEN** displays specific validation error message
- **AND** shows correct usage example

---

### Requirement: Interactive Prompts

The CLI SHALL provide interactive prompts for missing required values when running in a terminal.

#### Scenario: Interactive organization selection

- **WHEN** user runs command requiring organization
- **AND** no `--org` flag or default is set
- **AND** CLI is running in interactive terminal
- **THEN** CLI fetches available organizations
- **AND** displays selection prompt with arrow key navigation
- **AND** uses selected organization for command

#### Scenario: Interactive project selection

- **WHEN** user runs command requiring project
- **AND** no `--project` flag or default is set
- **AND** CLI is running in interactive terminal
- **THEN** CLI fetches available projects for selected organization
- **AND** displays selection prompt
- **AND** uses selected project for command

#### Scenario: Non-interactive mode

- **WHEN** CLI is not running in interactive terminal (piped or redirected)
- **AND** required value is missing
- **THEN** CLI displays error message with required flags
- **AND** does not attempt interactive prompt

---

### Requirement: Chat Operations

The CLI SHALL support chat interactions with the knowledge base.

#### Scenario: Send chat message

- **WHEN** user runs `emergent-cli chat send "What is in the knowledge base?"`
- **THEN** CLI sends message to chat API
- **AND** displays streaming response as it arrives
- **AND** formats markdown in terminal (bold, lists, code blocks)

#### Scenario: View chat history

- **WHEN** user runs `emergent-cli chat history`
- **THEN** CLI displays recent conversations
- **AND** shows conversation ID, title, and last message date

---

### Requirement: Extraction Operations

The CLI SHALL support extraction job management.

#### Scenario: Run extraction on document

- **WHEN** user runs `emergent-cli extraction run doc-123`
- **THEN** CLI creates extraction job
- **AND** displays job ID
- **AND** shows initial status

#### Scenario: Check extraction status

- **WHEN** user runs `emergent-cli extraction status job-456`
- **THEN** CLI displays job status (pending, running, completed, failed)
- **AND** shows progress percentage for running jobs
- **AND** shows extracted entities count for completed jobs

#### Scenario: List extraction jobs

- **WHEN** user runs `emergent-cli extraction list-jobs`
- **THEN** CLI displays table of jobs with ID, document, status, and created date
- **AND** supports `--status` filter (pending, running, completed, failed)

---

### Requirement: Admin Operations

The CLI SHALL support administrative operations for organizations, projects, and users.

#### Scenario: List organizations

- **WHEN** user runs `emergent-cli admin orgs list`
- **THEN** CLI displays table of organizations user has access to
- **AND** shows org ID, name, and role

#### Scenario: List projects

- **WHEN** user runs `emergent-cli admin projects list --org org-123`
- **THEN** CLI displays projects in specified organization
- **AND** shows project ID, name, and document count

#### Scenario: List users

- **WHEN** user runs `emergent-cli admin users list`
- **THEN** CLI displays users in current organization
- **AND** shows user ID, email, and role

---

### Requirement: Server Health

The CLI SHALL support server health and info commands.

#### Scenario: Check server health

- **WHEN** user runs `emergent-cli server health`
- **THEN** CLI calls server health endpoint
- **AND** displays "Server is healthy" with response time
- **OR** displays specific health issues if unhealthy

#### Scenario: Get server info

- **WHEN** user runs `emergent-cli server info`
- **THEN** CLI displays server version, API version, and capabilities
- **AND** does not require authentication

---

### Requirement: Shell Completion

The CLI SHALL support shell completion for bash, zsh, fish, and PowerShell.

#### Scenario: Generate bash completion

- **WHEN** user runs `emergent-cli completion bash`
- **THEN** CLI outputs bash completion script
- **AND** includes instructions for installation

#### Scenario: Generate zsh completion

- **WHEN** user runs `emergent-cli completion zsh`
- **THEN** CLI outputs zsh completion script
- **AND** includes instructions for installation

#### Scenario: Command completion

- **WHEN** user types `emergent-cli doc<TAB>` in configured shell
- **THEN** shell completes to `emergent-cli documents`

#### Scenario: Flag completion

- **WHEN** user types `emergent-cli documents list --out<TAB>`
- **THEN** shell completes to `--output`
- **AND** shows available values (table, json, yaml, csv)

---

### Requirement: Template Pack Management

The CLI SHALL support template pack discovery, installation, and management for configuring extraction schemas.

#### Scenario: List available template packs

- **WHEN** user runs `emergent-cli template-packs list`
- **THEN** CLI displays table of all available template packs
- **AND** shows pack ID, name, version, and description

#### Scenario: View template pack details

- **WHEN** user runs `emergent-cli template-packs get <pack-id>`
- **THEN** CLI displays full pack details including object types and relationship types
- **AND** shows descriptions for each type

#### Scenario: Create custom template pack

- **WHEN** user runs `emergent-cli template-packs create --file pack.json`
- **THEN** CLI validates the JSON schema
- **AND** creates the template pack in the system
- **AND** displays the new pack ID

#### Scenario: List installed template packs

- **WHEN** user runs `emergent-cli template-packs installed`
- **THEN** CLI displays template packs installed in the current project
- **AND** shows installation date and status

#### Scenario: Install template pack to project

- **WHEN** user runs `emergent-cli template-packs install <pack-id>`
- **THEN** CLI installs the pack to the current project
- **AND** displays confirmation with available object types

#### Scenario: Uninstall template pack

- **WHEN** user runs `emergent-cli template-packs uninstall <pack-id>`
- **AND** objects exist using types from this pack
- **THEN** CLI warns about affected objects count
- **AND** prompts for confirmation before uninstalling

#### Scenario: View compiled types

- **WHEN** user runs `emergent-cli template-packs compiled-types`
- **THEN** CLI displays merged object types from all installed packs
- **AND** shows which pack each type originates from

#### Scenario: Validate template pack before creation

- **WHEN** user runs `emergent-cli template-packs validate --file pack.json`
- **THEN** CLI validates JSON structure against expected schema
- **AND** checks required fields (name, version, object_type_schemas)
- **AND** validates object_type_schemas structure (each type has properties)
- **AND** validates relationship_type_schemas structure (fromTypes, toTypes arrays)
- **AND** reports specific validation errors if invalid (missing fields, wrong types)
- **AND** displays "✓ Valid template pack" if validation passes

#### Scenario: Create new version of existing template pack

- **WHEN** user runs `emergent-cli template-packs create --file pack.json`
- **AND** a pack with same name but different version already exists
- **THEN** CLI creates new version in database
- **AND** does not modify existing pack (immutability)
- **AND** displays "✓ Created pack version: <name>@<version> (ID: <pack-id>)"

---

### Requirement: Serve Mode - Documentation Server

The CLI SHALL provide a built-in HTTP server that serves auto-generated documentation from the command structure.

#### Scenario: Start documentation server

- **WHEN** user runs `emergent-cli serve --docs-port 8080`
- **THEN** CLI starts HTTP server on specified port
- **AND** displays URL to access documentation
- **AND** serves HTML documentation at root path

#### Scenario: Live documentation generation

- **WHEN** user accesses documentation server
- **THEN** HTML is generated directly from Cobra command tree
- **AND** reflects current command structure without rebuild
- **AND** includes all flags, descriptions, and examples

#### Scenario: Documentation endpoints

- **WHEN** documentation server is running
- **THEN** `/` serves main documentation page
- **AND** `/schema.json` serves JSON schema of all commands
- **AND** `/cmd/{name}` serves individual command documentation

---

### Requirement: Serve Mode - MCP Proxy Server

The CLI SHALL provide an MCP (Model Context Protocol) server that exposes CLI commands as tools for AI agents.

#### Scenario: Start MCP server over stdio

- **WHEN** user runs `emergent-cli serve --mcp-stdio`
- **THEN** CLI starts MCP server reading from stdin and writing to stdout
- **AND** uses JSON-RPC protocol per MCP specification
- **AND** registers all CLI commands as MCP tools

#### Scenario: Start MCP server over HTTP

- **WHEN** user runs `emergent-cli serve --mcp-port 3100`
- **THEN** CLI starts HTTP server with SSE transport on specified port
- **AND** exposes MCP protocol endpoint
- **AND** displays URL for client connection

#### Scenario: Tool generation from commands

- **WHEN** MCP server initializes
- **THEN** each leaf CLI command becomes an MCP tool
- **AND** command flags become tool input schema properties
- **AND** tool names follow pattern `{command}_{subcommand}` (e.g., `documents_list`)

#### Scenario: Claude Desktop integration

- **GIVEN** Claude Desktop config includes emergent-cli as MCP server
- **WHEN** Claude Desktop starts
- **THEN** it spawns `emergent-cli serve --mcp-stdio`
- **AND** Claude can invoke tools like `documents_list`, `extraction_run`

#### Scenario: Combined serve mode

- **WHEN** user runs `emergent-cli serve --docs-port 8080 --mcp-port 3100`
- **THEN** CLI starts both documentation and MCP servers
- **AND** both servers share the same authentication context
- **AND** displays URLs for both services

