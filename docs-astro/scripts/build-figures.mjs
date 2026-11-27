#!/usr/bin/env node
/**
 * build-figures — turn tutorial captures into the WebP the docs serve.
 *
 * The capture plans (`scripts/shots.mjs`) and the redaction pass
 * (`scripts/redact-tutorial-shots.mjs`) write PNGs into
 * `public/assets/tutorials/<slug>/`, beside the `.shots.json` that records the
 * ratio each capture was made at. Those PNGs are gitignored: the picture the
 * site ships is the WebP, and this script writes it — resized to the display
 * ceiling, with the real pixel size and the on-screen size recorded in
 * `manifest.json`, which is how a page reserves a figure's box before the image
 * loads.
 *
 * It works in place. There is one docs tree — the Astro site serves `public/`
 * at the host root — so there is nothing to mirror, and a recording's `.webm`,
 * `.vtt` and poster are committed where they play and are left alone here.
 *
 * Run from `docs-astro/`:  npm run shots:build
 */
import sharp from 'sharp';
import { mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';

const REPO = path.resolve(import.meta.dirname, '../..');
const PUBLIC = path.join(REPO, 'docs-astro/public');
const TUTORIALS = path.join(PUBLIC, 'assets/tutorials');

// The WebP is what the site serves, so its ceiling is the *display* ceiling:
// a figure is shown at most 2x its CSS size, and a 4x capture has to survive
// that without being downsampled first.
const MAX_WIDTH = 2400;

const tutorialDirs = (await readdir(TUTORIALS, { withFileTypes: true }))
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name)
  .sort();

for (const slug of tutorialDirs) {
  const dir = path.join(TUTORIALS, slug);
  await mkdir(dir, { recursive: true });

  // `shots.mjs` records the device pixel ratio each picture was captured at.
  // Hand-captured pictures have no entry and are 1:1 by definition.
  let scales = {};
  try {
    scales = JSON.parse(await readFile(path.join(dir, '.shots.json'), 'utf8'));
  } catch {
    /* no plan captured here yet */
  }

  // The committed manifest describes the WebPs that are already in the tree, and
  // a re-run only adds to it: a WebP whose PNG is gone is the normal committed
  // state, and dropping its entry would leave the page unable to size it.
  const manifestPath = path.join(dir, 'manifest.json');
  let manifest = {};
  try {
    manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
  } catch {
    /* first build in this directory */
  }

  const files = (await readdir(dir))
    .filter((f) => !f.startsWith('.') && /\.(png|jpe?g)$/i.test(f))
    .sort();
  if (!files.length) {
    console.log(`${slug}: no rasters found — nothing to build`);
    continue;
  }

  for (const file of files) {
    const webp = file.replace(/\.(png|jpe?g)$/i, '.webp');
    const from = path.join(dir, file);
    const info = await sharp(from)
      .resize({ width: MAX_WIDTH, withoutEnlargement: true })
      .webp({ quality: 82, effort: 5 })
      .toFile(path.join(dir, webp));
    // `width`/`height` are real pixels; `cssWidth`/`cssHeight` are the size the
    // app drew on screen (real pixels ÷ capture ratio). The page needs both: the
    // first bounds how far a figure may be zoomed before it stops being sharp,
    // the second is the figure's true size, which the card must not exceed.
    // Computed from the *captured* size, not the resized one, so a full-frame
    // shot that hits MAX_WIDTH still declares its real on-screen width.
    const recorded = scales[file.replace(/\.(png|jpe?g)$/i, '')];
    const scale = recorded?.scale ?? 1;
    const captured = recorded?.width ?? info.width;
    const capturedHeight = recorded?.height ?? info.height;
    manifest[webp] = {
      width: info.width,
      height: info.height,
      cssWidth: Math.round(captured / scale),
      cssHeight: Math.round(capturedHeight / scale),
    };
    console.log(
      `${slug}/${webp}  ${info.width}x${info.height} @${scale}x → ${manifest[webp].cssWidth}css  ${Math.round((await stat(path.join(dir, webp))).size / 1024)}K`,
    );
  }
  await writeFile(manifestPath, JSON.stringify(manifest, null, 2) + '\n');
}
