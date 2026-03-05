## Context

The Emergent server needs a production home at `memory.emergent-company.ai`. A dedicated physical machine has been provisioned; it has an internal/private IP only, so TLS termination and virtual-host routing are handled by an external reverse proxy managed by the operator. This infrastructure repo (`emergent-company/emergent.memory.infra`) is the single source of truth for everything that runs on that machine. The main `emergent-company/emergent` monorepo already publishes versioned release artifacts (GitHub Releases + container images via `ghcr.io`). The deployment workflow consumes those artifacts rather than building from source.

The host's SSH port is not exposed to the public internet. The GitHub Actions runner reaches the host exclusively over a Tailscale private mesh network, so no firewall rule ever opens SSH to the world.

## Goals / Non-Goals

**Goals:**
- Reproducible, version-controlled production stack for the physical host
- Docker Compose-based service definition (Postgres, MinIO, server, and optional supporting services)
- Multi-stage Dockerfile that produces a minimal production image for the Go server
- Manual-trigger (`workflow_dispatch`) GitHub Actions workflow that finds the latest release, connects to the host over Tailscale, and performs a smooth rolling update
- All secrets stored as GitHub repository secrets — nothing committed to the repo
- Health-check-gated restart so users experience minimal disruption
- Support for running database migrations as part of the deploy step
- SSH port never exposed to the public internet — all runner↔host traffic goes over Tailscale

**Non-Goals:**
- Kubernetes, Helm, or any cloud-managed compute
- Automatic deploy on every push (all deploys are human-triggered)
- SSL certificate management (handled externally by the operator's reverse proxy)
- CI builds of the Go binary (the workflow downloads a published release artifact/image)
- Frontend deployment (separate concern, separate repo)
- Tailscale ACL management (operator configures the tailnet; this repo only consumes it)

## Decisions

### 1. Container image source: `ghcr.io` via GitHub Packages

**Decision:** The deploy workflow pulls `ghcr.io/emergent-company/emergent-server:<tag>` rather than building on the host or downloading a binary tarball.

**Rationale:** Images are pre-built, checksummed, and immutable. No build toolchain needed on the production host. GitHub Packages is already used for the monorepo — no new registry to manage.

**Alternative considered:** Download binary tarball from GitHub Release assets. Rejected: more moving parts (extract, restart systemd service), and no layer caching benefit for incremental updates.

---

### 2. Network path: Tailscale mesh instead of public SSH

**Decision:** The GitHub Actions runner joins the Tailscale network at the start of each workflow run using the `tailscale/github-action` action and a Tailscale OAuth client stored as a secret. It then SSHes to the host's Tailscale IP (e.g. `100.x.x.x`) or Tailscale MagicDNS hostname. The host's SSH daemon listens normally but the firewall blocks port 22 from all public interfaces — only Tailscale interface traffic is accepted.

**Rationale:** The host has no public IP. Even if it did, exposing SSH to the internet is a significant attack surface. Tailscale provides mutual authentication at the network layer (WireGuard + device certificates) before a single SSH packet is exchanged. The runner's ephemeral Tailscale node is automatically cleaned up when the job ends. No VPN server to maintain — Tailscale is peer-to-peer.

**Alternative considered:** Cloudflare Tunnel. Rejected: requires a persistent cloudflared daemon on the host and a Cloudflare account dependency; Tailscale is already conceptually simpler for a single-machine setup.

**Alternative considered:** Traditional SSH via public IP with fail2ban + allowlist. Rejected: requires maintaining a static IP allowlist for GitHub Actions runner IPs, which GitHub rotates and publishes as a large CIDR range — effectively open to all GitHub-hosted runners.

**How it works in the workflow:**
1. `tailscale/github-action` step joins the runner to the tailnet using `TS_OAUTH_CLIENT_ID` + `TS_OAUTH_CLIENT_SECRET` secrets (ephemeral node, no stored key)
2. Subsequent `ssh` commands target `${{ secrets.PROD_TAILSCALE_HOST }}` (the host's Tailscale hostname or IP)
3. SSH key auth still used on top of Tailscale for defence-in-depth (deploy user with restricted shell)
4. Runner's ephemeral Tailscale node is deleted when the job completes

---

### 3. Deploy mechanism: SSH + `docker compose pull && up`

**Decision:** The GitHub Actions runner SSHs into the production host using a deploy key stored as a secret, then runs `docker compose pull && docker compose up -d --no-deps server` followed by a health-check wait.

**Rationale:** Simple, auditable, no agent to maintain on the host. The host only needs Docker and an SSH daemon.

**Alternative considered:** Watchtower (auto-pull on image push). Rejected: we want explicit human control over when production is updated.

**Alternative considered:** Ansible playbook. Rejected: adds a Python dependency and significant boilerplate for what is essentially three shell commands.

---

### 3. Deploy mechanism: SSH + `docker compose pull && up`

**Decision:** All credentials (`PROD_SSH_KEY`, `PROD_SSH_USER`, `PROD_TAILSCALE_HOST`, `TS_OAUTH_CLIENT_ID`, `TS_OAUTH_CLIENT_SECRET`, `PROD_POSTGRES_PASSWORD`, `PROD_MINIO_SECRET_KEY`, etc.) are stored as GitHub repo secrets and injected at workflow run time. The `.env` file on the host is written by the workflow from secrets, never committed.

**Rationale:** GitHub secrets are encrypted at rest, scoped to the repo, and audit-logged. The infra repo is private, adding another layer of protection.

**Alternative considered:** HashiCorp Vault. Rejected: over-engineered for a single-host deployment.

---

### 4. Secrets management: GitHub repository secrets

**Decision:** Update sequence:
1. `docker compose pull server` (pulls new image in background while old container still serves traffic)
2. `docker compose up -d --no-deps server` (starts new container; Docker stops old one after new one is healthy)
3. Wait for `/health` to return 200 (configurable timeout, default 60 s)
4. If health check fails within timeout, run `docker compose up -d --no-deps --scale server=1 server` with previous image tag to roll back

**Rationale:** Single-container deployment can't do true blue-green without a second host, but health-check-gated restart keeps the downtime window to the container startup time (typically < 5 s for the Go server).

---

### 5. Smooth rolling update strategy

**Decision:** Update sequence:
1. `docker compose pull server` (pulls new image in background while old container still serves traffic)
2. `docker compose up -d --no-deps server` (starts new container; Docker stops old one after new one is healthy)
3. Wait for `/health` to return 200 (configurable timeout, default 60 s)
4. If health check fails within timeout, run `docker compose up -d --no-deps server` with previous image tag to roll back

**Rationale:** Single-container deployment can't do true blue-green without a second host, but health-check-gated restart keeps the downtime window to the container startup time (typically < 5 s for the Go server).

---

### 6. Database migrations run before server restart

**Decision:** The deploy workflow runs `docker compose run --rm migrator` (a one-shot container using the same server image, entrypoint overridden to `goose up`) before rolling the server container.

**Rationale:** Migrations must be applied before the new server version starts. Running as a separate one-shot service keeps the compose file clean and the migration log separate from server logs.

**Constraint:** Migrations must be backward-compatible (new columns nullable or with defaults) so the old server can still run during the pull phase.

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Tailscale OAuth secret compromise | Rotate via Tailscale admin console; ephemeral node is auto-removed after job ends — no persistent node to abuse |
| SSH key compromise | Rotate key via GitHub secret update; deploy user has restricted shell and no sudo |
| Tailscale service outage blocks all deploys | Fall back to direct SSH via temporary firewall rule; restore Tailscale and re-lock afterward |
| Migration fails mid-deploy | Health check gates server restart; operator runs rollback migration manually via `docker compose run` |
| Image pull fails (registry down) | Workflow fails before `up`; running container is untouched |
| `docker compose up` starts new container before old one stops → port conflict | Use `--no-deps` with a single replicated service; Docker handles the swap atomically |
| Secrets drift (host `.env` out of sync with GitHub secrets) | Workflow always rewrites `.env` from secrets on every deploy |

## Migration Plan

1. **Bootstrap** (one-time, manual by operator):
   - Install Docker + Docker Compose v2 on the host
   - Install Tailscale on the host and join it to the tailnet
   - Create a Tailscale OAuth client (with `devices` write scope) for the GitHub Actions runner; store as `TS_OAUTH_CLIENT_ID` + `TS_OAUTH_CLIENT_SECRET` secrets
   - Lock down the host firewall: block port 22 from all interfaces except the Tailscale interface (`tailscale0`)
   - Create deploy user with SSH access (key auth only) and permission to run `docker compose` commands
   - Add host's Tailscale hostname/IP as `PROD_TAILSCALE_HOST` secret; add SSH key fingerprint as `PROD_SSH_KNOWN_HOSTS`
   - Run `docker compose up -d` manually on the host to bring up Postgres and MinIO for the first time
   - Run `docker compose run --rm migrator` to apply all migrations on the fresh DB
2. **Normal deploys** (via GitHub Actions):
   - Operator navigates to Actions → `deploy-production` → Run workflow
   - Selects optional version tag (defaults to `latest`)
   - Workflow: runner joins tailnet → SSH to host → pull → migrate → restart → health check → runner leaves tailnet
3. **Rollback**:
   - Re-run workflow with previous version tag
   - If DB migration was destructive, operator runs manual `goose down` on host via Tailscale SSH

## Open Questions

- Should the workflow post a Slack/email notification on success/failure? (Left as a follow-up enhancement)
- Zitadel auth — is it self-hosted on this same machine or external SaaS? If self-hosted, it needs to be in the Compose file. (Assumption: external Zitadel SaaS — not included in this scope)
- Should `pgvector` extension be enabled automatically in the init script, or is it already in the base image? (Assumption: use `pgvector/pgvector:pg16` image which includes the extension)
