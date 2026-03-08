# API Tokens

API tokens let scripts, CI/CD pipelines, and third-party tools authenticate to Emergent Memory without an interactive login. Tokens are scoped to a project and carry only the permissions you grant.

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
      --scopes "data:read,agents:write" \
      --project my-project
    ```

=== "API"
    ```http
    POST /api/projects/{projectId}/tokens
    Content-Type: application/json

    {
      "name": "CI/CD Pipeline",
      "scopes": ["data:read", "agents:write"]
    }
    ```

    Response (token value shown **once only**):
    ```json
    {
      "id": "tok_abc123",
      "name": "CI/CD Pipeline",
      "token": "emt_abc123...",
      "scopes": ["data:read", "agents:write"],
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
| `schema:read` | Read type definitions and template packs |
| `data:read` | Read graph objects, documents, chunks, search |
| `data:write` | Create and update graph objects, documents |
| `agents:read` | Read agents, runs, definitions, questions |
| `agents:write` | Trigger agents, respond to questions, manage hooks |
| `projects:read` | Read project metadata and members |
| `projects:write` | Update project settings and members |

Assign only the scopes your integration needs.

---

## Using a Token

=== "CLI"
    ```bash
    memory --project-token emt_abc123... agents list
    # or via environment variable:
    export MEMORY_PROJECT_TOKEN=emt_abc123...
    memory agents list
    ```

=== "HTTP header"
    ```http
    Authorization: Bearer emt_abc123...
    ```

=== "Go SDK"
    ```go
    client, err := sdk.New(sdk.Config{
        ServerURL: "https://api.dev.emergent-company.ai",
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
    MEMORY_SERVER_URL: https://api.dev.emergent-company.ai
    MEMORY_PROJECT_TOKEN: ${{ secrets.MEMORY_TOKEN }}
```
