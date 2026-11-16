# docs-astro — Astro/Starlight port of the Memory docs (POC)

Status: **proof of concept**. 20 pages of the 77 in `docs/site` — 15 converted by
`scripts/port_mkdocs.py`, 5 hand-written (`index.mdx`, `api-reference.mdx`, and
the three `user-guide/getting-started/` pages).
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
overwrites them). The hand-written pages are excluded from its page list:
`index.mdx`, `api-reference.mdx`, and the Getting Started trio below.

It handles, and reports:

| MkDocs construct | Starlight |
|---|---|
| `!!! note "Title"` (24 pages) | `<Aside type="note\|tip\|caution\|danger">`, title kept |
| `=== "Label"` content tabs (10 pages) | `<Tabs>/<TabItem>`, recursion inside them |
| `<div class="grid cards" markdown>` (2 pages) | `<CardGrid>`/`<Card>`; the item's `:material-*:` becomes the card icon, the bold link becomes the card title |
| `:material-<name>:` (13 names, one page) | `<Icon name="lucide:…"/>` inline, or the `icon` prop inside a card — see "Icons" |
| duplicate leading `# H1` | dropped — Starlight renders `title` as the H1 |
| `[x](page.md#anchor)` | rewritten to `/emergent.memory/<route>/#anchor` |
| `<https://…>` autolink | `[url](url)` — MDX reads `<https:` as the start of a JSX tag and fails the build |
| `![alt](assets/…png)` | `<Screenshot>` sized from the ported-image manifest — see "Screenshots" |
| unescaped `\|` inside inline code in a table row | escaped (a real MDX breakage: it truncates the code span and leaves raw `<` JSX) |
| `--8<--` snippets (3 pages) | **not converted** — reported |

The converter prints any link target that has no source page ("BROKEN LINK
TARGETS"), lists pages linked from the subset but not yet converted (22 in this
POC), and reports an unmapped `:material-*:` name or an unparsed grid-card line
instead of guessing. A full port needs the snippet rule and the remaining 59
pages.

## Getting Started — the one place the POC changes the IA

The MkDocs site opens the User Guide with a single `getting-started.md` that
starts at "create an organization", which assumes an instance already exists and
never says how you got one. The POC replaces that page with three, hand-written
because the structure does not exist upstream:

| Page | Holds |
|---|---|
| `user-guide/getting-started.mdx` | The choice: self-hosted or Memory Cloud, a comparison table, and what both paths then share |
| `…/getting-started/self-hosted.mdx` | Four target cards that open the installer, what each target needs, the install per target, then first admin → project → blueprint → client |
| `…/getting-started/memory-cloud.mdx` | Sign in → project → blueprint → client, on the hosted instance |

The sidebar puts the group at the top of the sidebar, above the User Guide,
matching the order a reader actually needs — the guide is what you read before
you have an instance, not after. Both detail pages end at the same third of the
work — a project, a blueprint, a client — so that section is one component,
`src/components/ConnectOptions.astro`, parameterised by the instance's API and
app hostnames rather than written twice: the MCP share, the CLI commands and the
`curl` call cannot drift apart between the two pages. It is an Astro component,
so its prose is HTML (no backtick fences) and the snippets go through
`astro-expressive-code`'s `<Code>`, which is what fenced markdown compiles to —
same theme slots, same frame.

The self-hosted page deliberately does **not** screenshot the installer's target
chooser. A picture of four buttons makes the reader read them in an image and
then go hunting for the site; four `<Card>`s with a `<LinkButton>` each deep-link
straight into that target's page in the installer
(`deploy.memory.emergent-company.ai/setup/railway|docker|gcp|aws`), opening in a
new tab so the guide stays behind the wizard. Each card is an equal option — no
"recommended" badge and no primary button — because the right target is the one
the reader already runs. What each target needs is a table, and the install is a
paragraph per target: Railway keeps the three figures that show its wizard
(authorize, workspace + admin email, install complete) while Docker and the two
cloud bundles are described in prose, since what they hand back is a script we
have not run end to end.

The rest of the figures are the self-hosted install's, re-used from the
tutorial: the sign-in and first-password screens are the same whatever the
target. The blueprint install, the MCP Sharing screen and the API-token screen
still need captures; they belong in
`shots/installer.plan.json` (which already ends logged in as the admin it just
created, so the flow continues with no second login) rather than being taken by
hand.

## Screenshots

The tutorial page is the first page that carries pictures, so it also defines the
image path both sites share:

1. **Capture** — `scripts/shots.mjs` writes one PNG per `shot` action into
   `captures/<run>/` on the capture host, at **4× device pixel ratio**, and a
   `.shots.json` beside them recording that ratio and each frame's real pixel
   size. Nothing is hand-captured any more.

   Cropping happens **here, not afterwards**: a `shot` may carry `clip` (a
   selector, or a measured `{x, y, width, height}` in CSS pixels) and `pad`, so
   the figure is the subject the reader is looking at — the Zitadel card, the
   sign-in card — rather than the whole browser window, and a re-capture lands
   ready to publish. `scripts/redact-tutorial-shots.mjs` (an `OPS` table of crops
   and colour-sampled masks) was the ingest for the hand-taken originals; it
   stays only for that legacy path and deliberately `SKIP`s the files the runner
   produces.

2. `scripts/port-images.mjs` — caps each picture at **2400 px wide** (a 2× zoom
   ceiling over a 4× capture's on-screen width, so zooming never has to
   downsample first), writes a WebP next to the PNG for MkDocs plus a copy under
   `public/`, and records `width`/`height` (real pixels) with `cssWidth`/
   `cssHeight` (pixels ÷ the `.shots.json` ratio) in `manifest.json`.

3. `scripts/port_mkdocs.py` — reads that manifest and turns each
   `![alt](path)` into `<Screenshot src alt width height />` sized to the **CSS**
   size, so the browser reserves the right box and the page does not jump as the
   images arrive. A picture missing from the manifest is reported as
   `missing_shots` rather than emitted with no dimensions.

Regenerating the set after a new app version is therefore: capture, copy,
`npm run shots:port` (steps 2 and 3 in one), `npm run build`.

```bash
# on the capture host
node scripts/shots.mjs scripts/shots/installer.plan.json \
  --profile .profile --out "captures/$(date +%Y-%m-%dT%H-%M)"

# where the docs are built
cp captures/<run>/*.png captures/<run>/.shots.json docs/site/assets/tutorials/railway/
cd docs-astro && npm run shots:port && npm run build
```

`.shots.json` is merged rather than replaced, so re-shooting two figures against
an existing deployment updates those entries and leaves the rest alone.

`src/components/Screenshot.astro` is a figure with click-to-zoom: a native
`<dialog>` created on first click (no framework, no hydration; Escape and
backdrop close work by themselves). Two rules keep the zoom honest:

- **never wider than the pixels that exist** — the cap is
  `min(naturalWidth, cssSize × 2)`, so a 440 css Zitadel card opens at 880 and a
  1600 css full frame at its own 1600, but neither is invented;
- **never wider than the window** — `96vw`, with `94vh` of height.

A figure narrower than the text column stays at its own size and sits centred: a
card stretched to 723 px is exactly the mush this avoids.

Product screenshots are **dark UI captures** and stay that way in both themes;
terminal output and JSON stay copyable text, never a picture.

### Capturing a flow, not just a page

Four of the tutorial's screens — Zitadel's login name, its password prompt, the
**2-Factor Setup** step and the forced password change — are states you can only
reach by walking the flow. The originals for them were captured in a Polish
browser, so Zitadel rendered "Witaj z powrotem!" into published docs: it picks the
login language from `Accept-Language` whenever the account carries no stored
preference, which makes the operator's OS locale part of the artifact.

`scripts/shots.mjs` replaces the human with a script, so the capture cannot
depend on whose machine produced it:

```bash
SHOT_USER=you@example.com SHOT_PASSWORD='…' \
  node scripts/shots.mjs scripts/shots/zitadel.plan.json
npm run shots:port && npm run build
```

A plan is one browser session and an ordered list of steps (`goto`, `type`,
`click`, `waitFor`, `expect`, `shot`), because a tutorial is one path through
the UI.

**The page is generated from the graph; the plan is a script it links to.** The
page — steps, prose and the figures each step shows — lives in the `tutorials`
pack of the Memory graph (infra repo: `docs/tutorials-graph.md`). The plan stays
here, hand-authored, because a selector cannot be run, diffed or debugged where
it is stored. The two cannot quietly disagree: the Tutorial names this plan in
`capture_plan`, and a check compares the figures on the page with the plan's
`shot` entries, in order. The retracted draft of this tutorial is what drift
looks like: a page describing v4's login beside pictures of v1's.

```bash
EXP=/root/emergent.memory.infra/scripts/tutorials-export.py
python3 $EXP --md    deploy-to-railway --out docs/site/tutorials/deploy-to-railway.md
python3 $EXP --check --captures /root/emergent.memory-wt/astro-docs-poc
```

`--md` reproduces the published page **byte for byte**, so a diff is the review.
`--check` reports graph-vs-snapshot drift; with `--captures` it also refuses a
page whose figures and plan disagree — a figure nothing captures, a shot nothing
uses, a reordered step (reported at the position they diverge), a capture below
4×, a light-theme capture, a non-English browser, a figure with no alt text, or a
page with pictures and no `capture_plan`. A plan it cannot read at all is exit 2
(`cannot proceed`), not drift. `shots/installer.plan.json`, the end-to-end
62-step run that produces all thirteen figures, is that linked plan.

Edit the page in the graph — hand-editing `deploy-to-railway.md` loses the next
`--md` — and edit the plan on a capture change. Nothing about the two is
generated from the other.

Credentials come from `vars`, which name **environment variables**, so no secret
is committed. `--only <shot-id>` replays the plan up to one picture.

Three guards make a bad capture fail instead of being written:

- **language** — the page's `<html lang>` must match the plan's `locale`;
- **theme** — the painted background must be dark when the plan says dark, so a
  shot cannot land in the gap before the login UI applies its stored theme
  (Zitadel reads `cp-theme`, not `prefers-color-scheme`, so the plan pins it
  with `themeStorage`);
- **expect** — required page text, so a redirect into a login wall is caught.

### Flows that need a human once: the Railway installer

`shots/installer.plan.json` walks the public installer at
`deploy.memory.emergent-company.ai` through a **real** Railway deployment — the
page claims its figures come from a real installation, so they have to. Railway
will not authorise a script it cannot see signed in, so the login happens once,
by hand, in the browser the capture uses:

```bash
node scripts/shots.mjs --login --profile .profile      # a window opens; sign in; press Enter
SHOT_NEW_PASSWORD='…' node scripts/shots.mjs scripts/shots/installer.plan.json \
  --profile .profile --out "captures/$(date +%Y-%m-%dT%H-%M)" --review
```

`SHOT_NEW_PASSWORD` is the password the plan sets when Zitadel forces the
first-admin change; it only has to satisfy Zitadel's checklist.

The profile is a capture-host file: the session stays where the human signed in
and is never copied off the machine, committed or pasted into a chat. On a host
without `sharp` (a Mac with Chrome) the runner skips the trim step and writes the
raw frames, which is why the bundle there needs only `playwright-core`.

The run takes around twenty minutes — it provisions a real deployment and waits
for Zitadel to bootstrap — so on a capture host you reach over ssh it goes under
`nohup` and is watched for the runner's own completion line rather than polled:

```bash
ssh <capture-host> 'cd ~/railway-capture && SHOT_NEW_PASSWORD=… \
  nohup node shots.mjs scripts/shots/installer.plan.json \
    --profile .profile --out "captures/$(date +%Y-%m-%dT%H-%M)" \
    --vars-out "$PWD/run.vars.json" > /tmp/capture.log 2>&1 < /dev/null &'
```

`--vars-out` writes the values the plan read or generated into
`run.vars.json` — it lifts `adminPassword` off `#admin-password`, because the
installer prints the temporary password exactly once and a resume plan has no
other way to know it. That file is a credential: it stays on the capture host
with the profile, and is never committed.

**This run creates a real Railway project** in the selected workspace, plus five
services and a bucket. Delete it afterwards unless the deployment is meant to
stay. Nothing is configured by hand: the admin address is the plan's literal
`admin@example.com`, and the generated password is lifted off `#admin-password`
with `setVar`, because the installer prints it exactly once.

The plan drives the portal's own markup, so it is a guard as well as a recipe:
`#deploy`, `#progress-step`, `#success-card`, `#open-memory` are the portal's
`cmd/deploy-portal/portal.templ` ids (infra repo), and the stage names it waits
for — *Creating Railway project*, *Waiting for Zitadel*, *Restarting Memory* —
are the strings `runner.go` writes. Rename one and the run fails instead of
publishing a stale figure.

The step kinds this flow needs, on top of `goto`/`type`/`click`/`waitFor`/
`expect`/`shot`:

- `clickIfPresent` — Railway's cookie banner is regional, so absence is not an
  error;
- `clickNewPage` — `Open Memory` opens a second tab and the plan continues there;
- `setVar` — read a value off the page into a `${var}`; the installer prints the
  temporary password exactly once;
- `waitHidden` — wait for something the *server rendered* to disappear. The
  projects page ships its bulk-delete toolbar in the HTML and hides it on boot,
  so a shot on first paint publishes a red "Delete N selected" control that no
  reader ever sees;
- `when` — guard a step on `url` or `text`, so a screen that offers itself on one
  install and not another skips instead of failing the run.

Two details are specific to Zitadel's v4 login and worth knowing before editing
any plan: its submit button only enables after **real key events**, so the plan
says `type` (character by character) rather than `fill`; and its theme and
language switchers are pinned to the viewport corner, so each `shot` clips to
the card (`clip`) rather than capturing and trimming the whole frame.

The Zitadel sequence on a **fresh install** is login name → temporary password →
**2-Factor Setup** (**Skip**, bottom left) → forced password change → "Your
password was changed successfully." → **Next** → the app. The confirmation does
not advance by itself, so the plan clicks it. The installer plan therefore starts
at `/oauth/railway/logout`, which puts the consent screen in every run instead of
only the first.

`shots/zitadel-railway.plan.json`, `app-landing.plan.json` and
`zitadel-finish.plan.json` re-shoot a subset against an **existing** install,
which is how figures 09–13 were captured without paying for a second deployment.
They pin that install's `baseUrl`, so they are a convenience for a run that is
still up, not a reproducible entry point — the reproducible one is
`installer.plan.json` against a fresh install. `shots/zitadel.plan.json` is the
different one: a throwaway dev account in the `passwordChangeRequired` state at
`https://zitadel.dev.memory.emergent-company.ai`, deleted afterwards.

Figures never carry the operator's address: the admin email is a literal
`admin@example.com` in the plan, and the temporary password is masked by the
app's own password field. The Railway workspace name is the account's own label
and stays, because the reader is looking at a real workspace.

The captures land in `docs/site/assets/tutorials/railway/`.

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
6. **The capture plan is a script, not graph data** — deliberately, and settled
   the other way round from the first draft. `installer.plan.json`, the run that
   produces all thirteen figures, is hand-authored; the graph links to it with
   `capture_plan` and `--check --captures` compares the page's figures against
   its `shot` entries, in order, so the two cannot drift silently. Storing the
   selectors as entities was tried and dropped: it made four of the runner's
   features (`when`, `clip`+`pad`, `setVar`, literal-vs-env values) into schema
   fields and left the selectors unreviewable. What the split cannot do is notice
   a *stale* picture — a capture that still matches the plan but no longer
   matches the app — which stays a `status: stale` decision and a re-run.
