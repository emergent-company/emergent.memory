# CLI Authentication

The `memory` CLI supports three credential sources. It picks the first one that is
set, in this order:

| Priority | Mode | Environment | Config key | Scope |
|---|---|---|---|---|
| 1 | **Project token** (`emt_…`) | `MEMORY_PROJECT_API_KEY`, `MEMORY_PROJECT_TOKEN` | `project_token` | one project |
| 2 | **Account API key** / standalone key | `MEMORY_ACCOUNT_API_KEY`, `MEMORY_API_KEY` | `api_key` | account / org |
| 3 | **OAuth session** | `memory login` → `~/.memory/credentials.json` | — | account, with project selection |

`memory status` prints which mode is active, the server URL, a masked credential,
and connection health.

## Config file

Defaults to `~/.memory/config.yaml`; override the path with `MEMORY_CONFIG`.

```yaml
server_url: https://memory.example.com
api_key: ''          # account-level key
project_token: ''    # emt_… project token
project_id: ''
org_id: ''
```

Environment variables win over the file. `MEMORY_PROJECT` selects a project by
name or slug; `MEMORY_SERVER_URL` overrides `server_url`.

## OAuth (interactive, full server)

```bash
memory login      # device flow; stores credentials in ~/.memory/credentials.json
memory status     # shows the active project, or a numbered list to pick from
```

Tokens refresh automatically. Use this when you want one login to cover several
projects without pasting keys.

## Project token vs account key

Project tokens are scoped to a single project and are the right credential for
CI that operates on one project. Some commands are account- or org-level (listing
projects, managing account tokens); with only a project token configured they
fail with:

```
account-level credentials required for this command.
```

Fix: run `memory login`, or set `MEMORY_ACCOUNT_API_KEY` to an account-level key.

## Standalone servers (API key only)

A self-hosted server started with `STANDALONE_API_KEY` accepts that key as an
account credential — no Zitadel, no OAuth:

```bash
export MEMORY_SERVER_URL=http://localhost:3002   # self-hosted default (SERVER_PORT)
export MEMORY_ACCOUNT_API_KEY=<STANDALONE_API_KEY from the server .env>

memory status
```

## CI example

```yaml
- name: Query the graph
  env:
    MEMORY_SERVER_URL: ${{ vars.MEMORY_SERVER_URL }}
    MEMORY_PROJECT_TOKEN: ${{ secrets.MEMORY_PROJECT_TOKEN }}
  run: memory query "Which documents were added this week?"
```

## Troubleshooting

| Symptom | Cause |
|---|---|
| `401` / `not authenticated` | No credential resolved — set a token or run `memory login` |
| `403 Insufficient permissions` on an account command | A project token is configured; use an account key or OAuth |
| Config file seems ignored | An environment variable is set and takes priority |
| Wrong project | `MEMORY_PROJECT`, `project_id` in the config file, or `memory status` to switch |
