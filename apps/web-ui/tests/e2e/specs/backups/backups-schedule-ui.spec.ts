import { test, expect, type Page } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

// Scheduled backups (gateway/backups.go + gateway/backups.templ):
//
//   POST /backups/schedule         save (Save schedule)  → PRG /backups?schedule_saved=1
//   POST /backups/schedule/delete  delete (Delete schedule) → PRG /backups?schedule_deleted=1
//
// The card lives on /backups and reads its state from the project's one
// schedule (GET /api/v1/projects/:projectId/backup-schedule, 404 → "Not
// configured"). Every assertion targets the card itself, never list counts.
//
// A schedule is project-scoped shared state, so the lifecycle test captures the
// prior configuration and restores it at the end; the validation test only
// submits a rejected expression and changes nothing. Backups may be
// feature-gated off entirely → the beforeEach skips on the "Backups unavailable"
// state, following backups-lifecycle-ui.spec.ts.

// card is the Scheduled backups section (ui.Section heading).
function card(page: Page) {
  return page.locator('section').filter({ has: page.getByRole('heading', { name: 'Scheduled backups' }) });
}

// scheduleForm is the card's save form.
function scheduleForm(page: Page) {
  return card(page).locator('form[action="/backups/schedule"]');
}

// readScheduleForm captures the currently rendered configuration so the
// lifecycle test can restore it after exercising save/delete.
async function readScheduleForm(page: Page) {
  return {
    enabled: await scheduleForm(page).locator('input[name="enabled"]').isChecked(),
    cadence: await scheduleForm(page).locator('select[name="cadence"]').inputValue(),
    cron: await scheduleForm(page).locator('input[name="cron"]').inputValue(),
    retention: await scheduleForm(page).locator('input[name="retentionDays"]').inputValue(),
    chat: await scheduleForm(page).locator('input[name="includeChat"]').isChecked(),
    journal: await scheduleForm(page).locator('input[name="includeJournal"]').isChecked(),
    deleted: await scheduleForm(page).locator('input[name="includeDeleted"]').isChecked(),
  };
}

// fillScheduleForm writes a configuration into the form without submitting.
async function fillScheduleForm(
  page: Page,
  cfg: { enabled: boolean; cadence: string; cron?: string; retention?: string; chat?: boolean; journal?: boolean; deleted?: boolean },
) {
  const form = scheduleForm(page);
  const enabled = form.locator('input[name="enabled"]');
  if (cfg.enabled) {
    await enabled.check();
  } else {
    await enabled.uncheck();
  }
  await form.locator('select[name="cadence"]').selectOption(cfg.cadence);
  if (cfg.cron !== undefined) {
    await form.locator('input[name="cron"]').fill(cfg.cron);
  }
  if (cfg.retention !== undefined) {
    await form.locator('input[name="retentionDays"]').fill(cfg.retention);
  }
  for (const [name, value] of [
    ['includeChat', cfg.chat],
    ['includeJournal', cfg.journal],
    ['includeDeleted', cfg.deleted],
  ] as const) {
    if (value === undefined) continue;
    const box = form.locator(`input[name="${name}"]`);
    if (value) {
      await box.check();
    } else {
      await box.uncheck();
    }
  }
}

test.beforeEach(async ({ page }) => {
  await goto(page, '/backups');
  await expectAppPage(page, /Backups/);
  test.skip(
    (await page.getByText('Backups unavailable', { exact: true }).count()) > 0,
    'backups are feature-gated off in the connected Memory service',
  );
});

test('scheduled backups: save, disable, delete, and restore the prior schedule', async ({ page }) => {
  const hadSchedule = (await page.getByRole('button', { name: 'Delete schedule' }).count()) > 0;
  const prior = hadSchedule ? await readScheduleForm(page) : null;

  // --- save (enabled, every 6 hours, retention 14) ---
  await fillScheduleForm(page, { enabled: true, cadence: '0 */6 * * *', retention: '14' });
  await page.getByRole('button', { name: 'Save schedule' }).click();
  await page.waitForURL(/\/backups\?schedule_saved=1/, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');

  await expect(page.getByText('Backup schedule saved.')).toBeVisible();
  await expect(card(page).getByText('Enabled', { exact: true })).toBeVisible();
  await expect(card(page).getByText(/Next run /)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete schedule' })).toBeVisible();

  // The saved values round-trip through the API into the form.
  const saved = await readScheduleForm(page);
  expect(saved.cadence).toBe('0 */6 * * *');
  expect(saved.retention).toBe('14');
  expect(saved.enabled).toBe(true);

  // --- save as disabled: the same schedule, not running ---
  await fillScheduleForm(page, { enabled: false, cadence: '0 */6 * * *', retention: '14' });
  await page.getByRole('button', { name: 'Save schedule' }).click();
  await page.waitForURL(/\/backups\?schedule_saved=1/, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');
  await expect(card(page).getByText('Disabled', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete schedule' })).toBeVisible();

  // --- delete ---
  await page.getByRole('button', { name: 'Delete schedule' }).click();
  await page.waitForURL(/\/backups\?schedule_deleted=1/, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');
  await expect(page.getByText('Backup schedule deleted.')).toBeVisible();
  await expect(card(page).getByText('Not configured', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete schedule' })).toHaveCount(0);

  // --- restore the prior configuration, if there was one ---
  if (prior) {
    await fillScheduleForm(page, prior);
    await page.getByRole('button', { name: 'Save schedule' }).click();
    await page.waitForURL(/\/backups\?schedule_saved=1/, { timeout: 30_000 });
    await page.waitForLoadState('domcontentloaded');
    await expect(card(page).getByText('Enabled', { exact: true })).toBeVisible();
  }
});

test('scheduled backups: a cron faster than 15 minutes is rejected', async ({ page }) => {
  // Custom cron is the path that can express a sub-interval cadence; the server
  // rejects it and the message arrives through the ?err= flash.
  await fillScheduleForm(page, { enabled: true, cadence: 'custom', cron: '*/5 * * * *' });
  await page.getByRole('button', { name: 'Save schedule' }).click();
  await page.waitForURL(/\/backups\?err=/, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');

  await expect(page.getByText(/minimum interval is 15 minutes/)).toBeVisible();
  // The rejection must not have created or replaced a schedule.
  await expect(card(page).getByText(/Next run |Not configured/).first()).toBeVisible();
});
