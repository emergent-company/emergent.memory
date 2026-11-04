import path from 'node:path';

/**
 * Base URL of the running gateway. Defaults to the local gateway (localhost);
 * override with E2E_BASE_URL when targeting a remote/dev environment whose
 * Zitadel OIDC redirect_uri + PUBLIC_BASE_URL are bound to that origin.
 */
export const BASE_URL =
  process.env.E2E_BASE_URL || 'http://localhost:8095';

/** Where the setup project persists the signed-in session (gitignored). */
export const STORAGE_STATE = path.join(__dirname, '..', '.auth', 'state.json');

/** Where the setup project records the created org/project ids for teardown. */
export const BOOTSTRAP_FILE = path.join(
  __dirname,
  '..',
  'test-results',
  '.bootstrap.json',
);

export interface TestUserCredentials {
  email: string;
  password: string;
}

export function getTestUserCredentials(): TestUserCredentials {
  const email = process.env.E2E_TEST_USER_EMAIL;
  const password = process.env.E2E_TEST_USER_PASSWORD;
  if (!email || !password) {
    throw new Error(
      'E2E_TEST_USER_EMAIL and E2E_TEST_USER_PASSWORD must be set. ' +
        'Copy tests/e2e/.env.e2e.example → .env.e2e and fill in a Zitadel test user.',
    );
  }
  return { email, password };
}
