# docs-astro — Astro/Starlight port of the Memory docs (POC)

Status: **proof of concept**. 17 pages converted out of the 77 in `docs/site`.
The site builds, serves, and is themed against the product web UI. It is not
wired into CI, not published, and the old MkDocs site is untouched.

```
docs-astro/
  astro.config.mjs        site config, sidebar, Expressive Code theme
  src/content.config.ts   Starlight content collection (Content Layer)
  src/styles/brand.css    ← the deliverable: product tokens → Starlight tokens
  src/assets/monogram.svg brass "M" logo (copied from the app's brand mark)
  public/favicon.svg
  src/components/         Card.astro, CardGrid.astro (lucide icons), ThemeSelect
  src/content/docs/       converted MDX (generated — see "Porting")
  scripts/port_mkdocs.py  MkDocs Material → Starlight MDX converter
  scripts/material-icons.json  `:material-*:` → lucide icon mapping
```

## Run it

```bash
cd docs-astro
npm install
npm run dev       # http://localhost:4321/emergent.memory/
npm run build     # dist/ (base path /emergent.memory baked in)
npm run preview   # serve dist/ on :4322
```

Astro 7 runs `dev` as a background daemon: `npx astro dev status|logs|stop`.

Measured on this host for the 16-page subset: **cold build 4.4 s**, home page
6.2 KB gzipped HTML, User Guide 8.9 KB, Developer Guide 6.5 KB, `dist/` 2.4 MB
(of which Pagefind's index is 816 KB), `_astro/` 220 KB — 97 KB of that is
Scalar's client bundle, the only hydrated island on the site.

## Why Astro over Zensical

Zensical is the low-risk MkDocs exit (Material is EOL 2027-05-05), but it has no
component model — its Module API is unshipped. Astro/Starlight gives the docs:

- **Islands** — a page stays static HTML with one hydrated widget on top
  (`<ApiExplorer>`, `<GraphViewer>`) without a React app build.
- **Content Layer** — generate one page per object type / endpoint from
  `schema.dbml` and the OpenAPI spec, validated by zod at build time, instead of
  hand-maintaining `developer-guide/schema.md` (23 KB of prose).
- **A settled ecosystem** — Scalar for the API reference (used below), Pagefind
  search, `llms.txt`, ~60 maintained Starlight plugins.

## Theme: product tokens, not a borrowed theme

The web UI theme is daisyUI dark-only, declared once in
`apps/web-ui/gateway/webui/css/app.css` (`@plugin "daisyui/theme"`). Third-party
"looks like X" themes were rejected: the ones that resemble the product are
single-maintainer and stale, and adjusting one costs more than writing the
mapping. `src/styles/brand.css` maps the app's tokens onto Starlight's variables
and ports the `.memory-md` prose rules one-for-one:

| Product (`app.css`) | Docs (`brand.css`) |
|---|---|
| `--color-base-100/200/300` (ink navy) | `--sl-color-bg-nav`, `--sl-color-bg`, `--sl-color-black` |
| `--color-base-content` | `--sl-color-text`, `--sl-color-white`, gray ramp |
| `--color-primary` brass | `--sl-color-text-accent`, `--sl-color-accent*`, links, buttons |
| `--color-accent` mint / `--color-secondary` steel | aside variants (`tip` mint, `note` steel) |
| `html { font-size: 15px }` | same (docs type scale identical to the app) |
| body brass/steel radial glows | same gradients on `body` |
| `--radius-field .25 / --radius-box .5 / --radius-selector 1rem` | buttons + inline code, cards + code frames, sidebar pills |
| `.memory-md` h1/h2 hairlines, table header tint, brass inline code, blockquote bar | same rules |
| dark-only (`themes: dark --default`) | theme switcher removed (`ThemeSelect.astro`), pre-paint guard in `<head>` |

The results of that mapping are the screenshots in the POC report: headings,
tables, asides, code chips, sidebar pills, cards and buttons all come from the
product's own values.

Two Starlight details cost real time and are worth knowing before editing
`brand.css`:

- Starlight nests `<div class="header">` inside `<header class="header">`, so a
  header rule must target `header.header` or it draws a second, inset hairline.
- `.not-content` is an **opt-out class the element carries** (LinkButton, card
  CTAs), so `:not(:where(.not-content *))` does not exempt it — that selector
  misses the element itself and underlined every button. The prose link rule
  needs both `:not(.not-content)` and `:not(:where(.not-content *))`.

## Porting

`scripts/port_mkdocs.py` is the converter and the single source of the
generated MDX — **edit the script, not the `.mdx` files** (re-running it
overwrites them; the two hand-written pages, `index.mdx` and
`api-reference.mdx`, are excluded from its page list).

It handles, and reports:

| MkDocs construct | Starlight |
|---|---|
| `!!! note "Title"` (24 pages) | `<Aside type="note\|tip\|caution\|danger">`, title kept |
| `=== "Label"` content tabs (10 pages) | `<Tabs>/<TabItem>`, recursion inside them |
| `<div class="grid cards" markdown>` (2 pages) | `<CardGrid>`/`<Card>`; the item's `:material-*:` becomes the card icon, the bold link becomes the card title |
| `:material-<name>:` (13 names, one page) | `<Icon name="lucide:…"/>` inline, or the `icon` prop inside a card — see "Icons" |
| duplicate leading `# H1` | dropped — Starlight renders `title` as the H1 |
| `[x](page.md#anchor)` | rewritten to `/emergent.memory/<route>/#anchor` |
| unescaped `\|` inside inline code in a table row | escaped (a real MDX breakage: it truncates the code span and leaves raw `<` JSX) |
| `--8<--` snippets (3 pages) | **not converted** — reported |

The converter prints any link target that has no source page ("BROKEN LINK
TARGETS"), lists pages linked from the subset but not yet converted (22 in this
POC), and reports an unmapped `:material-*:` name or an unparsed grid-card line
instead of guessing. A full port needs the snippet rule and the remaining 59
pages.

## Icons

The docs draw the **product UI's icon set**. The gateway renders
`lucide--<name>` Iconify classes (`app.css` → `@plugin "@iconify/tailwind4"`),
so the docs use the same `lucide` set through `astro-icon` +
`@iconify-json/lucide`: a concept is the same glyph in the app and on the page.

Icons are inlined as SVG at build time — no runtime JS, no icon font. Two entry
points:

- `<Card title="…" icon="lucide:rocket">` — `src/components/Card.astro`.
- `<Icon name="lucide:rocket" />` inline, for an icon in prose.

Starlight's own `Icon`/`Card` come with a fixed set that has no network, robot,
key, bell or package glyph, and its `Card` tints a rotating accent palette the
app doesn't have — hence the two local components (`Card`, `CardGrid`), which
keep Starlight's `.card` markup so `brand.css` styles both identically.
`scripts/material-icons.json` is the only place a Material name is mapped; add
a row there rather than hand-editing generated MDX.

## What the POC demonstrates

- **Interactive API reference**: `@scalar/astro` in `renderMode="client"` (the
  mode its docs prescribe for Starlight's client-side navigation) renders all
  420 endpoints from the published `swagger.json`, full width, with try-it.
- **Search**: Pagefind, indexed at build time (`dist/pagefind/`).
- **Mobile**, dark-only, no FOUC (pre-paint colour guard mirroring
  `foucBackgroundGuard()` in `ui.templ`).

## Known gaps / follow-ups

1. **Scalar is not branded.** Its runtime theme wins over `--scalar-*` from our
   stylesheet (verified: pixel-identical renders with and without the
   overrides), so the embedded reference is Scalar's own dark palette. Needs
   Scalar's `customCss`/theme hook, not a CSS tweak.
2. **Sidebar is hand-written** in `astro.config.mjs`, not `autogenerate` — a
   full port should switch to autogenerate so a new page cannot be invisible.
3. **Versioning.** The current site publishes `latest` via `mike` and
   `e2e/tests/docs/docs_site_test.go` hardcodes `.../latest`. Starlight has no
   built-in versioning: either a path prefix (`/latest/`) served by the workflow
   or one deployment per version path. This must be settled before switching
   over, or the docs e2e test breaks.
4. **Publishing** still has to be written: `docs.yml` currently runs `mike` over
   `gh-pages`. The Astro build would publish `docs-astro/dist` instead, with the
   CI-generated `swagger.json` copied in (then `api-reference.mdx` can drop the
   absolute CDN-era URL for `/emergent.memory/openapi.json`).
5. **Fonts**: the app declares no webfont (system sans), so the docs do the
   same. If the product ever adopts one, `--sl-font` follows.
