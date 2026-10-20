import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { expectAppPage, goto } from '../../helpers/page';
import { memoryAuthHeaders } from '../../helpers/objects';
import { MEMORY_API_URL } from '../../helpers/tokens';
import { GDriveFixture } from '../../helpers/gdrive';

// Live (non-mocked) Google Drive end-to-end: a project-scoped Google OAuth app
// connects a REAL Drive folder, and the memory backend's source-sync worker
// ingests a real file through the full version lifecycle.
//
// This is opt-in and NEVER runs in CI: it needs a real Google OAuth app plus a
// human to complete the Google consent screen mid-test. See the e2e README
// ("Live Google Drive gate") for the env contract and run instructions.
//
// Flow:
//   1. Fresh scratch project (isolates the source + documents).
//   2. Configure the PROJECT-scoped Drive OAuth client (id/secret + the
//      drive.readonly strategy) via the real settings UI.
//   3. Create the scratch Drive folder (the post-consent resource step's sync
//      target) and start the OAuth connect from the UI connect surface
//      (/data-sources/new?providerType=google_drive → the panel's Connect
//      control, which drives /data-sources/oauth/start). The test intercepts
//      that route's 303 redirect to capture the Google authorize URL (state +
//      PKCE, offline access, consent) — printed + attached, never navigated by
//      the test. A human opens it and grants consent; the memory callback
//      creates the source in the scratch project (state carries the project id)
//      with NO sync target pinned — exercising the shipped #1988 fix.
//   4. Poll for the source, assert the detail page prompts for a sync target
//      (ds-resource-needed), then select the scratch folder on the resource
//      step → the edit form prefills the folder (config round-trip).
//   5. Prove stored credentials reach Drive via the connection test, then the
//      fixture creates a plain-text file (v1) → sync → doc version 1.
//   6. Fixture rewrites the file (v2) and asserts headRevisionId moved → sync →
//      version 2 sharing the SAME canonicalId with a NEW sourceRevisionId.
//   7. Fixture trashes the file → sync → head marked deleted_from_source.
// Cleanup is best-effort: delete source, delete scratch folder/project.

const CLIENT_ID = process.env.E2E_GDRIVE_CLIENT_ID ?? '';
const CLIENT_SECRET = process.env.E2E_GDRIVE_CLIENT_SECRET ?? '';
const FIXTURE_REFRESH_TOKEN = process.env.E2E_GDRIVE_FIXTURE_REFRESH_TOKEN ?? '';
// Optional: reuse a pre-provisioned folder; created fresh when unset.
const FOLDER_ID = process.env.E2E_GDRIVE_FOLDER_ID ?? '';

// The memory OAuth state is short-lived (server TTL); give the human the full
// window to open the printed URL and finish consent.
const OAUTH_WAIT_MS = 10 * 60_000;
const SYNC_WAIT_MS = 5 * 60_000;
const POLL_MS = 3_000;

const CONTENT_V1 = '# gdrive-live fixture\n\nversion one\n';
const CONTENT_V2 = '# gdrive-live fixture\n\nversion two (edited)\n';

function missingEnv(): string[] {
  const missing: string[] = [];
  if (process.env.E2E_GDRIVE_LIVE !== '1') missing.push('E2E_GDRIVE_LIVE=1');
  if (!CLIENT_ID) missing.push('E2E_GDRIVE_CLIENT_ID');
  if (!CLIENT_SECRET) missing.push('E2E_GDRIVE_CLIENT_SECRET');
  if (!FIXTURE_REFRESH_TOKEN) missing.push('E2E_GDRIVE_FIXTURE_REFRESH_TOKEN');
  // Document/version/content assertions hit the memory API directly; without it
  // MEMORY_API_URL is '' and the calls silently become relative /api/documents
  // against the gateway (no such route) — so it is part of the live gate.
  if (!MEMORY_API_URL) missing.push('E2E_MEMORY_API_URL');
  return missing;
}

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/** The memory API document row (head) returned by GET /api/documents. */
interface MemoryDoc {
  id: string;
  filename?: string;
  canonicalId: string;
  version: number;
  sourceRevisionId?: string;
  sourceStatus: string;
}

/** One version row from GET /api/documents/:id/versions. */
interface MemoryVersion {
  id: string;
  canonicalId: string;
  version: number;
  sourceRevisionId?: string;
  sourceStatus?: string;
}

/** A gateway data-source integration (GET /api/data-source-integrations). */
interface DsIntegration {
  id: string;
  providerType: string;
  projectId?: string;
  name?: string;
}

interface SyncJob {
  id: string;
  status: string;
  errorMessage?: string;
}

/** Auth headers for the memory API, scoped to the scratch project. */
async function memoryHeaders(page: Page, projectId: string): Promise<Record<string, string>> {
  const headers = await memoryAuthHeaders(page);
  headers['X-Project-ID'] = projectId;
  return headers;
}

/** List a project's drive documents, optionally including deleted-from-source. */
async function listDocs(
  page: Page,
  projectId: string,
  includeDeleted: boolean,
): Promise<MemoryDoc[]> {
  const headers = await memoryHeaders(page, projectId);
  const qs = new URLSearchParams({ sourceType: 'drive', limit: '100' });
  if (includeDeleted) qs.set('includeDeletedFromSource', 'true');
  const resp = await page.request.get(`${MEMORY_API_URL}/api/documents?${qs.toString()}`, {
    headers,
  });
  expect(resp.ok(), `list documents failed (HTTP ${resp.status()})`).toBeTruthy();
  const body = (await resp.json()) as { documents?: MemoryDoc[] };
  return body.documents ?? [];
}

/** Version history for a document (ordered oldest-first). */
async function listVersions(page: Page, projectId: string, docId: string): Promise<MemoryVersion[]> {
  const headers = await memoryHeaders(page, projectId);
  const resp = await page.request.get(
    `${MEMORY_API_URL}/api/documents/${encodeURIComponent(docId)}/versions`,
    { headers },
  );
  expect(resp.ok(), `list versions failed (HTTP ${resp.status()})`).toBeTruthy();
  const body = (await resp.json()) as { versions?: MemoryVersion[] };
  return body.versions ?? [];
}

/** Raw text content of a document (or version) row. */
async function getContent(page: Page, projectId: string, docId: string): Promise<string> {
  const headers = await memoryHeaders(page, projectId);
  const resp = await page.request.get(
    `${MEMORY_API_URL}/api/documents/${encodeURIComponent(docId)}/content`,
    { headers },
  );
  expect(resp.ok(), `get content failed (HTTP ${resp.status()})`).toBeTruthy();
  const body = (await resp.json()) as { content?: string | null };
  return body.content ?? '';
}

/** The latest sync job for a source, or null when none has run yet. */
async function latestJob(page: Page, sourceId: string): Promise<SyncJob | null> {
  const resp = await page.request.get(
    `/api/data-source-integrations/${sourceId}/sync-jobs/latest`,
  );
  if (!resp.ok()) return null;
  const job = (await resp.json().catch(() => null)) as SyncJob | null;
  return job && typeof job === 'object' ? job : null;
}

/** Trigger a sync and wait for a NEW terminal job (completed is asserted). */
async function triggerAndWait(page: Page, sourceId: string, label: string): Promise<SyncJob> {
  const before = await latestJob(page, sourceId);
  const beforeId = before?.id ?? null;
  const trigger = await page.request.post(`/api/data-source-integrations/${sourceId}/sync`);
  expect(trigger.ok(), `${label}: trigger sync failed (HTTP ${trigger.status()})`).toBeTruthy();

  const deadline = Date.now() + SYNC_WAIT_MS;
  let job: SyncJob | null = null;
  while (Date.now() < deadline) {
    const cur = await latestJob(page, sourceId);
    if (cur && cur.id !== beforeId && ['completed', 'failed', 'cancelled'].includes(cur.status)) {
      job = cur;
      break;
    }
    await page.waitForTimeout(POLL_MS);
  }
  expect(job, `${label}: no terminal sync job within ${SYNC_WAIT_MS}ms`).toBeTruthy();
  expect(
    job!.status,
    `${label}: sync ${job!.status}${job!.errorMessage ? ` — ${job!.errorMessage}` : ''}`,
  ).toBe('completed');
  return job!;
}

test('project OAuth app → live Drive folder → sync → edit → v2 → trash → deleted_from_source', async ({
  page,
}) => {
  const missing = missingEnv();
  test.skip(
    missing.length > 0,
    'Google Drive live gate is opt-in and needs real Google credentials. Missing: ' +
      missing.join(', ') +
      '. See tests/e2e/.env.e2e.example and README ("Live Google Drive gate").',
  );
  test.setTimeout(12 * 60_000);

  const bootstrap = requireBootstrap();
  const gdrive = new GDriveFixture(CLIENT_ID, CLIENT_SECRET, FIXTURE_REFRESH_TOKEN);
  const nonce = Date.now().toString(36);
  const filename = `e2e-gdrive-${nonce}.txt`;
  let projectId = '';
  let sourceId = '';
  let createdFolderId = '';
  let createdFileId = '';

  try {
    // 1. Fresh project isolates the source + documents.
    projectId = await createProject(page, bootstrap.orgId, `E2E gdrive live ${nonce}`);

    // Pre-flight the fixture token BEFORE the human consents, so a stale or
    // invalid refresh token fails now instead of burning the ~10-minute OAuth
    // consent window. getFile never logs or returns the token.
    try {
      await gdrive.getFile('root');
    } catch (e) {
      throw new Error(
        'Google Drive fixture pre-flight failed: check E2E_GDRIVE_FIXTURE_REFRESH_TOKEN ' +
          '(re-issue via scripts/gdrive-fixture-token.mjs) and the client id/secret. ' +
          `Underlying error: ${(e as Error).message}`,
      );
    }

    // 2. Configure the PROJECT-scoped Drive OAuth client via the settings UI.
    //    The scope is EXPLICIT: goto defaults to the bootstrap project, which
    //    would (a) configure the wrong project and (b) silently move the
    //    session's active project back to bootstrap — breaking the source poll
    //    and every scratch-project API call below.
    await goto(page, '/settings/oauth-clients', { orgId: bootstrap.orgId, projectId });
    await expectAppPage(page, /OAuth clients/);
    await expect(page.getByTestId('ds-oauth-clients')).toBeVisible();
    // Two providers (google_drive + github) both render a client form; only the
    // Drive card offers the drive.readonly strategy option.
    const driveForm = page
      .getByTestId('ds-oauth-client-form')
      .filter({ has: page.locator('option[value="drive.readonly"]') });
    await expect(driveForm, 'Drive OAuth client card must render').toHaveCount(1);
    await driveForm.getByTestId('ds-field-client_id').fill(CLIENT_ID);
    await driveForm.getByTestId('ds-field-client_secret').fill(CLIENT_SECRET);
    await driveForm.getByTestId('ds-field-strategy').selectOption('drive.readonly');
    await driveForm.getByRole('button', { name: 'Save', exact: true }).click();
    await page.waitForURL(/saved=1/, { timeout: 30_000 });

    // Re-read the configured card: the server now resolves the deployment's
    // callback values, which the authorize URL must match. The console-guide
    // block that carries them lives at CARD level, as a sibling of the
    // <form data-testid="ds-oauth-client-form">, never inside it — scoping this
    // locator to the form silently waits forever.
    const redirectUriEl = page.getByTestId('ds-oauth-redirect-uri');
    await expect(
      redirectUriEl,
      'OAuth client card must show the resolved redirect URI',
    ).toHaveCount(1);
    await expect(redirectUriEl).toHaveValue(/\S/);
    const redirectUri = (await redirectUriEl.inputValue()).trim();

    // 3. Create the scratch Drive folder up front so it exists for the
    //    post-consent resource step, then start the connect from the REAL UI
    //    connect surface (/data-sources/new?providerType=google_drive → the
    //    panel's Connect control, which drives /data-sources/oauth/start). The
    //    UI route builds the Google authorize URL server-side (state + PKCE,
    //    bound to the scratch project) and answers with a 303 redirect; the
    //    test intercepts that redirect to capture the URL for the human WITHOUT
    //    following it — the test browser never navigates to Google, and no
    //    config is pinned, so the callback-created source lands with an empty
    //    sync target (the shipped #1988 fix).
    const folderId = FOLDER_ID || (await gdrive.createFolder(`e2e-gdrive-${nonce}`));
    if (!FOLDER_ID) createdFolderId = folderId;

    let location = '';
    await page.route('**/data-sources/oauth/start*', async (route) => {
      // maxRedirects: 0 captures the 303 (Location = Google authorize URL)
      // instead of following it; fulfill with a stub so the test browser stays
      // on the connect surface rather than navigating to Google.
      const resp = await route.fetch({ maxRedirects: 0 });
      location = (resp.headers()['location'] ?? '').trim();
      await route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<html><body>OAuth start intercepted</body></html>',
      });
    });

    await goto(page, '/data-sources/new?providerType=google_drive', {
      orgId: bootstrap.orgId,
      projectId,
    });
    await expect(page.getByTestId('ds-oauth-connect')).toBeVisible();
    await page.getByTestId('ds-oauth-connect').click();
    await expect.poll(() => location, { timeout: 30_000 }).not.toBe('');
    await page.unroute('**/data-sources/oauth/start*');
    expect(location, 'UI OAuth start must redirect to a Google authorize URL').not.toBe('');

    const authUrl = new URL(location);
    expect(authUrl.origin + authUrl.pathname, 'authorize URL must point at Google').toBe(
      'https://accounts.google.com/o/oauth2/v2/auth',
    );
    expect(authUrl.searchParams.get('client_id')).toBe(CLIENT_ID);
    expect(authUrl.searchParams.get('redirect_uri')).toBe(redirectUri);
    expect(authUrl.searchParams.get('scope') ?? '').toContain('drive.readonly');
    expect(authUrl.searchParams.get('access_type')).toBe('offline');
    expect(authUrl.searchParams.get('prompt')).toBe('consent');
    expect(authUrl.searchParams.get('response_type')).toBe('code');
    expect(authUrl.searchParams.get('code_challenge_method')).toBe('S256');
    expect(authUrl.searchParams.get('state') ?? '').not.toBe('');
    expect(authUrl.searchParams.get('code_challenge') ?? '').not.toBe('');

    await test.info().attach('google-oauth-authorize-url', {
      body: location,
      contentType: 'text/uri-list',
    });
    // Clear marker so the human can find the URL in the log.
    console.log(
      '[gdrive-live] ⚠️ AUTHORIZE URL (open in a browser, grant consent for the fixture account):\n' +
        location,
    );

    // 4. Poll for the source the memory OAuth callback creates (state carries
    //    the scratch project). The human completes consent in their own browser.
    const deadline = Date.now() + OAUTH_WAIT_MS;
    let source: DsIntegration | null = null;
    while (Date.now() < deadline) {
      const listResp = await page.request.get('/api/data-source-integrations');
      if (listResp.ok()) {
        const items = (await listResp.json().catch(() => [])) as DsIntegration[];
        const found = items.find(
          (s) =>
            s.providerType === 'google_drive' &&
            // The integration JSON carries projectId (gateway DSIntegration);
            // require it to avoid adopting a source owned by another project.
            (s.projectId === undefined || s.projectId === projectId),
        );
        if (found) {
          source = found;
          break;
        }
      }
      await page.waitForTimeout(5_000);
    }
    expect(
      source,
      `no google_drive source appeared within ${OAUTH_WAIT_MS}ms. Open the printed ` +
        'AUTHORIZE URL and finish consent; if Google landed elsewhere, check the ' +
        'server SOURCES_OAUTH_LANDING_URL config. The OAuth state expires in ~10 min.',
    ).toBeTruthy();
    sourceId = source!.id;

    // 5. The callback created the source with NO sync target (the connect sent
    //    no config — the shipped #1988 fix). The detail page must therefore
    //    prompt for a folder: assert the ds-resource-needed banner + link, walk
    //    the resource step, and select the scratch folder. After the save the
    //    edit form prefills the stored folder (config round-trip). Explicit
    //    scope again: goto defaults to the bootstrap project, which would build
    //    /o/{org}/p/{bootstrap}/data-sources/{id} → 404 for a scratch-owned
    //    source (and reset the session's active project).
    await goto(page, `/data-sources/${sourceId}?created=1`, {
      orgId: bootstrap.orgId,
      projectId,
    });
    await expect(page.getByTestId('ds-edit-form')).toBeVisible();
    await expect(
      page.getByTestId('ds-resource-needed'),
      'detail page must prompt for a sync target when none is selected',
    ).toBeVisible();
    await expect(
      page.getByTestId('ds-resource-needed-link'),
      'resource prompt must link to the resource step',
    ).toBeVisible();

    await page.getByTestId('ds-resource-needed-link').click();
    await expect(page.getByTestId('ds-resource-step'), 'resource step must render').toBeVisible();
    const folderRow = page
      .getByTestId('ds-resource-row')
      .filter({ has: page.locator(`input[name="resource_id"][value="${folderId}"]`) });
    await expect(
      folderRow,
      `resource step must list the scratch folder ${folderId}`,
    ).toHaveCount(1);
    await folderRow.getByTestId('ds-resource-select').click();
    await page.waitForURL(/\?updated=1/, { timeout: 30_000 });

    await expect(page.getByTestId('ds-edit-form')).toBeVisible();
    await expect(page.getByTestId('ds-field-folder_id')).toHaveValue(folderId);

    // Prove the stored credentials actually reach Drive before syncing.
    await page.getByTestId('ds-test-connection').click();
    await expect(page.getByTestId('ds-test-result')).toContainText(/connection successful/i, {
      timeout: 30_000,
    });

    // 6. v1: create the file, sync, assert document version 1.
    const created = await gdrive.uploadTextFile(folderId, filename, CONTENT_V1);
    createdFileId = created.id;
    await triggerAndWait(page, sourceId, 'first sync');

    let docs = await listDocs(page, projectId, false);
    let doc = docs.find((d) => d.filename === filename);
    expect(doc, `first sync must create a document named "${filename}"`).toBeTruthy();
    expect(doc!.version).toBe(1);
    expect(doc!.sourceStatus).toBe('active');
    const rev1 = doc!.sourceRevisionId ?? '';
    expect(rev1, 'v1 must carry a source revision').not.toBe('');
    expect(rev1).toBe(created.headRevisionId ?? '');
    const canonicalId = doc!.canonicalId;
    expect(canonicalId).not.toBe('');

    // 7. v2: rewrite the file, prove the revision moved BEFORE re-sync (the
    //    worker skips unchanged head revisions), then sync to a new version.
    const updated = await gdrive.updateFileContent(created.id, CONTENT_V2);
    const rev2 = updated.headRevisionId ?? '';
    expect(rev2, 'edited file must carry a headRevisionId').not.toBe('');
    expect(rev2, 'edit must advance the Drive head revision').not.toBe(rev1);

    await triggerAndWait(page, sourceId, 'second sync');

    docs = await listDocs(page, projectId, false);
    doc = docs.find((d) => d.filename === filename);
    expect(doc, `re-sync must keep a head document named "${filename}"`).toBeTruthy();
    expect(doc!.version).toBe(2);
    expect(doc!.sourceStatus).toBe('active');
    expect(doc!.canonicalId, 'v2 must share the v1 canonicalId').toBe(canonicalId);
    expect(doc!.sourceRevisionId ?? '').toBe(rev2);

    let versions = await listVersions(page, projectId, doc!.id);
    expect(versions, 'version history must list v1 and v2').toHaveLength(2);
    expect(versions.map((v) => v.version)).toEqual([1, 2]);
    expect(versions.map((v) => v.canonicalId)).toEqual([canonicalId, canonicalId]);
    expect(versions.map((v) => v.sourceRevisionId)).toEqual([rev1, rev2]);

    // The point of the v2 check is content RETENTION, not just metadata: the
    // v1 body must survive as an immutable earlier version, and the head must
    // carry the v2 body. versions[] is oldest-first, so [0] is v1.
    const v1Content = await getContent(page, projectId, versions[0].id);
    expect(v1Content, 'v1 version must retain the v1 body').toContain('version one');
    expect(v1Content, 'v1 version must not contain the v2 body').not.toContain('version two');
    const headContent = await getContent(page, projectId, doc!.id);
    expect(headContent, 'head document must carry the v2 body').toContain('version two');

    // 8. Trash the file → sync → the head is marked deleted_from_source (and
    //    the default document listing drops it).
    await gdrive.trashFile(created.id);
    await triggerAndWait(page, sourceId, 'trash sync');

    docs = await listDocs(page, projectId, true);
    doc = docs.find((d) => d.filename === filename);
    expect(doc, 'deleted-from-source head must remain in the include-deleted listing').toBeTruthy();
    expect(doc!.sourceStatus).toBe('deleted_from_source');

    versions = await listVersions(page, projectId, doc!.id);
    expect(versions).toHaveLength(2);
    const head = versions[versions.length - 1];
    expect(head.sourceStatus).toBe('deleted_from_source');
  } finally {
    // Best-effort cleanup, idempotent across repeated runs / skips / failures.
    if (sourceId) {
      await page.request.delete(`/api/data-source-integrations/${sourceId}`).catch(() => {});
    }
    // Delete the fixture file FIRST: a folder with children cannot be removed,
    // and the trash step leaves it in the folder. Both are best-effort.
    if (createdFileId) {
      await gdrive.deleteFile(createdFileId).catch(() => {});
    }
    if (createdFolderId) {
      await gdrive.deleteFolder(createdFolderId).catch(() => {});
    }
    if (bootstrap.projectId) {
      await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
    }
    if (projectId && bootstrap.orgId) {
      await page.request
        .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
        .catch(() => {});
    }
  }
});
