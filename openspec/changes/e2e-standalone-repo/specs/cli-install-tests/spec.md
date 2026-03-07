## ADDED Requirements

### Requirement: Each test runs in an isolated HOME directory
Every CLI subprocess invoked via `mustRunCLIInDir()` SHALL receive a unique `HOME` directory (via `t.TempDir()`) injected into its environment. No test SHALL read or write `~/.memory/credentials.json` of another test, regardless of execution order.

#### Scenario: Credentials do not bleed between tests
- **WHEN** `TestCLIInstalled_SetToken` and `TestCLIInstalled_ProjectsList` both run in the same `go test` invocation
- **THEN** the `set-token` call in one test does not affect the credential state seen by CLI subprocesses in the other test

#### Scenario: Each subprocess HOME is a temp directory
- **WHEN** `mustRunCLIInDir()` executes any `memory` sub-command
- **THEN** the subprocess has `HOME` set to a directory under `os.TempDir()` that is unique to the calling test

---

### Requirement: CLI binary is installed via install.sh at runtime
The test container entrypoint SHALL install the `memory` CLI by running the official `install.sh` script from the GitHub Release at container startup — not at image build time. This ensures the install path itself is tested.

#### Scenario: CLI installed from release script
- **WHEN** the test container starts
- **THEN** `install.sh` downloads and installs the `memory` binary, placing it on `PATH` before any tests run

---

### Requirement: Version and help smoke tests
The suite SHALL include tests that verify the installed `memory` binary responds to basic introspection commands.

#### Scenario: memory version
- **WHEN** `TestCLIInstalled_Version` runs
- **THEN** `memory version` exits 0 and produces output containing `"memory"` or `"version"`

#### Scenario: memory --help
- **WHEN** `TestCLIInstalled_Help` runs
- **THEN** `memory --help` exits 0 and the output contains the sub-commands `skills`, `projects`, and `login`

---

### Requirement: set-token writes credentials file
The suite SHALL verify that `memory set-token <token> --server <url>` stores credentials on disk.

#### Scenario: Credentials file created
- **WHEN** `TestCLIInstalled_SetToken` runs against a reachable server
- **THEN** `~/.memory/credentials.json` exists after the command exits 0

#### Scenario: Server unreachable skips test
- **WHEN** `MEMORY_TEST_SERVER` is empty or the server is down
- **THEN** the test is skipped (not failed)

---

### Requirement: Emergent skills are installed by memory skills install
The suite SHALL verify that `memory skills install --force` creates the expected `emergent-*` skill directories under `.agents/skills/` in a fresh workspace.

#### Scenario: All emergent skills present
- **WHEN** `TestCLIInstalled_SkillsInstall` runs
- **THEN** directories for `emergent-onboard`, `emergent-query`, `emergent-agents`, `emergent-mcp-servers`, `emergent-providers`, `emergent-template-packs` all exist under `<workspace>/.agents/skills/`

#### Scenario: Non-emergent skills absent
- **WHEN** `TestCLIInstalled_SkillsInstall_NonEmergentSkillsAbsent` runs
- **THEN** directories for `commit`, `release`, `pr-review-and-fix` do NOT exist under `<workspace>/.agents/skills/`

---

### Requirement: Installed skills have valid SKILL.md frontmatter
The suite SHALL verify that every installed skill directory contains a `SKILL.md` with non-empty `name` and `description` fields, and that the `name` matches the directory name.

#### Scenario: SKILL.md frontmatter valid for all skills
- **WHEN** `TestCLIInstalled_SkillsValid` runs after `skills install`
- **THEN** every skill directory passes: `SKILL.md` exists, `name` is non-empty and matches the directory name, `description` is non-empty

---

### Requirement: skills list reflects installed skills
The suite SHALL verify that `memory skills list` reports installed skills after `memory skills install` has run.

#### Scenario: List output contains installed skills
- **WHEN** `TestCLIInstalled_SkillsList` runs
- **THEN** the output of `memory skills list` contains `"emergent-onboard"` and `"emergent-query"`

---

### Requirement: opencode binary is present and functional
The suite SHALL verify that `opencode` is on `PATH` and produces output when invoked.

#### Scenario: opencode found on PATH
- **WHEN** `TestOpencodeInstalled` runs
- **THEN** `opencode --version` produces non-empty output (exit code may be non-zero)

---

### Requirement: opencode boots successfully with installed skills
The suite SHALL verify the full pipeline: install skills → start `opencode serve` → server reaches "listening on".

#### Scenario: opencode serve starts with skills
- **WHEN** `TestOpencodeSeesInstalledSkills` runs in a workspace with skills installed
- **THEN** `opencode serve` outputs `"listening on"` within 25 seconds

---

### Requirement: Authenticated projects list round-trip
The suite SHALL verify a full set-token → projects list authenticated round-trip against the test server.

#### Scenario: Authenticated list succeeds
- **WHEN** `TestCLIInstalled_ProjectsList` runs against a reachable server
- **THEN** `memory projects list` exits 0 after authenticating with the static test token

---

### Requirement: Static test token accepted by dev server
The test container SHALL use the static Bearer token `e2e-test-user` (configurable via `MEMORY_TEST_TOKEN` env var) which the Memory dev server is configured to accept.

#### Scenario: Static token authenticates
- **WHEN** `memory set-token e2e-test-user --server <dev-server>` is run
- **THEN** subsequent CLI commands complete without authentication errors

---

### Requirement: Per-test structured session logs
Each test SHALL write a structured log of all CLI invocations and their output to `TEST_LOG_DIR` (default: `./logs/`, fallback: `/test-logs` inside Docker). Log files SHALL be named `<timestamp>-<TestName>.log`.

#### Scenario: Log file created after test run
- **WHEN** any test in the suite completes
- **THEN** at least one `.log` file exists in the configured log directory containing the invocation and output
