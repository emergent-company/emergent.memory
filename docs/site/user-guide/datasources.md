# Data Sources

Data sources let you connect external systems to Emergent Memory and automatically import their content as documents. The platform periodically syncs new and updated content, keeping your knowledge graph current.

## Supported providers

| Provider type | Source type | Description |
|---|---|---|
| `imap` | `email` | Email inbox via IMAP |
| `gmail_oauth` | `email` | Gmail via OAuth |
| `google_drive` | `drive` | Google Drive files and folders |
| `clickup` | `clickup-document` | ClickUp documents and spaces |

!!! note "Google Drive is OAuth-only"
    Google Drive can only be connected via OAuth. There is no paste-a-refresh-token option. Data
    sources that were connected before this change with a stored refresh token keep syncing and do
    not need to be reconnected.

---

## Configuring a Google OAuth app

Google Drive connects require an OAuth 2.0 client (an "app") that Memory uses to obtain Drive
access. The app can be configured at three levels, resolved in this order when a connection starts:

1. **Project** — a project-level client, managed by a `project_admin` on the project's
   **Data source OAuth clients** page. It overrides everything else for that project.
2. **Organization** — an org-level client, managed by an `org_admin` under
   **Organization Settings → Data source OAuth clients**.
3. **Deployment default** — a centrally configured app set via `SOURCES_OAUTH_GOOGLE_CLIENT_ID` /
   `SOURCES_OAUTH_GOOGLE_CLIENT_SECRET` (no setup needed by users).

If none is configured, connecting fails with an actionable message telling you where to add one.

The project page is reachable three ways: from the **Data source OAuth clients** entry in
**Project Settings**, from the **OAuth clients** shortcut on the **Data Sources** page, or from the
"no OAuth client configured" prompt shown when a connect fails.

### Create the app in Google Cloud Console

1. Go to [Google Cloud Console](https://console.cloud.google.com/) and create or select a project.
2. Enable the **Google Drive API**.
3. Configure the **OAuth consent screen** (External or Internal; for Workspace-internal apps use
   Internal).
4. Under **Credentials → Create credentials → OAuth client ID**, choose **Web application**.
5. Under **Authorized redirect URIs**, add the **Redirect URI** shown on the OAuth client settings
   page (it points at the Memory API callback, e.g.
   `https://<api-host>/api/data-source-integrations/oauth/google_drive/callback`).
6. Under **Authorized JavaScript origins**, add the **JavaScript origin** shown on the same page
   (the Memory UI origin).
7. Copy the **Client ID** and **Client secret**.

### Enter the client

=== "Project-level"
    A `project_admin` opens the project's **Data source OAuth clients** page (from
    **Project Settings → Data source OAuth clients**, the **OAuth clients** shortcut on the Data
    Sources page, or the failed-connect prompt), chooses Google Drive, and enters the Client ID,
    Client secret, scopes, and strategy from the console. This client is used only for that
    project.

=== "Organization-level"
    An `org_admin` opens **Organization Settings → Data source OAuth clients**, chooses Google
    Drive, and enters the Client ID, Client secret, scopes, and strategy. This client is used for
    every project in the org that has not configured its own.

The Client secret is stored encrypted and is never shown again after saving — leave the secret field
blank when updating other settings to keep the stored secret.

---

## Setting Up a Data Source

=== "Admin UI"
    Go to **Project Settings → Data Sources → Add Data Source** and select your provider.

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
| `providerType` | The provider: `imap`, `gmail_oauth`, `google_drive`, `clickup` |
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
