## Why

The Go SDK (`apps/server-go/pkg/sdk`) and the Swift SDK (Mac app `Emergent/Core/`) have no public-facing documentation beyond a basic README. Developers integrating with Emergent have no reference site, no per-package API guide, and no LLM-consumable reference — creating friction for adoption, integration, and AI-assisted development. This change establishes a comprehensive documentation system published to GitHub Pages with MkDocs Material, covering all 30 Go SDK clients and the full Swift Core API layer, plus machine-readable LLM reference files.

## What Changes

- **New `docs/site/` directory** — MkDocs Material source tree with full nav structure for both SDKs
- **New `mkdocs.yml`** — MkDocs Material config at repo root (or `docs/mkdocs.yml`) with nav, plugins, and theme settings
- **Go SDK reference pages** — Dedicated markdown page per package (30 packages: `graph`, `documents`, `search`, `chat`, `chunks`, `chunking`, `agents`, `agentdefinitions`, `branches`, `projects`, `orgs`, `users`, `apitokens`, `mcp`, `mcpregistry`, `datasources`, `discoveryjobs`, `embeddingpolicies`, `integrations`, `templatepacks`, `typeregistry`, `notifications`, `tasks`, `monitoring`, `useractivity`, `superadmin`, `health`, `apidocs`, `provider`) plus top-level `auth`, `errors`, `testutil`, and `graphutil` sub-packages
- **Go SDK narrative guides** — Quickstart, authentication (API key / OAuth / device flow), multi-tenancy context, error handling, streaming/SSE, the dual-ID graph model (ID/VersionID vs CanonicalID/EntityID), working with examples
- **Swift SDK section** — Documents `EmergentAPIClient` (all 15 methods), all 16 `Models.swift` public types, `EmergentAPIError` cases, plus a note on the planned formal `EmergentSwiftSDK` (CGO bridge, depends on upstream `emergent-company/emergent#49`)
- **LLM-friendly reference files** — `docs/llms.md` (combined both SDKs), `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md` — structured markdown optimised for LLM context injection
- **New `.github/workflows/docs.yml`** — GitHub Actions workflow that builds MkDocs and deploys to the `gh-pages` branch on every push to `main`; runs a build-only check on PRs

## Capabilities

### New Capabilities

- `go-sdk-docs`: Full per-package API reference + narrative guides for the Go SDK (30 clients + auth/errors/testutil), published as a MkDocs Material site section
- `swift-sdk-docs`: API reference for the Swift Core layer (`EmergentAPIClient`, all public models), published as a MkDocs Material site section, with forward-reference to the planned formal Swift SDK
- `docs-site-infrastructure`: MkDocs Material site scaffold (`mkdocs.yml`, `docs/site/` structure, navigation, theme config, search plugin, and GitHub Pages deployment via CI)
- `llm-reference-files`: Machine-readable SDK reference markdown files (`docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`) suitable for LLM context injection and `llms.txt` conventions

### Modified Capabilities

- `go-sdk`: The `apps/server-go/pkg/sdk/README.md` will be updated to link to the new GitHub Pages site and the per-package reference; the existing content is retained but restructured as a "Getting Started" entry point
- `api-documentation`: The existing OpenAPI spec capability (`/openapi.json`) will be cross-referenced from the new docs site — no requirement changes, only a nav link

## Impact

- **New files:** `mkdocs.yml`, `docs/site/**` (~40+ markdown pages), `docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md`, `.github/workflows/docs.yml`
- **Modified files:** `apps/server-go/pkg/sdk/README.md` (add GitHub Pages link)
- **No Go source changes:** All documentation is external markdown; no godoc comment changes are required (though inline improvements will be noted as stretch tasks)
- **GitHub repo settings:** GitHub Pages must be enabled for the `emergent-company/emergent` repo, pointing to the `gh-pages` branch (one-time manual step)
- **Python dependency:** MkDocs Material requires `pip install mkdocs-material` (captured in `docs/requirements.txt`)
- **Cross-repo:** Swift SDK source lives in `emergent-company/emergent.memory.mac`; its documentation is authored here in the server repo under `docs/site/swift-sdk/` and will reference the source file locations
