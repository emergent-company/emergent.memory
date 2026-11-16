import fs from 'node:fs';

/**
 * The hermetic mock harness (`mock-memory.mjs`) is one stateful process shared
 * by every spec file in a run (datasources.config.ts: `workers: 1`,
 * `fullyParallel: false`). It therefore has to be reset, or a spec's empty-list
 * assertion silently depends on which file ran first (#633).
 *
 * The mock exposes one mock-only control route for that, `POST /__mock/reset`,
 * which restores the Data Sources store to its seed (see `dsResetStore` in
 * mock-memory.mjs). These helpers reach the mock directly — the gateway has no
 * route for it, and going through the product's API would be a mock concern
 * leaking into the product's surface.
 */

/**
 * The mock's origin. The Playwright configs select the gateway/mock port pair
 * via `ports.mjs` and record it in `E2E_PORTS_FILE` (the same reservation
 * `run-e2e.sh` boots from), so that file is the single source of truth here too;
 * `MOCK_PORT` is accepted for a direct invocation that exported the pair.
 */
export function mockBaseURL(): string {
  const file = process.env.E2E_PORTS_FILE;
  if (file && fs.existsSync(file)) {
    const port = /^MOCK_PORT=(\d+)$/m.exec(fs.readFileSync(file, 'utf8'))?.[1];
    if (port) return `http://localhost:${port}`;
  }
  const port = process.env.MOCK_PORT;
  if (port) return `http://localhost:${port}`;
  throw new Error(
    'no mock port: run this suite through a config that selects ports (datasources.config.ts) so E2E_PORTS_FILE or MOCK_PORT is set',
  );
}

/** Restore the Data Sources mock store to its seed state. */
export async function resetMockStore(): Promise<void> {
  const url = `${mockBaseURL()}/__mock/reset`;
  const res = await fetch(url, { method: 'POST' });
  if (!res.ok) {
    throw new Error(`mock reset failed: ${res.status} ${res.statusText} (${url})`);
  }
}
