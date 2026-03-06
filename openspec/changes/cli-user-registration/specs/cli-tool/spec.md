## MODIFIED Requirements

### Requirement: Authentication and Credential Management

The CLI SHALL authenticate and register users via OAuth 2.0 Device Authorization Grant flow and manage credentials securely.

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

#### Scenario: Unauthenticated status hints at register

- **WHEN** user runs `emergent status`
- **AND** no credentials file exists and no API key is configured
- **THEN** CLI SHALL print "Not authenticated."
- **AND** CLI SHALL print a message suggesting both `emergent login` (returning users) and `emergent register` (new users) as next steps

## ADDED Requirements

### Requirement: Register command is a top-level CLI command

The CLI SHALL expose `emergent register` as a top-level command, parallel to `emergent login`, as the recommended entry point for new users creating an account for the first time.

#### Scenario: Register command is discoverable

- **WHEN** user runs `emergent --help`
- **THEN** `register` SHALL appear in the list of available commands
- **AND** its short description SHALL indicate it is for creating a new account
