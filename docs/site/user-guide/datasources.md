# Data Sources

Data sources let you connect external systems to Memory and automatically import their content as documents. The platform periodically syncs new and updated content, keeping your knowledge graph current.

## Supported providers

| Provider type | Source type | Description |
|---|---|---|
| `google_drive` | `drive` | Google Drive files and folders |
| `gmail` | `email` | Gmail threads |
| `github` | `repo` | GitHub repositories (OAuth or a personal access token) |
| `trale` | `meeting` | Trale meeting transcripts and summaries |
| `web` | `web` | Web pages crawled from one or more start URLs |

!!! note "Google Drive and Gmail are OAuth-only"
    Both Google providers can only be connected via OAuth. There is no paste-a-refresh-token option.
    Data sources that were connected before this change with a stored refresh token keep syncing and
    do not need to be reconnected.

---

## Saved credentials

The **Credentials** page has two sections, each with its own heading. **Saved credentials** lists the
project's reusable tokens; **OAuth apps** lists the vendor apps available to the project. The two are
different things: a saved credential is a token you paste, an OAuth app is a client id/secret created
in the vendor console — adding one never changes the other's status.

A saved credential is a token — today, a GitHub personal access token — stored once, encrypted, and
reused by any number of data sources in the project. The list shows the name, the provider, how many
sources use it, and when it was last changed; the token itself is never displayed again.

- **Add credential** opens a dialog: choose the provider, name the credential, and paste the token.
  Only the fields that provider requires are asked for.
- **Edit** reopens the same dialog with the name filled in. Renaming is safe, and leaving the token
  field empty keeps the stored token. Editing a credential changes it for every source that uses it,
  so a rotated token only needs to be updated once.
- **Reconnecting a source that uses a saved credential re-authorises the credential.** The new grant
  replaces the stored token rather than being attached to that one source, so every enabled source
  using the credential resumes — including any that had parked as **Needs reconnect**, since a parked
  source is skipped by the scheduler and would otherwise never notice. A source you paused yourself
  stays paused: reconnecting repairs the credential, it does not override that decision. A credential
  that carries a token but no OAuth grant — a GitHub personal access token, say — is repaired by
  **Reconnect** as well, whenever the provider's OAuth client is available: consent replaces the stored
  token with an OAuth grant for the account that consents, for every source on the credential. To keep
  using the token instead, replace it by editing the credential, then set `enabled: true` on any of its
  sources that parked, because editing a credential does not change a source's status.
- **Delete** is refused while a source still uses the credential; switch those sources to another
  credential first.

On a credentials connect form you can either select one of these saved credentials or create a new
one without leaving the page — the new credential is selected for you when you return.

## Credentials (OAuth apps)

OAuth-based providers (Google Drive, Gmail, GitHub) need an OAuth 2.0 **app** — a client that Memory
uses to obtain access. An app is keyed by **vendor**, not by data source: one **Google app** can back
Google Drive and Gmail, and a **GitHub app** backs GitHub. A scope can hold several named apps per
vendor, and each data source chooses which one it uses.

The app can be configured at three levels, resolved in this order when a connection starts:

1. **The data source's own selected app** — chosen on the connect/edit form when more than one app
   applies.
2. **Project** — the only project-level app for the vendor. Managed by a `project_admin` on
   **Settings → Credentials**. If the project has several apps for that vendor, connecting without a
   selection fails with an error asking you to choose one.
3. **Organization** — the only org-level app for the vendor. Managed by an `org_admin` under
   **Organization Settings → Data source OAuth apps**.
4. **Deployment default** — a centrally configured app set via `SOURCES_OAUTH_GOOGLE_CLIENT_ID` /
   `SOURCES_OAUTH_GOOGLE_CLIENT_SECRET` (no setup needed by users).

If none is configured, connecting fails with an actionable message linking to the credentials page.

### The Credentials table

**Project Settings → Credentials** lists the OAuth apps that exist in the project, plus the
organization apps an org admin can manage. The list starts empty — it does not pre-render one row per
provider — and an **Add OAuth app** action opens the editor.

| Column | Meaning |
|---|---|
| App | The app's name, e.g. **Production Google** |
| Data source | The vendor, e.g. **Google** |
| Scope | **Project** or **Organization** |
| Client ID | The non-secret client id (secrets are never displayed) |
| Status | **In use** when at least one data source uses it, otherwise **Configured** |
| Updated | When the app was last changed |
| Actions | **Manage** for admins; a role note for everyone else |

Adding an app asks for a **vendor**, a **name**, the client id and secret, and — for Google — the
Drive scope (`drive.readonly` by default, or `drive.file`). The same app then serves Drive and Gmail;
Gmail always requests its read-only scope. Edit an app to change its name or its Drive scope.
Replacing the client id or secret does not update data sources already connected through it: each
source keeps the grant it received at connect time, so reconnect those sources after a rotation.
Deleting an app is refused while a data source still uses it.

Every project member can view the table. Only a `project_admin` can add or change project apps;
organization apps are managed by org admins.

### The organization OAuth apps table

**Organization Settings → Data source OAuth apps** is the same list at organization scope, with an
**Add OAuth app** action. The editor shows the authorized redirect URIs for every provider of the
vendor, so one Google app can be registered for both the Drive and Gmail callbacks.

When no project or organization app exists and the deployment does not provide one, the page carries
a note explaining that the connection will fail until an app is added.

The **Data Sources** page is a plain connections table (name, provider, status, sync mode, last sync)
with a **New data source** action. It never asks you to configure credentials.

### Create the app in the vendor console

For Google (one app can back Drive and Gmail):

1. Go to [Google Cloud Console](https://console.cloud.google.com/) and create or select a project.
2. Enable the **Google Drive API** and, if you use Gmail sources, the **Gmail API**.
3. Configure the **OAuth consent screen** (External or Internal; for Workspace-internal apps use
   Internal).
4. Under **Credentials → Create credentials → OAuth client ID**, choose **Web application**.
5. Under **Authorized redirect URIs**, add every **Redirect URI** shown on the app editor (one per
   data source the app backs, e.g.
   `https://<api-host>/api/data-source-integrations/oauth/google_drive/callback` and
   `https://<api-host>/api/data-source-integrations/oauth/gmail/callback`).
6. Under **Authorized JavaScript origins**, add the **JavaScript origin** shown on the same page
   (the Memory UI origin).
7. Copy the **Client ID** and **Client secret**.
8. Enable the **Google Picker API** for the same project.
9. Create an **API key** (Credentials → Create credentials → API key) and restrict it to the
   Memory UI origin plus `https://docs.google.com/*`. Copy the key and the **project number** —
   they become the picker's **Developer key** and **App ID** in the credentials editor.

For Gmail, use the same Google Cloud project and OAuth client:

1. Enable the **Gmail API**.
2. Add the `https://www.googleapis.com/auth/gmail.readonly` scope to the consent screen.
3. Add the Gmail callback as an authorized redirect URI on the same web client:
   `https://<api-host>/api/data-source-integrations/oauth/gmail/callback`.

The Gmail scope is a Google **restricted** scope: an Internal (Workspace-only) consent screen can use
it as-is, while an External app needs Google verification and a CASA assessment before non-test users
can consent. See [Gmail](#gmail-email) for the operational caveats.

!!! warning "A consent screen in Testing expires grants after 7 days — for Drive as well as Gmail"
    How long a connection lasts is decided by the **publishing status** of the OAuth client, not by the
    scope a source requested, so this applies to **every** Google source. While the app is in
    **Testing**, Google expires the authorization 7 days after consent, refresh token included: each
    source stops with its provider's message — *"the Google Drive connection for this source has
    expired or was revoked"*, or the Gmail equivalent — parks as **Needs reconnect**, works again
    after reconnecting, and dies again a week later. Reconnecting cannot prevent it — the source is
    behaving correctly and reporting a grant that Google really did expire. Fix the client instead:

    - **In production** (or **Internal** for a Workspace) removes the 7-day clock, which is the only
      limit publishing status controls: the grant then lasts until it is revoked, or until another of
      Google's rules applies (six months unused, a password change on an account that granted Gmail
      scopes, the cap of 100 live refresh tokens per user per client). Publishing an Internal app
      needs no Google verification, though a Workspace administrator can still restrict which scopes
      the app is allowed to use.
    - An **External** app in production that uses `drive.readonly` or `gmail.readonly` needs Google's
      restricted-scope verification (and an annual security assessment), because both scopes give
      server-side access to user data. `drive.file` is not a restricted scope and takes the shorter
      review path.

    Check **Google Auth Platform → Audience**: it shows the user type and publishing status of the
    project that owns the client. A client id pasted into a deployment inherits whatever that project
    says.

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
  says so and links to **Reconnect**, which re-runs consent for that same source.

Whichever strategy you choose, the publishing status of the OAuth client decides how long a Drive
connection lasts: an app left in **Testing** expires the grant 7 days after consent and the source
parks as **Needs reconnect** until it is reconnected — see
[Create the app in the vendor console](#create-the-app-in-the-vendor-console).

The strategy resolves project first, then organization, then the provider default. Leaving the
project strategy blank inherits the organization's strategy; it does not reset to the default.

Changing the strategy applies to the next consent, not to sources that are already connected — a
source keeps the scope its token was granted with. To move an existing source onto
`drive.readonly`, switch the strategy and then press **Reconnect** on that source: consent runs
again with the new scope and the source keeps its folders, cursor and history.

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

### Gmail (email)

Select **Gmail** on **Data Sources → New data source** and authorize with the Google account whose mail
you want to sync. A Gmail source ingests **one document per thread**: a Markdown document with a header
(subject, participants, latest message, labels) and one section per message, oldest first. Message
bodies use the plain-text part, falling back to a conversion of the HTML part; attachments are counted
(`_N attachment(s) omitted._`) but never downloaded.

| Field | Meaning |
|---|---|
| **Query** (`query`) | Gmail search expression selecting which threads sync. Default `in:inbox`. Examples: `label:work`, `from:alice@example.com`, `-label:spam`, `has:attachment`. |
| **Label ids** (`label_ids`) | Optional label ids; a thread must carry every listed label. List them from the source's resources (`GET /api/data-source-integrations/{id}/resources`) or the provider catalog (`POST /api/data-source-integrations/resources`). When labels are set and no query is given, the source syncs everything carrying those labels instead of the inbox. |
| **Since** (`since`) | Optional `YYYY-MM-DD`. Threads dated before it are never synced. The date is absolute, so the window only grows: a source never drops previously synced threads just because time passed. Gmail's `after:` filter is day-granular in the mailbox's own timezone, so a thread dated on the boundary day may be included or excluded by a few hours. |
| **Max threads** (`max_threads`) | Ceiling for one pass. Default `10000`, maximum `50000`. Every sync lists the newest matching threads; when the query matches more than the ceiling, older threads are **backfilled** a calendar window at a time on later syncs (see below), so a large mailbox is eventually covered instead of only ever contributing its newest slice. |

A capped listing is not a dead end. The sync that hits the ceiling starts walking older windows
(30 days at a time, narrowing when a window is larger than one sync may ingest) and stores a coverage
watermark in the source's cursor. Each following sync continues from that watermark — head first, so
new mail still arrives immediately — until the walk reaches the `since` floor (or, without a `since`,
Gmail's earliest possible date). Because every window overlaps the next by a day, re-listing a thread
costs only a skipped revision, never a gap — the one exception is a day holding more matching threads
than `max_threads`, which is capped exactly as a capped listing always was. A sync whose threads fail
to fetch leaves the watermark where it was, so that window is re-listed and the failures retried. That
retry is bounded per thread: a thread that fails to fetch or apply on **three consecutive syncs** is
abandoned so one broken message cannot freeze the backfill forever, and the sync that moves past it
records the abandoned thread ids in its error detail and leaves the reason on the source's last error.
Different threads each failing once never exhaust that budget. Because a Gmail source never deletes, a
window you later tightened keeps what it already ingested.

Windows are calendar ranges, not listing pages, so a modified old thread is picked up when its window
is next re-listed rather than on the very next sync.

Change detection uses Gmail's per-thread history id: an unchanged thread is skipped without downloading
its body, so re-syncing a large mailbox is cheap. A thread that gains a message or changes labels is
re-read, and produces a new document version only when its text actually changed. Gmail bills
`threads.list` at 10 units and `threads.get` at 120 units (the connector requests full threads) against
6,000 units per minute per user, so a rate-limited call is retried with exponential backoff — honouring
`Retry-After` — instead of failing the thread until the next scheduled sync.

!!! warning "Gmail scope and Google verification"
    The connector requests `gmail.readonly`, a Google **restricted** scope. A Google Cloud project whose
    consent screen is **Internal** (Workspace-only) can use it without review; an **External** app needs
    Google verification and a CASA assessment before non-test users can consent. A consent screen left in
    **Testing** also expires every Google grant after 7 days, Drive included — see
    [Create the app in the vendor console](#create-the-app-in-the-vendor-console). Enable the
    **Gmail API**, add the scope, and register the callback
    `https://<api-host>/api/data-source-integrations/oauth/gmail/callback` — see
    [Create the app in the vendor console](#create-the-app-in-the-vendor-console).

!!! note "Deleted mail is not detected in this version"
    A Gmail sync is a filtered view of a live mailbox: a thread absent from it may have been archived,
    relabelled, or moved out of the query rather than deleted. Gmail sources therefore never mark
    documents deleted-from-source. Deletion detection via the mailbox history is a follow-up.

### Reconnecting a source

An OAuth grant can stop working long before you want to delete the source: the user revokes the app
in their Google or GitHub account, the refresh token expires, or the provider rotates it away. The
sync then fails with the provider's error, and the source's **Credentials** card offers
**Reconnect**.

A failure of this kind **parks** the source: its status becomes **Needs reconnect** and scheduled
syncs stop until it is reconnected. That is deliberate. Re-sending a grant the provider has already
rejected cannot succeed, so a parked source is not retried — before this it kept producing a failed
sync every minute while the connections table still called it active. The error stays on the source
and in its sync history, and no documents are removed. A transient failure (a 5xx, a timeout, a rate
limit) never parks a source; it stays active and retries on its normal cadence.

If you believe the grant is fine — the provider had an outage, or the failure was misclassified —
you can retry without a new consent by setting `enabled: true` on the source
(`PATCH /api/data-source-integrations/{id}`; there is no on/off control for a data source in the UI
yet). That returns it to active and clears the recorded failure, so the API stops reporting the old
message (`errorMessage`) on a source that is syncing again; the failed run stays in the sync history.
A scheduled source syncs on its next sweep, and a manual one syncs when you press **Sync now**. A grant
that really is dead parks the source again.

Consent runs again for the same source. The stored grant is replaced in place, so the source keeps
its id, name, sync targets, cursor and sync history — the next sync resumes instead of re-importing
everything into a second source. The connected account and the granted scope strategy are refreshed
from the new consent, and the recorded error clears.

The action follows the provider's advertised auth methods. A **web** source has no OAuth method and
shows none. A **GitHub** source always shows it, including one connected with a personal access
token — reconnecting replaces that token with an OAuth grant for the same source, so it needs the
GitHub OAuth app to be configured.

!!! note "Reconnect is not a new source"
    Reconnecting never creates a second source. If you see a duplicate after a provider
    authorization, it came from the connect flow, not from **Reconnect**.

Over the API, name the source on the authorize request and the callback updates that source. The
endpoint takes a JSON body, so send the content type with it:

```bash
curl -X GET "$MEMORY_API_URL/api/data-source-integrations/providers/google_drive/oauth/authorize" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"reconnectSourceId": "<source id>"}'
```

The response is the provider's authorize URL; without a readable body the request is treated as a
plain connect and creates a **new** source.

### Connect GitHub with a personal access token

GitHub can be connected either with OAuth or with a personal access token. The token path uses a
saved credential:

1. Open **Project Settings → Credentials** and choose **Add credential**: pick **GitHub**, give the
   credential a name, and paste the token. (You can also do this inline from the connect form.)
2. On **Data Sources → New data source**, pick **GitHub** and choose **Use a personal access token**.
3. Select the saved credential. The list shows how many sources already use each one, so reusing an
   existing token is an informed choice.
4. Fill in the repository scope below and press **Connect**.

The token is stored encrypted and write-only — it is never shown again. One saved credential can
back several GitHub sources; editing it updates all of them.

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
  only when the grant itself is revoked or has expired; the source then stops with an actionable error,
  and the source's detail page offers **Reconnect** to repair the grant in place.
- **A park that survives a reconnect is usually a client problem, not a grant problem.** Trale's
  authorization server reports a dead grant and a misconfigured client registration with the same error
  code (`invalid_credentials`), so the message cannot tell them apart. If the source parks again after a
  successful reconnect, stop reconnecting — the OAuth client registration is wrong or was deleted, and
  no number of reconnects will fix it, so an administrator has to correct the client configuration.
  A sync that was already running when you reconnected cannot cause it: it keeps failing against the
  grant it started with, but it records that failure without parking the source, because the grant you
  just replaced is not the one the source reads any more.

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
      "name": "Team inbox",
      "providerType": "gmail",
      "sourceType": "email",
      "syncMode": "scheduled",
      "syncIntervalMinutes": 60,
      "config": { "query": "in:inbox -label:spam" }
    }
    ```

After creation you will be redirected (or given a URL) to complete the OAuth flow for providers that require it.

### Test the connection before saving

```http
POST /api/data-source-integrations/test-config
{
  "providerType": "web",
  "config": {
    "start_urls": ["https://docs.example.com/"]
  }
}
```

A saved source can be tested without resending config:

```http
POST /api/data-source-integrations/{id}/test-connection
```

---

## Data source fields

| Field | Description |
|---|---|
| `name` | Display name for this integration |
| `providerType` | The provider: `google_drive`, `gmail`, `github`, `trale`, `web` |
| `sourceType` | Category of content: `drive`, `email`, `repo`, `meeting`, `web` |
| `syncMode` | `manual` (sync only when you trigger it) or `scheduled` (automatic on the interval). `recurring` is accepted as an alias for `scheduled`. A `manual` source never syncs on its own, and switching a source to `manual` clears its stored interval. |
| `syncIntervalMinutes` | Minutes between automatic syncs. Required in spirit for `scheduled`: omitting it applies the provider's default (60 minutes for the web crawler and Gmail). Ignored for `manual`. |
| `enabled` | Write-only on update: `false` pauses the source (`status: disabled`), `true` resumes it. Applied to a `needs_reconnect` source, `true` is an explicit retry — it returns the source to `active` and the next failure parks it again. |
| `status` | `active` (admitted for scheduled sync) · `needs_reconnect` (the stored credential was rejected — reconnect it; scheduled syncs stop) · `disabled` (paused; never admitted). See [Reconnecting a source](#reconnecting-a-source). |

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
GET /api/data-source-integrations/{id}/sync-jobs?limit=50&offset=0  # one page, newest first
GET /api/data-source-integrations/{id}/sync-jobs/latest             # most recent
GET /api/data-source-integrations/{id}/sync-jobs/{jobId}            # specific job
```

The listing is paged. `limit` defaults to 50 and is capped at 200; `offset` skips
that many jobs so older history can be fetched page by page. The response's
`items` is the requested page and `total` is the source's full job count (not the
page length). Pass `status` to filter the listing.

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

`enabled` is the pause/resume switch, and the only way to retry a parked source without reconnecting:

```http
PATCH /api/data-source-integrations/{id}
Content-Type: application/json

{ "enabled": true }
```

!!! note
    Deleting a data source does **not** delete the documents already imported from it. Those remain in the project.

## Extraction after sync

A source sync always chunks and embeds what it ingests, so the content is searchable. Object extraction is a separate, per-project switch: **Settings → Extraction → Auto-extract objects after a data-source sync** (`auto_extract_source_syncs`). It is off by default, applies to every synced item and every new version, and can be turned off to stop a high-volume source (mail, large drives) from spending extraction tokens. With the switch off, select an individual document and trigger extraction from its detail view.
