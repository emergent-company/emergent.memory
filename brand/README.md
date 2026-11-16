# Brand system

One token file, everything else generated. This directory is the single source of
truth for how Memory looks: colour, type, space, radii, motion, the mark and its
grid. Application code consumes the output; nobody writes a brand value by hand
outside `tokens/memory.tokens.json`.

```
brand/
  tokens/memory.tokens.json   ← the only hand-written brand source (DTCG)
  build.py                    ← validator + emitter + gate
  dist/                       ← generated, never edited
  logo/
    mark/                     ← the shipping mark and its two tiles
      DIRECTION.md            ← the grid contract, in prose
    explore/                  ← rejected constructions, kept as evidence
    preview.html              ← human eyeball check, consumes dist/css
  brand.go                    ← generated Go constants
```

## Build

```bash
task brand:build     # regenerate every artefact
task brand:check     # verify: DTCG, drift, WCAG, grid contract
task brand:preview   # open logo/preview.html
```

`brand:check` is also a pre-commit job (`lefthook run pre-commit`) and part of
`scripts/preflight/all.sh`, so a stale artefact or a contrast regression cannot
be committed. It fails on four things:

1. **Drift** — a generated file no longer matches what the token file produces.
2. **Contrast** — any pair in `CONTRAST_PAIRS` (in `build.py`) below WCAG 2.2 AA:
   4.5:1 for text, 3:1 for UI (SC 1.4.3, 1.4.11).
3. **Grid** — a glyph whose segment is thicker than 1u (any filled 2×2 block),
   the wrong cap height, or a box narrower than the letter width.
4. **Conformance** — a malformed token file: no `$type`, a non-`oklch` source
   colour, a broken or circular alias.

## What is generated, and who reads it

| Output | Consumer |
|---|---|
| `dist/css/brand.theme.css` | daisyUI 5 theme (dark, default). See the import snippet below. |
| `dist/css/brand.tokens.css` | Semantic custom properties, Tailwind radius/font bridges, muted ladder, chart vars, reduced-motion fallbacks. |
| `dist/css/brand.theme.light.css` | The light daisyUI theme. **Defined, not shipped** — nothing imports it. |
| `dist/swift/MemoryBrand.swift` | SwiftUI / UIKit: resolved sRGB, text styles, radii, space, motion. |
| `brand.go` | Go (`package brand`): hex constants for the CLI's lipgloss styling. |
| `dist/json/memory.tokens.flat.json` | Any tool that wants resolved values without a CSS engine. |
| `dist/js/brand.js` | Charts and non-Tailwind web surfaces. |
| `dist/mkdocs/brand.css` + `palette.yml` | The docs site, replacing Material's stock indigo. |
| `dist/contrast.md` | The published contrast matrix — the receipt for every pair above. |
| `logo/manifest.json` | The grid contract and asset list, for tooling. |
| `logo/mark/*.svg`, `logo/explore/glyphs.svg` | The mark, its tiles, and the lattice proof. |

### The daisyUI import contract

```css
@import "tailwindcss";
@plugin "daisyui" { themes: false; }
@import "brand/dist/css/brand.theme.css";
@import "brand/dist/css/brand.tokens.css";
```

`themes: false` is **required**, and this was verified against Tailwind 4.2.2 /
daisyUI 5.5.19 rather than assumed: daisyUI emits its own light and dark themes
after an imported file, at equal specificity, so without it daisyUI's stock
indigo wins and every brand colour is silently replaced. The theme itself is
plain custom properties rather than `@plugin "daisyui/theme"`, because a bare
`@plugin` specifier resolves relative to the file containing it — a theme living
under `brand/dist/css/` is not inside a node project and can never resolve
`daisyui`. Unlayered custom properties sidestep both problems, and the same file
works with no Tailwind at all.

`brand.go` lives at the package root rather than under `dist/go/` on purpose: it
is imported as the `brand` package, so it has to sit where Go can resolve it.

## The three tiers

- **Reference** (`color.*`) — raw ramps, named by hue and step (`ink.800`,
  `brass.500`). Never used directly in product code.
- **Semantic** (`semantic.dark.*`, `semantic.light.*`) — the roles a surface asks
  for (`surface.base`, `content.body`, `interactive.primary`). This is what
  application code and daisyUI both read.
- **Component** — *not here.* Shape, structure and component primitives belong to
  `go-daisy`, which owns them as CSS custom properties. Memory supplies values;
  go-daisy supplies anatomy. The dependency is one-way: go-daisy never imports
  brand values, and this directory never redefines a component.

## Adding or changing a value

1. Edit `tokens/memory.tokens.json`. Source colours are `oklch` only — the build
   converts to sRGB and hex for every platform, so a colour is defined once.
2. `task brand:build`, then `task brand:check`.
3. If you changed a colour that appears in `CONTRAST_PAIRS`, expect the gate to
   fail and move the ramp step instead of loosening the threshold.

Adding a *role* means adding it to the semantic tier **and** to the emitter in
`build.py` (the daisyUI mapping, the CSS role list, or the Swift/Go lists). The
gate catches the omission as drift.

## Scope

- **Dark is the shipped theme.** Light is generated and documented so the choice
  is reversible, but no product surface imports it. A surface opts in with
  `[data-theme="brand-light"]`.
- **Type stays on the system stack.** No web font, no licence, native rendering.
  The scale, weight and tracking are tokens, which is why they can be tuned
  without shipping a file. A future typeface is a change here plus woff2 files.
- **The mark is data.** Glyphs are ASCII cells in `build.py`; the generator merges
  them into the fewest rectangles so no hairline seam lands inside the mark, and
  the gate rejects anything thicker than 1u. Read `logo/mark/DIRECTION.md` before
  touching `MARK_V1`.

## Status

Merged in this change: the token source, the generator, the gate, and mark v1
with its direction document.

Deliberately **not** in this change — these consumers still carry their own copy
of the theme block and are migrated next, one surface at a time:

- `apps/web-ui/gateway/webui/css/app.css`
- `cmd/deploy-portal` and `cmd/ememory-infra-dashboard` (in `emergent.memory.infra`)
- `emergent.memory.website/theme/input.css`
- `mkdocs.yml` (still on Material `indigo`)
- `apps/server/templates/email`
- `apps/cli/internal/ui`

Also deferred: the lint rule that forbids a raw hex outside `brand/`. The copies
have to be consolidated first, or the rule would fire on day one against files
nobody has migrated yet.
