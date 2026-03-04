## 1. File Format Types (`internal/apply/types.go`)

- [x] 1.1 Define `PackFile` struct with `json:` and `yaml:` tags matching the install-format spec (name, version, description, author, license, repositoryUrl, documentationUrl, objectTypes, relationshipTypes, uiConfigs, extractionPrompts)
- [x] 1.2 Define `AgentFile` struct with `json:` and `yaml:` tags matching the install-format spec (name, description, systemPrompt, model, tools, flowType, isDefault, maxSteps, defaultTimeout, visibility, config, workspaceConfig)
- [x] 1.3 Define `ObjectTypeDef` and `RelationshipTypeDef` structs (nested in PackFile)
- [x] 1.4 Define `ApplyResult` struct to track per-resource outcome (resource type, name, action: created/updated/skipped/error, error message)

## 2. File Loader (`internal/apply/loader.go`)

- [x] 2.1 Implement `LoadDir(dir string) ([]PackFile, []AgentFile, error)` that walks `packs/` and `agents/` subdirs
- [x] 2.2 Support `.json`, `.yaml`, `.yml` extensions; skip all other files silently
- [x] 2.3 Detect file format by extension and route to JSON or YAML decoder into the appropriate struct
- [x] 2.4 On parse error for a file, record the error in results and continue processing remaining files (do not abort)
- [x] 2.5 Validate required fields after parsing (`name` for agents; `name`, `version`, `objectTypes` for packs); record validation errors and skip invalid files
- [x] 2.6 Return gracefully if `packs/` or `agents/` subdir does not exist (not an error)

## 3. GitHub Fetcher (`internal/apply/github.go`)

- [x] 3.1 Implement `FetchGitHubRepo(rawURL, token string) (localDir string, cleanup func(), err error)` that parses a `https://github.com/org/repo[#ref]` URL
- [x] 3.2 Download `https://codeload.github.com/{org}/{repo}/tar.gz/{ref}` (default ref: `HEAD`) with optional `Authorization: token <tok>` header
- [x] 3.3 Extract the tar.gz to a temp directory, stripping the top-level repo prefix so `packs/` and `agents/` are at the root
- [x] 3.4 Return cleanup function (`os.RemoveAll`) for the caller to defer
- [x] 3.5 On HTTP 404 with no token, return error: `repository not found or requires authentication — set EMERGENT_GITHUB_TOKEN or pass --token`
- [x] 3.6 Detect GitHub URL vs local path in the applier by checking if the source string starts with `https://github.com/`

## 4. Pack Update Endpoint (server-side prerequisite for `--upgrade`)

- [x] 4.1 Add `PUT /api/template-packs/:packId` route and `UpdatePack` handler in `apps/server-go/domain/templatepacks/`
- [x] 4.2 Add `UpdatePackRequest` struct (same fields as `CreatePackRequest`, all optional via pointers)
- [x] 4.3 Implement `UpdatePack` service method with partial update logic (only update non-nil fields)
- [x] 4.4 Add `UpdatePack(ctx, packID, req)` method to `apps/server-go/pkg/sdk/templatepacks/client.go`

## 5. Applier (`internal/apply/applier.go`)

- [x] 5.1 Implement `Applier` struct holding SDK clients (templatepacks, agentdefinitions), flags (dryRun, upgrade), and output writer
- [x] 5.2 On `Run()`: fetch all existing packs (`GetAvailablePacks`) and agents (`List`) once up front; build name→ID maps for O(1) lookup
- [x] 5.3 For each parsed pack file: check name against existing packs map
  - not found → create pack via `CreatePack` then assign to project via `AssignPack`
  - found + `--upgrade` → update pack via `UpdatePack`
  - found + no `--upgrade` → skip with hint message
- [x] 5.4 For each parsed agent file: check name against existing agents map
  - not found → create via `AgentDefinitions.Create`
  - found + `--upgrade` → update via `AgentDefinitions.Update` (using existing agent ID)
  - found + no `--upgrade` → skip with hint message
- [x] 5.5 In `--dry-run` mode: print `[dry-run] would <action> <type> "<name>"` for every file; make zero API calls (skip fetch-existing calls too, treat all as "would create" unless `--upgrade` context matters)
- [x] 5.6 Collect all `ApplyResult` entries; print per-resource output line after each action
- [x] 5.7 After processing all files, print summary line: `Apply complete: N created, N updated, N skipped, N errors` (or `Dry run complete: ...` variant)

## 6. CLI Command (`internal/cmd/apply.go`)

- [x] 6.1 Create `applyCmd` cobra command with `Use: "apply <source>"`, `Args: cobra.ExactArgs(1)`
- [x] 6.2 Add flags: `--project` (string, overrides `EMERGENT_PROJECT_ID`), `--upgrade` (bool), `--dry-run` (bool), `--token` (string, GitHub token)
- [x] 6.3 Resolve source: if GitHub URL call `github.FetchGitHubRepo` with `--token` / `EMERGENT_GITHUB_TOKEN`, defer cleanup; otherwise treat as local path
- [x] 6.4 Call `loader.LoadDir(dir)` to parse files
- [x] 6.5 Build `Applier` with resolved SDK clients and flags; call `applier.Run()`
- [x] 6.6 Exit non-zero if any `ApplyResult` has action `error`
- [x] 6.7 Wire `applyCmd` into `rootCmd` in `init()`

## 7. Tests

- [x] 7.1 Unit test `LoadDir`: valid YAML pack, valid JSON agent, unknown extension skipped, missing required field skipped with error, missing subdir is not an error
- [x] 7.2 Unit test `Applier` with mock SDK clients: create-only (default), skip existing (no `--upgrade`), update existing (`--upgrade`), dry-run outputs correct lines and makes no API calls
- [x] 7.3 Unit test GitHub URL parsing and ref extraction (no network calls needed — test URL parsing logic only)
