import { Page } from '@playwright/test';

/**
 * UI-install a bundled blueprint pack by name from the /blueprints gallery.
 * Bundled rows post a hidden input[name="name"] to /blueprints/install, so the
 * form filter below is the precise anchor (the same pack can otherwise appear
 * in more than one section). Waits for the PRG redirect: /blueprints?installed=1,
 * or /blueprints/migrations?migrateMsg=… when a migration drift is detected.
 */
export async function installBlueprint(page: Page, name: string): Promise<void> {
  await page.goto('/blueprints');

  const form = page
    .locator('form[action="/blueprints/install"]')
    .filter({ has: page.locator(`input[name="name"][value="${name}"]`) })
    .first();
  try {
    await form.waitFor({ state: 'visible', timeout: 10_000 });
  } catch (err) {
    throw new Error(`installBlueprint("${name}"): no install form for that pack on /blueprints`, {
      cause: err,
    });
  }

  await form.locator('button[type="submit"]').click();

  // Success: installed → list with ?installed=1; migration drift → /blueprints/migrations.
  const success = (url: URL) =>
    url.pathname === '/blueprints/migrations' ||
    (url.pathname === '/blueprints' && url.searchParams.get('installed') === '1');

  try {
    await page.waitForURL(success, { timeout: 10_000 });
  } catch (err) {
    // Any other landing (?err= flash, error toast, failed install) is a failure:
    // surface the URL plus whatever error copy the page is showing.
    let bodyText = '';
    try {
      bodyText = await page.evaluate(() => document.body.innerText);
    } catch {
      // page may be mid-navigation; the URL in the error below still helps
    }
    const errLine =
      (bodyText.match(/^.*(?:error|failed|couldn't|could not).*$/im) || [])
        .slice(0, 3)
        .join(' | ') || '(no visible error copy)';
    throw new Error(
      `installBlueprint("${name}"): install did not succeed; landed on ${page.url()} — ${errLine}`,
      { cause: err },
    );
  }
}
