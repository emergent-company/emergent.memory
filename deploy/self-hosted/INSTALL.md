# Installing Memory (self-hosted)

One command installs the Memory server, its backing services, and the `memory`
CLI on a single machine:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

Rolling it out to a team or a server without internet access? Jump to
[Manual installation](#manual-installation).

## Requirements

| Requirement | Notes |
|---|---|
| Docker Engine 29.5.1+ with Compose v2 | `docker compose version` must work. This is the floor for sandbox deployments: sandboxes drive the host's `dockerd` through `/var/run/docker.sock`, so the engine version — not our Go module pin — decides whether the `docker cp`/archive and plugin-privilege advisories are fixed. |
| 4 GB RAM, 10 GB disk | 2 GB RAM is enough with no local embedding model |
| Linux or macOS | the CLI also ships for Windows; the server stack needs Docker |
| Ports | `3002` (API/MCP), `15432` (Postgres), `18000` (extraction), `19000` (object store) |
| curl + tar/gzip | used by the installer |

No Python, Go or Node toolchain is needed — the installer pulls pre-built
images and binaries.

Docker Engine 29.5.1 fixes the three `docker cp`/archive advisories named in
that release — `GHSA-x86f-5xw2-fm2r`, `GHSA-vp62-88p7-qqf5` and
`GHSA-rg2x-37c3-w2rh` — and also covers `GHSA-pxq6-2prw-chj9`, which is fixed
from 29.3.1. An out-of-date daemon is the real attack surface when sandboxes run
untrusted code, and no Go dependency bump can remediate it: the vulnerable code
is daemon-side, executed by the host's Engine.

## What the installer does

1. Checks Docker, Compose and the required tools.
2. Pulls `ghcr.io/emergent-company/memory-server` (tag taken from `CLI_VERSION`).
3. Generates the database password and the server API key.
4. Writes the environment to `~/.memory/config/.env.local` and the compose
   file to `~/.memory/docker/docker-compose.yml`.
5. Starts the stack with `docker compose up -d` and waits for `/health`.
6. Downloads the `memory` CLI to `~/.memory/bin/memory` and adds that directory
   to `PATH` in your shell profile.
7. Writes `~/.memory/config.yaml` (server URL + API key) and prints an MCP
   client snippet.

Re-running the same command upgrades in place: it keeps `~/.memory/config/`,
the Docker volumes holding the database and object store, and your API key, and
only swaps the server image.

## What gets installed

| Container | Role | Host port |
|---|---|---|
| `memory-server` | API + MCP server | 3002 |
| `memory-db` | PostgreSQL 18 + pgvector | 15432 |
| `memory-xberg` | document extraction (56+ formats) | 18000 |
| `memory-seaweedfs` | S3-compatible object storage | 19000 |
| `memory-storage-init` | one-shot bucket initialisation | — |

These are the ports the installer writes to `~/.memory/config/.env.local`
(`POSTGRES_PORT`, `XBERG_PORT`, `OBJECT_STORE_API_PORT`, `SERVER_PORT`); change
them there and restart the stack.

Everything lives under `~/.memory`:

```
~/.memory/
├── bin/            memory CLI + helper scripts (emergent-ctl, emergent-auth)
├── config/         .env.local (secrets, ports, provider keys)
├── config.yaml     CLI config, written by the installer
├── docker/         docker-compose.yml used by the stack
└── version         installed version
```

Database and object-store contents live in Docker named volumes
(`postgres_data`, `object_store_data`) declared by that compose file, not in a
directory under `~/.memory`.

## After installation

The installer prints the API key; it is also in
`~/.memory/config/.env.local` (`STANDALONE_API_KEY`).

```bash
memory projects list                     # list projects (creates none)
memory status                            # server + service health
memory config show                       # resolved CLI configuration
docker compose -f ~/.memory/docker/docker-compose.yml logs -f server
```

Open a new terminal if `memory` is not on `PATH` yet, or add it yourself:

```bash
export PATH="$HOME/.memory/bin:$PATH"
```

### Connect an AI agent over MCP

Memory is a **remote MCP server** (Streamable HTTP); there is no local stdio
command. Point the client at the server with the project to expose:

```json
{
  "mcpServers": {
    "memory": {
      "type": "http",
      "url": "http://localhost:3002/api/mcp",
      "headers": {
        "X-Project-ID": "<PROJECT_ID>",
        "X-API-Key": "<API_KEY>"
      }
    }
  }
}
```

`memory projects list` prints the project IDs; the API key is
`STANDALONE_API_KEY` in `~/.memory/config/.env.local` (the installer also prints
it). `memory mcp-guide` reprints this config with the live project ID at any
time, and also emits the OpenCode and legacy-SSE variants.

### Embeddings

Memory needs an embedding provider for semantic search. Either pass a key to the
installer (`GOOGLE_API_KEY=...`) or add one later:

```bash
memory config set google_api_key YOUR_KEY
```

Without a provider, ingestion and graph CRUD work; embeddings stay pending.

## Custom configuration

| Variable | Default | Effect |
|---|---|---|
| `INSTALL_DIR` | `~/.memory` | where binaries, config and data live |
| `SERVER_PORT` | `3002` | host port for the API/MCP server |
| `CLI_VERSION` | `latest` | the one version knob: CLI release *and* server image tag |
| `MEMORY_VERSION` | `main` | branch used to download the helper scripts and `init.sql` |
| `GOOGLE_API_KEY` | — | embedding provider key |
| `CLIENT_ONLY` | `false` | install only the CLI against an existing server |

```bash
INSTALL_DIR=/opt/memory SERVER_PORT=8080 \
  curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

Client-only install, for a workstation that talks to a shared server:

```bash
CLIENT_ONLY=true \
  curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
memory login --server-url https://memory.example.com --api-key emt_...
```

## Manual installation

For a from-checkout deployment, or when you want to build the image yourself:

```bash
git clone https://github.com/emergent-company/emergent.memory.git
cd emergent.memory/deploy/self-hosted

cp .env.example .env      # set SERVER_PORT, POSTGRES_PASSWORD, STANDALONE_API_KEY
docker compose -f docker-compose.local.yml up -d --build
curl -s localhost:3002/health
```

`docker-compose.local.yml` builds the server from this repository and reads
`.env` from this directory. It is the local-testing compose (its `server`
service also mounts the Firecracker sandbox runtime, so the host needs
`/dev/kvm`); the compose file the installer writes under `~/.memory/docker/`
references the published image and has no sandbox mounts. Use the installer if
you do not specifically want the sandbox.

## Verify

```bash
./verify-install.sh
# or, for a non-default layout:
INSTALL_DIR=/opt/memory SERVER_PORT=8080 ./verify-install.sh
```

It checks the compose file, the running `server` service, `/health`, and CLI
authentication inside the container. The script lives in this directory of the
repository — the installer does not copy it into `~/.memory/bin`.

## Upgrade

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

The installer detects the existing installation, moves the compose file to the
new image, and restarts the stack. To pin a release:
`CLI_VERSION=v0.88.2`.

Database migrations run automatically on server start.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/uninstall.sh | bash
```

The script stops the stack, then asks twice: whether to delete the Docker
volumes (all documents and database contents) and whether to delete the server
image. Either way it removes `~/.memory` at the end. For an unattended run set
`REMOVE_DATA=y` / `REMOVE_IMAGE=y` in the environment.

## Troubleshooting

**Server container exits immediately**

```bash
docker compose -f ~/.memory/docker/docker-compose.yml logs server --tail 100
```

The usual causes are a port already in use (`SERVER_PORT`), insufficient memory,
or a `~/.memory/config/.env.local` with a missing `STANDALONE_API_KEY`.

**`/health` never becomes ready**

Database initialisation is the slow step on a cold start; `docker compose ps`
shows `memory-db` health. Give it 60–90 s, then check the server logs as above.

**`memory` command not found**

```bash
export PATH="$HOME/.memory/bin:$PATH"     # then restart your shell
```

**Re-running the installer after editing ports or keys**

Edit `~/.memory/config/.env.local` and restart:

```bash
docker compose -f ~/.memory/docker/docker-compose.yml --env-file ~/.memory/config/.env.local up -d
```

## Related

- [README.md](./README.md) — deployment overview
- [UPGRADING_OBJECT_STORE.md](./UPGRADING_OBJECT_STORE.md) — object-store migration
- [../../docs/site/](../../docs/site/) — API, SDK and MCP documentation
