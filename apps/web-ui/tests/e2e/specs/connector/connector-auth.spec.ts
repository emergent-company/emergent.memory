import { test, expect } from '@playwright/test';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { completeZitadelLogin } from '../../helpers/auth';

/**
 * Authenticate the same dev Zitadel test user through the memory-connector CLI.
 *
 * The dev Zitadel device flow is disabled, so `auth login` cannot be used;
 * instead this drives the connector's two-step Authorization Code + PKCE flow:
 * `auth start` → Playwright signs in at Zitadel → the custom-scheme callback is
 * captured → `auth complete`. The CLI store is isolated with a temp `--config`.
 */

const EMAIL = process.env.E2E_TEST_USER_EMAIL;
const PASSWORD = process.env.E2E_TEST_USER_PASSWORD;

// Dev defaults overridable per environment.
const SERVER =
  process.env.E2E_CONNECTOR_SERVER || 'https://api.dev.emergent-company.ai';
const CLIENT_ID = process.env.E2E_CONNECTOR_CLIENT_ID || '390138928478289930';
const REDIRECT_URI =
  process.env.E2E_CONNECTOR_REDIRECT || 'com.emergent.memory.connector://callback';

const REPO_ROOT = path.resolve(__dirname, '..', '..', '..', '..');
const CONNECTOR_DIR = path.join(REPO_ROOT, 'connector');

interface RunResult {
  status: number | null;
  stdout: string;
  stderr: string;
}

function runCli(bin: string, configPath: string, args: string[]): RunResult {
  const res = spawnSync(bin, [...args, '--config', configPath], {
    encoding: 'utf8',
    timeout: 60_000,
  });
  return {
    status: res.status,
    stdout: String(res.stdout ?? ''),
    stderr: String(res.stderr ?? ''),
  };
}

function runCliJson<T>(bin: string, configPath: string, args: string[]): T {
  const res = runCli(bin, configPath, args);
  if (res.status !== 0) {
    throw new Error(
      `memory-connector ${args.join(' ')} failed (exit ${res.status}): ${res.stderr || res.stdout}`,
    );
  }
  try {
    return JSON.parse(res.stdout) as T;
  } catch {
    throw new Error(`memory-connector ${args.join(' ')} returned non-JSON output: ${res.stdout}`);
  }
}

/** Build the connector once into the temp dir unless MEMORY_CONNECTOR_BIN is set. */
function resolveConnectorBinary(tmpDir: string): string {
  if (process.env.MEMORY_CONNECTOR_BIN) return process.env.MEMORY_CONNECTOR_BIN;
  const name = process.platform === 'win32' ? 'memory-connector-bin.exe' : 'memory-connector-bin';
  const bin = path.join(tmpDir, name);
  const res = spawnSync('go', ['build', '-o', bin, './cmd/memory-connector'], {
    cwd: CONNECTOR_DIR,
    encoding: 'utf8',
    timeout: 180_000,
  });
  if (res.status !== 0) {
    throw new Error(
      `go build ./cmd/memory-connector failed (exit ${res.status}): ${res.stderr || res.stdout}`,
    );
  }
  return bin;
}

interface AuthStartDoc {
  login_id: string;
  authorize_url: string;
  state: string;
  expires_at: string;
}

interface AuthStatusDoc {
  signed_in: boolean;
  email?: string;
  server?: string;
}

interface ProjectsListDoc {
  schema_version: number;
  server: string;
  projects: unknown[];
}

test.describe('memory-connector auth', () => {
  test('signs in the dev test user through the connector PKCE flow', async ({ page }) => {
    test.skip(
      !EMAIL || !PASSWORD,
      'E2E_TEST_USER_EMAIL / E2E_TEST_USER_PASSWORD not set — connector auth spec skipped',
    );
    test.setTimeout(180_000);

    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'connector-e2e-'));
    const configPath = path.join(tmpDir, 'config.yml');
    let bin = '';

    try {
      bin = resolveConnectorBinary(tmpDir);

      const start = runCliJson<AuthStartDoc>(bin, configPath, [
        'auth',
        'start',
        '--server',
        SERVER,
        '--client-id',
        CLIENT_ID,
        '--redirect-uri',
        REDIRECT_URI,
        '--json',
      ]);
      expect(start.login_id, 'auth start returned no login_id').toBeTruthy();
      expect(new URL(start.authorize_url).protocol).toBe('https:');

      // Capture the custom-scheme callback. Chromium cannot follow a non-http
      // scheme, so the top-level navigation to the callback is aborted — expose
      // it from either the redirect response's Location header or the failed
      // request URL. Attach both listeners *before* navigating.
      let callbackUrl: string | undefined;
      const capture = (url: string | undefined): void => {
        if (url && url.startsWith(REDIRECT_URI) && !callbackUrl) callbackUrl = url;
      };
      page.on('response', (resp) => capture(resp.headers()['location']));
      page.on('requestfailed', (req) => capture(req.url()));

      await page.goto(start.authorize_url, {
        waitUntil: 'domcontentloaded',
        timeout: 45_000,
      });
      await completeZitadelLogin(
        page,
        { email: EMAIL as string, password: PASSWORD as string },
        () => !!callbackUrl,
      );

      // The sign-in form can race the aborted navigation; bound the wait.
      await expect
        .poll(() => callbackUrl, {
          timeout: 30_000,
          message: 'custom-scheme callback was not observed after sign-in',
        })
        .toBeTruthy();

      const callback = new URL(callbackUrl as string);
      const code = callback.searchParams.get('code');
      const state = callback.searchParams.get('state');
      expect(code, 'authorization code missing from callback').toBeTruthy();
      expect(state, 'state in callback does not match auth start').toBe(start.state);

      const completed = runCliJson<AuthStatusDoc>(bin, configPath, [
        'auth',
        'complete',
        '--login-id',
        start.login_id,
        '--code',
        code as string,
        '--state',
        state as string,
        '--server',
        SERVER,
        '--json',
      ]);
      expect(completed.signed_in).toBe(true);
      expect(completed.email).toBe(EMAIL);

      const statusRes = runCli(bin, configPath, ['auth', 'status', '--json']);
      expect(statusRes.status, statusRes.stderr).toBe(0);
      const status = JSON.parse(statusRes.stdout) as AuthStatusDoc;
      expect(status.signed_in).toBe(true);
      expect(status.email).toBe(EMAIL);

      // projects list must return a valid (possibly empty) array, not an error.
      const projectsRes = runCli(bin, configPath, ['projects', 'list', '--json']);
      expect(projectsRes.status, projectsRes.stderr).toBe(0);
      const projects = JSON.parse(projectsRes.stdout) as ProjectsListDoc;
      expect(Array.isArray(projects.projects)).toBe(true);
    } finally {
      // Never touch the real store: logout the temp account, then drop the dir.
      if (bin) {
        runCli(bin, configPath, ['auth', 'logout', '--server', SERVER]);
      }
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
  });
});
