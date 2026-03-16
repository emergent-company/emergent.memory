# post-login-context-hint

Context-aware post-login output that checks folder initialization state and either suggests `memory init` or displays inline authentication status with current project info.

## Requirements

### Requirement: Post-login folder initialization detection
After successful OAuth login, the CLI SHALL read `.env.local` in the current working directory and check for the presence of `MEMORY_PROJECT_ID` to determine whether the folder has an initialized Memory project.

#### Scenario: .env.local missing
- **WHEN** `memory login` completes successfully
- **AND** no `.env.local` file exists in the current directory
- **THEN** the CLI treats the folder as uninitialized

#### Scenario: .env.local exists without MEMORY_PROJECT_ID
- **WHEN** `memory login` completes successfully
- **AND** `.env.local` exists but does not contain `MEMORY_PROJECT_ID`
- **THEN** the CLI treats the folder as uninitialized

#### Scenario: .env.local exists with MEMORY_PROJECT_ID
- **WHEN** `memory login` completes successfully
- **AND** `.env.local` contains a non-empty `MEMORY_PROJECT_ID`
- **THEN** the CLI treats the folder as initialized

### Requirement: Suggest memory init for uninitialized folders
When the folder is not initialized, the CLI SHALL print a suggestion to run `memory init` instead of the previous `memory status` hint.

#### Scenario: Uninitialized folder after login
- **WHEN** login succeeds
- **AND** the folder is not initialized
- **THEN** the CLI prints the logged-in identity line (e.g., `Logged in as user@example.com`)
- **AND** prints `Run 'memory init' to set up a project in this folder.`
- **AND** does NOT print the previous `Run 'memory status'` message

### Requirement: Inline status display for initialized folders
When the folder is already initialized, the CLI SHALL display authentication status and current project information inline immediately after login, without requiring the user to run a separate command.

#### Scenario: Initialized folder after login — full info
- **WHEN** login succeeds
- **AND** the folder is initialized with `MEMORY_PROJECT_ID` and `MEMORY_PROJECT_NAME` in `.env.local`
- **THEN** the CLI prints the logged-in identity line
- **AND** prints an authentication status block showing Mode (OAuth), User email, and Status (Authenticated)
- **AND** prints a current project line showing the project name and ID from `.env.local`

#### Scenario: Initialized folder after login — name missing
- **WHEN** login succeeds
- **AND** `.env.local` contains `MEMORY_PROJECT_ID` but not `MEMORY_PROJECT_NAME`
- **THEN** the CLI prints the authentication status block
- **AND** prints the project line using only the project ID

### Requirement: No static memory status hint
The previous static message `Run 'memory status' to see your account and available projects.` SHALL no longer be printed after login. It is replaced entirely by the context-aware behavior described above.

#### Scenario: Old hint removed
- **WHEN** login completes successfully under any folder state
- **THEN** the string `Run 'memory status' to see your account and available projects.` is never printed
