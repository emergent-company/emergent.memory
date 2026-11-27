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
 * Then, for the docs:   npm run shots:build && npm run build
 *
 * A plan is one browser session and an ordered list of steps, because a tutorial
 * is one path through the UI: navigating, typing, clicking and taking a picture
 * at the points the reader needs to see. `--only <shot>` replays the plan up to
 * that shot and stops, which is how a single stale image is refreshed.
 *
 * Nothing here authenticates on its own: credentials come from the plan's
 * `vars`, which name environment variables, so no secret is committed.
 *
 * The browser is a bare Playwright Chromium: no extensions, no profile, no
 * browser chrome. That is why a capture cannot show a password manager's
 * autofill overlay or toolbar badge — a class of leak a hand-taken screenshot
 * from a personal profile has, and the reason a figure is re-shot through this
 * script rather than cropped out of whatever the operator had open.
 *
 * ## Flows that need a human once — `--login` and `--profile`
 *
 * Some flows cannot be driven from a plan alone because a person has to
 * authenticate first: Railway's installer needs a Railway login before the
 * consent screen exists. `--profile <dir>` keeps a browser profile on the
 * capture host, and `--login` opens a *visible* window on that profile for the
 * human to sign in. The session then stays in that profile, so every later run
 * is a plain scripted run with no prompts:
 *
 *     node scripts/shots.mjs --login --profile .profile [--login-done-file /tmp/x]
 *                                                              # sign in, press Enter
 *     node scripts/shots.mjs plan.json --profile .profile      # scripted from here on
 *
 * The profile is a capture-host file, never committed and never transferred:
 * whatever the human signed into stays where they signed in. If the session has
 * expired the run fails loudly at the first page that needed it rather than
 * silently capturing a login form.
 *
 * `--out <dir>` writes into a run directory instead of the docs tree, so a run
 * can be reviewed before it touches the page, and `--review` adds a `review.html`
 * contact sheet to that directory. A failure writes the page it failed on —
 * screenshot, HTML and the labels on screen — into `failures/`, which is what
 * makes a plan iterable without a person describing every screen.
 */
import { access, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { chromium } from 'playwright-core';

const REPO = path.resolve(import.meta.dirname, '../..');

/** Flags that consume the next argument, so it is not mistaken for the plan. */
const VALUE_FLAGS = new Set(['--only', '--out', '--profile', '--channel', '--login-url', '--login-done-file', '--vars-out']);
const argv = process.argv.slice(2);
const flag = (name, fallback = null) => {
  const at = argv.indexOf(name);
  return at === -1 ? fallback : (argv[at + 1] ?? null);
};
const has = (name) => argv.includes(name);
const positional = argv.filter((a, i) => !a.startsWith('--') && !VALUE_FLAGS.has(argv[i - 1]));

const loginMode = has('--login');
const planPath = positional[0] ?? null;
const only = flag('--only');
const headed = has('--headed') || loginMode || Boolean(flag('--profile'));
const profile = flag('--profile');
const review = has('--review');
const trim = !has('--no-trim');
const outFlag = flag('--out');
const varsOut = flag('--vars-out');

if (!loginMode && !planPath) {
  console.error(
    'usage:\n' +
      '  node scripts/shots.mjs <plan.json> [--only <shot-id>] [--out <dir>] [--review]\n' +
      '                                    [--profile <dir>] [--vars-out <file>]\n' +
      '  node scripts/shots.mjs --login --profile <dir> [--login-url <url>] [--login-done-file <path>]',
  );
  process.exit(2);
}

const OUT = outFlag
  ? path.resolve(outFlag)
  : (process.env.SHOTS_OUT ?? path.join(REPO, 'docs-astro/public/assets/tutorials/railway'));
const FAILED = path.join(OUT, 'failures');
const LOGIN_URL = flag('--login-url', 'https://railway.com/login');

/** A device pixel ratio is what makes a figure sharp; say so when one is small. */
const plan = planPath ? JSON.parse(await readFile(planPath, 'utf8')) : null;
const scale = plan?.deviceScaleFactor ?? 1;

let page;
const fail = (step, msg) => {
  const err = new Error(msg);
  err.step = step;
  throw err;
};

/**
 * sharp is only used to trim uniform borders, and it is a native dependency.
 * A capture host that only has to take pictures (a Mac with Chrome) can leave it
 * out, so it is loaded on demand and its absence is not an error.
 */
let sharpCache;
async function sharp() {
  if (sharpCache === undefined) {
    try {
      sharpCache = (await import('sharp')).default;
    } catch {
      sharpCache = null;
    }
  }
  return sharpCache;
}

/** PNG dimensions without a dependency: IHDR carries them at bytes 16 and 20. */
function pngSize(buffer) {
  if (buffer.length < 24 || buffer.readUInt32BE(0) !== 0x89504e47) return null;
  return { width: buffer.readUInt32BE(16), height: buffer.readUInt32BE(20) };
}

async function imageSize(buffer) {
  const png = pngSize(buffer);
  if (png) return png;
  const lib = await sharp();
  if (!lib) return fail('-', 'the capture is not a PNG and sharp is unavailable to measure it');
  const meta = await lib(buffer).metadata();
  return { width: meta.width, height: meta.height };
}

/**
 * `${var}` in a plan value. A var comes from an env var (`env`) or is fixed in
 * the plan (`literal`). The literal exists for values a figure should show but
 * that nothing real should be published for — the tutorial's admin address is
 * `admin@example.com` whether or not the operator's own account is real.
 */
const vars = {};
for (const [name, spec] of Object.entries(plan?.vars ?? {})) {
  if (spec.literal !== undefined) {
    vars[name] = spec.literal;
    continue;
  }
  const value = process.env[spec.env];
  if (!value) fail('-', `plan var "${name}" needs env ${spec.env}`);
  vars[name] = value;
}
/** Collapse runs of whitespace, so a guard matches words rather than DOM splits. */
const flat = (value) => String(value).replace(/\s+/g, ' ').trim();

const expand = (value) =>
  typeof value === 'string'
    ? value.replace(/\$\{(\w+)\}/g, (_, name) => {
        if (!(name in vars)) fail('-', `unknown plan var \${${name}}`);
        return vars[name];
      })
    : value;

// The launch target differs by host: the rig has a bare chromium binary, a Mac
// has Chrome. A channel launch still gets a throwaway profile — Playwright
// passes its own `--user-data-dir` — so the operator's extensions (and their
// password manager) are never loaded.
const launchOpts = { headless: !headed };
if (process.env.CHROME) launchOpts.executablePath = process.env.CHROME;
else if (flag('--channel')) launchOpts.channel = flag('--channel');
else if (process.platform === 'darwin') launchOpts.channel = 'chrome';
else launchOpts.executablePath = '/usr/local/bin/chromium';

const primary = (plan?.locale ?? 'en-US').split('-')[0];
const contextOpts = {
  locale: plan?.locale ?? 'en-US',
  timezoneId: plan?.timezoneId ?? 'UTC',
  viewport: plan?.viewport ?? { width: 1600, height: 827 },
  deviceScaleFactor: scale,
  colorScheme: plan?.colorScheme ?? 'dark',
  reducedMotion: 'reduce',
  extraHTTPHeaders: { 'Accept-Language': `${plan?.locale ?? 'en-US'},en;q=0.9` },
};

let browser = null;
let context;
if (profile) {
  await mkdir(profile, { recursive: true });
  context = await chromium.launchPersistentContext(path.resolve(profile), { ...launchOpts, ...contextOpts });
} else {
  browser = await chromium.launch(launchOpts);
  context = await browser.newContext(contextOpts);
}
const shutdown = async () => {
  await context.close().catch(() => {});
  if (browser) await browser.close().catch(() => {});
};

if (loginMode) {
  page = context.pages()[0] ?? (await context.newPage());
  await page.goto(LOGIN_URL, { waitUntil: 'domcontentloaded' });
  // Two ways to say "I am signed in": press Enter, or create the sentinel file.
  // The file exists because the person signing in and the person driving the run
  // are not always at the same keyboard — an agent on another host can watch a
  // window it cannot type into, and `touch` is the only channel it has.
  const doneFile = flag('--login-done-file');
  await rm(doneFile, { force: true }).catch(() => {});
  console.log(`\n  A browser window is open on ${LOGIN_URL}`);
  console.log('  Sign in (and clear any "verify you are human" check), then press Enter here.');
  if (doneFile) console.log(`  (or, from another host: touch ${doneFile})`);
  console.log('');
  await Promise.race([
    new Promise((resolve) => {
      process.stdin.resume();
      process.stdin.once('data', resolve);
    }),
    (async () => {
      if (!doneFile) return new Promise(() => {});
      for (;;) {
        if (await access(doneFile).then(() => true).catch(() => false)) return;
        await new Promise((r) => setTimeout(r, 1500));
      }
    })(),
  ]);
  // Names only: a cookie value is a credential and does not belong in a log.
  const cookies = await context.cookies();
  const domains = [...new Set(cookies.map((c) => c.domain))].sort();
  console.log(`\n  profile ${path.resolve(profile ?? '.')}`);
  console.log(`  ${cookies.length} cookie(s) across: ${domains.join(', ') || 'none'}`);
  console.log('  Run the plan now; an expired session fails at the page that needed it.\n');
  await shutdown();
  process.exit(0);
}

page = context.pages()[0] ?? (await context.newPage());
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

/**
 * What a failure leaves behind. A plan for a flow nobody has scripted yet is
 * wrong about a selector on the first run; the useful output is not the error
 * message but the page, so the next edit can be made from evidence instead of
 * somebody's description of what they saw.
 */
async function writeEvidence(step, message) {
  await mkdir(FAILED, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const base = path.join(FAILED, `${stamp}-${String(step).replace(/[^a-z0-9]+/gi, '-').slice(0, 40)}`);
  const notes = [`step: ${step}`, `error: ${message}`];
  try {
    notes.push(`url: ${page.url()}`, `title: ${await page.title()}`);
    const labels = await page.evaluate(() =>
      [...document.querySelectorAll('a,button,[role=button],input,select,textarea')]
        .map((el) => {
          const text = (el.innerText || el.value || el.getAttribute('placeholder') || '').trim().replace(/\s+/g, ' ');
          const name = el.getAttribute('name') ? ` name=${el.getAttribute('name')}` : '';
          const id = el.id ? ` #${el.id}` : '';
          const href = el.getAttribute('href') ? ` href=${el.getAttribute('href')}` : '';
          return `${el.tagName.toLowerCase()}${id}${name}${href} :: ${text.slice(0, 60)}`;
        })
        .filter((line) => line.split('::')[1].trim())
        .slice(0, 40),
    );
    notes.push('', 'on screen:', ...labels.map((l) => `  ${l}`));
    await page.screenshot({ path: `${base}.png`, animations: 'disabled' });
    await writeFile(`${base}.html`, await page.content());
  } catch (error) {
    notes.push(`(could not inspect the page: ${error.message})`);
  }
  await writeFile(`${base}.txt`, notes.join('\n') + '\n');
  console.error(`\n  evidence: ${path.relative(process.cwd(), base)}.{png,html,txt}`);
}

/** The contact sheet for a run: every figure, its size, and what it should show. */
async function writeReview(shots, elapsed) {
  const rows = shots
    .map((s) => {
      const css = Math.round(s.width / scale);
      const expect = s.expect.length ? `<br><span class=dim>expected: ${s.expect.map((e) => `<code>${e}</code>`).join(' ')}</span>` : '';
      return `<figure><a href="${s.file}"><img src="${s.file}" loading="lazy" alt="${s.id}"></a>
  <figcaption><b>${s.id}</b><br><span class=dim>${s.width}×${s.height}px · ${css}css · step ${s.step}</span>${expect}</figcaption></figure>`;
    })
    .join('\n');
  const html = `<!doctype html>
<meta charset="utf-8">
<title>Capture review — ${plan.tutorial}</title>
<style>
  :root { color-scheme: dark }
  body { margin: 0; padding: 2rem; background: #0b0e14; color: #cfd6e4;
         font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif }
  h1 { font-size: 1.1rem; margin: 0 0 .25rem }
  p.meta { margin: 0 0 2rem; color: #7d8799; font-size: .8rem }
  .grid { display: grid; gap: 1.5rem; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)) }
  figure { margin: 0; background: #12161f; border: 1px solid #202839; border-radius: .5rem; overflow: hidden }
  figure img { display: block; width: 100%; height: auto; background: #0b0e14 }
  figcaption { padding: .6rem .75rem; font-size: .78rem; border-top: 1px solid #202839 }
  .dim { color: #7d8799 }
  code { background: #1b2130; padding: .05rem .25rem; border-radius: .2rem }
</style>
<h1>${plan.tutorial} — ${shots.length} figure(s)</h1>
<p class="meta">${path.basename(planPath)} · ${plan.baseUrl} · ${plan.viewport?.width}×${plan.viewport?.height} @${scale}x · ${plan.locale} · ${plan.colorScheme} · ${(elapsed / 1000).toFixed(0)}s
</p>
<p class="meta">Click a figure for its full size. Sizes are captured pixels, then the size the app drew on screen (what the docs will show).</p>
<div class="grid">
${rows}
</div>`;
  await writeFile(path.join(OUT, 'review.html'), html);
  console.log(`  review: ${path.relative(process.cwd(), path.join(OUT, 'review.html'))}`);
}

let masks = [];
let wrote = 0;
const shots = [];
let lastExpect = [];
// Capture scale for the pictures this run writes, kept beside them in
// `.shots.json` (see the write below). Merged, not replaced, so refreshing one
// shot with `--only` does not lose the scale of the others.
const metaPath = path.join(OUT, '.shots.json');
let shotMeta = {};
try {
  shotMeta = JSON.parse(await readFile(metaPath, 'utf8'));
} catch {
  /* first run in this directory */
}

/**
 * `--vars-out <file>` keeps what `setVar` read, so a run that fails after an
 * install can be retried without creating a second one. It holds credentials:
 * mode 600, written inside the run directory, never committed.
 */
async function writeVars() {
  await writeFile(path.resolve(varsOut), JSON.stringify(vars, null, 2) + '\n', { mode: 0o600 });
  console.log(`  vars: ${path.relative(process.cwd(), path.resolve(varsOut))} (mode 600)`);
}

const startedAt = Date.now();
// The step being run, so a raw Playwright timeout (a click that never lands, a
// fill on a field that vanished) is attributed to a step and leaves evidence
// instead of printing a one-line message with no page to look at.
let currentStep = null;
try {
  for (const [i, step] of plan.steps.entries()) {
    const label = `#${i + 1}${step.name ? ` ${step.name}` : ''}`;
    currentStep = label;
    // A flow through a third party has two possible shapes, and the plan cannot
    // branch: Railway shows its consent screen on the first run and skips
    // straight to the setup page once the portal holds a session. A step that
    // only applies at one of those places says so with `when.url` and is
    // skipped elsewhere — which is what lets one plan survive both.
    if (step.when?.url && !new RegExp(step.when.url).test(page.url())) {
      console.log(`  skip ${label} (at ${page.url().slice(0, 80)})`);
      continue;
    }
    // A screen that only some deployments show keeps the same URL as the one
    // that replaces it (Zitadel's optional 2FA setup), so the guard is the text
    // on the page, not the address.
    if (step.when?.text) {
      const onScreen = flat(await page.locator('body').innerText());
      if (!onScreen.includes(flat(expand(step.when.text)))) {
        console.log(`  skip ${label} (no ${JSON.stringify(step.when.text)} on screen)`);
        continue;
      }
    }
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
    // A click whose target may not be there. Third-party pages carry a cookie
    // banner (Railway uses Osano) that covers the bottom of the viewport in
    // some regions but not others, so a plan that waits for it fails on the
    // runs that did not get one. Absence is not an error here.
    if (step.clickIfPresent) {
      const spec = step.clickIfPresent;
      const locator = page.locator(expand(spec.selector)).first();
      const found = await locator
        .waitFor({ state: 'visible', timeout: spec.timeout ?? 5_000 })
        .then(() => true)
        .catch(() => false);
      if (found) {
        await locator.click();
        console.log(`  pressed ${String(spec.selector).slice(0, 60)}`);
      }
    }
    // A link with `target=_blank` opens a second tab, and the rest of the plan
    // continues there. Masks belong to the page they were declared on, so they
    // reset: a portal figure that masks the admin email must not try to mask a
    // locator on the app page that replaced it.
    if (step.clickNewPage) {
      const spec = step.clickNewPage;
      const [popup] = await Promise.all([
        context.waitForEvent('page', { timeout: spec.timeout ?? 30_000 }),
        page.locator(expand(spec.selector)).first().click(),
      ]);
      await popup.waitForLoadState('domcontentloaded');
      page = popup;
      page.setDefaultTimeout(20_000);
      masks = [];
      console.log(`  new tab: ${page.url()}`);
    }
    // Read a value off the page into a plan variable. The installer generates
    // the first admin's password and shows it once, so an end-to-end capture
    // cannot carry it in an environment variable: the plan reads it from the
    // page it was printed on and signs in with it.
    // Third-party pages settle with their own timers (a redirect that renders a
    // second form), and there is no element to wait for yet when the point of
    // the step is to find out what appeared.
    if (step.pause) await page.waitForTimeout(step.pause);
    if (step.setVar) {
      const spec = step.setVar;
      const el = page.locator(expand(spec.from.selector)).first();
      const prop = spec.from.prop ?? 'text';
      const value =
        prop === 'value'
          ? await el.inputValue()
          : prop === 'href'
            ? await el.getAttribute('href')
            : (await el.textContent())?.trim();
      if (!value) fail(label, `setVar "${spec.name}" read nothing from ${spec.from.selector}`);
      vars[spec.name] = value;
      // Never the value: half of these are credentials.
      console.log(`  var ${spec.name} <- ${spec.from.selector} (${value.length} chars)`);
    }
    if (step.waitFor) {
      const { selector, text, url } = step.waitFor;
      const timeout = step.waitFor.timeout ?? 20_000;
      if (selector)
        await page
          .locator(expand(selector))
          .first()
          .waitFor({ state: 'visible', timeout })
          .catch(() => fail(label, `selector ${JSON.stringify(selector)} never became visible (at ${page.url()})`));
      // "the page shows this text" — not "some element matching it is
      // visible". getByText also matches every ancestor of the text node, so
      // `.first()` is the page container, which is visible before the text
      // exists: a wait that returns immediately is worse than no wait at all.
      if (text) {
        const needle = flat(expand(text));
        // Whitespace-insensitive: a sentence split across elements comes back
        // from innerText with the break inside it, and the guard is about the
        // words on screen, not how the DOM happened to divide them.
        await page
          .waitForFunction((n) => document.body.innerText.replace(/\s+/g, ' ').includes(n), needle, {
            timeout,
            polling: 1000,
          })
          .catch(() => fail(label, `page never showed ${JSON.stringify(needle)} (at ${page.url()})`));
      }
      // A flow that hands off to another host (an OAuth provider, then a
      // callback) has no stable element to wait for, so the URL is the signal.
      // An array accepts either outcome, which is how a plan works whether or
      // not the session was already signed in.
      if (url) {
        const patterns = [].concat(url);
        await page
          .waitForURL((u) => patterns.some((p) => new RegExp(p).test(u.href)), { timeout })
          .catch(() => fail(label, `url never matched ${patterns.join(' | ')} (at ${page.url()})`));
      }
    }
    // `waitHidden` is the mirror of `waitFor`: some chrome is server-rendered
    // visible and hidden once the page's own script boots (Alpine's `x-show`
    // before it runs), so a shot taken on first paint ships a control the app
    // never shows a reader. Wait for the settled state, do not mask the flash.
    if (step.waitHidden) {
      const { selector, timeout = 20_000 } = step.waitHidden;
      await page
        .locator(expand(selector))
        .first()
        .waitFor({ state: 'hidden', timeout })
        .catch(() => fail(label, `${selector} never went away — the page would ship a flash (at ${page.url()})`));
    }
    if (step.mask) {
      masks = masks.concat([].concat(step.mask).map((s) => page.locator(expand(s))));
    }
    if (step.expect) lastExpect = [].concat(expand(step.expect));
    if (step.expect || step.shot) await assertLanguage(label);
    if (step.expect) {
      const body = flat(await page.locator('body').innerText());
      for (const needle of [].concat(expand(step.expect))) {
        if (!body.includes(flat(needle))) {
          fail(label, `expected page text ${JSON.stringify(needle)}\n  got: ${body.slice(0, 400)}`);
        }
      }
    }
    if (step.shot) {
      const spec = typeof step.shot === 'string' ? { id: step.shot } : step.shot;
      const file = path.join(OUT, `${spec.id}.png`);
      await mkdir(OUT, { recursive: true });
      await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {});
      await page.waitForTimeout(250);
      await assertTheme(label);
      const shotOpts = { animations: 'disabled', caret: 'hide', mask: masks, maskColor: plan.maskColor ?? '#0a0e14' };
      let raw;
      if (spec.clip) {
        // Clip to the subject: the login page pins its theme and language
        // switchers to the viewport corner, and a full-frame capture drags them
        // into the picture where they are chrome the reader never touches.
        //
        // A clip is a selector or a measured `{x, y, width, height}` in CSS
        // pixels. The rect exists for a subject with no element of its own —
        // Zitadel's 2FA screen draws a content column, not a card — and it
        // carries its own padding, so it is not padded again.
        const isRect = typeof spec.clip === 'object';
        const box = isRect
          ? { ...spec.clip }
          : await page.locator(expand(spec.clip)).first().boundingBox();
        if (!box) fail(label, `clip selector matched nothing: ${spec.clip}`);
        const pad = spec.pad ?? (isRect ? 0 : 16);
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
      let buffer = raw;
      if (trim) {
        const lib = await sharp();
        if (lib) {
          const { trimDeadSpace } = await import('./lib/frame.mjs');
          buffer = (await trimDeadSpace(raw, spec.pad ?? plan.pad)).buffer;
        }
      }
      await writeFile(file, buffer);
      const dims = await imageSize(buffer);
      // Record the capture scale next to the pictures: the docs need the figure's
      // *CSS* size (what the app drew on screen) to size the card and to cap the
      // zoom at a size the pixels can actually support. A PNG cannot carry that,
      // and guessing from the file name would break the first time a shot is
      // captured at another ratio.
      shotMeta[spec.id] = { width: dims.width, height: dims.height, scale };
      shots.push({ id: spec.id, file: `${spec.id}.png`, step: i + 1, expect: lastExpect, ...dims });
      console.log(`  ${spec.id.padEnd(26)} ${dims.width}x${dims.height}  ${path.relative(process.cwd(), file)}`);
      wrote += 1;
      if (only === spec.id) break;
    }
  }
} catch (error) {
  if (!error.step && currentStep) error.step = currentStep;
  if (error.step) {
    console.error(`\n  FAILED at step ${error.step}: ${error.message}`);
    await writeEvidence(error.step, error.message);
  } else {
    console.error(`\n  FAILED: ${error.message}`);
  }
  await writeFile(metaPath, JSON.stringify(shotMeta, null, 2) + '\n').catch(() => {});
  // The scraped values survive the failure too: a plan that dies after an
  // install must not need a second install to be retried.
  if (varsOut) await writeVars();
  // A plan that fails late has still banked every figure before the failure,
  // and those are exactly what a reviewer needs to see next to the evidence.
  if (shots.length) {
    try {
      await writeReview(shots, Date.now() - startedAt);
    } catch {
      /* the evidence files are enough */
    }
  }
  await shutdown();
  process.exit(1);
}

await shutdown();
await writeFile(metaPath, JSON.stringify(shotMeta, null, 2) + '\n');
if (varsOut) await writeVars();
if (review && shots.length) await writeReview(shots, Date.now() - startedAt);
if (only && wrote === 0) {
  console.error(`\n  no shot named "${only}" in ${planPath}`);
  process.exit(1);
}
console.log(`\n  ${wrote} shot(s) written to ${path.relative(process.cwd(), OUT)}`);
