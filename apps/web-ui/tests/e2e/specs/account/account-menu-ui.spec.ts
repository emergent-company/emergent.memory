import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Signed-in user's Memory profile (GET /api/user/profile). The gateway only
// maps displayName/firstName/lastName/email — reconstructing the display name
// client-side mirrors profileDisplayNameOf (DisplayName, else first+last).
interface ProfileDto {
  displayName?: string;
  firstName?: string;
  lastName?: string;
  email?: string;
}

async function fetchProfile(page: Page): Promise<ProfileDto> {
  const resp = await page.request.get('/api/user/profile');
  if (!resp.ok()) {
    throw new Error(`GET /api/user/profile failed (HTTP ${resp.status()}): ${await resp.text()}`);
  }
  return (await resp.json()) as ProfileDto;
}

function displayNameOf(p: ProfileDto): string {
  if (p.displayName) return p.displayName.trim();
  return `${p.firstName ?? ''} ${p.lastName ?? ''}`.trim();
}

/**
 * Open the topbar account dropdown. daisyUI reveals .dropdown-content via
 * :focus-within on the wrapper — the trigger is focusable (tabindex=0) so a
 * real .click() focuses it; guard a headless quirk where the click lands but
 * focus does not move by falling back to explicit .focus().
 */
async function openAccountMenu(page: Page) {
  const trigger = page.getByTestId('account-menu-trigger');
  const menu = page.getByRole('menu', { name: 'Account' });
  await trigger.click();
  if (!(await menu.isVisible())) {
    await trigger.focus();
  }
  await expect(menu).toBeVisible();
  return { trigger, menu };
}

test.describe('Account menu', () => {

test('topbar account trigger is avatar-only; dropdown shows name + email identity', async ({
  page,
}) => {
  const profile = await fetchProfile(page);
  const apiName = displayNameOf(profile);
  const apiEmail = (profile.email ?? '').trim();

  await page.goto('/agents');
  await expectAppPage(page, /Agents/);

  // Avatar-only trigger: the account menu opens from a bare avatar button.
  const { trigger, menu } = await openAccountMenu(page);
  await expect(trigger.locator('.avatar')).toHaveCount(1);
  // The name text was dropped from the topbar (commit 0a78ab7); no chevron
  // icon either (removed in add7993).
  await expect(trigger.locator('[class*="lucide--chevron"]')).toHaveCount(0);
  const triggerText = ((await trigger.textContent()) ?? '').trim();
  if (apiName !== '') {
    expect(triggerText).not.toContain(apiName);
  }

  // Menu header + active-account identity row (avatar, bold name, email).
  await expect(menu.getByText('Signed in as', { exact: true })).toBeVisible();
  const nameLine = menu.locator('p.truncate.text-sm.font-semibold').first();
  await expect(nameLine).toBeVisible();
  const menuName = ((await nameLine.textContent()) ?? '').trim();
  expect(menuName).not.toBe('');
  // The trigger must not carry whatever name the menu shows either.
  expect(triggerText).not.toContain(menuName);

  // Email line directly under the name (second <p> in the identity row's
  // text block). The gateway backfills it from the Memory profile when the
  // IdP token omits email (commit 09062ef), so the row shows it whenever the
  // session OR the profile carries one.
  const rowEmail = nameLine.locator('xpath=following-sibling::p[1]');
  if (apiEmail !== '') {
    // Profile email non-empty → the menu must show exactly that email
    // (covers the blank-email backfill regression).
    await expect(rowEmail).toBeVisible();
    await expect(rowEmail).toHaveText(apiEmail);
    const gotEmail = ((await rowEmail.textContent()) ?? '').trim();
    expect(gotEmail).not.toBe(menuName); // identity line, not a name repeat
  } else {
    // Profile email empty: the row shows no email only when the session
    // token also omits it (precedence: session email, else profile email).
    // The E2E user's IdP omits email, so an empty profile email means no
    // email line renders.
    await expect(rowEmail).toHaveCount(0);
  }

  // Menu nav affordances: My Profile links to /profile; Log out present.
  const profileLink = menu.getByRole('link', { name: 'My Profile' });
  await expect(profileLink).toBeVisible();
  await expect(profileLink).toHaveAttribute('href', '/profile');
  await expect(menu.getByRole('button', { name: 'Log out' })).toBeVisible();
});

test('profile identity card second line is the email, not a duplicated name', async ({
  page,
}) => {
  const profile = await fetchProfile(page);
  const apiEmail = (profile.email ?? '').trim();

  await page.goto('/profile');
  await expectAppPage(page, /Profile/);

  // The Profile section identity card (avatar button + name + second line).
  const card = page
    .locator('section')
    .filter({ has: page.getByRole('button', { name: 'Edit profile photo' }) });
  await expect(card).toBeVisible();

  const nameLine = card.locator('p.truncate.text-lg.font-semibold').first();
  await expect(nameLine).toBeVisible();
  const cardName = ((await nameLine.textContent()) ?? '').trim();
  expect(cardName).not.toBe('');

  // Second line of the identity text block (line 2 under the name).
  const secondLine = nameLine.locator('xpath=following-sibling::p[1]');
  if (apiEmail !== '') {
    // Regression: the second line used to duplicate the display name when the
    // profile had no distinct DisplayName — it must be the email now.
    await expect(secondLine).toBeVisible();
    await expect(secondLine).toHaveText(apiEmail);
    const secondText = ((await secondLine.textContent()) ?? '').trim();
    expect(secondText).not.toBe(cardName);
  } else {
    // No profile email → no email line; guard the duplicate-name regression
    // by requiring that any rendered second line differs from the name.
    if ((await secondLine.count()) > 0) {
      const secondText = ((await secondLine.textContent()) ?? '').trim();
      expect(secondText).not.toBe(cardName);
    }
  }
});
});
