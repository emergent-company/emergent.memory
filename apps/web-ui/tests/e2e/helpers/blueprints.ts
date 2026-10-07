import { Page } from '@playwright/test';

// External public repo that now ships the personal-memory pack. Pin the ref so
// the e2e fixture cannot silently change with a future push to the repo's HEAD.
export const PERSONAL_MEMORY_BLUEPRINT_URL =
  'https://github.com/mkucharz/personal-memory-blueprint';
export const PERSONAL_MEMORY_BLUEPRINT_REF = 'v3.0.0';
// The blueprint manifest version declared at that pinned tag
// (packs/personal-memory.yaml `version: "3.0.0"`). The installed-blueprints API
// reports it, so setup can reject a stale pack that predates the pin — matching
// on name alone let an old v2.x silently satisfy the guard.
export const PERSONAL_MEMORY_BLUEPRINT_VERSION = '3.0.0';

// The import backend allows the archive fetch alone up to 60s; give the
// post-submit wait a budget consistent with that (button/form waits stay at 10s).
const IMPORT_TIMEOUT_MS = 90_000;

/** Best-effort visible error copy for a failed blueprint install/import. */
async function visibleErrorText(page: Page): Promise<string> {
  let bodyText = '';
  try {
    bodyText = await page.evaluate(() => document.body.innerText);
  } catch {
    // page may be mid-navigation; callers still surface the URL
  }
  return (
    (bodyText.match(/^.*(?:error|failed|couldn't|could not).*$/im) || [])
      .slice(0, 3)
      .join(' | ') || '(no visible error copy)'
  );
}

/**
 * UI-install a blueprint from a GitHub repository URL via the "Import from
 * GitHub" modal on /blueprints. Mirrors installBlueprint's PRG landing:
 * /blueprints?installed=1, or /blueprints/migrations?migrateMsg=… on migration
 * drift. `opts.ref` pins a branch/tag/commit; `opts.token` supplies a token for
 * a private repo.
 */
export async function importGitHubBlueprint(
  page: Page,
  url: string,
  opts?: { ref?: string; token?: string },
): Promise<void> {
  await page.goto('/blueprints');

  const openBtn = page.locator('[data-testid="blueprint-import-open"]');
  try {
    await openBtn.waitFor({ state: 'visible', timeout: 10_000 });
  } catch (err) {
    throw new Error(`importGitHubBlueprint("${url}"): no Import button on /blueprints`, {
      cause: err,
    });
  }
  await openBtn.click();

  const form = page.locator('form[action="/blueprints/import"]');
  try {
    await form.waitFor({ state: 'visible', timeout: 10_000 });
  } catch (err) {
    throw new Error(`importGitHubBlueprint("${url}"): import form did not open on /blueprints`, {
      cause: err,
    });
  }

  await form.locator('input[name="url"]').fill(url);
  if (opts?.ref) await form.locator('input[name="ref"]').fill(opts.ref);
  if (opts?.token) await form.locator('input[name="token"]').fill(opts.token);

  await form.locator('button[type="submit"]').click();

  // Success: installed → list with ?installed=1; migration drift → /blueprints/migrations.
  const success = (u: URL) =>
    u.pathname === '/blueprints/migrations' ||
    (u.pathname === '/blueprints' && u.searchParams.get('installed') === '1');
  // Failure: the handler PRG-redirects to /blueprints?err=<message> (the legacy
  // sentinel is ?err=1). Match it too so a failed import throws the instant the
  // error landing loads instead of burning the full IMPORT_TIMEOUT_MS waiting on
  // a success URL that will never arrive (#1530).
  const errorLanding = (u: URL) => u.pathname === '/blueprints' && u.searchParams.has('err');

  try {
    await page.waitForURL((u) => success(u) || errorLanding(u), { timeout: IMPORT_TIMEOUT_MS });
  } catch (err) {
    // Timed out with neither landing: surface the URL plus visible error copy.
    throw new Error(
      `importGitHubBlueprint("${url}"): import did not settle; landed on ${page.url()} — ${await visibleErrorText(page)}`,
      { cause: err },
    );
  }

  if (errorLanding(new URL(page.url()))) {
    // Prefer the message the handler carried in ?err=; fall back to page copy.
    const surfaced = new URL(page.url()).searchParams.get('err') || '';
    throw new Error(
      `importGitHubBlueprint("${url}"): import failed; landed on ${page.url()} — ${
        surfaced && surfaced !== '1' ? surfaced : await visibleErrorText(page)
      }`,
    );
  }
}

/**
 * UI-install a blueprint by name from the /blueprints gallery. The Available
 * list is now registry-backed: each row is anchored by
 * `[data-testid="available-blueprint-<name>"]` and its install submit by
 * `[data-testid="blueprint-install-<name>"]` (a form POST to /blueprints/install
 * with a hidden schemaId = blueprint id). Waits for the PRG redirect:
 * /blueprints?installed=1, or /blueprints/migrations?migrateMsg=… when a
 * migration drift is detected.
 */
export async function installBlueprint(page: Page, name: string): Promise<void> {
  await page.goto('/blueprints');

  const row = page.locator(`[data-testid="available-blueprint-${name}"]`);
  try {
    await row.waitFor({ state: 'visible', timeout: 10_000 });
  } catch (err) {
    throw new Error(`installBlueprint("${name}"): no available row on /blueprints`, {
      cause: err,
    });
  }

  await row.locator(`[data-testid="blueprint-install-${name}"]`).click();

  // Success: installed → list with ?installed=1; migration drift → /blueprints/migrations.
  const success = (url: URL) =>
    url.pathname === '/blueprints/migrations' ||
    (url.pathname === '/blueprints' && url.searchParams.get('installed') === '1');

  try {
    await page.waitForURL(success, { timeout: 10_000 });
  } catch (err) {
    // Any other landing (?err= flash, error toast, failed install) is a failure:
    // surface the URL plus whatever error copy the page is showing.
    throw new Error(
      `installBlueprint("${name}"): install did not succeed; landed on ${page.url()} — ${await visibleErrorText(page)}`,
      { cause: err },
    );
  }
}

/**
 * Remove a non-built-in blueprint from the registry via its Available row: find
 * the row, click its `[data-testid^="blueprint-remove-"]` button, then the
 * confirm button inside the confirm dialog. Waits for the PRG landing:
 * /blueprints?removed=1 on success, or /blueprints?err=… (blocked while applied
 * or a global built-in). Built-in blueprints render no remove action, so this
 * helper only applies to user/import blueprints.
 */
export async function removeBlueprint(page: Page, name: string): Promise<void> {
  await page.goto('/blueprints');

  const row = page.locator(`[data-testid="available-blueprint-${name}"]`);
  try {
    await row.waitFor({ state: 'visible', timeout: 10_000 });
  } catch (err) {
    throw new Error(`removeBlueprint("${name}"): no available row on /blueprints`, {
      cause: err,
    });
  }

  const removeBtn = row.locator('[data-testid^="blueprint-remove-"]');
  const testId = await removeBtn.getAttribute('data-testid');
  if (!testId) {
    throw new Error(`removeBlueprint("${name}"): no remove button on the available row`);
  }
  await removeBtn.click();

  // The confirm button lives in the (now-open) confirm dialog, keyed by the same
  // blueprint id as the row's remove button.
  const id = testId.replace('blueprint-remove-', '');
  await page.locator(`[data-testid="blueprint-remove-confirm-${id}"]`).click();

  // Success: removed → list with ?removed=1; blocked → ?err=….
  const success = (u: URL) => u.pathname === '/blueprints' && u.searchParams.get('removed') === '1';
  const errorLanding = (u: URL) => u.pathname === '/blueprints' && u.searchParams.has('err');

  try {
    await page.waitForURL((u) => success(u) || errorLanding(u), { timeout: 10_000 });
  } catch (err) {
    throw new Error(
      `removeBlueprint("${name}"): remove did not settle; landed on ${page.url()} — ${await visibleErrorText(page)}`,
      { cause: err },
    );
  }

  if (errorLanding(new URL(page.url()))) {
    const surfaced = new URL(page.url()).searchParams.get('err') || '';
    throw new Error(
      `removeBlueprint("${name}"): remove failed; landed on ${page.url()} — ${
        surfaced && surfaced !== '1' ? surfaced : await visibleErrorText(page)
      }`,
    );
  }
}
