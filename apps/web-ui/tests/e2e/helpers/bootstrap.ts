import { Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { BOOTSTRAP_FILE } from '../constants/storage';

export interface BootstrapState {
  orgId: string;
  projectId: string;
  orgName: string;
  projectName: string;
}

/**
 * Create an org via POST /api/orgs. The page's context carries the session
 * cookie, so the gateway resolves the per-session memory token + tenant.
 */
export async function createOrg(page: Page, name: string): Promise<string> {
  const resp = await page.request.post('/api/orgs', { data: { name } });
  if (!resp.ok()) {
    throw new Error(`createOrg failed (HTTP ${resp.status()}): ${await resp.text()}`);
  }
  const org = await resp.json();
  return org.id as string;
}

/** Create a project in an org (also activates it in the session). */
export async function createProject(
  page: Page,
  orgId: string,
  name: string,
): Promise<string> {
  const resp = await page.request.post('/api/projects', {
    data: { name, orgId },
  });
  if (!resp.ok()) {
    throw new Error(`createProject failed (HTTP ${resp.status()}): ${await resp.text()}`);
  }
  const project = await resp.json();
  return project.id as string;
}

/**
 * Configure a project provider via the PRG form (POST /settings/providers/config).
 * Uses an official-API provider (deepseek/google) so there is no base_url
 * probe/test gate; the backend upserts the config and redirects on success.
 */
export async function configureProvider(
  page: Page,
  provider: 'deepseek' | 'google' | 'openai',
  apiKey: string,
  baseUrl?: string,
): Promise<void> {
  const form: Record<string, string> = { provider, api_key: apiKey };
  if (baseUrl) form.base_url = baseUrl;
  const resp = await page.request.post('/settings/providers/config', { form });
  // A failed save re-renders the form with an error at HTTP 200 (not a redirect),
  // so check the body, not just the status.
  const body = await resp.text();
  if (!resp.ok() || /couldn't save provider|could not save provider/i.test(body)) {
    const m = body.match(/could not save provider[^<]*/i) || body.match(/couldn't save provider[^<]*/i);
    throw new Error(`configureProvider failed (HTTP ${resp.status()}): ${m ? m[0].trim() : 'unknown'}`);
  }
}

/**
 * Live-validatable provider for dev e2e: the OpenAI-compatible ("openai") slot
 * pointed at the dev LiteLLM proxy. The memory backend live-tests credentials on
 * save (catalog sync + a real generate call), so a placeholder key is rejected.
 * These env vars carry the real callable creds — the same names the scenario
 * suite uses (see tests/e2e/.env.e2e.example).
 */
export const LIVE_PROVIDER = 'openai' as const;
export const LIVE_PROVIDER_API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
export const LIVE_PROVIDER_BASE_URL =
  process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
/** Already-prefixed "provider/model" catalog value. */
export const LIVE_PROVIDER_MODEL =
  process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';

/** True when the env carries a key the memory backend can live-validate. */
export function hasLiveProviderCreds(): boolean {
  return Boolean(LIVE_PROVIDER_API_KEY);
}

/**
 * Upsert the live provider on the page's active project. Throws like
 * `configureProvider`; callers skip gracefully when the save is rejected or the
 * env has no credentials.
 */
export function configureLiveProvider(page: Page): Promise<void> {
  return configureProvider(page, LIVE_PROVIDER, LIVE_PROVIDER_API_KEY, LIVE_PROVIDER_BASE_URL);
}

/** Persist the bootstrap state so globalTeardown can delete the org. */
export function writeBootstrap(state: BootstrapState): void {
  fs.mkdirSync(path.dirname(BOOTSTRAP_FILE), { recursive: true });
  fs.writeFileSync(BOOTSTRAP_FILE, JSON.stringify(state, null, 2));
}

/** Read the bootstrap state (used by teardown and by specs needing ids). */
export function readBootstrap(): BootstrapState | null {
  if (!fs.existsSync(BOOTSTRAP_FILE)) return null;
  return JSON.parse(fs.readFileSync(BOOTSTRAP_FILE, 'utf8')) as BootstrapState;
}
