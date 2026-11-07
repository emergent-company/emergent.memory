// ports.mjs — single source of truth for the hermetic e2e harness ports.
//
// The self-hosted `memory` pool runs several jobs on shared hosts, and this
// workflow is path-filtered, so two PRs can be in the same suite step at the
// same time on one host. The harness used to boot every suite on a pinned
// default pair (gateway 8097, mock 5301): the second job bound the first job's
// ports and `run-e2e.sh` refused to attach — correct behaviour, wrong port
// assignment, and a merge-blocking red that only a re-run could clear (#810).
//
// Selection is deterministic per GitHub Actions run/job — concurrent runs
// therefore prefer disjoint ranges — and then probed by actually binding each
// candidate, so a crashed sibling's leftover process is skipped instead of
// being mistaken for ours. The chosen pair is written to a file; the Playwright
// config builds its baseURL from that file and `run-e2e.sh` boots from the same
// file, so the two cannot disagree.
//
// Usage:
//   node ports.mjs --lane datasources        # JSON: { gateway, mock, file }
//   node ports.mjs --lane smoke --shell      # MEMORY_PORT=… MOCK_PORT=…
//   node ports.mjs --lane smoke --out FILE   # explicit file path
//
// Env overrides (for humans, never for CI):
//   MEMORY_PORT + MOCK_PORT   use both as-is (written to the file)
//   E2E_PORTS_FILE            use this reservation file
//   E2E_PORT_SEED             deterministic candidate offset (tests)
import { createServer } from 'node:net';
import { existsSync, renameSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

// Both bands sit below the Linux ephemeral range (32768–60999), so the kernel
// does not hand them to outbound connections while the suites run.
const GATEWAY_LO = 18000;
const GATEWAY_SPAN = 6000; // 18000–23999
const MOCK_LO = 24000;
const MOCK_SPAN = 6000; // 24000–29999

function fnv1a(text) {
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

function seedFor(lane) {
  const explicit = process.env.E2E_PORT_SEED;
  if (explicit !== undefined && explicit !== '') {
    const value = Number(explicit);
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new Error(`E2E_PORT_SEED must be a non-negative integer, got ${JSON.stringify(explicit)}`);
    }
    return value;
  }

  const runId = process.env.GITHUB_RUN_ID;
  if (runId) {
    // Run ids are monotonic per repo, so concurrent runs (a delta far smaller
    // than the band width) never share an offset. Folding in the job name keeps
    // two different jobs of one run apart as well.
    const id = Number(runId);
    if (!Number.isSafeInteger(id)) {
      throw new Error(`GITHUB_RUN_ID is not a safe integer: ${JSON.stringify(runId)}`);
    }
    return (id + fnv1a(`${process.env.GITHUB_JOB || ''}:${lane}`)) % GATEWAY_SPAN;
  }

  // No CI identity (local run): pid + time keeps two concurrent terminals apart.
  return (process.pid * 7919 + Date.now()) % GATEWAY_SPAN;
}

function canListen(port) {
  return new Promise((resolve) => {
    const server = createServer();
    server.unref();
    server.once('error', () => resolve(false));
    server.listen({ port, exclusive: true }, () => {
      server.close(() => resolve(true));
    });
  });
}

async function findFree(lo, span, seed) {
  const start = seed % span;
  for (let i = 0; i < span; i += 1) {
    const port = lo + ((start + i) % span);
    if (await canListen(port)) return port;
  }
  throw new Error(`no free port in ${lo}–${lo + span - 1}`);
}

function portsFileFor(lane) {
  const root = process.env.RUNNER_TEMP || tmpdir();
  const run = process.env.GITHUB_RUN_ID || 'local';
  const attempt = process.env.GITHUB_RUN_ATTEMPT || '0';
  // pid keeps two local invocations from sharing a reservation; CI runs differ
  // by run/attempt already, and the pid suffix is harmless there.
  return join(root, `web-ui-e2e-${lane}-${run}-${attempt}-${process.pid}.env`);
}

function readPorts(file) {
  const body = readFileSync(file, 'utf8');
  const gateway = Number(/^MEMORY_PORT=(\d+)$/m.exec(body)?.[1]);
  const mock = Number(/^MOCK_PORT=(\d+)$/m.exec(body)?.[1]);
  if (!Number.isInteger(gateway) || !Number.isInteger(mock)) return null;
  return { gateway, mock };
}

function writePorts(file, gateway, mock) {
  const tmp = `${file}.${process.pid}.tmp`;
  writeFileSync(tmp, `MEMORY_PORT=${gateway}\nMOCK_PORT=${mock}\n`, { mode: 0o600 });
  renameSync(tmp, file);
}

/**
 * Return { gateway, mock, file }. Precedence, in both the configs and the
 * runner: an existing reservation file wins (that is the one source of truth),
 * then an explicit MEMORY_PORT/MOCK_PORT pair, then a fresh probe.
 */
export async function selectPorts({ lane = 'local', file } = {}) {
  const portsFile = file || process.env.E2E_PORTS_FILE || portsFileFor(lane);
  const envGateway = Number(process.env.MEMORY_PORT);
  const envMock = Number(process.env.MOCK_PORT);
  const bothSet = Number.isInteger(envGateway) && Number.isInteger(envMock);

  if (existsSync(portsFile)) {
    const ports = readPorts(portsFile);
    if (!ports) {
      throw new Error(`${portsFile} exists but does not contain MEMORY_PORT/MOCK_PORT`);
    }
    // Drift guard: two sources telling us different ports is always a bug.
    if (process.env.MEMORY_PORT && envGateway !== ports.gateway) {
      throw new Error(`MEMORY_PORT=${process.env.MEMORY_PORT} disagrees with ${portsFile} (${ports.gateway})`);
    }
    if (process.env.MOCK_PORT && envMock !== ports.mock) {
      throw new Error(`MOCK_PORT=${process.env.MOCK_PORT} disagrees with ${portsFile} (${ports.mock})`);
    }
    return { ...ports, file: portsFile };
  }

  if (bothSet) {
    writePorts(portsFile, envGateway, envMock);
    return { gateway: envGateway, mock: envMock, file: portsFile };
  }

  const seed = seedFor(lane);
  const gateway = await findFree(GATEWAY_LO, GATEWAY_SPAN, seed);
  const mock = await findFree(MOCK_LO, MOCK_SPAN, seed);
  writePorts(portsFile, gateway, mock);
  return { gateway, mock, file: portsFile };
}

function option(name) {
  const index = process.argv.indexOf(name);
  return index === -1 ? undefined : process.argv[index + 1];
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const ports = await selectPorts({
      lane: option('--lane') || process.env.E2E_LANE || 'local',
      file: option('--out'),
    });
    if (process.argv.includes('--shell')) {
      process.stdout.write(`MEMORY_PORT=${ports.gateway}\nMOCK_PORT=${ports.mock}\n`);
    } else {
      process.stdout.write(`${JSON.stringify(ports)}\n`);
    }
  } catch (error) {
    console.error(`ports.mjs: ${error instanceof Error ? error.message : String(error)}`);
    process.exit(1);
  }
}
