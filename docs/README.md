# Documentation

What lives here, and how much to trust it.

| Tree | What it is | Published? |
|---|---|---|
| `docs-astro/` | The documentation site: Astro + Starlight, with every page a `.mdx` file under `docs-astro/src/content/docs/`. This is the only copy of each page. | **Yes** — `docs.memory.emergent-company.ai` |
| `docs/public/` | Markdown served through the `/api/docs` endpoint (guides), with categories from `docs/public/index.json`. | **Yes**, via the API |
| `docs/llms*.md` | The `llms.txt`-style entry points, hand-maintained and synced to the gh-pages root by the docs workflow. | **Yes** |
| `docs/schema.dbml` | The database schema, generated from the migrations by `apps/server/cmd/schema-dbml`. `server.yml` fails when it drifts. Do not edit by hand. | With the repo |

The authoritative source for behaviour is the code (`apps/server/`, `apps/cli/`,
`apps/web-ui/`, `apps/server/pkg/`). How we build, test, deploy and operate the
product lives in the separate infrastructure repository, not here.

## Adding a page

- User-facing guide or reference → `docs-astro/src/content/docs/`, plus a
  sidebar entry in `docs-astro/astro.config.mjs` (a page missing from the
  sidebar is reachable only by URL).
- Machine-readable guide served by the API → `docs/public/`, plus an entry in
  `docs/public/index.json`.
- Internal design note, investigation or benchmark write-up → it does not belong
  in this repository. Keep it in the infrastructure repository or the issue.
- A tutorial page is **generated** from the Memory graph, never written here —
  see `docs-astro/README.md`.

There is no MkDocs tree: `docs/site/` and `mkdocs.yml` were removed when Astro
became the only docs system.
