## Why

Configuring an Emergent instance today is a manual, UI-driven process with no way to version-control or reproduce the setup. A declarative file format — applied with a single `memory blueprints` command — enables sharing configurations as git repos, bootstrapping new projects from templates, and treating Emergent config as code.

## What Changes

- New `memory blueprints <path>` CLI subcommand that reads a structured directory and applies its contents to an Emergent project
- A defined folder-based config format: `packs/` (one file per template pack) and `agents/` (one file per agent definition), each supporting both JSON and YAML
- Apply logic: parse all files in each subfolder, upsert template packs and agent definitions into the target project
- GitHub URL support: `memory blueprints <github-url>` fetches the repo contents before applying (same logic as local folder)

## Capabilities

### New Capabilities

- `blueprint-format`: The on-disk directory structure and per-file schema for template packs and agent definitions (JSON and YAML, folder-per-resource-type, one resource per file)
- `blueprints-command`: The `memory blueprints <path|github-url>` CLI command — parses the format, resolves GitHub URLs, and applies resources to a project via the Emergent API

### Modified Capabilities

- `agent-definitions`: Agent definitions can now be created/updated via the blueprints command in addition to product manifests — no new requirements, but the command calls the existing agent definition APIs
- `template-packs`: Template packs can now be created/updated via blueprints — no new requirements, but the command calls existing template pack APIs

## Impact

- **CLI** (`tools/emergent-cli/`): new `blueprints` subcommand and supporting parser/loader packages in `internal/blueprints/`
- **API**: no new endpoints required — blueprints command drives existing template pack and agent definition REST APIs
- **File format**: new schema definition (documented in spec, not enforced server-side)
- **GitHub URL support**: HTTP fetch of raw repo contents (no GitHub API auth required for public repos; token support for private repos via `--token` flag or `MEMORY_GITHUB_TOKEN` env var)
- **No breaking changes**
