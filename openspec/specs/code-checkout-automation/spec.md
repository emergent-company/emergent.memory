# code-checkout-automation Specification

## Purpose
TBD - created by archiving change agent-workspace-infrastructure. Update Purpose after archive.
## Requirements
### Requirement: Automatic repository clone on workspace creation

The system SHALL automatically clone a specified git repository into the workspace container when a `repository_url` is provided in the workspace creation request.

#### Scenario: Clone public repository

- **WHEN** a workspace is created with `repository_url = "https://github.com/org/public-repo"` and `branch = "main"`
- **THEN** the system clones the repository into `/workspace` directory inside the container, checks out the specified branch, and the workspace status becomes `ready` only after the clone completes

#### Scenario: Clone private repository with server-managed credentials

- **WHEN** a workspace is created with `repository_url = "https://github.com/org/private-repo"` and the Emergent server has a configured GitHub App with a valid installation
- **THEN** the system generates a short-lived installation access token (1-hour expiry), clones the repository using `https://x-access-token:${TOKEN}@github.com/org/private-repo.git`, the token is NEVER written to the workspace filesystem or exposed to the agent, and the token expires automatically

#### Scenario: Clone fails due to invalid credentials

- **WHEN** a workspace is created with a private repository URL and the GitHub App is not configured or the installation token generation fails
- **THEN** the system returns a clear error "Repository authentication failed — connect GitHub in Settings > Integrations" without exposing any credentials, the workspace is still created (empty, without code), and the agent is informed they can manually clone

#### Scenario: Clone fails due to network error

- **WHEN** a workspace is created and the git clone fails due to network issues
- **THEN** the system retries up to 3 times with exponential backoff (2s, 4s, 8s), and if all retries fail, returns the workspace without code and an error message

#### Scenario: Workspace creation without repository

- **WHEN** a workspace is created without a `repository_url`
- **THEN** the system creates an empty workspace with a `/workspace` directory and sets status to `ready` immediately

### Requirement: Branch selection and checkout

The system SHALL support checking out specific branches, tags, or commits in the cloned repository.

#### Scenario: Checkout specific branch

- **WHEN** a workspace is created with `branch = "feature/auth"`
- **THEN** the system clones the repository and checks out the `feature/auth` branch

#### Scenario: Checkout specific commit SHA

- **WHEN** a workspace is created with `branch = "abc123def456"` (a commit SHA)
- **THEN** the system clones the repository and checks out the specific commit in detached HEAD state

#### Scenario: Default branch when none specified

- **WHEN** a workspace is created with `repository_url` but no `branch` specified
- **THEN** the system clones the repository's default branch (typically `main` or `master`)

### Requirement: Credential management

The system SHALL manage git credentials centrally via GitHub App installation tokens without exposing them to workspace containers or agents.

#### Scenario: GitHub App token generation

- **WHEN** a private repository operation is needed (clone, push, pull)
- **THEN** the system generates a short-lived installation access token from the stored GitHub App credentials (app_id + encrypted PEM), caches it for 55 minutes, and uses it for the operation without storing it in any workspace

#### Scenario: GitHub App validation on connection

- **WHEN** a GitHub App is connected via the manifest flow or CLI setup
- **THEN** the system validates the credentials by generating a test installation token and making a test API call, and stores the credentials only if validation succeeds

#### Scenario: Git operations use server credentials transparently

- **WHEN** an agent uses the git tool to push commits from a workspace
- **THEN** the system injects a short-lived installation token into the git remote URL temporarily for the push operation only, removes it immediately after, and the agent never sees the credential. Commits are authored as `emergent-app[bot]`.

### Requirement: Repository synchronization

The system SHALL support updating the code in an existing workspace to the latest version.

#### Scenario: Pull latest changes

- **WHEN** an agent requests a git pull operation via the git tool with `action = "pull"`
- **THEN** the system performs `git pull origin <current-branch>` using server-managed credentials and returns the pull result (files changed, conflicts if any)

#### Scenario: Pull with local changes

- **WHEN** a git pull is requested but there are uncommitted local changes that would conflict
- **THEN** the system returns an error describing the conflict without overwriting local changes, and suggests the agent commit or stash changes first

