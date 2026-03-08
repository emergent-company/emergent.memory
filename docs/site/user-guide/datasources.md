# Data Sources

Data sources let you connect external systems to Emergent Memory and automatically import their content as documents. The platform periodically syncs new and updated content, keeping your knowledge graph current.

## Supported providers

| Provider type | Source type | Description |
|---|---|---|
| `imap` | `email` | Email inbox via IMAP |
| `gmail_oauth` | `email` | Gmail via OAuth |
| `google_drive` | `drive` | Google Drive files and folders |
| `clickup` | `clickup-document` | ClickUp documents and spaces |

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
POST /discovery-jobs/projects/{projectId}/start
{
  "integrationId": "<data-source-id>"
}
```

Monitor the discovery job:

```http
GET /discovery-jobs/{jobId}
```

Finalize (apply discovered types to the type registry):

```http
POST /discovery-jobs/{jobId}/finalize
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
