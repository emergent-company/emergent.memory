## Context

The CLI already has a top-level `install` command (`internal/cmd/install.go`) that installs the Emergent standalone server via Docker Compose. The new config-apply command needs a distinct name to avoid collision. The CLI also already has individual `template-packs` and `agent-definitions` subcommands backed by SDK clients in `apps/server-go/pkg/sdk/templatepacks` and `apps/server-go/pkg/sdk/agentdefinitions`. This feature composes those existing SDK clients rather than calling raw HTTP.

`gopkg.in/yaml.v3` is already a dependency of the CLI module, so YAML parsing requires no new dependencies.

## Goals / Non-Goals

**Goals:**
- New `emergent apply <path|github-url>` command (see Decision 1 on naming)
- Parse `packs/` and `agents/` subdirectories, one file per resource, JSON or YAML
- Additive-only by default; `--upgrade` enables updates
- `--dry-run` previews actions without mutating
- GitHub URL support (public repos; private repos via token)
- Per-resource output lines + final summary; non-zero exit on any error

**Non-Goals:**
- Server-side enforcement of the file schema (CLI-only validation)
- Supporting resource types beyond packs and agents in v1
- Authentication flow changes — the command uses existing CLI auth
- Recursive or nested subdirectory scanning

## Decisions

### Decision 1: Command name — `apply` not `install`

`emergent install` is taken (standalone server installer). Using `emergent apply <source>` follows the convention of config-as-code tooling (e.g., `kubectl apply`) and clearly signals "apply this config to a running instance."

**Alternative considered:** `emergent config apply`, `emergent sync`. Rejected as more verbose than needed.

### Decision 2: New package `internal/cmd/apply.go` + `internal/apply/` loader

The loader logic (file discovery, YAML/JSON parsing, GitHub fetching, resource matching) belongs in a dedicated `internal/apply/` package rather than inline in the cobra command. This keeps the command handler thin and makes the loader unit-testable.

Package layout:
```
tools/emergent-cli/internal/apply/
  loader.go       # walk packs/ and agents/, parse files
  github.go       # fetch repo as tar.gz, extract to temp dir
  applier.go      # orchestrate: load → match existing → create/update/skip
  types.go        # PackFile, AgentFile structs (the on-disk schema)
tools/emergent-cli/internal/cmd/apply.go  # cobra command, flags, output
```

**Alternative considered:** Single file in `internal/cmd/apply.go`. Rejected — loader + GitHub fetch logic is substantial enough to warrant isolation.

### Decision 3: Upsert matching by `name` field

Existing-resource detection compares the `name` field in each file against the list of resources already in the project (fetched once at the start of the run). This is a simple string match — no UUID tracking, no lockfile.

Implication: if a pack or agent is renamed in the file, the old resource is left untouched and a new one is created. The `--upgrade` flag does not rename; it only updates the matched resource.

**Alternative considered:** Matching by filename stem. Rejected — the filename is a human hint, not a stable identifier. The `name` field is what the API uses.

### Decision 4: GitHub URL fetch via archive download

For `https://github.com/org/repo[#ref]`, the loader downloads `https://codeload.github.com/org/repo/tar.gz/<ref>` (defaulting to `HEAD`), extracts to a temp directory, then applies the same local-folder logic. This avoids the GitHub API rate limits that apply to tree/contents endpoints.

For private repos, an `Authorization: token <tok>` header is added to the archive request. Token source priority: `--token` flag → `EMERGENT_GITHUB_TOKEN` env var.

**Alternative considered:** GitHub API `/repos/{owner}/{repo}/contents/{path}` per-file. Rejected — requires N API calls for N files and has tighter rate limits.

### Decision 5: YAML and JSON use the same Go structs

`types.go` defines `PackFile` and `AgentFile` structs with both `json:` and `yaml:` tags. The loader detects extension (`.yaml`/`.yml` → YAML decoder, `.json` → JSON decoder) and unmarshals into the same struct. No separate type tree for JSON vs YAML.

### Decision 6: Two-phase run (fetch-all, then apply)

The applier fetches the full list of existing packs and agents once before processing any files. This avoids N+1 API calls during the apply loop and gives `--dry-run` accurate skip/update predictions without any mutations.

## Risks / Trade-offs

- **Name collision false positives** — Two packs with the same name but different content are treated as the same resource. This is intentional (name is the stable identity), but users must be aware that renaming a resource in the file will create a duplicate rather than rename the existing one. → Mitigated by clear output lines explaining what was skipped/updated.

- **GitHub rate limiting** — The archive download endpoint is unversioned and unauthenticated for public repos; GitHub may rate-limit heavy use. → Mitigated by downloading once per run (not per file). Private repo support uses a token, which has a higher rate limit.

- **Temp directory cleanup on failure** — If the process is killed mid-run after extracting a GitHub archive, the temp dir is left behind. → Use `defer os.RemoveAll(tmpDir)` so cleanup happens on normal and panic exit; document that SIGKILL can leave temp files.

- **No rollback** — If the run creates 3 packs then fails on the 4th, the first 3 are already created. → `--dry-run` is the recommended pre-flight check. A future `--atomic` flag could be added to roll back on partial failure, but is out of scope for v1.

## Migration Plan

No server migrations required. The command is purely additive to the CLI binary.

1. Add `internal/apply/` package with tests
2. Add `internal/cmd/apply.go` cobra command
3. Wire into `rootCmd` via `init()`
4. Update `README.md` in `tools/emergent-cli/` with usage examples
5. Ship in next CLI release

## Open Questions

- Should `emergent apply` also support assigning (installing) a pack to the project after creating it, or only create the pack in the global registry? Current template-packs flow requires two steps: create pack → assign to project. The spec says "upsert template packs into the target project" — this likely means create + assign in one shot.
- For `--upgrade` on packs: the existing API has `CreatePack` (global registry) and `AssignPack` (project assignment). Updating a pack definition means calling an update endpoint on the global pack, not on the assignment. We should verify an update endpoint exists on the templatepacks SDK before tasks are written.
