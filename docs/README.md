# Documentation map

Three kinds of prose live under `docs/`, and they are not equally trustworthy.

| Tree | What it is | Trust it as current? |
|---|---|---|
| `docs/site/` | The documentation site. `mkdocs.yml` sets `docs_dir: docs/site`, so this is what gets published. | **Yes** — user-facing, kept current. |
| `docs/public/` | Markdown served through the `/api/docs` endpoint (guides and API reference). | **Yes** — user-facing, kept current. |
| `docs/llms*.md` | The `llms.txt`-style entry points, published by the docs workflow. | **Yes** — kept current. |
| `docs/features/`, `docs/technical/`, `docs/integrations/`, `docs/security/`, `docs/guides/`, `docs/database/`, `docs/graph/`, `docs/patterns/`, `docs/evaluation/`, `docs/wireframes/`, `docs/research/`, and the remaining top-level `docs/*.md` | Contributor and design notes, written over the project's life. Some describe the current server; many describe the NestJS backend that was removed. | **Only after checking for the marker below.** |
| `docs/archive/` | Historical by definition — records, not guidance. Created when the first note is archived. | **No.** |
| `docs/hooks/` | A build script for the `llms*.md` files, not prose. | n/a |

The authoritative source for current behaviour is the code (`apps/server/`,
`apps/cli/`, `apps/web-ui/`, `apps/server/pkg/`), and the published docs in
`docs/site/`. How the team builds, tests, deploys and operates the product lives
in the separate infrastructure repository, not here.

## The historical marker

A note written against the removed NestJS backend quotes paths under
`apps/server/src/...`, which no longer exist. Those notes keep real design
value, so they are kept — but they must not read as current guidance. Every one
of them carries a banner near the top, either this uniform one:

```markdown
> **Status: historical design note (pre-Go).** Written against the NestJS
> backend (`apps/server/src/...`), which has been removed; those paths no longer
> exist. Kept for design history — check the current Go server under
> `apps/server/` and the published docs in `docs/site/` before relying on
> anything here.
<!-- status: historical (pre-Go) -->
```

or a specific one written when the note was retired, with the same
`<!-- status: historical (pre-Go) -->` marker appended.

`scripts/preflight/docs-stale-paths.sh` enforces it: a markdown file outside
`docs/archive/` that names `apps/server/src` must carry the marker **and** a
visible line saying it is historical, or the pre-commit hook and the `ci`
preflight job fail (#2159).

The way out of the marker is a rewrite: describe the behaviour against the Go
server, name the package that implements it, and drop the stale references. A
rewritten note is current guidance again and does not need the banner. A note
that is only a record moves to `docs/archive/`, which is exempt.
