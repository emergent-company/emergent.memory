# Brand system

One token file, everything else generated. This directory is the single source of
truth for how Memory looks: colour, type, space, radii, motion, the mark and its
grid. Application code consumes the output; nobody writes a brand value by hand
outside `tokens/memory.tokens.json`.

```
design-system/
  tokens/memory.tokens.json   ← the only hand-written brand source (DTCG)
  build.py                    ← validator + emitter + gate
  dist/                       ← generated, never edited
  logo/
    tile/                     ← the shipping mark: brass tile + drawn M (primary, mono, reverse)
      README.md               ← the tile contract, in prose
    glyph/                    ← the M alone, a secondary mark
    mark/                     ← lattice exploration (not the identity)
      DIRECTION.md            ← the lattice contract, in prose
      preview.html            ← lattice eyeball check, consumes dist/css
    explore/                  ← the lattice proved across other letters (not a product asset)
  catalog/
    catalog.tmpl.html         ← hand-written page template
    index.html                ← generated brand + design-system catalogue
  brand.go                    ← generated Go constants (module: design-system/go.mod)
```

## Build

```bash
task brand:build     # regenerate every artefact
task brand:check     # verify: DTCG, drift, WCAG, mark contract
task brand:preview   # open the generated catalogue page
```

`brand:check` is also a pre-commit job (`lefthook run pre-commit`) and part of
`scripts/preflight/all.sh`, so a stale artefact or a contrast regression cannot
be committed. It fails on five things:

1. **Drift** — a generated file no longer matches what the token file produces.
2. **Contrast** — any pair in `CONTRAST_PAIRS` (in `build.py`) below WCAG 2.2 AA:
   4.5:1 for text, 3:1 for UI (SC 1.4.3, 1.4.11).
3. **Mark** — the tile contract: radius / side inside 0.20–0.24, glyph / side
   inside 0.36–0.44, a glyph that is at least 6px at a 16px tile, and glyph ink
   at 4.5:1 against both ends of the gradient. The lattice exploration is checked
   separately for its 1u thickness and cap-height rules.
4. **Conformance** — a malformed token file: no `$type`, a non-`oklch` source
   colour, a broken or circular alias, or a semantic role present in only one
   mode.
5. **Go compilation** — `go vet ./design-system` on the generated package. Every other
   Go job in CI is rooted in its own module, so without this step a syntax error
   in `brand.go` would land silently. `design-system/` is its own module
   (`github.com/emergent-company/emergent.memory/design-system`, package `brand`)
   and is registered in `go.work`, which is what makes the file importable by a
   Go consumer at all — a `.go` file in a directory no module owns is never
   compiled.

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
| `dist/contrast.md` | The published contrast matrix — the receipt for every pair above. |
| `logo/manifest.json` | The tile contract, its contrast receipts and the asset list, for tooling. |
| `logo/tile/memory-tile.svg` | **The app icon.** Brass gradient plate, ink glyph. Favicon, app icon, avatar, social. |
| `logo/tile/memory-tile-mono.svg` | One-colour tile: `currentColor` plate, glyph knocked out. Inline HTML or CSS mask only. |
| `logo/tile/memory-tile-reverse.svg` | Ink plate, brass glyph. Brass-heavy or light surfaces. |
| `logo/glyph/memory-glyph.svg` | The M alone (`currentColor`, 24u). A quiet secondary mark. |
| `logo/mark/*.svg`, `logo/explore/glyphs.svg` | Lattice exploration only — not the identity. |

### The daisyUI import contract

```css
@import "tailwindcss";
@plugin "daisyui" { themes: false; }
@import "design-system/dist/css/brand.theme.css";
@import "design-system/dist/css/brand.tokens.css";
```

`themes: false` is **required**, and this was verified against Tailwind 4.2.2 /
daisyUI 5.5.19 rather than assumed: daisyUI emits its own light and dark themes
after an imported file, at equal specificity, so without it daisyUI's stock
indigo wins and every brand colour is silently replaced. The theme itself is
plain custom properties rather than `@plugin "daisyui/theme"`, because a bare
`@plugin` specifier resolves relative to the file containing it — a theme living
under `design-system/dist/css/` is not inside a node project and can never resolve
`daisyui`. Unlayered custom properties sidestep both problems, and the same file
works with no Tailwind at all.

`brand.go` lives at the package root rather than under `dist/go/` because it is
generated into a real Go module: `design-system/go.mod` declares
`github.com/emergent-company/emergent.memory/design-system`, and `go.work`
registers it, so `go vet ./design-system` compiles the generated file and a Go
consumer in this workspace can import it. A `.go` file under `dist/` would be in
nobody's module and would therefore never be built.

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
- **The mark is a brass tile with a drawn M.** `logo.mark.*` in the token file is
  the contract: a 64u squircle plate, a 140° brass ramp, an ink glyph drawn as a
  path in its own 24u frame. The letter is never live text — the shipped monogram
  set it in the system font, so its shape drifted per platform and could not be
  exported. Read `logo/tile/README.md` before editing the glyph, and use the
  catalogue's tile workbench to tune the three numbers against the gate.
- **The lattice is an exploration, not the identity.** `logo/mark/` and
  `logo/explore/` keep the letter system the M was first cut on, as a proof and a
  possible route to a wordmark. Nothing in the product should link to them.

## Status

Shipped in this directory: the token source, the generator and its gate, the
catalogue page, and the mark — a brass tile with a drawn M, as a gradient, a
one-colour and a reverse file plus the glyph and a v2 manifest. The shipped
monogram in the product is still the CSS/text version; repointing it at
`logo/tile/memory-tile.svg` is the next consumer migration.

Deliberately **not** here yet — these consumers still carry their own copy
of the theme block and are migrated next, one surface at a time:

- `apps/web-ui/gateway/webui/css/app.css`
- the deploy portal and the infrastructure dashboard (both in the infra control repo)
- the marketing site's `theme/input.css` (a separate repository)
- `apps/server/templates/email`
- `apps/cli/internal/ui`

Also deferred: PNG and `.ico` rasterisation of the tile (the SVGs are the source,
and the host has no rasteriser), the wordmark and lockups, and the lint rule that
forbids a raw hex outside `design-system/`. The copies have to be consolidated
first, or the rule would fire on day one against files nobody has migrated yet.
