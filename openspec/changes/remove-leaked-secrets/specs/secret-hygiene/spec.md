## ADDED Requirements

### Requirement: Secrets SHALL never be committed to tracked env files

The repository SHALL enforce a clear boundary between committed configuration files
(which contain only non-secret defaults) and gitignored local override files (which
contain real secrets). No API keys, private keys, tokens, or passwords SHALL appear
in any file tracked by git.

#### Scenario: Developer checks out repo and inspects committed env files

- **WHEN** a developer clones the repository and inspects any committed `.env` file
- **THEN** all credential values SHALL be obvious dummy defaults (e.g., `REPLACE_ME`,
  `your-key-here`, or functionally inert placeholder strings)
- **AND** no real API keys, private keys, tokens, or passwords SHALL be present

#### Scenario: Developer adds a new secret to the project

- **WHEN** a developer needs to introduce a new secret variable
- **THEN** they SHALL place the real value only in the appropriate `.env.local` file
  (e.g., `.env.local` for server, `docker/.env.local` for Docker bootstrap)
- **AND** the committed env file SHALL receive only a placeholder or be left unset

#### Scenario: GitHub Secret Scanning detects a committed secret

- **WHEN** GitHub Secret Scanning is enabled and a commit contains a recognized secret
  pattern
- **THEN** the push SHALL be blocked or an alert raised before the secret is merged

### Requirement: `.gitignore` SHALL cover all local-secret override files

The root `.gitignore` SHALL contain entries that prevent any `.env.local` or
`docker/.env` file from being accidentally committed.

#### Scenario: Developer creates a docker/.env.local file

- **WHEN** a developer creates `docker/.env.local` to hold real Zitadel credentials
- **THEN** git SHALL not track the file (it is covered by the `**/.env.local` pattern)

#### Scenario: Developer creates docker/.env with real credentials

- **WHEN** a developer edits `docker/.env`
- **THEN** git SHALL not track those changes because `docker/.env` is listed in
  `.gitignore`

### Requirement: `docker/.env` SHALL contain only safe dummy defaults

The committed `docker/.env` file SHALL provide a fully functional docker-compose
bootstrap using only non-sensitive placeholder values so the repository can be cloned
and docker-compose started without exposing real credentials.

#### Scenario: New developer clones repo and runs docker-compose

- **WHEN** a new developer clones the repository and runs `docker-compose up`
- **THEN** the stack SHALL start using the placeholder credentials in `docker/.env`
- **AND** no real production or shared credentials SHALL be required for initial startup

#### Scenario: Developer needs real Zitadel credentials for local dev

- **WHEN** a developer needs to connect their local stack to a real Zitadel instance
- **THEN** they SHALL create `docker/.env.local` with the real values
- **AND** docker-compose SHALL load overrides from `docker/.env.local` automatically

### Requirement: Tool config files SHALL reference secrets via env-var substitution

Configuration files tracked in git that require API keys (e.g., `opencode.jsonc`,
`.vscode/mcp.json`) SHALL reference secrets using the host tool's supported env-var
substitution syntax rather than embedding literal key values.

#### Scenario: opencode loads the Context7 API key at runtime

- **WHEN** opencode starts and reads `opencode.jsonc`
- **THEN** the `CONTEXT7_API_KEY` header value SHALL be resolved from the
  `CONTEXT7_API_KEY` environment variable using `"{env:CONTEXT7_API_KEY}"` syntax
- **AND** the literal key value SHALL NOT appear in the file

#### Scenario: VS Code loads MCP server configuration

- **WHEN** VS Code reads `.vscode/mcp.json` to configure MCP servers
- **THEN** any API key arguments SHALL be supplied via environment variable references
- **AND** no literal key values SHALL appear in the tracked file

### Requirement: Documentation SHALL use placeholders for example API keys

Any documentation, quickstart guides, or example configurations committed to the
repository SHALL use clearly marked placeholder values instead of real API keys.

#### Scenario: Developer follows MCP quickstart guide

- **WHEN** a developer reads `docs/integrations/mcp/MCP_INSPECTOR_QUICKSTART.md`
- **THEN** any example commands that require an API key SHALL show
  `<YOUR_CONTEXT7_API_KEY>` or equivalent placeholder
- **AND** no real key values SHALL appear in the document

### Requirement: Git history SHALL not contain committed secret values

After the history rewrite, no historical commit in the `emergent.memory` repository
SHALL contain the specific secret values that were previously leaked. The `emergent.strategy`
repository history SHALL likewise be scrubbed of its previously committed SigNoz and
Brave API keys.

#### Scenario: Auditor searches git history for leaked key

- **WHEN** an auditor runs `git log -p | grep <previously-leaked-key-pattern>`
  on either affected repository after the history rewrite
- **THEN** the search SHALL return no matches

#### Scenario: Attacker clones repository after history rewrite

- **WHEN** an attacker clones the repository and traverses all commits
- **THEN** no usable secret values SHALL be recoverable from any commit object
