## Context

The Memory CLI (`tools/cli/`) is a Go/Cobra application with an existing set of commands for managing projects (`projects set/create`), provider credentials (`provider configure`), and agent skills (`install-memory-skills`). Users must discover and run these commands independently, which creates friction for first-time setup.

The CLI already has:
- A Bubbletea-based interactive project picker (`picker.go`) with arrow-key navigation and filter/search
- `godotenv` for reading/writing `.env.local` files
- `golang.org/x/term` for terminal detection and password masking
- SDK client for all server API calls (projects, tokens, providers, orgs)
- A `runInstallMemorySkills` function that copies embedded skills to `.agents/skills/`

The new `memory init` command composes these existing pieces into a single guided wizard, adding minimal new code.

## Goals / Non-Goals

**Goals:**
- Single command (`memory init`) to go from zero to a fully configured Memory project in the current directory
- Detect current folder name and suggest it as the default project name
- Reuse existing Bubbletea picker for project selection with a "Create new" option prepended
- Check and configure org-level LLM provider credentials (Google AI or Vertex AI)
- For Vertex AI: detect `gcloud` CLI presence and authentication state, guide accordingly
- Install Memory skills to `.agents/skills/`
- Write `.env.local` with project context and auto-add it to `.gitignore`
- Idempotent re-runs: detect existing `.env.local` config and verify/offer to reconfigure each step

**Non-Goals:**
- Template pack design/installation (handled by the `memory-onboard` skill after init)
- Document upload or graph population (post-init workflow)
- Project-level provider overrides (org-level is sufficient for init)
- Server installation (`memory install` is a separate command)
- Changing the `projects set` or `provider configure` commands themselves

## Decisions

### 1. Single new file: `init_project.go`

All init logic lives in one new file `tools/cli/internal/cmd/init_project.go` registered via `init()` (same pattern as every other command file). No changes to `root.go` needed — cobra auto-discovers via the `init()` function.

**Rationale:** Keeps the change self-contained. The `init()` function in each Go file runs at import time, so adding `rootCmd.AddCommand(initProjectCmd)` in the new file's `init()` is the standard pattern used by all existing commands (e.g., `install.go`, `projects.go`).

### 2. Reuse `PickProject` with a synthetic "Create new" item

Prepend a `PickerItem{ID: "__create__", Name: "+ Create new project"}` to the items list passed to `PickProject`. After selection, check if the returned ID is the sentinel value and branch to creation flow.

**Alternative considered:** Building a separate custom Bubbletea model for project creation. Rejected because the existing picker already handles filtering, keyboard navigation, and timeouts — adding one synthetic item is simpler and maintains UI consistency.

### 3. Project name input via `bufio` with default prompt

Use `fmt.Printf("Project name [%s]: ", folderName)` + `bufio.Scanner`. Empty input = accept default. This matches the existing CLI's prompt style (used in `runSetProject` and `promptYesNo`).

**Alternative considered:** Using Bubbletea text input for editable pre-filled text. Rejected because it adds complexity for a one-shot input, and every other interactive prompt in the CLI uses simple `bufio`/`fmt.Scanln` patterns.

### 4. Masked API key input via `term.ReadPassword`

For Google AI API key entry, use `term.ReadPassword(int(os.Stdin.Fd()))` which suppresses echo. Already a dependency.

**Alternative considered:** Plain text input. Rejected because API keys are sensitive credentials that shouldn't be visible on screen.

### 5. gcloud detection via `exec.LookPath` + subprocess

For Vertex AI: (a) `exec.LookPath("gcloud")` to check installation, (b) `exec.Command("gcloud", "auth", "application-default", "print-access-token").CombinedOutput()` to check if user is authenticated. If both pass, prompt for GCP project ID and region, then call `c.SDK.Provider.UpsertOrgConfig`.

If gcloud is missing or not authenticated, print step-by-step instructions rather than failing.

**Alternative considered:** Requiring users to always provide a service account JSON file. Rejected because many developers use application default credentials which are simpler.

### 6. Provider picker via existing `pickResourceWithTitle`

Use the existing `pickResourceWithTitle` from `picker.go` to show a 3-item list: "Google AI (API key)", "Vertex AI (GCP)", "Skip for now".

### 7. Idempotent re-run detection

On entry, read `.env.local` and check for `MEMORY_PROJECT_ID`. If found:
- Validate the project still exists on the server (quick `Projects.Get` call)
- Show: "Already initialized for project X. Verify settings? [Y/n]"
- If yes: run through each step, showing current state and offering to change
- If no: exit cleanly

This avoids duplicating tokens or creating unwanted projects.

### 8. `.gitignore` auto-update

After writing `.env.local`, scan `.gitignore` for `.env.local`. If not found, append it. Create `.gitignore` if it doesn't exist. No prompt needed — this is a security best practice.

## Risks / Trade-offs

- **[Risk] gcloud subprocess calls may be slow or noisy** → Mitigated by only executing when user explicitly chooses Vertex AI, and using short timeouts via `context.WithTimeout`.
- **[Risk] Bubbletea picker with synthetic "Create new" item may confuse filter/search** → Mitigated because the picker's filter matches on Name, and "+ Create new project" is a clear label that users won't accidentally filter to.
- **[Risk] Re-running init with stale `.env.local` pointing to a deleted project** → Mitigated by validating the project ID with the server at the start of the re-run flow.
- **[Trade-off] No Bubbletea text input for project name** → Simpler code but less polished than a pre-filled editable field. Acceptable because project creation is a one-time action.
