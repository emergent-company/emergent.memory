import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { goto } from '../../helpers/page';
import { memoryAuthHeaders } from '../../helpers/objects';
import { MEMORY_API_URL } from '../../helpers/tokens';
import { GDriveFixture } from '../../helpers/gdrive';

// Live (non-mocked) Google Drive end-to-end. By default the gate is SELF-SEEDING
// and needs NO human interaction: it creates a scratch Drive folder through the
// Drive REST API (an access token minted from E2E_GDRIVE_FIXTURE_REFRESH_TOKEN),
// then seeds a project-scoped Drive source through the public API
// (POST /api/data-source-integrations) with the stored-credential config the
// product actually syncs from — client_id / client_secret / refresh_token live
// INSIDE the source `config` (see apps/server/domain/sources/gdrive_provider.go
// and entity.go CreateIntegrationRequest.Config). No browser ever visits Google.
//
// The human-consent OAuth flow (configure the project OAuth app → intercept the
// authorize URL → poll for the callback-created source) is retained ONLY behind
// the explicit opt-in E2E_GDRIVE_LIVE_CONSENT=1 flag, in a separate test below,
// so a manual consent check stays possible but never runs unattended.
//
// This project is opt-in and NEVER runs in CI. Env contract (see .env.e2e.example):
//   E2E_GDRIVE_LIVE=1                      gate — the spec skips unless set
//   E2E_GDRIVE_CLIENT_ID / _SECRET          OAuth client (also used to mint the
//                                           fixture access token)
//   E2E_GDRIVE_FIXTURE_REFRESH_TOKEN        refresh token (full drive scope) the
//                                           fixture exchanges to create/edit/trash
//   E2E_GDRIVE_FOLDER_ID                    optional: reuse a pre-made folder
//   E2E_MEMORY_API_URL                      memory API origin (documents/versions)
//   E2E_GDRIVE_LIVE_CONSENT=1               opt-in: run the manual consent test
//
// Flow (seeded, default):
//   1. Fresh scratch project (isolates the source + documents).
//   2. Create the scratch Drive folder + seed the source via the public API.
//   3. Detail page shows the stored folder as a sync-scope chip (round-trip).
//   4. Prove stored credentials reach Drive (connection test), then v1 → sync →
//      v2 → sync (v1 retained) → trash → sync → deleted_from_source.
//
// Folder *picking* is covered hermetically in specs/datasources/oauth-connect
// (the Google Picker modal, stubbed); this live gate seeds the target through
// the same canonical `folders` config the save endpoint writes.
// Cleanup is best-effort: delete source, delete scratch folder/project.

const CLIENT_ID = process.env.E2E_GDRIVE_CLIENT_ID ?? '';
const CLIENT_SECRET = process.env.E2E_GDRIVE_CLIENT_SECRET ?? '';
const FIXTURE_REFRESH_TOKEN = process.env.E2E_GDRIVE_FIXTURE_REFRESH_TOKEN ?? '';
// Optional: reuse a pre-provisioned folder; created fresh when unset.
const FOLDER_ID = process.env.E2E_GDRIVE_FOLDER_ID ?? '';
// Opt-in manual consent path (never runs unattended).
const CONSENT = process.env.E2E_GDRIVE_LIVE_CONSENT === '1';

// The memory OAuth state is short-lived (server TTL); give the human the full
// window to open the printed URL and finish consent. Only the consent test uses
// this.
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

/**
 * Seed a project-scoped Drive source through the public API, carrying the
 * stored-credential config the scheduled sync actually uses (client_id /
 * client_secret / refresh_token + folder_id). The gateway resolves the project
 * from the session, so the caller must have activated the scratch project first
 * (createProject does). Returns the created integration (id + non-secret config).
 */
async function seedSource(
  page: Page,
  projectId: string,
  folderId: string,
  folderName: string,
  name: string,
): Promise<{ id: string; config: Record<string, unknown> }> {
  // Node `fetch`, not `page.request`: the body carries the stored credentials and
  // Playwright records request post-data into the trace archive (this project
  // runs trace: 'retain-on-failure'), which would persist a refresh token in a
  // failed run's artifacts. Same reason helpers/gdrive.ts talks to Drive from
  // Node. The endpoint is addressed directly with the session's bearer token and
  // an explicit X-Project-ID, so the seed does not depend on the session's active
  // project either.
  const auth = await memoryAuthHeaders(page);
  const resp = await fetch(`${MEMORY_API_URL}/api/data-source-integrations`, {
    method: 'POST',
    headers: { ...auth, 'X-Project-ID': projectId, 'content-type': 'application/json' },
    body: JSON.stringify({
      name,
      providerType: 'google_drive',
      sourceType: 'google_drive',
      config: {
        client_id: CLIENT_ID,
        client_secret: CLIENT_SECRET,
        refresh_token: FIXTURE_REFRESH_TOKEN,
        folders: [{ folder_id: folderId, name: folderName }],
      },
    }),
  });
  const bodyText = await resp.text();
  expect(
    resp.ok,
    `seed source failed (HTTP ${resp.status}): ${redactCredentials(bodyText)}`,
  ).toBeTruthy();
  return JSON.parse(bodyText) as { id: string; config: Record<string, unknown> };
}

/** Strip credential material from a response before it can reach a report. */
function redactCredentials(text: string): string {
  return text.replace(
    /"(client_secret|refresh_token|access_token)"\s*:\s*"[^"]*"/g,
    '"$1":"[redacted]"',
  );
}

/** Drive-side evidence the file lifecycle produced, for the run report. */
interface SyncEvidence {
  createdFileId: string;
  docId: string;
  canonicalId: string;
  rev1: string;
  rev2: string;
  v1VersionId: string;
  v2VersionId: string;
}

/**
 * Drive the full source-file lifecycle against a connected source and assert the
 * version contract: v1 document → rewrite → v2 (v1 retained, same canonicalId,
 * new sourceRevisionId) → trash → head marked deleted_from_source.
 */
async function exerciseSyncLifecycle(
  page: Page,
  gdrive: GDriveFixture,
  projectId: string,
  sourceId: string,
  folderId: string,
  filename: string,
): Promise<SyncEvidence> {
  // 1. v1: create the file, sync, assert document version 1.
  const created = await gdrive.uploadTextFile(folderId, filename, CONTENT_V1);
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

  // 2. v2: rewrite the file, prove the revision moved BEFORE re-sync (the worker
  //    skips unchanged head revisions), then sync to a new version.
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

  // The point of the v2 check is content RETENTION, not just metadata: the v1
  // body must survive as an immutable earlier version, and the head must carry
  // the v2 body. versions[] is oldest-first, so [0] is v1.
  const v1Content = await getContent(page, projectId, versions[0].id);
  expect(v1Content, 'v1 version must retain the v1 body').toContain('version one');
  expect(v1Content, 'v1 version must not contain the v2 body').not.toContain('version two');
  const headContent = await getContent(page, projectId, doc!.id);
  expect(headContent, 'head document must carry the v2 body').toContain('version two');

  // 3. Trash the file → sync → the head is marked deleted_from_source (and the
  //    default document listing drops it).
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

  return {
    createdFileId: created.id,
    docId: doc!.id,
    canonicalId,
    rev1,
    rev2,
    v1VersionId: versions[0].id,
    v2VersionId: versions[1].id,
  };
}

test('seeded source → folder chip → sync → v2 → trash → deleted_from_source', async ({
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
  const folderName = `e2e-gdrive-${nonce}`;
  const filename = `${folderName}.txt`;
  let projectId = '';
  let sourceId = '';
  let createdFolderId = '';
  let createdFileId = '';

  try {
    // 1. Fresh project isolates the source + documents.
    projectId = await createProject(page, bootstrap.orgId, `E2E gdrive live ${nonce}`);

    // Pre-flight the fixture token so a stale/invalid refresh token fails now
    // instead of after the source is seeded. getFile never logs/returns the token.
    try {
      await gdrive.getFile('root');
    } catch (e) {
      throw new Error(
        'Google Drive fixture pre-flight failed: check E2E_GDRIVE_FIXTURE_REFRESH_TOKEN ' +
          '(re-issue via scripts/gdrive-fixture-token.mjs) and the client id/secret. ' +
          `Underlying error: ${(e as Error).message}`,
      );
    }

    // 2. Create the scratch Drive folder, then seed the source through the public
    //    API — no browser, no consent, the exact config the scheduled sync uses.
    const folderId = FOLDER_ID || (await gdrive.createFolder(folderName));
    if (!FOLDER_ID) createdFolderId = folderId;

    const seeded = await seedSource(
      page,
      projectId,
      folderId,
      folderName,
      `E2E gdrive live ${nonce}`,
    );
    sourceId = seeded.id;
    expect(sourceId, 'seed must return the source id').not.toBe('');
    const seededFolders = seeded.config['folders'] as Array<Record<string, unknown>> | undefined;
    expect(seededFolders, 'seeded config must carry the canonical folders list').toHaveLength(1);
    expect(seededFolders![0]['folder_id']).toBe(folderId);

    // 3. Detail page shows the stored folder as a sync-scope chip.
    await goto(page, `/data-sources/${sourceId}`, { orgId: bootstrap.orgId, projectId });
    await expect(page.getByTestId('ds-edit-form')).toBeVisible();
    await expect(
      // The chip carries the folder id on the element, and its label is the
      // name Drive resolved (which differs from the id when a fixture folder is
      // reused via E2E_GDRIVE_FOLDER_ID).
      page.locator(`[data-testid="ds-folder-target"][data-folder-id="${folderId}"]`),
      'detail page must render the stored folder target',
    ).toBeVisible();
    // The settings form never edits the folder list.
    await expect(page.getByTestId('ds-field-folders')).toHaveCount(0);

    // Prove the stored credentials actually reach Drive before syncing.
    await page.getByTestId('ds-test-connection').click();
    await expect(page.getByTestId('ds-test-result')).toContainText(/connection successful/i, {
      timeout: 30_000,
    });

    // 6. Full version lifecycle against the seeded source.
    const ev = await exerciseSyncLifecycle(page, gdrive, projectId, sourceId, folderId, filename);
    createdFileId = ev.createdFileId;
    console.log(
      `[gdrive-live] sync evidence: doc=${ev.docId} v1=${ev.v1VersionId} (rev ${ev.rev1}) ` +
        `v2=${ev.v2VersionId} (rev ${ev.rev2}) canonical=${ev.canonicalId}`,
    );
    await test.info().attach('sync-evidence', {
      body:
        `doc=${ev.docId}\ncanonical=${ev.canonicalId}\n` +
        `v1=${ev.v1VersionId} rev1=${ev.rev1}\nv2=${ev.v2VersionId} rev2=${ev.rev2}\n`,
      contentType: 'text/plain',
    });
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

// The human-consent OAuth path, retained ONLY for a manual consent check. It is
// skipped unless E2E_GDRIVE_LIVE_CONSENT=1 so it never runs unattended (and never
// in CI). It configures the project OAuth app, intercepts the authorize URL for
// a human to open, and polls for the callback-created source — the exact flow the
// seeded test above replaces by default.
test.describe('manual OAuth consent connect (opt-in)', () => {
  test.skip(
    !CONSENT,
    'E2E_GDRIVE_LIVE_CONSENT=1 required: the consent path needs a human to finish Google OAuth.',
  );

  test('project OAuth app → consent → live Drive folder → sync → edit → v2 → trash → deleted_from_source', async ({
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
    const folderName = `e2e-gdrive-${nonce}`;
    const filename = `${folderName}.txt`;
    let projectId = '';
    let sourceId = '';
    let createdFolderId = '';
    let createdFileId = '';

    try {
      projectId = await createProject(page, bootstrap.orgId, `E2E gdrive live ${nonce}`);

      try {
        await gdrive.getFile('root');
      } catch (e) {
        throw new Error(
          'Google Drive fixture pre-flight failed: check E2E_GDRIVE_FIXTURE_REFRESH_TOKEN ' +
            '(re-issue via scripts/gdrive-fixture-token.mjs) and the client id/secret. ' +
            `Underlying error: ${(e as Error).message}`,
        );
      }

      // Configure the PROJECT-scoped Drive OAuth client via the Credentials table.
      // Explicit scope: goto defaults to the bootstrap project, which would
      // configure the wrong project and reset the session's active project.
      await goto(page, '/settings/credentials', { orgId: bootstrap.orgId, projectId });
      await expect(page.getByTestId('ds-credentials')).toBeVisible();
      await page
        .locator(
          '[data-testid="ds-credential-row"][data-provider="google_drive"][data-scope="project"]',
        )
        .getByTestId('ds-credential-manage')
        .click();
      await expect(page.getByTestId('ds-credential-detail')).toBeVisible();
      const driveForm = page.getByTestId('ds-oauth-client-form');
      await expect(driveForm, 'Drive OAuth client card must render').toHaveCount(1);
      await driveForm.getByTestId('ds-field-client_id').fill(CLIENT_ID);
      await driveForm.getByTestId('ds-field-client_secret').fill(CLIENT_SECRET);
      await driveForm.getByTestId('ds-field-strategy').selectOption('drive.readonly');
      await driveForm.getByRole('button', { name: 'Save', exact: true }).click();
      await page.waitForURL(/saved=1/, { timeout: 30_000 });

      const redirectUriEl = page.getByTestId('ds-oauth-redirect-uri');
      await expect(redirectUriEl, 'OAuth client card must show the resolved redirect URI').toHaveCount(1);
      await expect(redirectUriEl).toHaveValue(/\S/);
      const redirectUri = (await redirectUriEl.inputValue()).trim();

      // Create the scratch Drive folder up front, then start the connect from the
      // real UI connect surface and intercept the authorize URL for the human.
      const folderId = FOLDER_ID || (await gdrive.createFolder(folderName));
      if (!FOLDER_ID) createdFolderId = folderId;

      let location = '';
      await page.route('**/data-sources/oauth/start*', async (route) => {
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
      console.log(
        '[gdrive-live] ⚠️ AUTHORIZE URL (open in a browser, grant consent for the fixture account):\n' +
          location,
      );

      // Poll for the source the memory OAuth callback creates.
      const deadline = Date.now() + OAUTH_WAIT_MS;
      let source: DsIntegration | null = null;
      while (Date.now() < deadline) {
        const listResp = await page.request.get('/api/data-source-integrations');
        if (listResp.ok()) {
          const items = (await listResp.json().catch(() => [])) as DsIntegration[];
          const found = items.find(
            (s) =>
              s.providerType === 'google_drive' &&
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

      // The browser-visible landing after consent must resolve.
      const landing = await page.request.get(`/data-sources/${sourceId}?created=1`, {
        maxRedirects: 0,
      });
      expect(landing.status(), 'unscoped /data-sources must redirect, not 404').toBe(303);
      expect(landing.headers()['location'] ?? '').toContain(
        `/o/${bootstrap.orgId}/p/${projectId}/data-sources/${sourceId}`,
      );

      // The callback created the source with NO sync target — the detail page
      // prompts for one and opens the Google Picker modal. A live picker needs a
      // human inside Google's iframe, so save the target through the endpoint the
      // modal uses and then assert the chip the UI renders.
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
        'resource prompt must open the folder picker',
      ).toBeVisible();

      const saved = await page.request.put(
        `/api/data-source-integrations/${sourceId}/sync-targets`,
        { data: { folderIds: [folderId] } },
      );
      expect(saved.ok(), `sync-targets save failed (HTTP ${saved.status()})`).toBeTruthy();

      await goto(page, `/data-sources/${sourceId}?updated=1`, {
        orgId: bootstrap.orgId,
        projectId,
      });
      await expect(
        page.locator(`[data-testid="ds-folder-target"][data-folder-id="${folderId}"]`),
        'saved folder must render as a sync-scope chip',
      ).toBeVisible();
      await expect(page.getByTestId('ds-resource-needed')).toHaveCount(0);

      await page.getByTestId('ds-test-connection').click();
      await expect(page.getByTestId('ds-test-result')).toContainText(/connection successful/i, {
        timeout: 30_000,
      });

      const ev = await exerciseSyncLifecycle(page, gdrive, projectId, sourceId, folderId, filename);
      createdFileId = ev.createdFileId;
    } finally {
      if (sourceId) {
        await page.request.delete(`/api/data-source-integrations/${sourceId}`).catch(() => {});
      }
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
});
