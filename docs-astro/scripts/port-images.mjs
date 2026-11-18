#!/usr/bin/env node
/**
 * port-images — turn the docs site's raster screenshots into web assets.
 *
 * `docs/site` (MkDocs) serves whatever is committed under `docs/site/assets`.
 * The Astro port serves the same pictures from `docs-astro/public/assets`, so
 * both sites reference one source of truth and this script keeps the two in
 * step: it resizes every PNG under the tracked source dirs to 1600px wide,
 * writes a WebP next to the originals (MkDocs serves WebP too), mirrors the
 * directory into `public/`, and records the real pixel dimensions in
 * `manifest.json` so the Markdown → MDX port can emit `width`/`height` and stop
 * the page jumping while images load. Recordings (`walkthrough.webm`, its
 * `.vtt`, and the poster) are mirrored verbatim — see `RECORDING`.
 *
 * Run from `docs-astro/`:  node scripts/port-images.mjs
 */
import sharp from 'sharp';
import { copyFile, mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';

const REPO = path.resolve(import.meta.dirname, '../..');
const SITE = path.join(REPO, 'docs/site');
const PUBLIC = path.join(REPO, 'docs-astro/public');

/** Source dir (relative to docs/site) → public dir (relative to docs-astro/public). */
const DIRS = ['assets/tutorials/railway', 'assets/tutorials/install-a-blueprint'];

// A tutorial may carry a recording as well as figures. Those files are not
// pictures to resize, and they are not Markdown references either — a `<video>`
// is raw HTML, which the MDX port leaves untouched, so nothing rewrites their
// paths and the copy has to be a plain mirror. MkDocs serves them straight out of
// `docs/site`; the Astro build serves only `public/`, so a `.webm`, its `.vtt`
// and the poster 404 there unless they are copied. `redact-tutorial-shots.mjs`
// ignores them for the same reason it ignores the WebPs.
const RECORDING = /\.(webm|mp4|vtt|webp)$/i;

// The WebP is what both sites serve, so its ceiling is the *display* ceiling:
// a figure is shown at most 2x its CSS size, and a 4x capture has to survive
// that without being downsampled first.
const MAX_WIDTH = 2400;

for (const dir of DIRS) {
  const srcDir = path.join(SITE, dir);
  const outDir = path.join(PUBLIC, dir);
  await mkdir(outDir, { recursive: true });
  await mkdir(srcDir, { recursive: true });

  // `shots.mjs` records the device pixel ratio each picture was captured at.
  // Hand-captured pictures have no entry and are 1:1 by definition.
  let scales = {};
  try {
    scales = JSON.parse(await readFile(path.join(srcDir, '.shots.json'), 'utf8'));
  } catch {
    /* no plan captured here yet */
  }

  // Recordings are mirrored verbatim, before (and independently of) the raster
  // pass: a video-only tutorial has no PNG here, and the `continue` below would
  // otherwise skip the copy and ship a page whose `<video>` 404s. A WebP that a
  // PNG or JPEG beside it regenerates is skipped — it is this script's own
  // output, and copying it first would only be overwritten.
  const entries = (await readdir(srcDir)).filter((f) => !f.startsWith('.'));
  const files = entries.filter((f) => /\.(png|jpe?g)$/i.test(f)).sort();
  const regenerated = new Set(files.map((f) => f.replace(/\.(png|jpe?g)$/i, '.webp')));
  for (const file of entries.filter((f) => RECORDING.test(f) && !regenerated.has(f)).sort()) {
    const from = path.join(srcDir, file);
    await copyFile(from, path.join(outDir, file));
    console.log(`${dir}/${file}  mirrored  ${Math.round((await stat(from)).size / 1024)}K`);
  }

  if (!files.length) {
    console.log(`${dir}: no rasters found — nothing to resize`);
    continue;
  }

  const manifest = {};
  for (const file of files) {
    const webp = file.replace(/\.(png|jpe?g)$/i, '.webp');
    const from = path.join(srcDir, file);
    const info = await sharp(from)
      .resize({ width: MAX_WIDTH, withoutEnlargement: true })
      .webp({ quality: 82, effort: 5 })
      .toFile(path.join(outDir, webp));
    // The source tree carries the WebP as well, so MkDocs renders the same file.
    await sharp(from)
      .resize({ width: MAX_WIDTH, withoutEnlargement: true })
      .webp({ quality: 82, effort: 5 })
      .toFile(path.join(srcDir, webp));
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
    console.log(`${dir}/${webp}  ${info.width}x${info.height} @${scale}x → ${manifest[webp].cssWidth}css  ${Math.round(info.size / 1024)}K`);
  }
  await writeFile(path.join(outDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
}
