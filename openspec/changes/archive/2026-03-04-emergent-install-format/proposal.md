## Why

Configuring an Emergent instance today is a manual, UI-driven process with no way to version-control or reproduce the setup. A declarative file format — installed with a single `emergent install` command — enables sharing configurations as git repos, bootstrapping new projects from templates, and treating Emergent config as code.

## What Changes

- New `emergent install <path>` CLI subcommand that reads a structured directory and applies its contents to an Emergent project
- A defined folder-based config format: `packs/` (one file per template pack) and `agents/` (one file per agent definition), each supporting both JSON and YAML
- Install logic: parse all files in each subfolder, upsert template packs and agent definitions into the target project
- GitHub URL support: `emergent install <github-url>` fetches the repo contents before applying (same logic as local folder)

## Capabilities

### New Capabilities

- `install-format`: The on-disk directory structure and per-file schema for template packs and agent definitions (JSON and YAML, folder-per-resource-type, one resource per file)
- `install-command`: The `emergent install <path|github-url>` CLI command — parses the format, resolves GitHub URLs, and applies resources to a project via the Emergent API

### Modified Capabilities

- `agent-definitions`: Agent definitions can now be created/updated via the install command in addition to product manifests — no new requirements, but the install command will call the existing agent definition APIs
- `template-packs`: Template packs can now be created/updated via install — no new requirements, but the install command will call existing template pack APIs

## Impact

- **CLI** (`tools/emergent-cli/`): new `install` subcommand and supporting parser/loader packages
- **API**: no new endpoints required — install command drives existing template pack and agent definition REST APIs
- **File format**: new schema definition (documented in spec, not enforced server-side)
- **GitHub URL support**: HTTP fetch of raw repo contents (no GitHub API auth required for public repos; token support for private repos via `--token` flag or `EMERGENT_GITHUB_TOKEN` env var)
- **No breaking changes**
