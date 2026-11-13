# Integrations

Integrations connect Memory to third-party services at the project or organization level. This page covers the **GitHub App** integration and general **third-party integration** management.

!!! info "Data source integrations"
    For importing content from Gmail, Google Drive, GitHub, and similar sources, see [Data Sources](datasources.md).

---

## GitHub App source (review integration)

Memory connects a GitHub App as a project source. The App receives
`pull_request` webhooks, a source trigger starts a run for the configured agent,
and the agent posts a review bound to the reviewed head SHA.

### Register the App (organization admin)

```http
POST /api/orgs/{orgId}/source-apps
Content-Type: application/json

{
  "provider": "github",
  "appId": 123456,
  "slug": "memory-reviewer",
  "privateKey": "-----BEGIN RSA PRIVATE KEY-----\n...",
  "webhookSecret": "<the App webhook secret>"
}
```

The private key and webhook secret are sealed at rest with
`LLM_ENCRYPTION_KEY` and never returned. The `201` response carries the App row
id and its `webhookKey`. The App must be granted `contents: read`,
`pull_requests: write`, `issues: write`, and `checks: write`.

```http
GET    /api/orgs/{orgId}/source-apps
PATCH  /api/orgs/{orgId}/source-apps/{appId}
DELETE /api/orgs/{orgId}/source-apps/{appId}
```

The PATCH body takes `slug` (required, trimmed) and the optional `privateKey`
and `webhookSecret`. A blank or omitted secret keeps the stored value; a
supplied private key is validated before it is sealed. `provider` and `appId`
are immutable: `(organization, provider, App id)` is what binds project
sources, so an edit renames or rotates credentials but never repoints an App
under sources that are already attached.

### Create the source

```http
POST /api/data-source-integrations
Content-Type: application/json

{
  "name": "GitHub reviews",
  "providerType": "github_app",
  "sourceType": "app",
  "config": {
    "app_id": "<App row id from the registration response>",
    "installation_id": "987654",
    "repos": ["owner/repo"]
  }
}
```

`github_app` is trigger-only: it never syncs documents, and webhooks for
repositories outside the allowlist are ignored.

### Bind a trigger

```http
POST /api/data-source-integrations/{sourceId}/triggers
Content-Type: application/json

{
  "eventType": "pull_request",
  "agentId": "<runtime agent uuid>",
  "queue": "security-review"
}
```

Use `GET`, `PATCH`, and `DELETE` on
`/api/data-source-integrations/{sourceId}/triggers[/{triggerId}]` to manage the
binding. `agentId` is a runtime agent in the source's project.

### Webhook

Point the App's webhook URL at:

```
POST /api/webhooks/source-apps/{webhookKey}
```

using the `webhookKey` returned by the App registration/list response.
Subscribe to `pull_request`; the endpoint verifies `X-Hub-Signature-256` and
deduplicates `X-GitHub-Delivery`.

!!! note "Legacy routes removed"
    The old `/api/v1/settings/github/*` OAuth routes no longer exist. The
    earlier `/api/orgs/{orgId}/github-app` surface and
    `POST /api/webhooks/github/app` were replaced by source apps above (#574).

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
