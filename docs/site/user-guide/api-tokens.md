# API Tokens

API tokens let scripts, CI/CD pipelines, and third-party tools authenticate to Memory without an interactive login. Tokens are scoped to a project and carry only the permissions you grant.

## Token format

All tokens use the prefix `emt_` (e.g. `emt_abc123...`). Tokens are:

- **Scoped** to a specific project
- **Revocable** at any time
- **Auditable** — `lastUsedAt` tracks recent activity
- Stored as a **bcrypt hash** — the plaintext value is only shown at creation

---

## Creating a Token

=== "CLI"
    ```bash
    memory tokens create \
      --name "CI/CD Pipeline" \
      --scopes "documents:read,search,agents:write" \
      --project my-project
    ```

    The CLI also accepts presets: `--scopes read` (the read-only set),
    `--scopes write` (the write set, which also implies the matching reads) or
    `--scopes read,write` for both.

=== "API"
    ```http
    POST /api/projects/{projectId}/tokens
    Content-Type: application/json

    {
      "name": "CI/CD Pipeline",
      "scopes": ["documents:read", "search", "agents:write"]
    }
    ```

    Response (token value shown **once only**):
    ```json
    {
      "id": "tok_abc123",
      "name": "CI/CD Pipeline",
      "token": "emt_abc123...",
      "scopes": ["documents:read", "search", "agents:write"],
      "tokenPrefix": "emt_abc",
      "createdAt": "2026-03-08T10:00:00Z"
    }
    ```

!!! warning "Save the token value"
    The full token is returned **only at creation**. It cannot be retrieved again. Store it in a secrets manager (GitHub Secrets, Vault, etc.) immediately.

---

## Scopes Reference

| Scope | Access |
|---|---|
| `documents:read` | Read documents |
| `documents:write` | Create and update documents |
| `documents:delete` | Delete documents |
| `ingest:write` | Trigger document ingestion/parsing |
| `chunks:read` | Read document chunks |
| `chunks:write` | Create and update chunks |
| `graph:read` | Read graph objects |
| `graph:write` | Create and update graph objects |
| `graph:search:read` | Graph vector search |
| `search` | Run hybrid and semantic search (MCP) |
| `search:read` | Unified REST search |
| `journal:read` | Read the project journal |
| `extraction:read` | View extraction jobs |
| `extraction:write` | Manage extraction jobs |
| `tasks:read` | Read tasks |
| `tasks:write` | Resolve and cancel tasks |
| `notifications:read` | Read notifications |
| `notifications:write` | Mark notifications as read |
| `user-activity:read` | View the activity log |
| `user-activity:write` | Write activity entries |
| `schema:read` | Read type definitions and template packs |
| `schema:write` | Install and modify schemas |
| `agents:read` | Read agents, runs, definitions, questions |
| `agents:write` | Trigger agents, respond to questions, manage hooks |
| `projects:read` | Read project metadata and members |
| `projects:write` | Update project settings and members |

Assign only the scopes your integration needs. The full list, including
branch, skill and admin scopes, is in
[Security and Scopes](../developer-guide/security-scopes.md).

The CLI also accepts two presets: `--scopes read` (the read-only set above) and
`--scopes write` (documents, graph, schema, agents and project writes, plus the
REST write scopes the retired `data:write` umbrella used to imply). Combine them
with `--scopes read,write`.

!!! note "`data:read` and `data:write` are retired"
    The coarse `data:read` / `data:write` umbrellas can no longer be requested
    when creating or editing a token — the CLI, web UI, API and connectors all
    reject them. Use the fine-grained scopes above instead. Tokens minted before
    the change keep exactly the access they had; a token that still holds one
    cannot be re-scoped in the web UI, so create a replacement and revoke it (or
    update its scopes through the API, which accepts a set that drops the
    umbrella).

    Every fine-grained scope the umbrellas implied is mintable, so a replacement
    token can request the exact REST scopes it needs. See
    [Security and Scopes](../developer-guide/security-scopes.md#umbrella-scopes).

---

## Using a Token

=== "CLI"
    ```bash
    memory --project-token emt_abc123... agents list
    # or via environment variable:
    export MEMORY_PROJECT_API_KEY=emt_abc123...
    memory agents list
    ```

=== "HTTP header"
    ```http
    Authorization: Bearer emt_abc123...
    ```

=== "Go SDK"
    ```go
    client, err := sdk.New(sdk.Config{
        ServerURL: "https://api.example.com",
        Auth: sdk.AuthConfig{
            Mode:   "apitoken",
            APIKey: "emt_abc123...",
        },
    })
    ```

---

## Listing Tokens

```bash
memory tokens list --project my-project
```

```http
GET /api/projects/{projectId}/tokens
```

Response shows token metadata but **never** the plaintext value. Use `tokenPrefix` to identify which token is which.

---

## Revoking a Token

```bash
memory tokens revoke <token-id> --project my-project
```

```http
DELETE /api/projects/{projectId}/tokens/{tokenId}
```

The token is invalidated immediately. Any requests using it will receive `401 Unauthorized`.

---

## CI/CD Example

```yaml
# GitHub Actions
- name: Trigger nightly agent
  run: |
    memory agents trigger $AGENT_ID --project $PROJECT_ID
  env:
    MEMORY_SERVER_URL: https://api.example.com
    MEMORY_PROJECT_API_KEY: ${{ secrets.MEMORY_TOKEN }}
```
