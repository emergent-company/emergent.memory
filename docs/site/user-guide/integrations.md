# Integrations

Integrations connect Memory to third-party services at the project or organization level. This page covers the **GitHub App** integration and general **third-party integration** management.

!!! info "Data source integrations"
    For importing content from Gmail, Google Drive, GitHub, and similar sources, see [Data Sources](datasources.md).

---

## GitHub App source (review integration)

Memory connects a GitHub App as a project source. The App receives
`pull_request` webhooks, a source trigger starts a run for the configured agent,
and the agent posts a review bound to the reviewed head SHA.

You can register the App from **Settings → Source apps → Register app** in the
web UI (the page carries a copy of the checklist below), or through the API.

### Create the App in GitHub

Create the App at [github.com/settings/apps/new](https://github.com/settings/apps/new)
**before** registering it in Memory. A GitHub App is not an OAuth app: Memory
authenticates as the App installation, so no user-authorization flow is
configured.

| GitHub field | Value |
|---|---|
| Homepage URL | Your Memory deployment's web address. Required by GitHub, unused by Memory. |
| **Callback URL** (under *Identifying and authorizing users*; labelled *Redirect URI* on some GitHub screens) | **Leave empty.** Do not add a callback. |
| *Request user authorization (OAuth) during installation* | **Unchecked.** |
| Setup URL | Leave empty. |
| Webhook → Active | **Leave inactive while creating the App** (see below). |
| Webhook → Secret | Generate the secret now and keep it; GitHub only offers the secret field once the webhook is active, and you enter the same value there in step 3. |
| Repository permissions | `Contents: read`; `Issues`, `Checks`, `Pull requests`: **read and write**. `Metadata: read` is added automatically and is mandatory. |
| Subscribe to events | `Pull request` only — selected in step 3, once the webhook is active. |
| Private key | Generate one on the App page and download the `.pem` file. |

The webhook is a **two-pass** setup, because the delivery URL does not exist
until Memory registers the App and issues its `webhookKey`:

1. Create the App with the webhook inactive. While it is inactive GitHub does
   not offer the webhook URL, secret or event fields at all — so pick the secret
   here (any random string, e.g. `openssl rand -hex 24`) and keep it.
2. Register the App in Memory (below). The response — and the App's page in the
   UI — carries the delivery URL.
3. Edit the App in GitHub: activate the webhook, paste the delivery URL, enter
   the **same** secret, and subscribe to the **Pull request event** (the event
   list appears with the active webhook). GitHub sends a `ping` delivery; a `202`
   proves Memory verified the signature.

### Register the App (org-wide or project-owned)

An App credential is either **org-wide** (managed by an organization
administrator, bindable by every project) or **project-owned** (managed by a
project administrator, bindable only by that project). App ids stay unique per
organization.

Org-wide registration:

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

In the UI, **Register app** posts the same fields and then opens the App's own
page, where the delivery URL is shown with a copy button — that URL is what you
paste back into the GitHub App's webhook.

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

Project surface (project membership may list; project admin creates project-owned;
org admin may choose `scope: "org"`):

```http
GET    /api/data-source-integrations/source-apps
POST   /api/data-source-integrations/source-apps
PATCH  /api/data-source-integrations/source-apps/{appId}
DELETE /api/data-source-integrations/source-apps/{appId}
```

The project `POST` body takes the same fields plus `scope` (`"project"` default,
`"org"` requires org admin). The list returns org-wide Apps plus Apps owned by
the current project. A project-owned App cannot be managed by another project;
an org-wide App cannot be deleted or edited by a project administrator.

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

`config.app_id` is the Memory **App row id** (a UUID) returned by the
registration, not GitHub's numeric App id. `config.installation_id` is the
installation on the repositories you granted: GitHub shows it in the URL of the
installation settings page —
`https://github.com/settings/installations/<installation_id>`.

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
