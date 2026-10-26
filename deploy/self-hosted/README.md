# Memory — self-hosted deployment

Single-user deployment of the Memory server, the `memory` CLI, and its backing
services, with MCP access and optional Tailscale networking.

This directory is the **only** supported self-hosted path. There is one
installer (`install-online.sh`); everything else here supports it or documents
the manual equivalent.

## One-command installation

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

The installer:

- pulls the pre-built server image (`ghcr.io/emergent-company/memory-server`)
- generates database and storage credentials
- writes configuration to `~/.memory/config/`
- starts every service with Docker Compose
- downloads the `memory` CLI to `~/.memory/bin/memory` and adds it to `PATH`
- prints an MCP client snippet

It takes about 2–3 minutes.

```bash
memory projects list            # verify the install
memory status                   # server + service health
cat ~/.memory/config.yaml       # CLI config (server URL, API key)
```

Full walkthrough, prerequisites and troubleshooting:
[INSTALL.md](./INSTALL.md).

## Stack components

| Service | Role |
|---|---|
| server | Go API + MCP server (port 3002) |
| PostgreSQL + pgvector | database, embeddings index |
| xberg | document extraction (56+ formats) |
| SeaweedFS | S3-compatible object storage (master + volume + filer + S3) |

## Manual installation

Use this when you want to build the image yourself or run the stack from a
checkout.

```bash
git clone https://github.com/emergent-company/emergent.memory.git
cd emergent.memory/deploy/self-hosted
cp .env.example .env            # or start from config/.env.local
docker compose -f docker-compose.local.yml up -d
```

`docker-compose.local.yml` builds the server from the repository; the compose
file the installer writes under `~/.memory/docker/` uses the published image.

## Configuration

The installer accepts environment variables for the common cases:

```bash
# custom installation directory (default: ~/.memory)
INSTALL_DIR=/opt/memory curl -fsSL .../install-online.sh | bash

# custom server port (default: 3002)
SERVER_PORT=8080 curl -fsSL .../install-online.sh | bash

# embedding provider key, or add it later with `memory config set`
GOOGLE_API_KEY=your-key curl -fsSL .../install-online.sh | bash

# pin a version (default: latest release)
CLI_VERSION=v0.88.2 curl -fsSL .../install-online.sh | bash
```

After installation everything lives under `INSTALL_DIR`:
`bin/` (CLI and helper scripts), `config/` (env), `config.yaml` (CLI config) and
`docker/` (compose file). Database and object-store contents are Docker named
volumes declared by that compose file.

## Upgrade, verify, uninstall

```bash
# upgrade
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash

# verify the running stack
./verify-install.sh

# move an existing object store to SeaweedFS
./UPGRADING_OBJECT_STORE.md

# stop the stack and remove ~/.memory (prompts before deleting data)
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/uninstall.sh | bash
```

## Further reading

- [INSTALL.md](./INSTALL.md) — detailed install guide and troubleshooting
- [UPGRADING_OBJECT_STORE.md](./UPGRADING_OBJECT_STORE.md) — object-store migration
- [../../docs/site/](../../docs/site/) — product documentation (API, SDKs, MCP)
