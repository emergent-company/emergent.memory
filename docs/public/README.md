# Public documentation (served by `/api/docs`)

Markdown in this directory is served by the server's documentation API, indexed by
[`index.json`](index.json).

## Layout

```
docs/public/
├── guides/          # how-to guides (schema creation, …)
├── api-reference/   # API / MCP references
└── index.json       # metadata catalog
```

## File format

Each document is Markdown with YAML frontmatter:

```markdown
---
id: unique-identifier
title: Document Title
category: guides|api-reference
tags: [tag1, tag2]
description: One or two sentences.
lastUpdated: YYYY-MM-DD
readTime: 15
related: [other-doc-id]
---

# Document content starts here
```

## Adding a document

1. Put the file under `guides/` or `api-reference/`.
2. Add the frontmatter above.
3. Add a matching entry to `index.json` — `GET /api/docs` walks this directory,
   but `GET /api/docs/categories` reads `index.json`, so an unlisted document is
   served under a category the API never advertises.
4. Restart the server: parsed documents are cached in memory and there is no hot
   reload.

## How the server finds this directory

The service reads `docs/public` relative to its **working directory**. Run from
`apps/server/`, that resolves to `apps/server/docs/public`, so development needs a
symlink once per checkout:

```bash
ln -s ../../docs apps/server/docs
```

The container image does not ship this directory, so the endpoint is a
development/self-hosted convenience rather than a hosted API surface.

## Endpoints

- `GET /api/docs` — list documents with metadata
- `GET /api/docs/:slug` — one document with its Markdown body
- `GET /api/docs/categories` — category summary

```bash
curl -s localhost:3002/api/docs | jq '.documents[].slug'
curl -s localhost:3002/api/docs/schema-creation | jq -r '.content' | head
```
