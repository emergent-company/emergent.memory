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
  src/components/         Card.astro, CardGrid.astro (lucide icons), ThemeToggle
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
| dark-only (`themes: dark --default`) | dark is canonical; light is derived (see below), one-button topbar switch, pre-paint guard in `<head>` |

The results of that mapping are the screenshots in the POC report: headings,
tables, asides, code chips, sidebar pills, cards and buttons all come from the
product's own values.

### Light theme: derived, not designed

The product app ships **one** theme, so dark stays canonical and the light theme
is arithmetic on the same tokens, not a second design:

- hue and chroma are kept, lightness is inverted (`L' = 1 − L`), and each value
  is pulled toward the surface until it clears AA as ink on white;
- accents are the exception: `--color-primary` brass at `L = 0.74` is a **fill**
  colour (2:1 on white), so as text it is darkened to `L = 0.52` — the same
  relationship the app has between a brass button and `primary-content` ink;
- `--sl-color-white` stays "highest-contrast ink" and `--sl-color-black` stays
  "background", because that is what Starlight's own components assume (they
  flip in its light theme too);
- every hairline, tint and overlay in the rules goes through a `--mem-*`
  variable, so a theme is one token block and the rules never branch.

Verified with computed-style sampling in Chrome DevTools on `/`, `/go-sdk/`,
`/swift-sdk/`, `/cli/`, `/developer-guide/security-scopes/` and `/api-reference/`, at 1440 px
and 620 px: no element below AA contrast in either theme, and no light-on-light
or dark-on-dark block in either direction.

Three cascade traps this costs, worth knowing before touching the file:

- **`html > body` for the page colour.** The embedded Scalar bundle injects its
  own preflight (`body{background-color:var(--scalar-background-1)}`) *later* in
  the cascade, so a plain `body` rule loses and a light API page paints a dark
  body under a light header. Specificity, not order, is what fixes it.
- **The pre-paint guard must be theme-aware** (`html[data-theme='light']`,
  plus a `prefers-color-scheme` twin for the first paint), or a light reader gets
  one frame of `#0e1017`.
- **Expressive Code needs both theme slots** (`themes: ['github-dark-default',
  'github-light-default']`) and its *frame* colours (`--ec-frm-*`: tab bar,
  active tab, terminal title bar, tooltip) live in `brand.css`, because they
  have to track `--mem-*`; setting them in `astro.config.mjs` pins them to one
  theme.

### The switch in the topbar

`src/components/ThemeToggle.astro` overrides Starlight's `ThemeSelect`, the
component slot Starlight renders in the topbar (and, below 50 rem, in the mobile
drawer footer). It is one ghost icon button (30 px, `--radius-field`, muted until
hover, brass `:focus-visible` ring) rather than a native `<select>`: two states,
one click, no menu.

**It stays in the topbar on mobile**, which is not where Starlight puts it:
Starlight hides the whole header `.right-group` below 50 rem because the fixed
menu button owns that corner, and offers the theme control only inside the
drawer. The override brings the group back, hides the two things the drawer
already duplicates (social icons, language select), and hides the drawer copy, so
the switch is on screen at every width without opening the menu. No margin or
offset is needed: Starlight pads the header's inline-end by the menu button's
width on exactly the routes that render that button (`[data-has-sidebar]
.header`), and splash routes render neither. Verified at 320/360/390/430/620 px
on a sidebar page and both splash routes: no overlap with the search control or
the menu button, no horizontal scroll.

Below 50 rem the search collapses to a glyph, so it is pushed to the end of the
bar to sit beside the switch (its wrapper takes `margin-inline-start: auto`, the
header gap sets the 15 px between them) and both controls are painted as **bare
glyphs**: no field background, no border, and hover changes the ink colour only.
A fill on hover is the wrong idiom on touch — a tapped button keeps `:hover`, so
the fill sticks and reads as a broken control. The desktop search keeps its
bordered field, where the background actually delineates an input.

- The glyph is the theme the reader is **in** — moon on dark, sun on light —
  and both icons ship in the HTML, swapped in CSS off `data-theme`, so the
  correct one is painted with the first frame instead of after hydration.
- `auto` remains the default: with no stored preference the pre-paint script
  resolves the theme from the OS and the button shows what was resolved. The
  first click stores an explicit `light`/`dark` in `localStorage.starlight-theme`;
  deleting that key returns the reader to OS-following.
- Overriding the component also overrides its script, which is where Starlight
  keeps the theme logic (`data-theme`, storage, the OS-change listener), so
  `ThemeToggle.astro` reimplements those three pieces. The `starlight-theme-select`
  element is kept as the wrapper, so any Starlight CSS still keyed on that tag
  keeps its hook.
- `aria-label`/`title` are rewritten per resolved theme ("Switch to dark theme"),
  so the announced action is never stale.

`color-scheme` flips with the theme, so native controls (scrollbars, form
widgets, the select popup of any future control) follow.

### The bar on splash routes

The home page and the API reference render no sidebar, and Starlight lays the
header out against the viewport on those routes while the content is confined to
`--sl-content-width` and centred. So the logo sat at the 22.5 px nav gutter and
the right-hand icons at the screen edge — 232 px outside the column at 1440 px
wide, and further out as the viewport grows. `brand.css` copies the column's own
geometry onto the bar
(`:root:not([data-has-sidebar]) header.header`), in Starlight's two regimes:

- **below 72 rem** the column is left-anchored in the content gutter, so the bar
  takes `--sl-content-pad-x` on the left and stops where the column stops
  (`100% - pad - width`, floored at the pad). Note that this is the *content*
  gutter, not `--sl-nav-pad-x`: between 50 and 72 rem those differ (15 px vs
  22.5 px) and the nav value leaves the logo 7.5 px out;
- **from 72 rem** Starlight centres the column (`margin-inline: auto`), so both
  sides become its margin, `(100% - width)/2`, floored at the pad so a full-bleed
  column (the API reference is 96 rem) is not pulled to zero.

The media query is the same one Starlight uses for the centring, so the bar
changes regime exactly when the prose does. `rem` in a media query resolves
against the *initial* font size (16 px), not `html { font-size: 15px }` — 72 rem
here is 1152 px, not 1080. Measured on `/` and `/api-reference/` from 390 to
1600 px: logo left edge and last icon right edge both exactly on the column
(0.0 px delta) at every width, including the left-anchored 945–1151 px band;
sidebar routes are untouched (logo at the nav gutter, icons at the header edge).


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
- **Mobile**, both themes, no FOUC (theme-aware pre-paint colour guard mirroring
  `foucBackgroundGuard()` in `ui.templ`).

## Known gaps / follow-ups

1. **Scalar is not branded, and not theme-aware.** Its runtime theme wins over
   `--scalar-*` from our stylesheet (verified: pixel-identical renders with and
   without the overrides), so the embedded reference is Scalar's own dark
   palette. `configuration.darkMode` is fixed at build time, so the reference
   stays dark in the light docs theme too — which is defensible only because it
   reads as an embedded tool: in light it gets a hairline ring
   (`:root[data-theme='light'] .api-page .scalar-app`). Making it follow the page
   needs Scalar's `customCss`/theme hook, not a CSS tweak, and a light Scalar
   palette that does not exist yet in its shipped themes.
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
