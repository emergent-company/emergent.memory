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
 * the page jumping while images load.
 *
 * Run from `docs-astro/`:  node scripts/port-images.mjs
 */
import sharp from 'sharp';
import { mkdir, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const REPO = path.resolve(import.meta.dirname, '../..');
const SITE = path.join(REPO, 'docs/site');
const PUBLIC = path.join(REPO, 'docs-astro/public');

/** Source dir (relative to docs/site) → public dir (relative to docs-astro/public). */
const DIRS = ['assets/tutorials/railway'];

const MAX_WIDTH = 1600;

for (const dir of DIRS) {
  const srcDir = path.join(SITE, dir);
  const outDir = path.join(PUBLIC, dir);
  await mkdir(outDir, { recursive: true });
  await mkdir(srcDir, { recursive: true });

  const files = (await readdir(srcDir)).filter((f) => /\.(png|jpe?g)$/i.test(f)).sort();
  if (!files.length) {
    console.log(`${dir}: no rasters found — nothing to do`);
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
    manifest[webp] = { width: info.width, height: info.height };
    console.log(`${dir}/${webp}  ${info.width}x${info.height}  ${Math.round(info.size / 1024)}K`);
  }
  await writeFile(path.join(outDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
}
