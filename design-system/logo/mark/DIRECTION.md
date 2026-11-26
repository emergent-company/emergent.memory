# Mark direction — lattice exploration

> **Status: exploration, not the identity.** The shipping mark is the brass tile
> with the drawn M in [`../tile/`](../tile/README.md). This document describes the
> letter lattice the M was first cut on: a useful system, a possible route to a
> wordmark, and a proof that the letterform is constructed rather than typed — but
> nothing in the product links to these files. The lattice assets are kept and
> still gated; they are not shipped.

The mark is a construction system, not a drawing. This document is the contract:
`design-system/build.py` asserts every rule here against `design-system/logo/mark/mark-v1.svg`,
and `scripts/preflight/design-system.sh` runs that assertion before a commit lands. If a
rule and the code disagree, the code is wrong — change the code in the same
commit.

## What this inherits, and what it does not

The starting point is the construction language of an earlier identity by the
same founders, from a company that has since closed: uppercase, orthogonal,
letters broken into **separated segments** rather than drawn as stems with arms,
and a stroke whose weight equals the gap beside it. That language was measured
off the reference artwork and is reproduced here as ratios.

What does **not** carry over is the artwork. No glyph, no wordmark, no lockup and
no outline geometry from that identity appears in this repository — and the
closed company is not named here, because a public document should not tie a
third party's dead brand to a live one. The system is a method — a grid, a weight
rule, a way of decomposing a letterform — and a method is not a trademark; the
glyphs and the goodwill are, and they belong to the company that closed. Every
letter here is drawn from scratch on the grid below.

This is why the direction is written down as numbers instead of being copied as
paths: a system can be re-derived by anyone on the team, and the result is ours.

## The grid

One unit `u` = cap-height / 5. Every number in the brandbook is `u`; nothing is
in pixels, so the mark is resolution-independent and the tile scales with it.

| Measure | Value | Why |
|---|---|---|
| Cap height | `5u` | The whole system derives from this. |
| Letter box | `4u × 5u` | Taller than wide, like a condensed grotesque. |
| Wide letter (M, W) | `5u × 5u` | The only exception, and it is enumerated. |
| Stroke | `1u` | A segment may be **long**; it may never be **thicker** than 1u. |
| Gap | `1u` | Equal to the stroke. This equality *is* the style. |
| Letter pitch | `8u` | Letter + one full letter-width of air. |
| Tile side | `15u` = 3 × cap | Square plate, mark centred. |
| Tile padding | `5u` | One cap-height of air on every side. |
| Clear space | `8u` | One letter pitch, minimum, on every side. |

The two geometric rules that do the most work:

1. **Stroke = gap = 1u.** Not approximately. A 1u-thick rule beside a 1u gap is
   what makes the letterforms read as a system rather than as a font.
2. **Thickness is constrained, length is not.** A bar may run 3u; a filled 2×2
   block is forbidden. On an orthogonal lattice that rule is exactly *"no filled
   2×2 block"*, and it is also what stops a stepped M from becoming a blob.

What the gate checks, precisely: cap height is 5 rows, the box is at least 4u
wide, and there is no filled 2×2 block anywhere — that last test is the encoded
form of "never thicker than 1u". It does **not** enforce rule 1: a 3u gap between
two segments builds cleanly. The equality of stroke and gap is asserted for the
*token pair* — `build.py` raises when `logo.grid.stroke != logo.grid.gap` — but a
glyph's drawn gaps are never measured, so the drawing is judged by eye on
`mark/preview.html`; only the thickness half is mechanical there. Stating that
difference here is deliberate, because a rule the gate pretends to enforce and
does not is worse than one it admits is judgement.

## The one idea

A letter is **not** a stem with arms attached. It is two to four separate marks
that happen to align. That is why the E in this system can be three bars with no
spine at all, and why the M's inner form is a mark of its own rather than a fold
in the outer strokes.

Applied to M, the rule produces the mark we ship:

```
#...#
##.##
#.#.#
#...#
#...#
```

The outer stems run the full cap height. The inner form is a stepped V that
starts one unit below the cap line, has 1u-wide arms, and puts its apex on row 3
of 5 — so it **stops one unit above the baseline** and the letter's two feet are
its outer stems. The top edge is therefore two separate marks, not a bar, which
is the detail that makes the mark ours instead of generic.

## Why this construction and not the other five

Six constructions were drawn on the same grid and rendered at 192, 112, 64, 48,
32, 24, 20 and 16px, light-on-ink and ink-on-paper. The deciding test was not the
large size — it was **16px beside 14px text**, because a mark that fails there
fails its most common appearance (favicon, app switcher, sidebar header).

What survived, and what did not:

- **Split stems with a chevron** — at 16px the stems and the chevron merge into a
  single block. Rejected.
- **Stems split at the waist** — reads as **H**, not M. Rejected.
- **Stems with a detached V** — reads as **N**. Rejected.
- **Narrow 4u box** — reads as **H**; the two interior columns leave no room for a
  diagonal that is not also a stem. Rejected.
- **Airy 7u box, every segment off its neighbour by 1u** — the purest expression of
  the rule, and it renders as three separate vertical bars: `|v|`. Rejected,
  regretfully. Air between the stem and the inner form costs the letter its M.
- **5u box, full-height stems, inset stepped V** — reads as M at every size down
  to 16px. **Shipped as mark v1.**

The rejected constructions are recorded above, each one a single edit of
`MARK_V1` away — they are evidence, not options. Do not restore one without
re-running the 16px test. `design-system/logo/explore/` holds only the accepted lattice
proved across other letters (`glyphs.svg`); it carries no rejected glyph.

## Known limitation

A 5-row lattice cannot draw a smooth diagonal. The V is stepped, and its steps
meet at a single corner point rather than along an edge. That is legible at every
shipping size but it is the first thing to revisit: a finer lattice (cap 7u, or
cap 10u with a 1u stroke) would allow a two-step diagonal and a more confident M.
That is a change to `logo.grid.capHeight` plus a redrawn ASCII glyph, and the gate
will verify the result — it is deliberately the cheapest thing in this system to
experiment with.

## How to change the mark

The glyph is **data**, not a drawing:

1. Edit the ASCII in `MARK_V1` in `design-system/build.py`. `#` is ink, `.` is air.
2. `task brand:build` — regenerates the mark, the two tiles and the glyph sheet.
3. `task brand:check` — the gate re-asserts cap height, stroke = gap, no 2×2
   block, the letter-box width, and every contrast pair.
4. `design-system/logo/mark/preview.html` — look at it at 16px before you believe it.

Nothing draws a curve, because the representation has no place to put one. That
is the point: the constraint is structural, not a guideline.

## Placement

- **Primary mark** — `mark-v1.svg`, `fill="currentColor"`. Colour comes from the
  surface. An `<img>` is an isolated document and cannot inherit CSS `color`, so
  this asset is for inline SVG or a CSS mask; use a tile for anything that has no
  CSS.
- **Tile** — `mark-v1-tile.svg` (ink plate) and `mark-v1-tile-light.svg` (paper
  plate). Self-contained, explicit colours from the palette. Favicon, app icon,
  avatar, email, og:image.
- **Minimum sizes** — mark 16px; tile 32px. Below those, use the tile: the plate
  buys contrast that the bare mark cannot.
- **Polarity** — the mark is monochrome. On ink, use `content.strong`; on paper,
  use ink. Never the accent colour, never a gradient, never a tint of its own.
- **Clear space** — 8u minimum. Nothing enters it: no rule, no badge, no crop.
- **Never** — rotate, stretch, outline, add a shadow, place on a photograph, or
  separate the stems from the inner form.

## Open questions

- **The accent segment.** One optional brass segment — a single mark recoloured,
  never the whole glyph — is the intended way the brand expresses "this one
  matters". Not designed yet; the monochrome mark ships first.
- **The wordmark.** Drawn from the same lattice as outlined paths, never typed.
  `explore/glyphs.svg` proves the lattice across M E A H N Y; the full set is the
  identity pass.
- **Optical cut below 24px.** The 16px render holds today. If a real surface
  disagrees, the answer is a 1u-bolder cut, not a new glyph.
