#!/usr/bin/env node
/**
 * shots — capture a tutorial's screenshots from a live instance.
 *
 * Why a script and not a person with a screenshot tool: a published screenshot
 * must not depend on the operator's machine. The Railway tutorial shipped
 * Polish Zitadel UI because the captures were taken in a Polish browser —
 * Zitadel resolves the login language from `Accept-Language` whenever the
 * account carries no stored preference, so the operator's locale leaked into
 * the published page. Every capture here runs in a browser context pinned to
 * the plan's `locale`, and any page that comes back in another language fails
 * the run instead of being written.
 *
 *     node scripts/shots.mjs scripts/shots/zitadel.plan.json
 *     node scripts/shots.mjs scripts/shots/zitadel.plan.json --only 10-zitadel-password
 *
 * Then, for the docs:   node scripts/port-images.mjs && npm run build
 *
 * A plan is one browser session and an ordered list of steps, because a tutorial
 * is one path through the UI: navigating, typing, clicking and taking a picture
 * at the points the reader needs to see. `--only <shot>` replays the plan up to
 * that shot and stops, which is how a single stale image is refreshed.
 *
 * Nothing here authenticates on its own: credentials come from the plan's
 * `vars`, which name environment variables, so no secret is committed.
 */
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import sharp from 'sharp';
import { chromium } from 'playwright-core';
import { trimDeadSpace } from './lib/frame.mjs';

const REPO = path.resolve(import.meta.dirname, '../..');
const CHROME = process.env.CHROME ?? '/usr/local/bin/chromium';
const OUT = process.env.SHOTS_OUT ?? path.join(REPO, 'docs/site/assets/tutorials/railway');

const args = process.argv.slice(2);
const planPath = args.find((a) => !a.startsWith('--'));
const only = args.includes('--only') ? args[args.indexOf('--only') + 1] : null;
const headed = args.includes('--headed');
if (!planPath) {
  console.error('usage: node scripts/shots.mjs <plan.json> [--only <shot-id>] [--headed]');
  process.exit(2);
}

const plan = JSON.parse(await readFile(planPath, 'utf8'));
const fail = (step, msg) => {
  console.error(`\n  FAILED at step ${step}: ${msg}`);
  process.exit(1);
};

/** `${var}` in a plan value, resolved from an env var named by `vars`. */
const vars = {};
for (const [name, spec] of Object.entries(plan.vars ?? {})) {
  const value = process.env[spec.env];
  if (!value) fail('-', `plan var "${name}" needs env ${spec.env}`);
  vars[name] = value;
}
const expand = (value) =>
  typeof value === 'string'
    ? value.replace(/\$\{(\w+)\}/g, (_, name) => {
        if (!(name in vars)) fail('-', `unknown plan var \${${name}}`);
        return vars[name];
      })
    : value;

const primary = (plan.locale ?? 'en-US').split('-')[0];
const browser = await chromium.launch({ executablePath: CHROME, headless: !headed });
const context = await browser.newContext({
  locale: plan.locale ?? 'en-US',
  timezoneId: plan.timezoneId ?? 'UTC',
  viewport: plan.viewport ?? { width: 1600, height: 827 },
  deviceScaleFactor: plan.deviceScaleFactor ?? 1,
  colorScheme: plan.colorScheme ?? 'dark',
  reducedMotion: 'reduce',
  extraHTTPHeaders: { 'Accept-Language': `${plan.locale ?? 'en-US'},en;q=0.9` },
});
const page = await context.newPage();
page.setDefaultTimeout(20_000);

// Some apps pick their own theme from a stored preference rather than from
// prefers-color-scheme (Zitadel's login reads `cp-theme` and defaults to
// "system"), and apply it after first paint. Pin it before any script runs,
// otherwise the first capture of a flow catches the default theme.
if (plan.themeStorage) {
  const [key, value] = Object.entries(plan.themeStorage)[0];
  await context.addInitScript(
    ([k, v]) => {
      try {
        localStorage.setItem(k, v);
      } catch {
        /* opaque origin */
      }
    },
    [key, value],
  );
}

/** The page must not have resolved a different UI language than the plan. */
async function assertLanguage(step) {
  const lang = await page.evaluate(() => document.documentElement.getAttribute('lang'));
  if (lang && !lang.toLowerCase().startsWith(primary)) {
    fail(step, `page rendered lang="${lang}", plan pins "${plan.locale}" — the capture would ship the operator's locale`);
  }
}

/**
 * A capture must not land mid-theme. Zitadel's login applies its stored theme
 * after the first paint, so a shot taken as soon as the form appears can be the
 * light card on a dark page. Measure what the page actually painted instead of
 * trusting `colorScheme`.
 */
async function assertTheme(step) {
  if ((plan.colorScheme ?? 'dark') !== 'dark') return;
  const lum = await page.evaluate(() => {
    const bg = getComputedStyle(document.body).backgroundColor;
    const [r, g, b] = bg.match(/[\d.]+/g)?.map(Number) ?? [255, 255, 255];
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  });
  if (lum > 0.5) fail(step, `page painted a light background (luminance ${lum.toFixed(2)}) — the theme had not settled`);
}

let masks = [];
let wrote = 0;
// Capture scale for the pictures this run writes, kept beside them in
// `.shots.json` (see the write below). Merged, not replaced, so refreshing one
// shot with `--only` does not lose the scale of the others.
const metaPath = path.join(OUT, '.shots.json');
const scale = plan.deviceScaleFactor ?? 1;
let shotMeta = {};
try {
  shotMeta = JSON.parse(await readFile(metaPath, 'utf8'));
} catch {
  /* first run in this directory */
}
for (const [i, step] of plan.steps.entries()) {
  const label = `#${i + 1}${step.name ? ` ${step.name}` : ''}`;
  if (step.goto) {
    const url = step.goto.startsWith('http') ? step.goto : `${plan.baseUrl}${step.goto}`;
    await page.goto(url, { waitUntil: 'domcontentloaded' });
  }
  if (step.fill) {
    await page.locator(expand(step.fill.selector)).first().fill(expand(step.fill.value));
  }
  // `type` sends real key events one character at a time. Zitadel's v2 login
  // (Angular) only enables its submit button once the field has seen real key
  // events, so `fill`, which sets the value in one assignment, leaves the button
  // disabled forever.
  if (step.type) {
    const el = page.locator(expand(step.type.selector)).first();
    await el.click();
    await el.pressSequentially(expand(step.type.value), { delay: 10 });
  }
  if (step.press) {
    await page.locator(expand(step.press.selector)).first().press(expand(step.press.key));
  }
  if (step.click) {
    await page.locator(expand(step.click.selector)).first().click();
  }
  if (step.waitFor) {
    const { selector, text } = step.waitFor;
    if (selector) await page.locator(expand(selector)).first().waitFor({ state: 'visible' });
    if (text) await page.getByText(expand(text), { exact: false }).first().waitFor();
  }
  if (step.mask) {
    masks = masks.concat([].concat(step.mask).map((s) => page.locator(expand(s))));
  }
  if (step.expect || step.shot) await assertLanguage(label);
  if (step.shot) {
    await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {});
    await page.waitForTimeout(250);
    await assertTheme(label);
  }
  if (step.expect) {
    const body = await page.locator('body').innerText();
    for (const needle of [].concat(expand(step.expect))) {
      if (!body.includes(needle)) {
        fail(label, `expected page text ${JSON.stringify(needle)}\n  got: ${body.replace(/\s+/g, ' ').slice(0, 400)}`);
      }
    }
  }
  if (step.shot) {
    const spec = typeof step.shot === 'string' ? { id: step.shot } : step.shot;
    const file = path.join(OUT, `${spec.id}.png`);
    await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {});
    await page.waitForTimeout(250);
    await assertTheme(label);
    const shotOpts = { animations: 'disabled', caret: 'hide', mask: masks, maskColor: plan.maskColor ?? '#0a0e14' };
    let raw;
    if (spec.clip) {
      // Clip to the subject: the login page pins its theme and language
      // switchers to the viewport corner, and a full-frame capture drags them
      // into the picture where they are chrome the reader never touches.
      const box = await page.locator(expand(spec.clip)).first().boundingBox();
      if (!box) fail(label, `clip selector matched nothing: ${spec.clip}`);
      const pad = spec.pad ?? 16;
      const vp = page.viewportSize();
      raw = await page.screenshot({
        ...shotOpts,
        clip: {
          x: Math.max(0, box.x - pad),
          y: Math.max(0, box.y - pad),
          width: Math.min(vp.width - Math.max(0, box.x - pad), box.width + pad * 2),
          height: Math.min(vp.height - Math.max(0, box.y - pad), box.height + pad * 2),
        },
      });
    } else {
      raw = await page.screenshot(shotOpts);
    }
    const { buffer } = await trimDeadSpace(raw, spec.pad ?? plan.pad);
    await writeFile(file, buffer);
    const meta = await sharp(buffer).metadata();
    // Record the capture scale next to the pictures: the docs need the figure's
    // *CSS* size (what the app drew on screen) to size the card and to cap the
    // zoom at a size the pixels can actually support. A PNG cannot carry that,
    // and guessing from the file name would break the first time a shot is
    // captured at another ratio.
    shotMeta[spec.id] = { width: meta.width, height: meta.height, scale };
    console.log(`  ${spec.id.padEnd(26)} ${meta.width}x${meta.height}  ${path.relative(REPO, file)}`);
    wrote += 1;
    if (only === spec.id) break;
  }
}

await browser.close();
await writeFile(metaPath, JSON.stringify(shotMeta, null, 2) + '\n');
if (only && wrote === 0) {
  console.error(`\n  no shot named "${only}" in ${planPath}`);
  process.exit(1);
}
console.log(`\n  ${wrote} shot(s) written to ${path.relative(REPO, OUT)}`);
