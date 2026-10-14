## Purpose

Defines the default agent sandbox workspace image's runtime tooling and the authentication of repository checkout, so that `run_python` and `run_go` work out of the box and private repositories can be cloned with the sandbox's configured git token.

## ADDED Requirements

### Requirement: The default workspace image SHALL provide Python and Go SDK execution tooling

The default agent sandbox workspace image SHALL provide, out of the box, both the Python and Go SDK execution tooling required by the `run_python` and `run_go` workspace tools: the `python3` interpreter, the `pyrunner` daemon at `/usr/local/bin/pyrunner.py`, and the emergent Python SDK installed; plus the `go` toolchain, the `/sdk` source tree, and the `/sdk-template` module with a `replace` directive pointing at `/sdk`.

#### Scenario: run_python works in the default image

- **GIVEN** a sandbox container is launched from the default workspace image
- **WHEN** the `run_python` tool executes a script that imports the emergent Python SDK
- **THEN** `python3` is present and the emergent SDK imports successfully
- **AND** the `pyrunner` daemon is available at `/usr/local/bin/pyrunner.py`

#### Scenario: run_go works in the default image

- **GIVEN** a sandbox container is launched from the default workspace image
- **WHEN** the `run_go` tool copies `/sdk-template` and builds an agent program that imports the Go SDK
- **THEN** `/sdk-template` exists with a `replace` directive to `/sdk`
- **AND** the Go toolchain is version 1.25 or newer so the SDK (which declares `go 1.25.0`) can be built

#### Scenario: the pyrunner daemon starts from the entrypoint

- **GIVEN** gVisor overrides the container CMD but preserves ENTRYPOINT
- **WHEN** the container boots
- **THEN** the pyrunner daemon starts from the entrypoint and creates its input/output FIFOs under `/tmp`

### Requirement: Repository checkout SHALL authenticate with the configured git token

When a sandbox is provisioned with a repository to check out, the checkout SHALL use the sandbox's configured git token when one is present. The token SHALL be resolved from `GH_TOKEN` then `GITHUB_TOKEN`, in the agent definition's sandbox env vars first, then the runtime env vars. For `https://github.com/` URLs the token SHALL be injected as `https://x-access-token:<token>@github.com/…`. The token SHALL NOT be logged and SHALL NOT appear in returned errors; stderr from the clone SHALL be sanitized. When no token is configured, checkout SHALL fall back to an unauthenticated clone for public repositories.

#### Scenario: A configured GH_TOKEN is used for a private clone

- **GIVEN** the agent definition's `sandbox_config.env_vars` contains `GH_TOKEN`
- **AND** the repository URL is a `https://github.com/` URL
- **WHEN** the workspace is provisioned and the repository is cloned
- **THEN** the clone command uses `https://x-access-token:<token>@github.com/…`

#### Scenario: The token is never logged or returned in errors

- **GIVEN** a git token is configured and the clone is attempted (success or failure)
- **WHEN** the checkout service logs progress or returns an error
- **THEN** the token value SHALL NOT appear in any log message or error string

#### Scenario: Public repositories clone unauthenticated when no token is configured

- **GIVEN** no `GH_TOKEN`/`GITHUB_TOKEN` is configured and no GitHub App credential provider is wired
- **WHEN** a public repository is cloned
- **THEN** the clone proceeds with the unauthenticated URL

### Requirement: The image build SHALL authenticate the private-repo SDK fetch without persisting the credential

The build of the default workspace image SHALL fetch the emergent SDK source from the private memory repo using an optional BuildKit secret (`github_token`). Within each `RUN --mount=type=secret,id=github_token,required=false` layer, the token SHALL be written to a temporary `~/.netrc` and removed before the same layer ends, so the token never persists in the final image. The publish workflow SHALL pass `secrets.GITHUB_TOKEN` as the secret, and the CLI SHALL pass `GH_TOKEN`/`GITHUB_TOKEN` when present. When no secret is supplied, the build SHALL proceed unauthenticated (a private-repo fetch then fails with git's own auth error).

#### Scenario: CI supplies the token and the image builds

- **GIVEN** the publish workflow runs with `contents: read`
- **WHEN** the image is built
- **THEN** `secrets.GITHUB_TOKEN` is passed as the `github_token` secret and the SDK fetch succeeds

#### Scenario: The token does not survive into the image

- **GIVEN** a successful build with a token supplied
- **WHEN** the resulting image is inspected
- **THEN** no `~/.netrc` exists and no layer history contains the token value

#### Scenario: No token falls back to unauthenticated

- **GIVEN** no `GH_TOKEN`/`GITHUB_TOKEN` and no `github_token` secret
- **WHEN** the image is built against a public fork
- **THEN** the fetch proceeds unauthenticated
