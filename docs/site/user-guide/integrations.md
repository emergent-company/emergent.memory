# Integrations

Integrations connect Emergent Memory to third-party services at the project or organization level. This page covers the **GitHub App** integration and general **third-party integration** management.

!!! info "Data source integrations"
    For importing content from Gmail, Google Drive, ClickUp, and similar sources, see [Data Sources](datasources.md).

---

## GitHub App

The GitHub App integration allows Emergent Memory to connect to your GitHub repositories — enabling repo-aware agents, automated knowledge extraction from code and issues, and OAuth-based repository access.

### Connect GitHub

=== "Admin UI"
    Go to **Project Settings → Integrations → GitHub** and click **Connect**.

=== "API"
    ```http
    POST /api/v1/settings/github/connect
    ```

    This returns an authorization URL. Visit it to grant access via GitHub OAuth.

### OAuth callback

GitHub redirects back to:

```
GET /api/v1/settings/github/callback?code=...
```

The platform exchanges the code for an access token and stores it.

### CLI setup (API key mode)

For non-browser environments:

```http
POST /api/v1/settings/github/cli
{ "token": "<github-personal-access-token>" }
```

### Check connection status

```http
GET /api/v1/settings/github
```

### Disconnect

```http
DELETE /api/v1/settings/github
```

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
