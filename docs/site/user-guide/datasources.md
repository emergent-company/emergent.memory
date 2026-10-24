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
| `trale` | `meeting` | Trale meeting transcripts and summaries |
| `web` | `web` | Web pages crawled from one or more start URLs |

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
8. Enable the **Google Picker API** for the same project.
9. Create an **API key** (Credentials → Create credentials → API key) and restrict it to the
   Memory UI origin plus `https://docs.google.com/*`. Copy the key and the **project number** —
   they become the picker's **Developer key** and **App ID** in the credentials editor.

The picker keys are stored on the OAuth client that authorizes the source, so set them on the same
**project** or **organization** client that holds the client id. A deployment-default client is
environment configuration and has no editable config, so a source that resolves to it cannot load
the picker until a project or organization client is configured.

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

### Drive scope strategy

Google Drive has two selectable strategies, and they decide what the folder picker can see:

- **`drive.readonly`** (default) — the app can list and read every file the connecting Google
  account can see, so the folder picker shows the whole drive. This is a restricted scope: a
  widely distributed app may need Google verification.
- **`drive.file`** — the app sees only files it created or that a user picked through the Google
  Picker. That grant covers the picked **object**, not its children, so a `drive.file` source
  cannot browse or sync a folder tree. Folder sync needs `drive.readonly`; the source's detail page
  says so and links to reconnect.

The strategy resolves project first, then organization, then the provider default. Leaving the
project strategy blank inherits the organization's strategy; it does not reset to the default.

Changing the strategy applies to the next consent, not to sources that are already connected — a
source keeps the scope its token was granted with. Delete the source and connect the provider again
after switching to `drive.readonly`.

### Choosing folders in the Google Picker

A Drive source syncs the folders selected in the Google Picker. Open the source's detail page and
press **Choose folders** (or follow the prompt on a freshly connected source):

- The modal shows the folders the connected Google account can read. Open a folder to browse its
  subfolders; use the sidebar to switch between **My Drive** and **Shared drives**.
- If Google asks you to sign in, use the account that connected this source. The Picker runs in
  your browser and needs a signed-in Google account — the token the server mints for the source is
  not enough on its own, and folders visible only to a different signed-in account are rejected
  when you save. Either pick again while signed in as the source's account, or share those folders
  with it; a folder shared with the source's account saves normally.
- Select more than one folder. Selections accumulate across Picker openings, so you can add a
  folder, close the picker, browse elsewhere, and add another.
- Remove a folder with the **×** on its chip, then press **Save folders**.

Each picked folder is stored as `{folder_id, name, drive_id}`. `name` and `drive_id` are resolved
from Drive when you save, so a renamed or moved folder stays identifiable. One source accepts at
most **50** folders; `mime_types` and `recursive` apply to all of them. A file that appears in two
selected folders is ingested once per sync.

Subfolder inclusion still follows the **Recursive** setting: recursive sync walks below each
selected folder, non-recursive sync ingests the folder's direct children only.

The source detail page also shows the credentials in use: the OAuth app scope (project,
organization, or deployment default), the short client id, the connected Google account, and the
granted scope. **Manage credentials** opens **Settings → Credentials**.

Free-text folder ids from older clients are still accepted: the first update rewrites
`folder_id`/`shared_drive_id` into the canonical `folders` list.

GitHub accepts either an OAuth app (the same project/organization resolution) or a personal access
token entered when connecting the source; a token connection does not need an OAuth app.

### Connect GitHub with a personal access token

Select **GitHub** on **Data Sources → New data source**, choose **Connect with credentials**, and
paste a token. The token is used to load the suggestions below and is then stored encrypted and
write-only — it is never shown again.

| Field | How to fill it |
|---|---|
| **Repository** | Press **Load repositories** to pick from the repositories the token can see (private ones included), or type `owner/name` directly. |
| **Ref** | Once a repository is set, press **Load branches and tags** to pick a branch or tag, or type any ref. An empty ref syncs the repository's default branch. Commit SHAs are valid refs but are not listed. |
| **Paths** / **Extensions** | Optional filters limiting which files sync. |

Press **Test connection** to validate the token and repository before saving.

The suggestion lists are best-effort. If a list cannot be loaded — an expired token, a rate limit, a
fine-grained token without repository-list permission — the field shows an inline message and keeps
the value you typed, so you can still connect. Picking from a list is never required: typing
`owner/name` and a ref always works, including for GitHub Enterprise Server (whose API endpoint is
configured for the whole deployment, not per source).

### GitHub token scopes

- **Classic token**: the `repo` scope to read private repositories and their contents
  (`public_repo` is enough for public repositories).
- **Fine-grained token**: **Repository metadata: Read** to list the repositories the token can see,
  and **Contents: Read** on the target repository to list branches and tags and sync its files.

Reading branches and tags needs no permission beyond contents access.

### Trale (meeting intelligence)

Trale imports meeting transcripts and their AI-generated summaries. It connects over OAuth, but unlike
Google Drive and GitHub it registers its own OAuth client at connect time using dynamic client
registration (RFC 7591). There is no app to create in a vendor console and no client id/secret to enter,
so the credentials pages simply do not list Trale — connect it directly from **Data Sources**.

| Field | Meaning |
|---|---|
| **Tags** | *Required.* The Trale tag slugs to import, for example `standup`. Pick from the suggestions the provider returns, or type them. |
| **Name pattern** | Optional regex matched against a meeting's title; only matching meetings are imported. |
| **Include transcript** | When enabled, the full transcript is imported as part of the document. Off (the default) imports the summary and structured summary only. |
| **Backfill days** | How far back to look for past meetings (default 90). |

Only completed meetings are imported. The document's identity is derived from the meeting's status,
participant count, tags, and summary, so a meeting that is later re-summarized updates in place rather
than duplicating.

Operational caveats:

- **No modification timestamp.** Trale does not expose one, so change detection rests on the composite
  identity above: a meeting whose status, participant count, tags and summary are unchanged is not fetched
  again. That keeps repeat syncs cheap, but an edit that only touches the transcript is not picked up
  until another part of that identity changes.
- **Plan or access lapses surface as errors.** If the Trale plan lapses or the connected account loses
  access, the sync fails with a connection error rather than reporting an empty success.
- **Per-user visibility.** Trale returns the meetings the authorizing account can see, so a data
  source imports exactly that account's meeting scope.
- **25 meetings per call.** Enumeration pages through meetings 25 at a time; a large backfill takes
  several calls and nothing is silently truncated.
- **Token rotation is persisted.** The authorization server rotates the refresh token on every use, and
  the source stores the rotated token, so routine rotation never interrupts syncing. A refresh fails
  only when the grant itself is revoked or has expired; the source then stops with an actionable error
  and must be reconnected from **Data Sources**.

### Web (website crawl)

The `web` provider crawls a website starting from one or more URLs and imports each page as a Markdown
document. It connects with **config only** — no credentials are collected or stored.

| Field | Meaning |
|---|---|
| **Start URLs** (`start_urls`) | *Required.* One or more absolute `http(s)` URLs to start from (up to 50). The crawl stays on the same origin as each start URL. |
| **Include globs** (`include_globs`) | Optional URL path globs to keep, for example `/docs/**`. Empty keeps every in-scope page. |
| **Exclude globs** (`exclude_globs`) | Optional URL path globs to skip, for example `/admin/**`. |
| **Max pages** (`max_pages`) | Maximum pages to crawl. Default and ceiling is 200. |
| **Max depth** (`max_depth`) | Maximum link depth to follow. Default 3; `0` ingests only the start URLs (no link following). |

- **Static HTML only.** Each page is fetched and converted to Markdown. The crawler does not execute
  JavaScript, so sites that render their content client-side will ingest poorly.
- **`robots.txt` is honored.** The crawler respects the site's `robots.txt` and identifies itself with an
  `EmergentMemoryBot/1.0` user agent.
- **Caps.** A single crawl is capped at 200 pages, 5 MiB per page, and 50 MiB in total.
- **Politeness.** The crawler waits at least 1 second between requests to the same host.
- **Sync cadence.** Sources sync every 60 minutes by default; the fastest allowed interval is 15 minutes.
- **Removals are proof-based.** A page returning `404`/`410` is re-checked once before it is marked
  `deleted_from_source` (soft-deleted) — a transient failure never deletes a page.

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
| `sourceType` | Category of content: `email`, `drive`, `repo`, `clickup-document` |
| `syncMode` | `manual` (trigger explicitly) or `recurring` (automatic on interval) |
| `syncIntervalMinutes` | Minutes between automatic syncs (for `recurring` mode) |
| `status` | `active` · `error` · `disabled` |

For `google_drive`, `config.folders` is the sync scope:

```json
{
  "config": {
    "folders": [
      { "folder_id": "1AbC…", "name": "Reports", "drive_id": "" },
      { "folder_id": "1XyZ…", "name": "Shared contracts", "drive_id": "0AFt…" }
    ],
    "mime_types": ["application/pdf"],
    "recursive": true
  }
}
```

When you write `folders` directly, `name` and `drive_id` are stored as you send them — send both so
the UI and sync can label the target. Only `PUT /sync-targets` resolves them from Drive for you (see
below). The legacy single-target keys `folder_id` and `shared_drive_id` are accepted and normalized
into `folders`.

---

## Choosing Sync Targets Through the API

The folder-picker modal saves through the same endpoint an API client can call. Submit only folder
ids; the server resolves each folder's name and shared-drive id from Drive and merges the result
into the source config while preserving its other keys:

```http
PUT /api/data-source-integrations/{id}/sync-targets
Content-Type: application/json

{ "folderIds": ["1AbC…", "1XyZ…"] }
```

An empty list, an inaccessible folder, or more than 50 ids is rejected. The response is the updated
integration. The picker itself mints a short-lived access token from the source's own stored refresh
token via `GET /api/data-source-integrations/{id}/picker-session` (UI-only; the token is never
logged or cached).

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
