# Memory CLI User Guide

The `memory` CLI is the command-line client for the Memory platform. It creates
and selects projects, uploads documents, queries the knowledge graph, and
manages the resources in a project.

This page is the operator-oriented walkthrough: install, authenticate, choose a
project, and run the everyday commands. The exhaustive command listing lives in
the [CLI Reference](developer-guide/cli-reference.md); credential resolution
details live in [CLI Authentication](developer-guide/cli-auth.md).

## Install the CLI

### One-line installer (recommended)

The published install script detects the OS and architecture, downloads the
latest release binary, installs it to `~/.memory/bin/memory`, adds that
directory to your shell `PATH`, and installs shell completions:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | bash
```

Pin a version by setting `VERSION` for the shell that runs the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | VERSION=v0.88.8 bash
```

### Pre-built archive

Each release attaches CLI archives to
[GitHub Releases](https://github.com/emergent-company/emergent.memory/releases).
Asset names follow `memory-cli-<os>-<arch>.tar.gz` (or `.zip` on Windows), for
example:

```bash
curl -L -o memory-cli.tar.gz \
  https://github.com/emergent-company/emergent.memory/releases/latest/download/memory-cli-linux-amd64.tar.gz
tar xzf memory-cli.tar.gz
sudo mv memory-cli-linux-amd64 /usr/local/bin/memory
```

### Build from source

Requires Go 1.25+:

```bash
task cli:install          # installs to ~/.memory/bin/memory
```

Without `task`:

```bash
cd apps/cli
go build -o ~/.memory/bin/memory ./cmd/main.go
```

### Verify the install

```bash
memory version
```

This prints the CLI version, commit hash, and build date. `memory --version`
and `memory -v` are aliases.

## Authenticate

The CLI resolves credentials from three sources, highest priority first:

1. A project token (`emt_*`) from `MEMORY_PROJECT_API_KEY` /
   `MEMORY_PROJECT_TOKEN`, the `project_token` config key, or `--project-token`
2. An account API key from `MEMORY_ACCOUNT_API_KEY` / `MEMORY_API_KEY`
3. An OAuth session stored in `~/.memory/credentials.json` by `memory login`

### OAuth device flow: `memory login`

```bash
memory login
```

`memory login` starts the OAuth device authorization flow, prints a user code,
opens the browser, and saves the resulting credentials to
`~/.memory/credentials.json`. Tokens refresh automatically. This is the right
choice when one login should cover several projects.

If the server is running in standalone mode it has no OAuth issuer; use an API
key or project token instead.

### Static bearer token: `memory set-token`

```bash
memory set-token <your-token>
```

`memory set-token` saves a pre-issued Bearer token to
`~/.memory/credentials.json`, bypassing the device flow. It is intended for CI,
test harnesses, and environments that hand out tokens directly. The default
validity is 24 hours; override it with `--duration`:

```bash
memory set-token <your-token> --duration 168h
```

!!! note "Command path"
    `set-token` is a **top-level** command. The CLI does not have a
    `memory auth` command group, so `memory auth set-token` is not a valid
    invocation.

### Account API key (standalone / self-hosted)

```bash
export MEMORY_SERVER_URL=http://localhost:3002
export MEMORY_ACCOUNT_API_KEY=<your-api-key>
memory status
```

Or persist it in the config file:

```bash
memory config set-server https://memory.example.com
memory config set api_key <your-api-key>
```

Secret values are not echoed back by `memory config set`.

### Check the session: `memory status`

```bash
memory status
```

`memory status` prints the authentication mode, server URL, a masked credential,
connection health and server version, the active project (or a numbered list of
projects to choose from), and usage statistics for the current project
(documents, graph objects, relationships, type registry, template packs, and
processing job queue depths).

### Sign out

```bash
memory logout          # remove the OAuth credentials file
memory logout --all    # also clear api_key and project_token from config
```

## Choose a project

Most commands need a project scope. There are four ways to supply it, in
precedence order:

1. `--project <project-id>` on the command (also available as a global flag)
2. `MEMORY_PROJECT` — a project name or slug, resolved to an ID
3. `MEMORY_PROJECT_ID` / `project_id` in `~/.memory/config.yaml`
4. A project token in `.env.local` or the config, which fixes the project server-side

### Interactive setup: `memory init`

```bash
cd my-project
memory init
```

`memory init` is an interactive wizard that:

1. selects an existing project or creates a new one
2. configures an LLM provider at the organisation level
3. installs the built-in Memory skills into `.agents/skills/`

It writes `MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, and
`MEMORY_PROJECT_API_KEY` to `.env.local` (mode `0600`) and adds `.env.local` to
`.gitignore`. Running it again detects the existing configuration and offers to
verify or reconfigure each step.

| Flag | Description |
|---|---|
| `--skip-provider` | Skip the LLM provider configuration step |
| `--skip-skills` | Skip the Memory skills installation step |

`memory init` requires an interactive terminal; stdin must be a TTY.

### Pick a project directly

```bash
memory projects list                 # numbered list of accessible projects
memory projects list --stats         # include document/object/job counts
memory projects set "Product Research"
memory projects set --clear          # remove the active project from config
```

`memory projects set` updates `project_id` in `~/.memory/config.yaml` and writes
`MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, and `MEMORY_PROJECT_API_KEY` into
`.env.local` in the current directory. If no token exists for the project, it
creates one.

## Everyday commands

### Ask the assistant: `memory ask`

```bash
memory ask "how do I ingest a PDF and query the resulting graph?"
```

`memory ask` is context-aware: without authentication it answers documentation
questions, with an account it can run account-level tasks, and with an active
project it can execute tasks against that project.

| Flag | Description |
|---|---|
| `--project <id>` | Project ID (uses the default project if omitted) |
| `--show-tools` | Show the tool calls made while answering |
| `--show-time` | Print elapsed time |
| `--json` | Emit JSON (`{question, response, tools, elapsedMs}`) |
| `--runtime <python\|go>` | Sandbox runtime for scripting tasks (default `python`) |
| `--v2` | Use the v2 code-generation agent |

Quotes are optional; the words after `ask` are joined into the question.

### Query the graph: `memory query`

```bash
memory query "what are the main services and how do they relate?"
memory query --mode=search "auth service"
memory query --mode=search --branch <branch-id> "planned services"
```

By default `--mode=agent` uses the server-side graph-query agent, which reasons
over the knowledge graph with search and traversal tools. `--mode=search` runs a
direct hybrid search without AI reasoning.

| Flag | Description |
|---|---|
| `--mode <agent\|search>` | Query mode (default `agent`) |
| `--project <id>` | Project to query |
| `--branch <branch-id>` | Search a graph branch (`--mode=search` only) |
| `--limit <n>` | Maximum results (`--mode=search` only, default 10) |
| `--result-types <graph\|text\|both>` | Result channels (`--mode=search` only) |
| `--fusion-strategy <weighted\|rrf\|interleave\|graph_first\|text_first>` | Fusion strategy (`--mode=search` only) |
| `--show-tools` | Show agent tool calls (`--mode=agent` only) |
| `--show-scores` | Print relevance scores (`--mode=search` only) |
| `--session <id>` | Continue a previous query session |
| `--json` | Emit JSON |
| `--show-time` | Print elapsed query time |

To inspect individual objects instead of asking a question, use the graph
commands:

```bash
memory graph objects list --type Person --limit 20
memory graph objects list --key user-alice
memory graph objects list --filter status=active --filter-op eq
memory graph objects get <id-or-key>
memory graph objects get <id-or-key> --branch <branch-id-or-name>
memory graph relationships list --type KNOWS --limit 20
```

Object creation:

```bash
memory graph objects create \
  --type Person \
  --name "Alice" \
  --properties '{"role":"engineer"}'
```

| Flag | Description |
|---|---|
| `--type <type>` | Object type (required) |
| `--name <name>` | Shorthand for a `name` property |
| `--description <text>` | Shorthand for a `description` property |
| `--properties <json>` | JSON properties object |
| `--key <key>` | Stable key for idempotent operations |
| `--status <status>` | Object status, e.g. `active` |
| `--branch <branch-id>` | Create on a branch instead of the main graph |
| `--upsert` | Update an existing object with the same `--key` |

### Ingest documents: `memory documents`

```bash
memory documents upload ./report.pdf --auto-extract
memory documents upload ./report.pdf
memory documents list --limit 20
memory documents list --status extracted
memory documents get <document-id>
memory documents extraction-summary <document-id>
memory documents delete <document-id>
```

| Command / flag | Description |
|---|---|
| `upload <file>` | Upload a local file as a document |
| `--auto-extract` | Trigger extraction after upload |
| `list` | List documents; supports `--limit`, `--cursor`, `--status`, `--sort`, `--order`, `--search`, `--source-type` |
| `get <id>` | Print a document by ID |
| `extraction-summary <id>` | Show the extraction summary for a document |
| `delete <id>` | Delete the document and its related entities |

`memory documents upload` prints the document ID, name, and status; if the file
is a duplicate it prints the existing document ID instead.

### Upgrade

```bash
memory upgrade           # download and replace the CLI binary
memory upgrade --force   # upgrade even from a dev build
```

`memory upgrade` only touches the CLI binary. A self-hosted server installation
is upgraded separately:

```bash
memory server upgrade
```

## Global flags and configuration

| Flag | Description |
|---|---|
| `--server <url>` | Memory server URL |
| `--project <id>` | Project ID (overrides config and environment) |
| `--project-token <token>` | Project token (overrides config and environment) |
| `--output <table\|json\|yaml\|csv>` | Output format (default `table`) |
| `--json` | Shorthand for `--output json` |
| `--config <path>` | Config file (default `~/.memory/config.yaml`) |
| `--debug` | Enable debug logging |
| `--no-color` | Disable coloured output |
| `--compact` | Use compact output layout |

The config file defaults to `~/.memory/config.yaml`. Environment variables
override the file, and flags override both:

```yaml
server_url: https://memory.example.com
api_key: ''
project_token: ''
org_id: ''
project_id: ''
```

| Environment variable | Meaning |
|---|---|
| `MEMORY_SERVER_URL` | Server URL |
| `MEMORY_ACCOUNT_API_KEY` | Account-level API key |
| `MEMORY_PROJECT_API_KEY` | Project token (`emt_*`) |
| `MEMORY_PROJECT` | Project name or slug to activate |
| `MEMORY_PROJECT_ID` | Default project ID |
| `MEMORY_ORG_ID` | Default organisation ID |
| `MEMORY_CONFIG` | Override the config file path |

!!! tip "Never commit credentials"
    Prefer environment variables or `.env.local` (which `memory init` writes
    with mode `0600`) for tokens. Do not paste real tokens into scripts,
    screenshots, or documentation — use `<your-token>` placeholders.

## Where to go next

- [CLI Reference](developer-guide/cli-reference.md) — `memory ask`, `memory adk-sessions`, `memory mcp-guide`, and `memory install-memory-skills`
- [CLI Authentication](developer-guide/cli-auth.md) — credential precedence, standalone servers, and CI examples
- [User Guide](user-guide/index.md) — knowledge graph, documents, agents, and chat in the platform
- [Python SDK](python-sdk.md) — programmatic access to the same API
- [Go SDK](go-sdk/index.md) — the fully typed Go client
- [Self-hosted install](https://github.com/emergent-company/emergent.memory/tree/main/deploy/self-hosted) — server installation and operations
