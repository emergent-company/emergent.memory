## Why

There is no unified onboarding command for the Memory CLI. Users must manually run `memory projects set`, `memory provider configure`, and `memory install-memory-skills` as separate steps, often not knowing what order to follow or what configuration is needed. A guided `memory init` wizard would reduce friction for first-time setup and provide a single command to initialize (or re-verify) a project in any directory.

## What Changes

- Add a new `memory init` CLI command that runs an interactive wizard combining project selection/creation, provider configuration, and skills installation into one guided flow.
- The wizard detects the current folder name and suggests it as the default project name when creating a new project.
- It writes `MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, and `MEMORY_PROJECT_TOKEN` to `.env.local` and auto-adds `.env.local` to `.gitignore`.
- It checks for existing org-level LLM provider credentials and offers to configure Google AI (API key) or Vertex AI (with gcloud detection) if none exist.
- It offers to install Memory skills to `.agents/skills/` for AI coding agents.
- Running `memory init` again in an already-initialized directory detects existing config and offers to verify/reconfigure each step rather than blindly overwriting.

## Capabilities

### New Capabilities
- `cli-init-wizard`: Interactive `memory init` command that orchestrates project selection/creation, provider setup, skills installation, and `.env.local` configuration in a single guided flow. Supports idempotent re-runs that verify existing settings.

### Modified Capabilities
<!-- No existing spec-level requirements are changing. The init command composes existing functionality (projects set, provider configure, install-memory-skills) behind a new interactive wrapper. -->

## Impact

- **Code**: New file `tools/cli/internal/cmd/init_project.go` registered in `rootCmd`. Reuses existing helpers: `PickProject`, `getClient`, `resolveProviderOrgID`, `godotenv`, `runInstallMemorySkills` logic, and `term.ReadPassword` for masked API key input.
- **Dependencies**: No new Go module dependencies. Uses `os/exec` for gcloud detection, `golang.org/x/term` (already a dependency) for masked input, `github.com/joho/godotenv` (already a dependency) for `.env.local` management.
- **APIs**: Calls existing SDK methods (`Projects.List`, `Projects.Create`, `APITokens.Create`, `Provider.ListOrgConfigs`, `Provider.UpsertOrgConfig`, `Provider.TestProvider`). No server-side changes needed.
- **User-facing**: New top-level CLI command `memory init` with flags `--skip-provider` and `--skip-skills`.
