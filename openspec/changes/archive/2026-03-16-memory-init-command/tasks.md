<!-- Baseline failures (pre-existing, not introduced by this change):
- None. `go build ./tools/cli/...` passes cleanly.
-->

## 1. Command Scaffolding

- [x] 1.1 Create `tools/cli/internal/cmd/init_project.go` with package declaration, imports, cobra command variable (`initProjectCmd`), `--skip-provider` and `--skip-skills` flags, and `init()` function that registers with `rootCmd`
- [x] 1.2 Add non-interactive terminal guard at the top of `RunE` — check `isInteractiveTerminal()` and return an error if not interactive

## 2. Idempotent Re-run Detection

- [x] 2.1 Read `.env.local` via `godotenv.Read` and check for `MEMORY_PROJECT_ID`; if absent, proceed to fresh-run flow
- [x] 2.2 If `MEMORY_PROJECT_ID` exists, validate the project on the server via `Projects.List` (find by ID); if not found, print warning and fall through to fresh-run
- [x] 2.3 If project is valid, prompt "Already initialized for project <name>. Verify settings? [Y/n]" using `promptYesNo`; if declined, exit cleanly

## 3. Project Selection and Creation

- [x] 3.1 Fetch projects via `SDK.Projects.List`, build `[]PickerItem` list, prepend synthetic `PickerItem{ID: "__create__", Name: "+ Create new project"}`
- [x] 3.2 Display picker using `pickResourceWithTitle` (or `PickProject` variant); capture selected item
- [x] 3.3 If selected ID is `"__create__"`: detect current folder name via `os.Getwd` + `filepath.Base`, prompt with `bufio` showing folder name as default in brackets, create project via `SDK.Projects.Create`
- [x] 3.4 If selected ID is an existing project: use that project's ID and name directly

## 4. Token and .env.local Persistence

- [x] 4.1 List existing tokens via `SDK.APITokens.List`; if a token exists, retrieve it via `SDK.APITokens.Get`
- [x] 4.2 If no token available, create one via `SDK.APITokens.Create` with name `"cli-auto-token"` and scopes `["data:read", "data:write", "schema:read"]`
- [x] 4.3 Write `MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, `MEMORY_PROJECT_TOKEN` to `.env.local` via `godotenv.Write`, preserving existing keys
- [x] 4.4 Update global config (`~/.memory/config.yaml`) with `ProjectID`; warn on failure but do not abort

## 5. .gitignore Auto-update

- [x] 5.1 Read `.gitignore` if it exists; check if `.env.local` is already listed (line-by-line scan)
- [x] 5.2 If not listed, append `.env.local` to `.gitignore`; if `.gitignore` does not exist, create it with `.env.local` as content

## 6. Provider Check and Configuration

- [x] 6.1 If `--skip-provider` is set, skip this entire section
- [x] 6.2 Resolve org ID via `resolveProviderOrgID`; list existing org provider configs via `SDK.Provider.ListOrgConfigs`
- [x] 6.3 If provider already configured, print confirmation and skip to next step
- [x] 6.4 Display provider picker via `pickResourceWithTitle` with options: "Google AI (API key)", "Vertex AI (GCP)", "Skip for now"
- [x] 6.5 Google AI path: prompt for API key via `term.ReadPassword`, call `SDK.Provider.UpsertOrgConfig` with provider `"google"`, run test, print result
- [x] 6.6 Vertex AI path: check `exec.LookPath("gcloud")` and run `gcloud auth application-default print-access-token` to verify auth; if both pass, prompt for GCP project ID and region via `bufio`, call `UpsertOrgConfig` with provider `"google-vertex"`; if gcloud missing or unauthed, print setup instructions and continue
- [x] 6.7 Skip path: proceed to next step with no action

## 7. Skills Installation

- [x] 7.1 If `--skip-skills` is set, skip this entire section
- [x] 7.2 Prompt "Install Memory skills? [Y/n]" using `promptYesNo`; if declined, skip
- [x] 7.3 If accepted, invoke the same logic as `runInstallMemorySkills` — enumerate embedded `memory-*` skills from `skillsfs.Catalog()`, copy to `.agents/skills/`, skip existing

## 8. Completion Summary

- [x] 8.1 Print a summary showing: project name and ID, provider status (configured / skipped / already present), skills status (installed / skipped), and a suggested next command

## 9. Verification During Re-run

- [x] 9.1 In verify mode (re-run with user accepting verification), show current project context and offer to switch projects (re-enter project picker)
- [x] 9.2 Show current provider status and offer to reconfigure (re-enter provider flow)
- [x] 9.3 Show current skills status and offer to reinstall (re-enter skills flow)
