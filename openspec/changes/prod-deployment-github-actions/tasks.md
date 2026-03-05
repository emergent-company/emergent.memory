## 1. Repository Bootstrap

- [x] 1.1 Initialize the `emergent-company/emergent.memory.infra` private repo with a README and `.gitignore` (ignore `.env`, `*.pem`, `*.key`)
- [x] 1.2 Set up the repo directory structure: `.github/workflows/`, `compose/`, `docs/`

## 2. Dockerfile

- [x] 2.1 Write multi-stage `Dockerfile` — builder stage: `golang:1.25-alpine`, compiles `apps/server-go` with `CGO_ENABLED=0`; accepts `VERSION` build-arg
- [x] 2.2 Write runtime stage: `gcr.io/distroless/static` (or `alpine:3.21`), copies binary + CA certs only
- [x] 2.3 Add `HEALTHCHECK` instruction polling `/health` every 10 s with 30 s start period
- [ ] 2.4 Verify image builds locally and binary runs: `docker build --build-arg VERSION=dev -t emergent-server:dev . && docker run --rm emergent-server:dev --version`

## 3. Docker Compose Stack

- [x] 3.1 Write `compose/docker-compose.yml` with services: `postgres` (`pgvector/pgvector:pg16`), `minio` (`minio/minio`), `server`, `migrator`
- [x] 3.2 Configure `postgres` service: named volume `pgdata`, env vars from `.env`, healthcheck on `pg_isready`
- [x] 3.3 Configure `minio` service: named volume `miniodata`, `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` from `.env`, internal port only
- [x] 3.4 Configure `server` service: image `ghcr.io/emergent-company/emergent-server:${SERVER_VERSION:-latest}`, bind `127.0.0.1:3012:3012`, `depends_on: postgres: condition: service_healthy`, all env from `.env`, Docker healthcheck
- [x] 3.5 Configure `migrator` service: same image as server, command override `["goose", "-dir", "/migrations", "postgres", "${DATABASE_URL}", "up"]`, `restart: "no"`, `depends_on: postgres: condition: service_healthy`
- [x] 3.6 Write `compose/.env.example` documenting every required variable with placeholder values and comments (no real secrets)

## 4. GitHub Actions Workflow

- [x] 4.1 Create `.github/workflows/deploy-production.yml` with `on: workflow_dispatch` trigger and `version` input (optional string, default empty)
- [x] 4.2 Add step to resolve latest release tag from `emergent-company/emergent` via `gh release view --repo emergent-company/emergent --json tagName` when `version` input is empty; export as `SERVER_VERSION` env var
- [x] 4.3 Add `tailscale/github-action` step: authenticate runner to tailnet using `TS_OAUTH_CLIENT_ID` + `TS_OAUTH_CLIENT_SECRET` secrets, `ephemeral: true` so the node is auto-removed after the job
- [x] 4.4 Add step to set up SSH: write `PROD_SSH_KEY` secret to `~/.ssh/id_deploy`, add `PROD_SSH_KNOWN_HOSTS` to `~/.ssh/known_hosts`, set correct permissions
- [x] 4.5 Add step to write `.env` file on host: SSH heredoc targeting `PROD_TAILSCALE_HOST` that writes all `PROD_*` secrets to `~/emergent/.env` (no values in logs — secrets masked by GitHub)
- [x] 4.6 Add step to pull new image on host: `ssh … "cd ~/emergent && docker compose pull server migrator"`
- [x] 4.7 Add step to run migrations: `ssh … "cd ~/emergent && docker compose run --rm migrator"` — fail workflow on non-zero exit
- [x] 4.8 Add step to restart server container: `ssh … "cd ~/emergent && docker compose up -d --no-deps server"`
- [x] 4.9 Add step to wait for health check: poll `http://127.0.0.1:3012/health` via SSH in a loop (60 s timeout); on timeout, roll back by restarting with previous image tag and fail the job
- [x] 4.10 Add workflow summary step: write deployed version, timestamp, and Tailscale host to `$GITHUB_STEP_SUMMARY`

## 5. GitHub Repository Secrets Documentation

- [x] 5.1 Write `docs/secrets.md` listing every required secret name, its purpose, and how to generate/obtain it
- [x] 5.2 List secrets: `TS_OAUTH_CLIENT_ID`, `TS_OAUTH_CLIENT_SECRET` (Tailscale OAuth client with `devices` write scope), `PROD_TAILSCALE_HOST` (host's Tailscale MagicDNS hostname or IP), `PROD_SSH_USER`, `PROD_SSH_KEY` (ED25519 private key for deploy user), `PROD_SSH_KNOWN_HOSTS`, `PROD_POSTGRES_PASSWORD`, `PROD_MINIO_SECRET_KEY`, `PROD_MINIO_ACCESS_KEY`, `PROD_DATABASE_URL`, `GHCR_PAT` (for pulling from ghcr.io on the host)

## 6. Host Bootstrap Guide

- [x] 6.1 Write `docs/host-bootstrap.md` with one-time setup steps: install Docker + Compose v2, install Tailscale and join tailnet (`tailscale up --authkey <key>`), create deploy user, add SSH public key, configure firewall to block port 22 except on `tailscale0` interface, `docker login ghcr.io`, create `~/emergent/` directory, first manual `docker compose up -d postgres minio`, first `docker compose run --rm migrator`
- [x] 6.2 Document how to create the Tailscale OAuth client in the Tailscale admin console and what scopes it needs (`devices` write for ephemeral node registration)
- [x] 6.3 Document reverse proxy configuration example (nginx snippet) for `memory.emergent-company.ai` → `http://127.0.0.1:3012`

## 7. Validation

- [ ] 7.1 Dry-run the workflow with a test tag against a staging host (or the production host with a known-good image) to confirm the full sequence works end-to-end
- [ ] 7.2 Verify rollback path: simulate health check failure and confirm previous container restarts
- [ ] 7.3 Confirm no secrets appear in workflow logs (GitHub masks them; review a sample run)
