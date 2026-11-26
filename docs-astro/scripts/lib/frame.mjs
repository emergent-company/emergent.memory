/**
 * Framing shared by the screenshot producers.
 *
 * Two producers write into `docs-astro/public/assets/tutorials/`: `redact-tutorial-shots.mjs`
 * (the hand-captured originals, which need PII covered) and `shots.mjs` (a live
 * capture, already clean). Both end up with the same problem — the app paints a
 * narrow column on a full-viewport background, so most of the frame is empty
 * paint and renders in the docs as a large black box with a small picture in
 * it — so both trim with the same rule.
 */
import sharp from 'sharp';

export const PAD = 32;
const TRIM_BELOW = 0.85;

/**
 * Trim a screenshot's dead space.
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
export async function trimDeadSpace(buffer, pad = PAD) {
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
