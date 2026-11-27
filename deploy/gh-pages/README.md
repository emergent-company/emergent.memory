# The gh-pages root, as files

`gh-pages` no longer carries a documentation site: the docs are the Astro build
in `docs-astro/`, served from `docs.memory.emergent-company.ai`. What is left on
the branch is what consumers fetch directly from its root — the API spec
(`swagger.json`, `swagger.yaml`, `openapi.json`), the `llms*.md` entry points,
and the Sparkle appcast under `appcast/` — plus the two files here:

| File | Why |
|---|---|
| `index.html` | The old site's home page. It is now a redirect to the docs host, so a bookmark or an old search result lands somewhere useful instead of on a 404. |
| `404.html` | GitHub Pages serves this for any missing path, including an old deep link like `/latest/user-guide/getting-started/`. It names the docs host and still points at `/swagger.json`. |

They live in the repo, not only on the branch, so the state is reviewable and the
publish workflow re-creates it: `.github/workflows/publish-gh-pages-raw.yml` copies
this directory to the branch root alongside the generated spec. A file added here
without a `cp` line in that workflow never reaches the branch.
