#!/usr/bin/env node
// ci-contract-check.mjs — the two invariants of #205, as a static gate.
//
// #205 had two halves.
//
// (a) The `datasources` and `share` steps of the one `web-ui` job both booted a
//     gateway on the same pinned port, so the second refused to start. #810
//     removed the pinned default: every hermetic config now selects a free
//     gateway/mock pair from `ports.mjs` and hands the same reservation file to
//     `run-e2e.sh`. That fix is only worth anything if nothing quietly re-pins a
//     port, gives two suites the same lane (and therefore the same reservation
//     file), or breaks the probe. Checks 1 and 2 below hold it down; the probe
//     half is exercised live — a lane's selection must never hand back a port
//     that another running harness already holds.
//
// (b) `Run share specs` is `continue-on-error: true`, so the job is only red if
//     the final `Report` step says so. A suite that failed has to fail the job,
//     and so does one that never ran. Check 3 asserts every softened step has an
//     id `Report` reads, and check 4 asserts `Report` does not accept `skipped`
//     as a pass.
//
// Static: no browser, no gateway, no build. Run it from CI as the
// `Contract: e2e suite gates` step, or locally:
//
//   node scripts/ci-contract-check.mjs
//   node scripts/ci-contract-check.mjs --root /path/to/checkout   (fixtures)
import { readFileSync, readdirSync, unlinkSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const rootFlag = process.argv.indexOf('--root');
const root = path.resolve(
  rootFlag === -1 ? path.join(here, '../../../../..') : process.argv[rootFlag + 1],
);
const e2eDir = path.join(root, 'apps/web-ui/tests/e2e');
const workflowPath = path.join(root, '.github/workflows/web-ui.yml');

// The hermetic suites: every config that boots `run-e2e.sh`. `playwright.config.ts`
// (the live session-mode suite) is not one — it targets a developer's gateway via
// E2E_BASE_URL, so a localhost default there is by design.
const HERMETIC_CONFIGS = [
  'share.config.ts',
  'datasources.config.ts',
  'gateway-smoke.config.ts',
  'mcp-inspect.config.ts',
  'dev-gallery.config.ts',
];

const failures = [];
let checks = 0;
function check(label, fn) {
  checks += 1;
  try {
    const detail = fn();
    console.log(`ok   ${label}${detail ? ` — ${detail}` : ''}`);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.log(`FAIL ${label}\n     ${message.replace(/\n/g, '\n     ')}`);
    failures.push(label);
  }
}
function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function read(file) {
  try {
    return readFileSync(file, 'utf8');
  } catch (error) {
    throw new Error(`cannot read ${file}: ${error.message}`);
  }
}

const configs = Object.fromEntries(
  HERMETIC_CONFIGS.map((name) => [name, read(path.join(e2eDir, name))]),
);

// ── 1. No hermetic config may name a port ────────────────────────────────────
check('hermetic configs pin no port and select through ports.mjs', () => {
  const lanes = [];
  for (const [name, source] of Object.entries(configs)) {
    const numericFallback = /\?\?\s*['"]?\d{2,5}['"]?/.exec(source);
    assert(
      !numericFallback,
      `${name}: numeric port fallback ${JSON.stringify(numericFallback?.[0])} — a pinned default is exactly what made two suites want one port (#810). Select from ports.mjs instead.`,
    );
    const literal = /(?:localhost|127\.0\.0\.1):\d+/.exec(source);
    assert(
      !literal,
      `${name}: literal gateway origin ${JSON.stringify(literal?.[0])} — derive the baseURL from the selected port.`,
    );
    assert(
      source.includes("'ports.mjs'") || source.includes('"ports.mjs"'),
      `${name}: does not invoke ports.mjs — ports are the single source of truth (#810).`,
    );
    const lane = /'--lane',\s*'([^']+)'/.exec(source)?.[1];
    assert(lane, `${name}: passes no --lane to ports.mjs; the lane names the reservation file.`);
    lanes.push([name, lane]);
  }
  const seen = new Map();
  for (const [name, lane] of lanes) {
    assert(
      !seen.has(lane),
      `lane '${lane}' is used by both ${seen.get(lane)} and ${name}; they would share one reservation file and one port pair.`,
    );
    seen.set(lane, name);
  }
  return `${lanes.length} lanes: ${lanes.map(([, lane]) => lane).join(', ')}`;
});

// ── 2. A lane's selection must skip a port another harness holds ─────────────
// The live half of #205(a): after #810 the second suite picks its own pair, and
// the probe is what makes it refuse the first suite's port. Bind a pair as a
// foreign harness would, then assert the next selection neither returns nor
// reuses it.
const { selectPorts } = await import(
  pathToFileURL(path.join(e2eDir, 'ports.mjs')).href
);

function listen(port) {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once('error', reject);
    server.listen({ port, host: '127.0.0.1', exclusive: true }, () => resolve(server));
  });
}
function close(server) {
  return new Promise((resolve) => server.close(resolve));
}

// The CI job sets neither, and a stale export would make both lanes return the
// same explicit pair — the override is for humans, never for CI.
delete process.env.MEMORY_PORT;
delete process.env.MOCK_PORT;

const reservations = [];
function reservation(lane) {
  const file = path.join(tmpdir(), `ci-contract-${lane}-${process.pid}.env`);
  reservations.push(file);
  return file;
}

try {
  const held = await selectPorts({ lane: 'ci-contract-hold', file: reservation('hold') });
  const gateway = await listen(held.gateway);
  const mock = await listen(held.mock);
  try {
    const next = await selectPorts({ lane: 'ci-contract-next', file: reservation('next') });
    check('a lane never selects a port another harness holds', () => {
      assert(
        next.gateway !== held.gateway,
        `selected gateway ${next.gateway} is held by the other harness — the probe did not skip a busy port.`,
      );
      assert(
        next.mock !== held.mock,
        `selected mock ${next.mock} is held by the other harness — the probe did not skip a busy port.`,
      );
      return `held ${held.gateway}/${held.mock}, selected ${next.gateway}/${next.mock}`;
    });
  } finally {
    await close(gateway);
    await close(mock);
  }
} catch (error) {
  checks += 1;
  console.log(`FAIL a lane never selects a port another harness holds\n     ${error.message}`);
  failures.push('port probe');
}

// ── 3 & 4. The job verdict cannot lose a suite ──────────────────────────────
const workflow = read(workflowPath);
const lines = workflow.split('\n');

/** The `web-ui` job body: indent-2 `key:` lines delimit the jobs. */
function jobBody(name) {
  const start = lines.findIndex((line) => line === `  ${name}:`);
  assert(start !== -1, `web-ui.yml has no '${name}' job`);
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i += 1) {
    if (/^ {2}[A-Za-z0-9_-]+:\s*$/.test(lines[i])) {
      end = i;
      break;
    }
  }
  return lines.slice(start, end).join('\n');
}

/** Step blocks of a job body (list items at indent 6). */
function stepBlocks(job) {
  const out = [];
  let current = null;
  for (const line of job.split('\n')) {
    if (/^ {6}- /.test(line)) {
      if (current) out.push(current.join('\n'));
      current = [line];
    } else if (current) {
      current.push(line);
    }
  }
  if (current) out.push(current.join('\n'));
  return out;
}

const job = jobBody('web-ui');
const blocks = stepBlocks(job);
const report = blocks.find((block) => /^ {6}- name: Report$/m.test(block));
assert(report, 'web-ui.yml has no `Report` step');
const verdict = /case "\$2" in\n([\s\S]*?)esac/.exec(report);
assert(verdict, 'the `Report` step has no verdict switch on the step outcome');

const softened = blocks.filter((block) => /^\s*continue-on-error:\s*true\s*$/m.test(block));

check('every continue-on-error step has an id the Report step reads', () => {
  assert(softened.length > 0, 'no continue-on-error steps found — the parser is wrong, not the job');
  const missing = [];
  const ids = [];
  for (const block of softened) {
    const name = /^ {6}- (?:name|uses): (.+)$/m.exec(block)?.[1]?.trim() ?? '<unnamed step>';
    const id = /^\s*id:\s*([A-Za-z0-9_-]+)\s*$/m.exec(block)?.[1];
    if (!id) {
      missing.push(`${name} (no id)`);
      continue;
    }
    ids.push(id);
    if (!report.includes(`steps.${id}.outcome`)) missing.push(`${name} (id ${id} not in Report)`);
  }
  assert(
    missing.length === 0,
    `a softened step that Report does not read is a step that can fail green (#205):\n${missing.join('\n')}`,
  );
  return `${ids.length} softened steps: ${ids.join(', ')}`;
});

check('Report does not accept a step that never ran as a pass', () => {
  assert(
    !/skipped/.test(verdict[1]),
    'the verdict switch is `success|skipped)`: a suite that never ran would read as a pass (#205). Only `success` is a pass.',
  );
  assert(
    /^\s*success\)\s*;;/m.test(verdict[1]),
    'the verdict switch has no plain `success) ;;` arm — the pass condition is not what this check expects.',
  );
  assert(
    /\*\s*\)\s*failed\+=\("\$1"\)/.test(verdict[1]),
    'the verdict switch no longer collects a non-success outcome into `failed`.',
  );
  return 'only `success` passes';
});

for (const file of reservations) {
  try {
    unlinkSync(file);
  } catch {
    // best effort: the file is this run's own temp reservation
  }
}

if (failures.length > 0) {
  console.error(`\nci-contract-check: ${failures.length} of ${checks} checks failed`);
  process.exit(1);
}
console.log(`\nci-contract-check: all ${checks} checks passed`);
