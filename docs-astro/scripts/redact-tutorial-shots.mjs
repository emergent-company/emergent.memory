#!/usr/bin/env node
/**
 * redact-tutorial-shots — strip personal data out of the Railway walkthrough
 * figures before they can reach a public docs site.
 *
 * The captures come from a real install, so they carry the installer's Railway
 * account address and workspace name. Docs publish to a public site, so the
 * pictures are redacted here, then `build-figures.mjs` derives the WebP the site
 * serve. Run from `docs-astro/`:
 *
 *   node scripts/shots.mjs            # (re)capture the 4x PNGs
 *   node scripts/redact-tutorial-shots.mjs
 *   node scripts/redact-tutorial-shots.mjs --self-test   # prove the guards bite
 *   node scripts/build-figures.mjs
 *
 * Redaction is deliberately dumb: cover the region with the background colour
 * measured from a clear strip of the same surface. No fake text is drawn, so
 * nothing anyone reads in a figure is invented — a blanked value reads as a
 * blanked value.
 *
 * Coordinates are NATIVE capture pixels (the PNGs are 4x device pixels), and
 * each entry declares the width it was measured at. A recapture at a different
 * scale would move every edge, so a width mismatch aborts rather than silently
 * masking empty panel next to the value — the failure mode this script had when
 * its masks were authored in the 1600px render space while the files were 4x.
 *
 * That width check alone is not enough, because a box in the wrong place is
 * still a *correct* box: uniform after the fill, a plausible surface colour and
 * stable across runs, which is exactly what #929 published. So each box is also
 * sampled from the SOURCE, before anything is composited, and must contain ink
 * (a pixel more than `INK_DELTA` away from the fill). A mask over bare panel
 * aborts, as does a `keep` region — the label or neutral value beside the mask —
 * that compositing changed. `--self-test` shifts a box by ±200 px and requires
 * those guards to reject it, so a guard that has only ever passed is not taken
 * as evidence.
 */
import sharp from 'sharp';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';

const REPO = path.resolve(import.meta.dirname, '../..');
const DIR =
  process.argv.slice(2).find((arg) => !arg.startsWith('--')) ??
  path.join(REPO, 'docs-astro/public/assets/tutorials/railway');

/**
 * How far (per channel) a pixel must sit from the mask's fill colour to count as
 * ink. Glyph edges antialias close to the surface; glyph cores, values and
 * borders sit far above this, while bare panel is zero away from its own fill.
 */
const INK_DELTA = 32;

/**
 * What this script wrote, so a re-run is not mistaken for a leak. The value is
 * `{ config, output }` per file: the op table that produced the published file
 * and the sha256 of that file. When both still match, the empty box is this
 * script's own fill rather than a mask over the wrong pixels, and the ink check
 * is skipped; anything else (a recapture, a hand edit, a shifted box) must prove
 * its boxes cover ink again. Without this, the second `shots:build` run would
 * abort on the first run's output and `npm run shots:build` would stop being
 * repeatable.
 */
const STATE = '.redaction-state.json';

/** The green "Railway connected as <email>." pill, identical on every progress screen. */
const BANNER = {
  width: 3008,
  // "as <email>." — leaving "Railway connected" intact so the banner still reads
  // as a sentence instead of a truncated fragment.
  box: { left: 686, top: 462, width: 490, height: 66 },
  // Pill surface, right of the sentence and clear of every glyph.
  from: { left: 1250, top: 462, width: 1400, height: 66 },
  // The word "Railway", left of the mask, must survive compositing.
  keep: { left: 218, top: 465, width: 184, height: 60 },
};

/**
 * The "Install ID <hex>" row. The same row sits on all four screens that share the
 * progress layout; only its `top` differs, because 07's success card pushes it
 * down the panel. The hex starts at x=354 and the "Install ID" label ends at 335,
 * so the label survives and the value does not.
 */
const installId = (top) => ({
  box: { left: 344, top, width: 886, height: 50 },
  from: { left: 1300, top, width: 1400, height: 50 },
  // "Install ID" — the label, not the value, must still be legible afterwards.
  keep: { left: 154, top: top + 2, width: 184, height: 46 },
});

const OPS = {
  '03-workspace-admin-email.png': {
    width: 2828,
    masks: [
      // The workspace the installer picked ("<name>'s Projects"). The admin email
      // beside it is already the neutral admin@example.com, so it stays.
      {
        box: { left: 110, top: 238, width: 640, height: 64 },
        from: { left: 800, top: 238, width: 1200, height: 64 },
        keep: [
          // The "Admin email" label, below the picker.
          { left: 62, top: 456, width: 276, height: 44 },
          // The neutral placeholder the capture fills in: admin@example.com.
          { left: 112, top: 595, width: 522, height: 56 },
        ],
      },
    ],
  },
  '04-progress-project.png': {
    width: BANNER.width,
    masks: [BANNER, installId(1004)],
  },
  '05-progress-zitadel.png': {
    width: BANNER.width,
    masks: [BANNER, installId(1004)],
  },
  '06-progress-restart.png': {
    width: BANNER.width,
    masks: [BANNER, installId(1004)],
  },
  '07-install-complete.png': { width: 3008, masks: [BANNER, installId(1944)] },
};

/**
 * Figures that carry no personal data, checked by eye against the current
 * captures. Listing them is the point: a recaptured or renamed figure that is in
 * neither table stops the run, so an unclassified picture cannot be published
 * unredacted by omission.
 */
const CLEAN = new Set([
  '01-choose-target.png',
  '02-authorize-railway.png',
  '08-memory-signin.png',
  '09-zitadel-login.png',
  '10-zitadel-password.png',
  '11-2fa-setup.png',
  '12-first-password-change.png',
  '13-memory-projects.png',
]);

const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');
const rectTuple = (r) => [r.left, r.top, r.width, r.height];
const asRects = (keep) => (keep ? [].concat(keep) : []);

/** The modal colour of a rect, and the share of its pixels that carry it. */
async function modalColour(img, rect) {
  const { data, info } = await img
    .extract(rect)
    .raw()
    .toBuffer({ resolveWithObject: true });
  const counts = new Map();
  for (let i = 0; i < data.length; i += info.channels) {
    const key = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const [key, n] = [...counts.entries()].sort((a, b) => b[1] - a[1])[0];
  const total = data.length / info.channels;
  return {
    r: (key >> 16) & 255,
    g: (key >> 8) & 255,
    b: key & 255,
    share: n / total,
  };
}

/** Every pixel of a rect must equal the colour — the proof the mask landed. */
async function assertUniform(img, rect, colour) {
  const { data, info } = await img
    .extract(rect)
    .raw()
    .toBuffer({ resolveWithObject: true });
  for (let i = 0; i < data.length; i += info.channels) {
    if (
      data[i] !== colour.r ||
      data[i + 1] !== colour.g ||
      data[i + 2] !== colour.b
    ) {
      const px = (i / info.channels) % rect.width;
      const row = Math.floor(i / info.channels / rect.width);
      throw new Error(
        `mask left an uncovered pixel at ${rect.left + px},${rect.top + row} ` +
          `(found ${data[i]},${data[i + 1]},${data[i + 2]}, fill ${colour.r},${colour.g},${colour.b})`,
      );
    }
  }
}

/**
 * The box must hold something a mask is for. Sampled from the SOURCE, before the
 * fill is composited: a box shifted off the value covers bare panel, passes
 * `assertUniform` afterwards, and publishes the value — the #929 leak. This is
 * the guard for it.
 */
async function assertMaskCoversInk(src, box, fill, label) {
  const { data, info } = await src
    .extract(box)
    .raw()
    .toBuffer({ resolveWithObject: true });
  const colours = new Set();
  let ink = 0;
  for (let i = 0; i < data.length; i += info.channels) {
    const r = data[i];
    const g = data[i + 1];
    const b = data[i + 2];
    colours.add((r << 16) | (g << 8) | b);
    const delta = Math.max(
      Math.abs(r - fill.r),
      Math.abs(g - fill.g),
      Math.abs(b - fill.b),
    );
    if (delta > INK_DELTA) ink++;
  }
  if (ink === 0 || colours.size < 2) {
    throw new Error(
      `${label}: this box masks nothing (${colours.size} colour(s), none more than ` +
        `${INK_DELTA} away from the fill ${fill.r},${fill.g},${fill.b}) — did the capture ` +
        'scale or the layout change? A mask over bare panel publishes the value beside it. ' +
        'If this capture was already redacted by an older run, re-copy the source PNG.',
    );
  }
}

/**
 * A `keep` rect is text the mask must not touch — a label, the neutral
 * placeholder, a caret. Compare it between the source and the composited file:
 * the "the label survives" property is otherwise verified by eye only.
 */
async function assertKeepIntact(src, out, keep, label) {
  // The composited PNG carries an alpha channel the capture may not: compare RGB
  // pixels, not buffer lengths.
  const [before, after] = await Promise.all([
    src.extract(keep).removeAlpha().raw().toBuffer(),
    out.extract(keep).removeAlpha().raw().toBuffer(),
  ]);
  if (!before.equals(after)) {
    throw new Error(
      `${label}: the keep region ${JSON.stringify(keep)} changed — the mask covers text ` +
        'that must survive (a label or a neutral value). Move the box, not the label.',
    );
  }
}

/** Stable description of an op table entry, for the state file. */
function configHash(ops) {
  return sha256(
    JSON.stringify({
      width: ops.width,
      masks: ops.masks.map((mask) => ({
        box: rectTuple(mask.box),
        from: rectTuple(mask.from),
        keep: asRects(mask.keep).map(rectTuple),
      })),
    }),
  );
}

/**
 * Redact every capture in `dir` that `ops` classifies, in place. Exported so
 * `--self-test` can run it against fixtures instead of the real captures.
 */
export async function redactFigures({
  dir,
  ops: table,
  clean,
  log = console.log,
}) {
  const where = path.relative(REPO, dir) || '.';
  const pngs = (await readdir(dir)).filter((f) => f.endsWith('.png')).sort();
  if (!pngs.length) {
    // A fresh clone carries no captures (they are gitignored), and neither does a
    // docs build that never ran the capture. Nothing to redact, and nothing to
    // publish either.
    log(`no PNG captures in ${where} — nothing to redact`);
    return { total: 0, redacted: 0 };
  }

  const unknown = pngs.filter((f) => !table[f] && !clean.has(f));
  if (unknown.length) {
    throw new Error(
      `unclassified figure(s): ${unknown.join(', ')}. Every capture must either ` +
        'carry a mask op (personal data is on it) or be listed in CLEAN — add it to ' +
        'the table that matches what the picture actually shows.',
    );
  }
  const files = pngs.filter((f) => table[f]);

  const statePath = path.join(dir, STATE);
  let state = {};
  try {
    state = JSON.parse(await readFile(statePath, 'utf8'));
  } catch {
    // No state (first run, or a fresh copy of the captures) means every box has to
    // prove it covers ink. A corrupt file is treated the same way: proving is
    // safe, skipping is not.
  }

  const nextState = {};
  for (const file of files) {
    const ops = table[file];
    const src = path.join(dir, file);
    const { width } = await sharp(src).metadata();
    if (width !== ops.width) {
      throw new Error(
        `${file} is ${width}px wide, but the masks were measured at ${ops.width}px. ` +
          'Re-measure the boxes for this capture scale rather than masking the wrong place.',
      );
    }

    const source = await readFile(src);
    const config = configHash(ops);
    // The file is byte-for-byte what this op table wrote: its boxes are the fill
    // this script composited, so there is no ink left to find (and re-running
    // must stay byte-identical). Any other file has to prove its boxes again.
    const alreadyRedacted =
      state[file]?.config === config && state[file]?.output === sha256(source);

    const masks = [];
    for (const mask of ops.masks) {
      const fill = await modalColour(sharp(src), mask.from);
      if (fill.share < 0.5) {
        throw new Error(
          `${file}: the sample strip ${JSON.stringify(mask.from)} is not one surface ` +
            `(modal colour only ${(fill.share * 100).toFixed(0)}%) — pick a clear strip`,
        );
      }
      if (!alreadyRedacted) {
        await assertMaskCoversInk(
          sharp(src),
          mask.box,
          fill,
          `${file} ${JSON.stringify(mask.box)}`,
        );
      }
      masks.push({ ...mask, fill });
    }

    const out = await sharp(src)
      .composite(
        masks.map(({ box, fill }) => ({
          input: {
            create: {
              ...box,
              channels: 4,
              background: { r: fill.r, g: fill.g, b: fill.b, alpha: 1 },
            },
          },
          left: box.left,
          top: box.top,
        })),
      )
      .png({ compressionLevel: 9 })
      .toBuffer();

    // Read the result back, not the source: this is the file the site publishes.
    // A sharp instance is single-use, so each check needs its own.
    for (const { box, fill } of masks)
      await assertUniform(sharp(out), box, fill);
    for (const mask of masks) {
      for (const keep of asRects(mask.keep))
        await assertKeepIntact(sharp(src), sharp(out), keep, file);
    }

    // Hash the file that was written, not the buffer handed to sharp: the encoder
    // is what the next run reads back.
    await sharp(out).png({ compressionLevel: 9 }).toFile(src);
    nextState[file] = { config, output: sha256(await readFile(src)) };
    const covered = masks.map(
      (m) => `${m.box.left}×${m.box.top}+${m.box.width}+${m.box.height}`,
    );
    log(`${file.padEnd(28)} ${masks.length} mask(s)  ${covered.join(' ')}`);
  }

  const next = JSON.stringify(
    Object.fromEntries(
      Object.entries(nextState).sort(([a], [b]) => a.localeCompare(b)),
    ),
    null,
    2,
  ).concat('\n');
  let current = null;
  try {
    current = await readFile(statePath, 'utf8');
  } catch {
    // Written below.
  }
  if (current !== next) await writeFile(statePath, next);

  log(
    `\nredacted ${files.length} of ${pngs.length} figure(s) in ${where}` +
      ` (${pngs.length - files.length} verified clean)`,
  );
  return { total: pngs.length, redacted: files.length };
}

/* ------------------------------------------------------------------ self-test */

/** Build a synthetic capture with a label at x 80-230 and a value at x 300-470. */
async function makeFixture(dir, name = 'fixture.png') {
  const width = 900;
  const height = 240;
  const PANEL = [26, 30, 36];
  const INK = [235, 238, 241];
  const raw = Buffer.alloc(width * height * 3);
  for (let i = 0; i < raw.length; i += 3) raw.set(PANEL, i);
  const paint = (left, top, w, h) => {
    for (let y = top; y < top + h; y++) {
      for (let x = left; x < left + w; x++) raw.set(INK, (y * width + x) * 3);
    }
  };
  paint(80, 100, 150, 40); // the kept label
  paint(300, 100, 170, 40); // the value the mask must cover
  const file = path.join(dir, name);
  await sharp(raw, { raw: { width, height, channels: 3 } })
    .png()
    .toFile(file);
  return { file, width };
}

/** Ops that mask the synthetic value and keep the synthetic label. */
const fixtureTable = (width, boxLeft = 290) => ({
  'fixture.png': {
    width,
    masks: [
      {
        box: { left: boxLeft, top: 95, width: 190, height: 50 },
        from: { left: 700, top: 95, width: 180, height: 50 },
        keep: { left: 85, top: 95, width: 140, height: 50 },
      },
    ],
  },
});

async function expectFailure(label, fn, pattern) {
  try {
    await fn();
  } catch (err) {
    if (!pattern.test(err.message)) {
      throw new Error(
        `self-test: ${label} failed for the wrong reason: ${err.message}`,
      );
    }
    console.log(`ok   ${label} — ${err.message.split('\n')[0]}`);
    return;
  }
  throw new Error(`self-test: ${label} did NOT fail`);
}

/**
 * A guard that has only ever passed is not evidence. Generate a capture where the
 * declared boxes are right, require the run to pass, then shift one box by ±200 px
 * and require each guard to reject it.
 */
async function selfTest() {
  const dir = await mkdtemp(path.join(tmpdir(), 'redact-shots-'));
  const quiet = () => {};
  try {
    // 1. A correct box passes, and the mask actually lands on the value.
    const { file, width } = await makeFixture(dir);
    const result = await redactFigures({
      dir,
      ops: fixtureTable(width),
      clean: new Set(),
      log: quiet,
    });
    if (result.redacted !== 1) {
      throw new Error(
        `self-test: expected 1 redacted figure, got ${result.redacted}`,
      );
    }
    const redacted = await readFile(file);
    console.log('ok   a declared box over the value passes');

    // 2. Running again is byte-identical, not an empty-box abort: the state file
    //    records that this exact file came from this exact op table.
    await redactFigures({
      dir,
      ops: fixtureTable(width),
      clean: new Set(),
      log: quiet,
    });
    if (!redacted.equals(await readFile(file))) {
      throw new Error('self-test: a second run rewrote the file');
    }
    console.log('ok   a second run with the same table is byte-identical');

    // 3. Shifting the box +200 px lands it on bare panel: "masks nothing".
    let fixture = await makeFixture(dir);
    await expectFailure(
      'a box shifted +200 px off the value aborts',
      () =>
        redactFigures({
          dir,
          ops: fixtureTable(fixture.width, 490),
          clean: new Set(),
          log: quiet,
        }),
      /masks nothing/,
    );

    // 4. Shifting it -200 px lands it on the label the keep rect protects.
    fixture = await makeFixture(dir);
    await expectFailure(
      'a box shifted -200 px over the label aborts',
      () =>
        redactFigures({
          dir,
          ops: fixtureTable(fixture.width, 90),
          clean: new Set(),
          log: quiet,
        }),
      /keep region .* changed/,
    );

    // 5. The other cheap guards still bite: a wrong declared width and an
    //    unclassified capture.
    fixture = await makeFixture(dir);
    await expectFailure(
      'a capture at another scale aborts',
      () =>
        redactFigures({
          dir,
          ops: fixtureTable(fixture.width + 4),
          clean: new Set(),
          log: quiet,
        }),
      /wide, but the masks were measured/,
    );
    await makeFixture(dir);
    await expectFailure(
      'an unclassified capture aborts',
      () => redactFigures({ dir, ops: {}, clean: new Set(), log: quiet }),
      /unclassified figure/,
    );

    console.log('\nself-test passed: the guards fail when they should.');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

const isMain =
  process.argv[1] && path.resolve(process.argv[1]) === import.meta.filename;
if (isMain) {
  if (process.argv.includes('--self-test')) {
    await selfTest();
  } else {
    await redactFigures({ dir: DIR, ops: OPS, clean: CLEAN });
    console.log('next: node scripts/build-figures.mjs');
  }
}
