#!/usr/bin/env node
/**
 * redact-tutorial-shots — strip personal data out of the Railway walkthrough
 * screenshots before they can reach a public docs site.
 *
 * The originals in `emergent.memory.infra/docs/guides/railway-install/assets/`
 * were captured from a real install, so they carry the installer's account
 * email (and, on the OAuth consent page, the account avatar). Docs publish to a
 * public site, so the images that reach `docs/site/assets/` are redacted here.
 *
 * Redaction is deliberately dumb: cover the region with the colour sampled from
 * the same UI surface, or crop the region away entirely. No fake text is drawn,
 * so nothing anyone reads on the page is invented.
 *
 * Run from `docs-astro/`:  node scripts/redact-tutorial-shots.mjs <originals-dir>
 * Then:                    node scripts/port-images.mjs
 */
import sharp from 'sharp';
import { mkdir, readdir } from 'node:fs/promises';
import path from 'node:path';

const SRC = process.argv[2];
if (!SRC) {
  console.error('usage: node scripts/redact-tutorial-shots.mjs <originals-dir>');
  process.exit(2);
}

const REPO = path.resolve(import.meta.dirname, '../..');
const OUT = path.join(REPO, 'docs/site/assets/tutorials/railway');
const WIDTH = 1600;

/**
 * Per-image operations in 1600px-wide coordinates. `pad` widens the dead-space
 * trim for one picture (default 32px). `crop` runs first, so a
 * mask's `box` and `sample` are in the cropped image's coordinate space; the
 * fill is sampled from `sample`, a point on the same UI surface clear of text.
 */
const OPS = {
  // The consent card is the subject. Cropping above it removes both the
  // account avatar and the "Signed in as <email>" line in one step, and cropping
  // below it drops Railway's marketing footer.
  '02-authorize-railway.png': { crop: { left: 0, top: 238, width: 1600, height: 574 } },
  // Cropping above the form removes the "Railway connected as <email>" banner
  // in one step, and Railway's marketing footer goes with the bottom crop.
  '03-workspace-admin-email.png': {
    // The banner (which named the installer's account) goes with the crop; the
    // two form fields keep their borders but lose their values.
    crop: { left: 0, top: 93, width: 1600, height: 734 },
    masks: [
      { box: { left: 597, top: 32, width: 412, height: 21 }, sample: { x: 900, y: 40 } }, // workspace value
      { box: { left: 597, top: 87, width: 412, height: 20 }, sample: { x: 900, y: 97 } }, // admin email value
    ],
  },
  // The sign-in screen is one small card in a large empty field. Cropped tight
  // it is ~310px wide; the page caps how far a screenshot may be scaled up, so
  // it renders near its own size rather than as a speck in a black panel.
  '08-memory-signin.png': { pad: 40 },
  // Every progress screen repeats the same green banner naming the installer's
  // Railway account. The address is blanked everywhere it appears — with the
  // leading "as", so the banner still reads as a sentence ("Railway connected")
  // instead of a dangling fragment. The fill is the pill's own colour, sampled
  // clear of the text.
  '04-progress-project.png': {
    masks: [{ box: { left: 670, top: 66, width: 80, height: 22 }, sample: { x: 900, y: 78 } }],
  },
  '05-progress-zitadel.png': {
    masks: [{ box: { left: 670, top: 66, width: 80, height: 22 }, sample: { x: 900, y: 78 } }],
  },
  '06-progress-restart.png': {
    masks: [{ box: { left: 670, top: 66, width: 80, height: 22 }, sample: { x: 900, y: 78 } }],
  },
  '07-install-complete.png': {
    masks: [
      { box: { left: 670, top: 66, width: 80, height: 22 }, sample: { x: 900, y: 78 } }, // banner email
      { box: { left: 594, top: 234, width: 416, height: 32 }, sample: { x: 900, y: 250 } }, // admin email row
    ],
  },
  // The password step shows the account as an avatar pill; only the address is
  // blanked, so the pill still reads as "the account you just typed".
  // Zitadel pads its pages with a huge empty field and pins a "powered by
  // ZITADEL" strip to the bottom-left, which would hold the trim box open
  // around the whole viewport. The strip is page furniture rather than part of
  // the step, so it goes before the trim measures the form.
  '09-zitadel-login.png': { crop: { left: 0, top: 0, width: 1600, height: 700 } },
  '10-zitadel-password.png': {
    crop: { left: 0, top: 0, width: 1600, height: 700 },
    masks: [{ box: { left: 698, top: 223, width: 82, height: 19 }, sample: { x: 950, y: 230 } }],
  },
  '11-zitadel-2fa-skip.png': {
    // Chrome's "Save login?" bubble sits over the top-right corner from this
    // step on, so the frame is cropped to end before it; the ZITADEL footer
    // strip at the bottom-left goes too, or it holds the trim box open across
    // the whole viewport.
    crop: { left: 0, top: 0, width: 1320, height: 780 },
    masks: [{ box: { left: 676, top: 220, width: 248, height: 19 }, sample: { x: 900, y: 230 } }],
  },
  // Chrome's "Save login?" bubble sits over the top-right corner of the page and
  // is browser chrome, not the product, so the frame is cropped to end before it.
  '13-memory-projects.png': { crop: { left: 0, top: 0, width: 1320, height: 827 } },
  '12-first-password-change.png': {
    crop: { left: 0, top: 0, width: 1320, height: 780 },
    masks: [{ box: { left: 676, top: 200, width: 248, height: 19 }, sample: { x: 900, y: 210 } }],
  },
};

/**
 * Trim a screenshot's dead space.
 *
 * The portal's pages are a narrow centred column on a full-viewport dark
 * background, and the app's sign-in screen is one small card in a large empty
 * field, so a capture is mostly empty paint. At docs width that renders as a
 * large black box with a small picture in it, so the empty margin is cropped
 * away.
 *
 * Content is found by looking for *edges*, not by comparing against the corner
 * colour: the app draws a soft radial gradient behind its pages, so a
 * colour-distance test sees the whole frame as "content" and never trims. A
 * pixel counts as an edge when it differs from its left neighbour by more than
 * 10 in total RGB (text, card borders and controls do; a smooth gradient does
 * not), and a row or column needs 3 such pixels so a stray artefact cannot hold
 * the box open.
 *
 * The crop only happens when the content is a small minority of the frame: a
 * full-bleed capture — a browser page, a dialog — has a bounding box close to
 * the whole image and is left exactly as captured.
 */
const PAD = 32;
const TRIM_BELOW = 0.85;

async function trimDeadSpace(buffer, pad = PAD) {
  const { data, info } = await sharp(buffer).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const { width: w, height: h, channels: c } = info;
  const rowHits = new Array(h).fill(0);
  const colHits = new Array(w).fill(0);
  for (let y = 0; y < h; y += 1) {
    for (let x = 1; x < w; x += 1) {
      const i = (y * w + x) * c;
      const j = i - c;
      const d =
        Math.abs(data[i] - data[j]) +
        Math.abs(data[i + 1] - data[j + 1]) +
        Math.abs(data[i + 2] - data[j + 2]);
      if (d > 10) {
        rowHits[y] += 1;
        colHits[x] += 1;
      }
    }
  }
  const first = (hits, min) => hits.findIndex((n) => n >= min);
  const last = (hits, min) => hits.length - 1 - [...hits].reverse().findIndex((n) => n >= min);
  const top = first(rowHits, 3);
  const bottom = last(rowHits, 3);
  const left = first(colHits, 3);
  const right = last(colHits, 3);
  if (top < 0 || left < 0) return { buffer, cropped: false };

  const box = {
    left: Math.max(0, left - pad),
    top: Math.max(0, top - pad),
    width: Math.min(w, right + pad + 1) - Math.max(0, left - pad),
    height: Math.min(h, bottom + pad + 1) - Math.max(0, top - pad),
  };
  const share = (box.width * box.height) / (w * h);
  if (share >= TRIM_BELOW) return { buffer, cropped: false };
  return {
    buffer: await sharp(buffer).extract(box).png({ compressionLevel: 9, palette: true }).toBuffer(),
    cropped: true,
    share,
  };
}

await mkdir(OUT, { recursive: true });
const files = (await readdir(SRC)).filter((f) => f.endsWith('.png')).sort();

for (const file of files) {
  const ops = OPS[file] ?? { masks: [] };
  let img = sharp(path.join(SRC, file)).resize({ width: WIDTH, withoutEnlargement: true });
  const { width, height } = await img.metadata().then((m) => ({
    width: Math.min(m.width, WIDTH),
    height: Math.round((m.height * Math.min(m.width, WIDTH)) / m.width),
  }));
  if (ops.crop) {
    img = sharp(await img.toBuffer()).extract(ops.crop);
    img = sharp(await img.toBuffer());
  }
  const composites = [];
  for (const mask of ops.masks ?? []) {
    const px = await sharp(await img.toBuffer())
      .extract({ left: mask.sample.x, top: mask.sample.y, width: 1, height: 1 })
      .raw()
      .toBuffer();
    const fill = { r: px[0], g: px[1], b: px[2], alpha: 1 };
    composites.push({
      input: {
        create: { width: mask.box.width, height: mask.box.height, channels: 4, background: fill },
      },
      left: mask.box.left,
      top: mask.box.top,
    });
  }
  const rendered = await img.composite(composites).png({ compressionLevel: 9, palette: true }).toBuffer();
  const trimmed = await trimDeadSpace(rendered, ops.pad);
  const meta = await sharp(trimmed.buffer).metadata();
  const { size } = await sharp(trimmed.buffer).toFile(path.join(OUT, file));
  const n = (ops.masks ?? []).length + (ops.crop ? 1 : 0);
  const trim = trimmed.cropped ? `  trimmed to ${Math.round(trimmed.share * 100)}%` : '';
  console.log(
    `${file.padEnd(34)} ${meta.width}x${meta.height}  ${n} redaction(s)${trim}  ${Math.round(size / 1024)}K`,
  );
}
