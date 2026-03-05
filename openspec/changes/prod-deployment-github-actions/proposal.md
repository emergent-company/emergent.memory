## Why

The Emergent server needs a reproducible, maintainable production deployment to `memory.emergent-company.ai` on a dedicated physical machine. Currently there is no infrastructure-as-code repo for this host, so deployments are manual and fragile. A proper GitHub Actions workflow with Docker-based packaging lets operators trigger controlled releases with zero ad-hoc SSH commands.

## What Changes

- New private repo `emergent-company/emergent.memory.infra` contains all production infrastructure for the dedicated host
- Dedicated `Dockerfile` for the Emergent Go server, optimised for production (multi-stage, minimal image)
- Docker Compose file describing the full production stack: server, Postgres, MinIO, and supporting services
- GitHub Actions workflow: manually-triggered (`workflow_dispatch`), finds the latest server package from `emergent-company/emergent` releases, pulls and deploys it to the production host via SSH
- GitHub repository secrets store all credentials (SSH key, DB passwords, env vars) — no secrets committed to the repo
- Deployment is rolling/blue-green friendly: new container starts, health check passes, old container stops
- SSL and virtual-host termination handled externally by the operator's reverse proxy (no TLS config in this repo)

## Capabilities

### New Capabilities

- `prod-docker-stack`: Dockerfile + Docker Compose for the production Emergent server stack on a bare-metal host (no Kubernetes, no cloud-managed services)
- `prod-github-actions-deploy`: Manual-trigger GitHub Actions workflow that finds the latest Emergent release, SSHs into the production host, and performs a smooth container rolling update

### Modified Capabilities

- `deployment`: Extend existing deployment spec to cover the production host scenario (GitHub Actions + dedicated machine). No breaking changes to existing local/dev Compose setup.

## Impact

- **New repo**: `emergent-company/emergent.memory.infra` (private, empty — all files are new)
- **No changes** to the main `emergent-company/emergent` monorepo
- **GitHub secrets required**: `PROD_SSH_HOST`, `PROD_SSH_USER`, `PROD_SSH_KEY`, `PROD_POSTGRES_PASSWORD`, `PROD_MINIO_SECRET_KEY`, and any other env vars the server needs
- **GitHub Packages / Releases**: the workflow consumes container images or release archives from `emergent-company/emergent` — release tagging convention must be stable
- **Reverse proxy**: the operator manages SSL + virtual host for `memory.emergent-company.ai`; this repo only exposes the server on an internal port (e.g. `3012`) on the private network interface
