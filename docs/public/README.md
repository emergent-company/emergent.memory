# Public Documentation

This directory contains documentation files served via the `/api/docs` REST API endpoint.

## Structure

```
docs/public/
├── guides/          # How-to guides and tutorials
├── tutorials/       # Step-by-step tutorials
├── examples/        # Example configurations and use cases
├── api-reference/   # API documentation and MCP references
└── index.json       # Metadata catalog
```

## File Format

All documentation files use Markdown with YAML frontmatter:

```markdown
---
id: unique-identifier
title: Document Title
category: guides|tutorials|examples|api-reference
tags: [tag1, tag2, tag3]
description: Brief description (1-2 sentences)
lastUpdated: YYYY-MM-DD
readTime: 15 (estimated minutes)
related: [doc-id-1, doc-id-2]
---

# Document content starts here...
```

## Adding New Documents

1. Create markdown file in appropriate category directory
2. Add YAML frontmatter with required fields
3. Update `index.json` with document metadata
4. Restart Go server to clear cache: `cd apps/server-go && make run`

## Important: Symlink Requirement

The Go server runs from `apps/server-go/` and expects docs at `docs/public/` relative to its working directory.

**Setup symlink** (required):

```bash
cd apps/server-go
ln -s /root/emergent/docs docs
```

This creates `apps/server-go/docs -> /root/emergent/docs` so the server can access `docs/public/`.

## Cache Behavior

The Go documentation service caches parsed documents in memory for performance. **Changes to markdown files require a server restart** to be visible:

```bash
# Restart Go server
cd /root/emergent/apps/server-go
make run
```

There is no hot reload for the docs service - this is by design for production performance.

## API Endpoints

- `GET /api/docs` - List all documents with metadata
- `GET /api/docs/:slug` - Get specific document with full markdown content
- `GET /api/docs/categories` - Get category information

## Testing

```bash
# List documents
curl http://localhost:5300/api/docs | jq '.'

# Get specific document
curl http://localhost:5300/api/docs/template-pack-creation | jq '.title'

# Get categories
curl http://localhost:5300/api/docs/categories | jq '.categories[].name'
```

## Current Documents

- **Template Pack Creation Guide** (guides) - 21KB
- **Environment Setup Guide** (guides) - 74KB
- **MCP Quick Reference** (api-reference) - 3.7KB
