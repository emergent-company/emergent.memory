## ADDED Requirements

### Requirement: Manual-trigger deployment workflow exists
The infra repo SHALL contain a GitHub Actions workflow file at `.github/workflows/deploy-production.yml` that is triggered only via `workflow_dispatch` (manual trigger from the GitHub Actions UI).

#### Scenario: Workflow appears in Actions UI
- **WHEN** an authorized user opens the Actions tab in the infra repo
- **THEN** a `Deploy to Production` workflow SHALL appear
- **AND** a `Run workflow` button SHALL allow selecting an optional version tag (defaulting to `latest`)

#### Scenario: Workflow does not trigger automatically
- **WHEN** code is pushed to any branch
- **THEN** the deploy workflow SHALL NOT run automatically

### Requirement: Workflow resolves the target release version
The workflow SHALL accept an optional `version` input. When omitted, it SHALL resolve the latest published release from `emergent-company/emergent` using the GitHub API.

#### Scenario: Default deploy uses latest release
- **WHEN** the workflow is triggered with no version input
- **THEN** the workflow SHALL query the GitHub API for the latest release tag of `emergent-company/emergent`
- **AND** that tag SHALL be used as the image tag for the deployment

#### Scenario: Pinned version deploy
- **WHEN** the workflow is triggered with `version: v1.5.2`
- **THEN** the workflow SHALL use `ghcr.io/emergent-company/emergent-server:v1.5.2` as the target image
- **AND** the resolved version SHALL be logged in the workflow summary

### Requirement: Workflow connects to the production host over Tailscale
The workflow SHALL join the GitHub Actions runner to the operator's Tailscale network before any SSH connection is made. The production host's SSH port SHALL NOT be reachable from the public internet — only from the Tailscale network interface.

#### Scenario: Runner joins tailnet before SSH
- **WHEN** the deployment workflow starts
- **THEN** the `tailscale/github-action` step SHALL authenticate the runner using `TS_OAUTH_CLIENT_ID` and `TS_OAUTH_CLIENT_SECRET` secrets
- **AND** the runner SHALL appear as an ephemeral node in the tailnet
- **AND** only after the Tailscale step succeeds SHALL any SSH command be executed

#### Scenario: Ephemeral runner node is cleaned up after the job
- **WHEN** the workflow job ends (success or failure)
- **THEN** the runner's Tailscale node SHALL be automatically removed from the tailnet
- **AND** no persistent Tailscale node SHALL remain from completed workflow runs

#### Scenario: SSH port is unreachable from public internet
- **WHEN** a connection attempt is made to the host on port 22 from a non-Tailscale source
- **THEN** the host firewall SHALL drop the connection
- **AND** the connection SHALL only succeed from within the Tailscale network

### Requirement: Workflow authenticates to the production host via SSH over Tailscale
The workflow SHALL connect to the production host using an SSH private key stored as a GitHub repository secret, targeting the host's Tailscale hostname or IP. The host fingerprint SHALL be verified against a known-hosts entry stored as a secret.

#### Scenario: SSH connection established over Tailscale
- **WHEN** the workflow runs the deployment step
- **THEN** it SHALL connect to the host at `${{ secrets.PROD_TAILSCALE_HOST }}` as `${{ secrets.PROD_SSH_USER }}`
- **AND** the connection SHALL use the private key from `${{ secrets.PROD_SSH_KEY }}`
- **AND** the connection SHALL route through the Tailscale network interface

#### Scenario: Unknown host is rejected
- **WHEN** the host fingerprint does not match `${{ secrets.PROD_SSH_KNOWN_HOSTS }}`
- **THEN** the SSH step SHALL fail with a host verification error
- **AND** the deployment SHALL NOT proceed

### Requirement: Workflow writes .env file from secrets before deploying
Before restarting any containers, the workflow SHALL SSH to the host and write a fresh `.env` file to the compose directory from GitHub secrets, ensuring the running configuration always matches the secrets store.

#### Scenario: .env file is refreshed on every deploy
- **WHEN** the deployment workflow runs
- **THEN** the `.env` file on the host SHALL be overwritten with values from GitHub secrets
- **AND** no plaintext secrets SHALL appear in the workflow logs

### Requirement: Workflow runs migrations before restarting the server
The deploy workflow SHALL run `docker compose run --rm migrator` on the host before replacing the server container, and SHALL fail if migrations return a non-zero exit code.

#### Scenario: Migrations succeed — deploy continues
- **WHEN** migrations complete with exit code 0
- **THEN** the workflow SHALL proceed to pull the new server image and restart the container

#### Scenario: Migrations fail — deploy halts
- **WHEN** `docker compose run --rm migrator` exits with a non-zero code
- **THEN** the workflow SHALL fail at that step
- **AND** the server container SHALL NOT be restarted

### Requirement: Workflow performs a health-check-gated rolling update
After pulling the new image, the workflow SHALL restart the server container and wait for the `/health` endpoint to return HTTP 200 before marking the deployment as successful.

#### Scenario: Successful rolling update
- **WHEN** the new server image starts and `/health` returns HTTP 200 within 60 seconds
- **THEN** the workflow job SHALL succeed
- **AND** the workflow summary SHALL log the deployed version

#### Scenario: Health check timeout triggers rollback
- **WHEN** `/health` does not return HTTP 200 within 60 seconds after the new container starts
- **THEN** the workflow SHALL restart the container using the previously deployed image tag
- **AND** the workflow job SHALL fail with a message indicating rollback was performed

### Requirement: All production secrets stored as GitHub repository secrets
No passwords, API keys, SSH private keys, or other credentials SHALL be committed to the infra repo. All secrets SHALL be stored as GitHub repository secrets and referenced in workflow files via the `${{ secrets.* }}` syntax.

#### Scenario: Required secrets documented
- **WHEN** a developer reads the repo README or `docs/secrets.md`
- **THEN** they SHALL find a complete list of required secret names and their descriptions
- **AND** no secret values SHALL appear in any committed file

#### Scenario: Missing secret causes workflow failure
- **WHEN** a required secret is not set and the workflow runs
- **THEN** the affected step SHALL fail with a clear error message identifying the missing secret
