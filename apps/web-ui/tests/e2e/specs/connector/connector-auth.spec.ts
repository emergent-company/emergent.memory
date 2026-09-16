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

// __dirname = apps/web-ui/tests/e2e/specs/connector — six segments to the repo
// root (connector → specs → e2e → tests → web-ui → apps → root).
const REPO_ROOT = path.resolve(__dirname, '..', '..', '..', '..', '..', '..');
const CONNECTOR_DIR = path.join(REPO_ROOT, 'apps', 'connector.linux');

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

// A cold `go build` can take minutes, so the build budget and the Playwright
// test budget are tied together: the test must outlive the build it triggers.
const BUILD_TIMEOUT_MS = 600_000;
const TEST_TIMEOUT_MS = BUILD_TIMEOUT_MS + 180_000; // build + sign-in/browser budget
const CACHE_DIR = path.join(os.tmpdir(), 'memory-e2e');
const CACHE_BIN = path.join(
  CACHE_DIR,
  process.platform === 'win32' ? 'memory-connector-bin.exe' : 'memory-connector-bin',
);
const CACHE_STAMP = `${CACHE_BIN}.stamp`;
const CACHE_LOCK = `${CACHE_BIN}.lock`;
const LOCK_WAIT_TIMEOUT_MS = BUILD_TIMEOUT_MS + 60_000;

const sleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Change detector for the connector module: newest source mtime + file count.
 * The cache is keyed by this, so a binary built from an older revision (branch
 * switch, `git pull`, local edit) is never reused.
 */
function sourceFingerprint(): string {
  let newest = 0;
  let count = 0;
  const walk = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === '.git' || entry.name === 'node_modules') continue;
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.isFile()) {
        count += 1;
        const { mtimeMs } = fs.statSync(full);
        if (mtimeMs > newest) newest = mtimeMs;
      }
    }
  };
  try {
    walk(CONNECTOR_DIR);
  } catch {
    return 'unknown';
  }
  return `${newest}:${count}`;
}

function isCacheValid(fingerprint: string): boolean {
  if (!fs.existsSync(CACHE_BIN)) return false;
  try {
    return fs.readFileSync(CACHE_STAMP, 'utf8').trim() === fingerprint;
  } catch {
    return false;
  }
}

/** A lock older than any plausible build belongs to a crashed process. */
function isLockStale(): boolean {
  try {
    return Date.now() - fs.statSync(CACHE_LOCK).mtimeMs > LOCK_WAIT_TIMEOUT_MS;
  } catch {
    return false;
  }
}

/**
 * Build into a unique staging dir and publish with an atomic rename: a reader
 * never observes a partial binary, and a failed/timed-out build never replaces
 * a good one (the stamp is only written after the rename succeeds).
 */
function buildConnectorBinary(): void {
  const staging = fs.mkdtempSync(path.join(CACHE_DIR, 'build-'));
  try {
    const staged = path.join(staging, path.basename(CACHE_BIN));
    const res = spawnSync('go', ['build', '-o', staged, './cmd/memory-connector'], {
      cwd: CONNECTOR_DIR,
      encoding: 'utf8',
      timeout: BUILD_TIMEOUT_MS,
    });
    if (res.status !== 0) {
      const detail = res.signal
        ? `killed by ${res.signal}`
        : res.error?.message
          ? `spawn error: ${res.error.message}`
          : res.stderr || res.stdout;
      throw new Error(`go build ./cmd/memory-connector failed (exit ${res.status}): ${detail}`);
    }
    fs.renameSync(staged, CACHE_BIN);
  } finally {
    fs.rmSync(staging, { recursive: true, force: true });
  }
}

type LockAcquisition = { fd: number } | { cached: true };

/**
 * Serialize cache fills across processes: the `wx` open is the critical
 * section. The winner builds, racing invocations wait for the stamp (or take
 * over a stale lock), so no two processes write the cache path at once.
 */
async function acquireCacheLock(fingerprint: string): Promise<LockAcquisition> {
  const deadline = Date.now() + LOCK_WAIT_TIMEOUT_MS;
  for (;;) {
    try {
      return { fd: fs.openSync(CACHE_LOCK, 'wx') };
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== 'EEXIST') throw err;
      if (Date.now() > deadline) {
        throw new Error('timed out waiting for another process to fill the connector binary cache');
      }
      await sleep(250);
      if (isCacheValid(fingerprint)) return { cached: true };
      if (isLockStale()) fs.rmSync(CACHE_LOCK, { force: true });
    }
  }
}

/**
 * Resolve the connector binary: `MEMORY_CONNECTOR_BIN` wins, else reuse a
 * cached build whose fingerprint matches the connector sources, else build once
 * into the cache dir. The binary is cached outside the per-test temp dir so
 * concurrent/serial suites don't each pay for a cold `go build`.
 */
async function resolveConnectorBinary(): Promise<string> {
  const override = process.env.MEMORY_CONNECTOR_BIN;
  if (override) return override;

  fs.mkdirSync(CACHE_DIR, { recursive: true });
  const fingerprint = sourceFingerprint();
  if (isCacheValid(fingerprint)) return CACHE_BIN;

  const lock = await acquireCacheLock(fingerprint);
  if ('cached' in lock) return CACHE_BIN;

  try {
    // Re-check under the lock: a racing builder may have just finished.
    if (isCacheValid(sourceFingerprint())) return CACHE_BIN;
    buildConnectorBinary();
    fs.writeFileSync(CACHE_STAMP, `${fingerprint}\n`);
    return CACHE_BIN;
  } finally {
    fs.closeSync(lock.fd);
    fs.rmSync(CACHE_LOCK, { force: true });
  }
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
    test.setTimeout(TEST_TIMEOUT_MS);

    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'connector-e2e-'));
    const configPath = path.join(tmpDir, 'config.yml');
    let bin = '';

    try {
      bin = await resolveConnectorBinary();

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
