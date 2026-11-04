import { Locator, Page } from '@playwright/test';
import { BASE_URL, getTestUserCredentials, TestUserCredentials } from '../constants/storage';

// The Zitadel authorization endpoint (issuer) redirects to the hosted login
// UI on a separate host. Match either so the helper works across environments.
const LOGIN_ORIGIN = /(?:login|zitadel)\.dev\.emergent-company\.ai/;

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Drive the full OIDC sign-in: /auth/login → /auth/start → Zitadel login form
 * (loginName → Next → password → Next) → gateway callback → app. The session
 * cookie (HttpOnly) lands in the page's context; callers persist it via
 * storageState(). Assumes the test user has NO MFA and no Zitadel account
 * selection prompt on a fresh browser context.
 */
export async function login(page: Page): Promise<void> {
  // Pre-flight: the gateway must already be running (session mode).
  const health = await page.request.get(`${BASE_URL}/api/health`);
  if (!health.ok()) {
    throw new Error(
      `Gateway not reachable at ${BASE_URL}/api/health (HTTP ${health.status()}). ` +
        'Start it first: `task dev` (session mode, see .env).',
    );
  }

  await page.goto(`${BASE_URL}/auth/login`);
  await page.getByRole('link', { name: 'Sign in' }).click();

  await page.waitForURL(LOGIN_ORIGIN, { timeout: 30_000 });

  // The login UI is a SPA; if it server-errors we get a bare JSON body instead
  // of the form. Surface that as a clear, actionable failure.
  const bodyText = await page.evaluate(() => document.body.innerText);
  if (/internal server error/i.test(bodyText)) {
    throw new Error(
      'Zitadel login UI returned "Internal server error". ' +
        'The login host/backend is misconfigured (see the e2e README). ' +
        `Landed on: ${page.url()}`,
    );
  }

  const { email, password } = getTestUserCredentials();

  const loginName = page.locator('input[name="loginName"], input#loginName').first();
  await loginName.waitFor({ state: 'visible', timeout: 30_000 });
  await loginName.fill(email);
  await page.getByRole('button', { name: /next|continue|weiter/i }).first().click();

  const passwordInput = page.locator('input[name="password"], input#password').first();
  await passwordInput.waitFor({ state: 'visible', timeout: 30_000 });
  await passwordInput.fill(password);
  await page.getByRole('button', { name: /next|continue|sign in|weiter|anmelden/i }).first().click();

  const gateway = new RegExp(escapeRegex(BASE_URL));
  const onGateway = (url: URL) =>
    gateway.test(url.origin) && !url.pathname.startsWith('/auth');

  // Zitadel may interpose first-login screens before the callback:
  //   2FA setup (skippable) and a forced password change (not skippable).
  // Settle them until we land back on the gateway.
  for (let i = 0; i < 6; i++) {
    try {
      await page.waitForURL(onGateway, { timeout: 8000 });
      break;
    } catch {
      // not on the gateway yet — handle the interposed screen below
    }
    const skip = page.getByRole('button', { name: 'Skip' });
    if (await skip.isVisible().catch(() => false)) {
      await skip.click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    const oldPw = page.getByRole('textbox', { name: 'Old Password' });
    if (await oldPw.isVisible().catch(() => false)) {
      // Forced password change: Zitadel requires a DIFFERENT new password to
      // clear the flag (re-entering the current one is rejected). Use the
      // one-time rotation value when set, else reuse the current password.
      const nextPassword = process.env.E2E_TEST_USER_NEW_PASSWORD || password;
      await page.getByRole('textbox', { name: 'New Password' }).fill(nextPassword);
      await page.getByRole('textbox', { name: 'Password confirmation' }).fill(nextPassword);
      await oldPw.fill(password);
      await page.getByRole('button', { name: /next|continue/i }).click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    // Post-change confirmation ("Your password was changed successfully") → Next.
    if (await page.getByText(/password was changed successfully/i).isVisible().catch(() => false)) {
      await page.getByRole('button', { name: /next|continue/i }).click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    break; // unknown state — the final waitForURL below will surface it
  }

  await page.waitForURL(onGateway, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');
}

/**
 * Fill one field of the Zitadel hosted-login Angular form and submit it.
 *
 * The form hydrates after the DOM is visible, so an early fill can update the
 * native input without reaching the framework model and the submit button stays
 * disabled. Verify the value and the enabled button, retrying while the SPA
 * settles, before clicking submit.
 */
async function submitZitadelField(page: Page, field: Locator, value: string): Promise<void> {
  await field.waitFor({ state: 'visible', timeout: 30_000 });

  const submit = page.locator('button[type="submit"]').first();
  await submit.waitFor({ state: 'visible', timeout: 30_000 });

  // The form hydrates asynchronously. Filling before hydration can leave the
  // framework model empty (the submit button stays disabled) or, when typing
  // key-by-key, drop the first character. Use an atomic `fill` and verify both
  // the DOM value and the enabled submit button, retrying while the SPA settles.
  for (let attempt = 0; attempt < 6; attempt++) {
    await field.fill(value);
    await page.waitForTimeout(200);
    if ((await field.inputValue().catch(() => '')) !== value) {
      await page.waitForTimeout(250);
      continue;
    }

    const enabled = await page
      .waitForFunction(
        () => {
          const b = document.querySelector('button[type="submit"]');
          return !!b && !(b as HTMLButtonElement).disabled;
        },
        undefined,
        { timeout: 2000 },
      )
      .then(() => true)
      .catch(() => false);
    if (!enabled) {
      await page.waitForTimeout(250);
      continue;
    }

    await submit.click();
    return;
  }

  throw new Error(
    `could not submit the Zitadel field for ${JSON.stringify(value)} — ` +
      'the login form never accepted the value and enabled its submit button',
  );
}

/**
 * Complete the Zitadel hosted-login form (loginName → Next → password → Next)
 * and settle any first-login interstitials (2FA Skip, forced password change).
 *
 * `isDone` is polled after each step and lets the caller define the final
 * destination: the gateway URL for the browser flow, or the captured
 * custom-scheme callback for a native PKCE flow. Assumes the page is already on
 * the Zitadel login UI (i.e. LOGIN_ORIGIN). Unlike `login()` this helper does
 * not know the destination URL, so it works for non-HTTP callback schemes.
 */
export async function completeZitadelLogin(
  page: Page,
  creds: TestUserCredentials,
  isDone: () => boolean | Promise<boolean>,
): Promise<void> {
  // The login UI is a SPA; if it server-errors we get a bare JSON body instead
  // of the form. Surface that as a clear, actionable failure.
  const bodyText = await page.evaluate(() => document.body.innerText).catch(() => '');
  if (/internal server error/i.test(bodyText)) {
    throw new Error(
      'Zitadel login UI returned "Internal server error". ' +
        'The login host/backend is misconfigured (see the e2e README). ' +
        `Landed on: ${page.url()}`,
    );
  }

  const passwordInput = page.locator('input[name="password"], input#password').first();
  const waitForProgress = async (timeoutMs: number): Promise<boolean> =>
    Promise.race([
      passwordInput
        .waitFor({ state: 'visible', timeout: timeoutMs })
        .then(() => true)
        .catch(() => false),
      (async () => {
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
          if (await isDone()) return true;
          await page.waitForTimeout(250);
        }
        return false;
      })(),
    ]);

  // Step 1: loginname. Retry when a submit is swallowed by the SPA (the form
  // can re-render and drop a click even after the button enabled).
  for (let attempt = 0; attempt < 3; attempt++) {
    if (await isDone()) return;
    if (await passwordInput.isVisible().catch(() => false)) break;
    const loginName = page.locator('input[name="loginName"], input#loginName').first();
    if (!(await loginName.isVisible().catch(() => false))) break;
    await submitZitadelField(page, loginName, creds.email);
    if (await waitForProgress(15_000)) break;
  }

  // Step 2: password. It appears for a normal fresh login; tolerate flows that
  // skip it (already authenticated). Retry when an invalid/early submit leaves
  // us on the password form.
  const passwordAppeared = await waitForProgress(20_000);
  if (passwordAppeared && (await passwordInput.isVisible().catch(() => false))) {
    for (let attempt = 0; attempt < 3; attempt++) {
      if (await isDone()) break;
      if (!(await passwordInput.isVisible().catch(() => false))) break;
      await submitZitadelField(page, passwordInput, creds.password);
      const moved = await Promise.race([
        passwordInput
          .waitFor({ state: 'hidden', timeout: 12_000 })
          .then(() => true)
          .catch(() => false),
        (async () => {
          const deadline = Date.now() + 12_000;
          while (Date.now() < deadline) {
            if (await isDone()) return true;
            await page.waitForTimeout(250);
          }
          return false;
        })(),
      ]);
      if (moved) break;
    }
  }

  // Zitadel may interpose first-login screens before the callback:
  //   2FA setup (skippable) and a forced password change (not skippable).
  // Settle them until `isDone` reports the caller's destination was reached.
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (await isDone()) return;

    const skip = page.getByRole('button', { name: 'Skip' });
    if (await skip.isVisible().catch(() => false)) {
      await skip.click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    const oldPw = page.getByRole('textbox', { name: 'Old Password' });
    if (await oldPw.isVisible().catch(() => false)) {
      // Forced password change: Zitadel requires a DIFFERENT new password to
      // clear the flag (re-entering the current one is rejected).
      const nextPassword = process.env.E2E_TEST_USER_NEW_PASSWORD || creds.password;
      await page.getByRole('textbox', { name: 'New Password' }).fill(nextPassword);
      await page.getByRole('textbox', { name: 'Password confirmation' }).fill(nextPassword);
      await oldPw.fill(creds.password);
      await page.getByRole('button', { name: /next|continue/i }).click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    // Post-change confirmation ("Your password was changed successfully") → Next.
    if (await page.getByText(/password was changed successfully/i).isVisible().catch(() => false)) {
      await page.getByRole('button', { name: /next|continue/i }).click();
      await page.waitForLoadState('domcontentloaded');
      continue;
    }
    await page.waitForTimeout(500);
  }

  if (!(await isDone())) {
    throw new Error(
      `Zitadel login did not reach the expected destination within 60s (at ${page.url()}).`,
    );
  }
}
