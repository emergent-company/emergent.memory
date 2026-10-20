# Data Sources

Data sources let you connect external systems to Emergent Memory and automatically import their content as documents. The platform periodically syncs new and updated content, keeping your knowledge graph current.

## Supported providers

| Provider type | Source type | Description |
|---|---|---|
| `imap` | `email` | Email inbox via IMAP |
| `gmail_oauth` | `email` | Gmail via OAuth |
| `google_drive` | `drive` | Google Drive files and folders |
| `github` | `repo` | GitHub repositories (OAuth or a personal access token) |
| `clickup` | `clickup-document` | ClickUp documents and spaces |

!!! note "Google Drive is OAuth-only"
    Google Drive can only be connected via OAuth. There is no paste-a-refresh-token option. Data
    sources that were connected before this change with a stored refresh token keep syncing and do
    not need to be reconnected.

---

## Credentials (OAuth apps)

OAuth-based providers (Google Drive, GitHub) need an OAuth 2.0 **app** — a client that Memory uses
to obtain access. Credentials are keyed by **vendor**, not by data source: one **Google OAuth app**
can back Google Drive, and one **GitHub OAuth app** backs GitHub. The **Data source** column tells
you which connection type the app backs.

The app can be configured at three levels, resolved in this order when a connection starts:

1. **Project** — a project-level app, managed by a `project_admin` on the project's
   **Settings → Credentials** page. It overrides everything else for that project.
2. **Organization** — an org-level app, managed by an `org_admin` under
   **Organization Settings → Data source OAuth clients**.
3. **Deployment default** — a centrally configured app set via `SOURCES_OAUTH_GOOGLE_CLIENT_ID` /
   `SOURCES_OAUTH_GOOGLE_CLIENT_SECRET` (no setup needed by users).

If none is configured, connecting fails with an actionable message linking to the credentials page.

### The Credentials table

**Project Settings → Credentials** lists every OAuth-capable vendor as a table. Each provider has one
row per scope, project first, then organization:

| Column | Meaning |
|---|---|
| App | The vendor-level app label, e.g. **Google OAuth app** |
| Data source | The connection type the app backs, e.g. **Google Drive** |
| Scope | **Project** or **Organization** |
| Client ID | The non-secret client id (secrets are never displayed) |
| Status | **In use** for the scope that wins resolution, **Configured** when it exists but is overridden, otherwise **Not configured** |
| Updated | When the app was last changed |
| Actions | **Manage** / **Set up** for admins; a role note for everyone else |

Every project member can view the table and see which app is in use. Only a `project_admin` can open
the per-provider editor and change the project app; organization-wide apps are managed by org admins.
Opening a per-provider editor as a non-admin returns 404.

When no project or organization app exists and the deployment does not provide one, the page carries
a note naming the providers that fall back to the deployment default.

The **Data Sources** page is a plain connections table (name, provider, status, sync mode, last sync)
with a **New data source** action. It never asks you to configure credentials.

### Create the app in the vendor console

For Google Drive:

1. Go to [Google Cloud Console](https://console.cloud.google.com/) and create or select a project.
2. Enable the **Google Drive API**.
3. Configure the **OAuth consent screen** (External or Internal; for Workspace-internal apps use
   Internal).
4. Under **Credentials → Create credentials → OAuth client ID**, choose **Web application**.
5. Under **Authorized redirect URIs**, add the **Redirect URI** shown on the credentials editor
   (it points at the Memory API callback, e.g.
   `https://<api-host>/api/data-source-integrations/oauth/google_drive/callback`).
6. Under **Authorized JavaScript origins**, add the **JavaScript origin** shown on the same page
   (the Memory UI origin).
7. Copy the **Client ID** and **Client secret**.

### Enter the app

=== "Project-level"
    A `project_admin` opens **Project Settings → Credentials**, finds the **Google OAuth app** row,
    and chooses **Manage** (or **Set up** when it is not configured). The editor shows the Redirect
    URI and JavaScript origin to register in the vendor console, then takes the Client ID, Client
    secret, scopes, and strategy. This app is used only for that project.

=== "Organization-level"
    An `org_admin` opens **Organization Settings → Data source OAuth clients**, chooses Google Drive,
    and enters the Client ID, Client secret, scopes, and strategy. This app is used for every project
    in the org that has not configured its own.

The Client secret is stored encrypted and is never shown again after saving — leave the secret field
blank when updating other settings to keep the stored secret.

GitHub accepts either an OAuth app (the same project/organization resolution) or a personal access
token entered when connecting the source; a token connection does not need an OAuth app.

---

## Setting Up a Data Source

=== "Admin UI"
    Go to **Data Sources → New data source** and select your provider.

=== "API"
    ```http
    POST /api/data-source-integrations
    Content-Type: application/json

    {
      "name": "Team Gmail",
      "providerType": "gmail_oauth",
      "sourceType": "email",
      "syncMode": "recurring",
      "syncIntervalMinutes": 60
    }
    ```

After creation you will be redirected (or given a URL) to complete the OAuth flow for providers that require it.

### Test the connection before saving

```http
POST /api/data-source-integrations/test-config
{
  "providerType": "imap",
  "config": {
    "host": "imap.example.com",
    "port": 993,
    "username": "user@example.com",
    "password": "..."
  }
}
```

---

## Data source fields

| Field | Description |
|---|---|
| `name` | Display name for this integration |
| `providerType` | The provider: `imap`, `gmail_oauth`, `google_drive`, `github`, `clickup` |
| `sourceType` | Category of content: `email`, `drive`, `clickup-document` |
| `syncMode` | `manual` (trigger explicitly) or `recurring` (automatic on interval) |
| `syncIntervalMinutes` | Minutes between automatic syncs (for `recurring` mode) |
| `status` | `active` · `error` · `disabled` |

---

## Triggering a Sync

Manually trigger a sync at any time:

```http
POST /api/data-source-integrations/{id}/sync
```

---

## Monitoring Sync Jobs

```http
GET /api/data-source-integrations/{id}/sync-jobs         # all jobs
GET /api/data-source-integrations/{id}/sync-jobs/latest  # most recent
GET /api/data-source-integrations/{id}/sync-jobs/{jobId} # specific job
```

### Sync job fields

| Field | Description |
|---|---|
| `status` | `pending` · `running` · `completed` · `failed` · `cancelled` |
| `totalItems` | Total items discovered |
| `processedItems` | Items processed so far |
| `successfulItems` | Items imported successfully |
| `failedItems` | Items that failed to import |
| `skippedItems` | Items skipped (already up to date) |
| `currentPhase` | Current processing phase label |
| `statusMessage` | Human-readable progress description |

---

## Cancelling a Sync

```http
POST /api/data-source-integrations/{id}/sync-jobs/{jobId}/cancel
```

---

## Schema Discovery

For structured data sources (e.g. databases, APIs), you can run a **discovery job** to infer object types from the source schema:

```http
POST /api/discovery-jobs/projects/{projectId}/start
{
  "integrationId": "<data-source-id>"
}
```

Monitor the discovery job:

```http
GET /api/discovery-jobs/{jobId}
```

Finalize (apply discovered types to the type registry):

```http
POST /api/discovery-jobs/{jobId}/finalize
```

---

## Listing Data Sources

```http
GET /api/data-source-integrations
GET /api/data-source-integrations/source-types        # available source types
GET /api/data-source-integrations/providers           # available provider configs
GET /api/data-source-integrations/providers/{type}/schema  # config schema for a provider
```

---

## Updating and Deleting

```http
PATCH  /api/data-source-integrations/{id}    # update name, syncMode, etc.
DELETE /api/data-source-integrations/{id}    # remove integration
```

!!! note
    Deleting a data source does **not** delete the documents already imported from it. Those remain in the project.
