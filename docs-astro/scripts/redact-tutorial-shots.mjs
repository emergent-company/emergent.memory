#!/usr/bin/env node
/**
 * redact-tutorial-shots — strip personal data out of the Railway walkthrough
 * figures before they can reach a public docs site.
 *
 * The captures come from a real install, so they carry the installer's Railway
 * account address and workspace name. Docs publish to a public site, so the
 * pictures are redacted here, then `port-images.mjs` derives the WebP both sites
 * serve. Run from `docs-astro/`:
 *
 *   node scripts/shots.mjs            # (re)capture the 4x PNGs
 *   node scripts/redact-tutorial-shots.mjs
 *   node scripts/port-images.mjs
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
 */
import sharp from 'sharp';
import path from 'node:path';
import { readdir } from 'node:fs/promises';

const REPO = path.resolve(import.meta.dirname, '../..');
const DIR = process.argv[2] ?? path.join(REPO, 'docs/site/assets/tutorials/railway');

/** The green "Railway connected as <email>." pill, identical on every progress screen. */
const BANNER = {
  width: 3008,
  // "as <email>." — leaving "Railway connected" intact so the banner still reads
  // as a sentence instead of a truncated fragment.
  box: { left: 686, top: 462, width: 490, height: 66 },
  // Pill surface, right of the sentence and clear of every glyph.
  from: { left: 1250, top: 462, width: 1400, height: 66 },
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
      },
    ],
  },
  '04-progress-project.png': { width: BANNER.width, masks: [BANNER, installId(1004)] },
  '05-progress-zitadel.png': { width: BANNER.width, masks: [BANNER, installId(1004)] },
  '06-progress-restart.png': { width: BANNER.width, masks: [BANNER, installId(1004)] },
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

/** The modal colour of a rect, and the share of its pixels that carry it. */
async function modalColour(img, rect) {
  const { data, info } = await img.extract(rect).raw().toBuffer({ resolveWithObject: true });
  const counts = new Map();
  for (let i = 0; i < data.length; i += info.channels) {
    const key = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const [key, n] = [...counts.entries()].sort((a, b) => b[1] - a[1])[0];
  const total = data.length / info.channels;
  return { r: (key >> 16) & 255, g: (key >> 8) & 255, b: key & 255, share: n / total };
}

/** Every pixel of a rect must equal the colour — the proof the mask landed. */
async function assertUniform(img, rect, colour) {
  const { data, info } = await img.extract(rect).raw().toBuffer({ resolveWithObject: true });
  for (let i = 0; i < data.length; i += info.channels) {
    if (data[i] !== colour.r || data[i + 1] !== colour.g || data[i + 2] !== colour.b) {
      const px = (i / info.channels) % rect.width;
      const row = Math.floor(i / info.channels / rect.width);
      throw new Error(
        `mask left an uncovered pixel at ${rect.left + px},${rect.top + row} ` +
          `(found ${data[i]},${data[i + 1]},${data[i + 2]}, fill ${colour.r},${colour.g},${colour.b})`,
      );
    }
  }
}

const pngs = (await readdir(DIR)).filter((f) => f.endsWith('.png')).sort();
if (!pngs.length) {
  // A fresh clone carries no captures (they are gitignored), and neither does a
  // docs build that never ran the capture. Nothing to redact, and nothing to
  // publish either.
  console.log(`no PNG captures in ${path.relative(REPO, DIR)} — nothing to redact`);
  process.exit(0);
}
const unknown = pngs.filter((f) => !OPS[f] && !CLEAN.has(f));
if (unknown.length) {
  throw new Error(
    `unclassified figure(s): ${unknown.join(', ')}. Every capture must either ` +
      'carry a mask op (personal data is on it) or be listed in CLEAN — add it to ' +
      'the table that matches what the picture actually shows.',
  );
}
const files = pngs.filter((f) => OPS[f]);

for (const file of files) {
  const ops = OPS[file];
  const src = path.join(DIR, file);
  const { width } = await sharp(src).metadata();
  if (width !== ops.width) {
    throw new Error(
      `${file} is ${width}px wide, but the masks were measured at ${ops.width}px. ` +
        'Re-measure the boxes for this capture scale rather than masking the wrong place.',
    );
  }

  const masks = [];
  for (const mask of ops.masks) {
    const fill = await modalColour(sharp(src), mask.from);
    if (fill.share < 0.5) {
      throw new Error(
        `${file}: the sample strip ${JSON.stringify(mask.from)} is not one surface ` +
          `(modal colour only ${(fill.share * 100).toFixed(0)}%) — pick a clear strip`,
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
  for (const { box, fill } of masks) await assertUniform(sharp(out), box, fill);

  await sharp(out).toFile(src);
  const covered = masks.map((m) => `${m.box.left}×${m.box.top}+${m.box.width}+${m.box.height}`);
  console.log(`${file.padEnd(28)} ${masks.length} mask(s)  ${covered.join(' ')}`);
}

console.log(
  `\nredacted ${files.length} of ${pngs.length} figure(s) in ${path.relative(REPO, DIR)}` +
    ` (${pngs.length - files.length} verified clean)`,
);
console.log('next: node scripts/port-images.mjs');
