# Integrations

Integrations connect Memory to third-party services at the project or organization level. This page covers the **GitHub App** integration and general **third-party integration** management.

!!! info "Data source integrations"
    For importing content from Gmail, Google Drive, GitHub, and similar sources, see [Data Sources](datasources.md).

---

## GitHub App (review integration)

Memory connects a GitHub App to review pull requests with an agent. The App
receives `pull_request` webhooks, Memory enqueues the configured review agent,
and the agent posts a review bound to the reviewed head SHA.

### Register the App (organization admin)

```http
POST /api/orgs/{orgId}/github-app
Content-Type: application/json

{
  "appId": 123456,
  "slug": "memory-reviewer",
  "privateKey": "-----BEGIN RSA PRIVATE KEY-----\n...",
  "webhookSecret": "<the App webhook secret>"
}
```

The private key and webhook secret are encrypted at rest with
`GITHUB_APP_ENCRYPTION_KEY` and never returned. The App must be granted
`contents: read`, `pull_requests: write`, `issues: write`, and `checks: write`.
Installation tokens are minted per review, scoped to one repository.

### Bind an installation

```http
POST /api/orgs/{orgId}/github-app/installations
Content-Type: application/json

{
  "projectId": "<uuid>",
  "installationId": 987654,
  "repos": ["owner/repo"],
  "agentId": "<runtime agent uuid>",
  "queue": "security-review"
}
```

`agentId` is a runtime agent in that project. Memory only reviews repositories
in the allowlist.

### Webhook

Point the App's webhook URL at:

```
POST /api/webhooks/github/app
```

Use the same webhook secret and subscribe to `pull_request`, `installation`, and
`installation_repositories`. The endpoint verifies `X-Hub-Signature-256` and
deduplicates `X-GitHub-Delivery`.

### Status and disconnect

```http
GET    /api/orgs/{orgId}/github-app
PATCH  /api/orgs/{orgId}/github-app/installations/{id}
DELETE /api/orgs/{orgId}/github-app/installations/{id}
DELETE /api/orgs/{orgId}/github-app/{appPK}
```

!!! note "Legacy routes removed"
    The old `/api/v1/settings/github/*` OAuth routes no longer exist.

---

## General Integrations

These are named integration configurations — typically used for custom LLM backends, vector stores, or webhooks.

### List available integrations

```http
GET /api/integrations/available
```

Returns integration types that can be configured in your project.

### List configured integrations

```http
GET /api/integrations
```

### Get an integration

```http
GET /api/integrations/{name}
```

### Create an integration

```http
POST /api/integrations
Content-Type: application/json

{
  "name": "my-integration",
  "displayName": "My Custom Backend",
  "enabled": true,
  "settings": { ... }
}
```

!!! note
    Integration settings are encrypted at rest (`AES-256-GCM`). They are never returned in API responses.

### Update an integration

```http
PUT /api/integrations/{name}
{ "settings": { ... }, "enabled": true }
```

### Test an integration

Verify credentials and connectivity before saving:

```http
POST /api/integrations/{name}/test
```

### Sync an integration

Trigger a manual sync or refresh:

```http
POST /api/integrations/{name}/sync
```

### Delete an integration

```http
DELETE /api/integrations/{name}
```

### Public integration info

Some integrations expose a public endpoint (no auth required) for capabilities discovery:

```http
GET /api/integrations/{name}/public
```
