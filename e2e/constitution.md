# E2E Test Constitution

This repository is the **end-to-end test suite for the Memory platform** (`github.com/emergent-company/emergent.memory`). Memory is a platform for building AI agents with persistent memory, knowledge graphs, and tool access. The `memory` CLI is the primary interface for interacting with the platform -- creating projects, managing agents, querying knowledge, and more.

Tests in this repository exercise the Memory platform the way a real user does: through the CLI and, where necessary, through direct HTTP calls. You can always get more information about the platform and its capabilities using:

```bash
memory ask "<your question>"
```

The `memory ask` command connects to the built-in assistant, which has full knowledge of the Memory platform, its CLI, API, and concepts. Use it whenever you need to understand how something works.

The rules below are **mandatory** for all e2e tests in this repository. Every new test must follow them. Every existing test should be migrated toward them. Skills that create, review, or verify tests must enforce them.

---

## Rule 1: CLI First

**If something can be done through the `memory` CLI, it must be done through the `memory` CLI.**

Tests exercise the system the way a real user does -- through the CLI. Direct HTTP calls are a last resort, permitted only when no CLI equivalent exists.

### What MUST use CLI

| Operation | CLI command |
|---|---|
| Create project | `memory projects create --name <name>` |
| Delete project | `memory projects delete <id>` |
| List projects | `memory projects list` |
| Set token / auth | `memory set-token <token> --server <url>` |
| Config values | `memory config set <key> <value>` |
| Install skills | `memory install-memory-skills --force` |
| Apply blueprint | `memory blueprints apply <path> --project <id>` |
| Create agent | `memory agents create ...` |
| Trigger agent | `memory agents trigger <id> --project <id>` |
| Poll agent runs | `memory agents runs <id> --project <id>` |
| List agent questions | `memory agents questions list-project --project <id>` |
| Respond to question | `memory agents questions respond <id> --project <id>` |
| List graph objects | `memory graph objects list --type <type> --project <id>` |
| List relationships | `memory graph relationships list --project <id>` |
| Create tokens | `memory tokens create ...` |
| Revoke tokens | `memory tokens revoke <id>` |
| Check status | `memory status` |
| Show version | `memory version` |
| Configure provider | `memory provider set ...` |

### When HTTP is permitted

Direct HTTP calls are allowed **only** when:

1. **No CLI equivalent exists** -- e.g. `/api/admin/mcp-servers`, `/api/admin/orgs/*/tool-settings`, `/api/auth/issuer`, MCP JSON-RPC protocol calls.
2. **The test specifically validates HTTP-level behavior** -- e.g. verifying SSE streaming protocol (meta/token/done events), verifying 401 on unauthenticated requests, verifying specific HTTP status codes or headers.
3. **The test validates an API contract** -- tests in `tests-api/` intentionally test the HTTP API surface and are exempt from this rule.

When HTTP is used, it must still be logged via `rl.Event()` or `rl.CLIStep()` so it appears in the run log.

### Memory binary location

The `memory` CLI binary is located at `/root/.memory/bin/memory`. In tests the binary is on `PATH` (the Dockerfile and `install.sh` ensure this), so tests invoke it as plain `memory`. When invoking outside of the test harness (e.g. from agent tooling or pre-condition scripts), use the full path `/root/.memory/bin/memory`.

### How to check for new CLI capabilities

When unsure whether a CLI command exists for something, use:

```bash
memory <subcommand> --help
```

Or ask the built-in assistant (see Rule 2).

---

## Rule 2: Ask Memory First

**When you need to understand how the Memory server or CLI works, ask it.**

```bash
memory ask "How do I trigger an agent with a prompt message?"
memory ask "What CLI commands are available for managing graph objects?"
memory ask "How do I configure a Google provider for a project?"
```

The `memory ask` command connects to the CLI assistant, which has full knowledge of the Memory platform. Use it before reading source code, searching docs, or guessing. This applies to both humans writing tests and agents generating tests.

For project-scoped questions (when you have a project context):

```bash
memory ask "How are WorkPackage objects structured?" --project <id>
```

---

## Rule 3: Every Test Has a RunLog

Every test function must create a RunLog and defer its close:

```go
rl := newRunLog(t)
defer rl.Close()
```

No exceptions. Tests without a RunLog are invisible to the run log system and cannot be reviewed, analyzed, or debugged through `runlog inspect`.

---

## Rule 4: Every Section Has Content

A section must never be empty. Every `rl.Section(...)` call must be followed by at least one logged event (`rl.CLI`, `rl.Printf`, `rl.Event`, `rl.CLIStep`) before the next section or test end.

Sections represent meaningful test phases. If a section only does in-memory assertions with no observable output, either:
- Add `rl.Printf(...)` calls to log what was asserted and the result.
- Merge the section with the preceding one.

### Good

```go
rl.Section("Verify skill directories exist")
for _, skill := range expectedSkills {
    dir := filepath.Join(ws, ".agents", "skills", skill)
    if _, err := os.Stat(dir); os.IsNotExist(err) {
        rl.Failf("expected skill directory not found: %s", dir)
    }
    rl.Printf("  found: %s", skill)
}
```

### Bad

```go
rl.Section("Verify skill directories exist")
for _, skill := range expectedSkills {
    dir := filepath.Join(ws, ".agents", "skills", skill)
    if _, err := os.Stat(dir); os.IsNotExist(err) {
        t.Errorf("expected skill directory not found: %s", dir)
    }
}
// Section has zero logged events -- invisible in runlog inspect
```

---

## Rule 5: CLI Steps Are Logged

Every CLI invocation must be logged to the RunLog. The pattern is:

```go
out := mustRunCLIInDirWithHome(t, dir, home, "projects", "list")
rl.CLI("memory projects list", out)
```

Or with a description:

```go
out := mustRunCLIInDirWithHome(t, dir, home, "agents", "trigger", agentID, "--project", projectID)
rl.CLIStep("Trigger orchestrator agent", "memory agents trigger "+agentID+" --project "+projectID, out)
```

The `rl.CLI()` / `rl.CLIStep()` call is what makes the command visible in `runlog inspect`. Without it, the command ran but left no trace.

---

## Rule 6: Describe the Test

Every test must call `rl.Describe()` immediately after creating the RunLog:

```go
rl := newRunLog(t)
defer rl.Close()
rl.Describe("Verify blueprint install creates expected agents and objects",
    "Create project and apply multi-agent-orchestrator blueprint",
    "Assert orchestrator, researcher, and coder agents exist",
    "Assert Task and WorkPackage seed objects are created",
)
```

The description appears in the RunLog TUI header and helps reviewers understand what the test does without reading the source code.

---

## Rule 7: Use `rl.Failf` Instead of `t.Fatalf`

When a test must abort due to a fatal condition, use `rl.Failf()` instead of `t.Fatalf()`:

```go
if projectID == "" {
    rl.Failf("could not parse project ID from: %q", createOut)
}
```

`rl.Failf()` logs a `failure` event to the RunLog before calling `t.Fatalf()`, so the failure reason is visible in `runlog inspect`. Plain `t.Fatalf()` kills the test immediately with no RunLog trace.

Exception: pre-condition checks that run before the RunLog is created (e.g. `requireServerReady`) may use `t.Fatalf` or `t.Skipf`.

---

## Rule 8: Project Isolation

Every test must create its own project and clean it up:

```go
home := t.TempDir()
setupCLIAuth(t, home)  // or manual config set

projectName := fmt.Sprintf("e2e-feature-%d", time.Now().UnixMilli())
createOut := mustRunCLIInDirWithHome(t, "", home, "projects", "create", "--name", projectName)
projectID := parseProjectID(createOut)

t.Cleanup(func() {
    mustRunCLIInDirWithHome(t, "", home, "projects", "delete", projectID)
})
```

No test may depend on a project created by another test. No test may leave projects behind after completion.

---

## Rule 9: Consistent Section Naming

Section names should be descriptive actions, not generic step numbers:

| Good | Bad |
|---|---|
| `"Create project"` | `"Step 1"` |
| `"Install blueprint"` | `"Step 2 — Setup"` |
| `"Trigger orchestrator agent"` | `"Execute"` |
| `"Poll for WorkPackage completion"` | `"Wait"` |
| `"Verify task results contain code"` | `"Assert"` |

Numbered prefixes are acceptable if combined with a descriptive name: `"Step 1 — Create project"`.

---

## Rule 10: No Silent HTTP Calls

If an HTTP call is made (when permitted per Rule 1), it must be logged:

```go
resp := doJSON(t, "PATCH", patchURL, token, projectID, patchBody)
body := readBody(t, resp)
rl.CLIStep("Configure Brave Search API key",
    fmt.Sprintf("PATCH %s", patchURL),
    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body))
```

HTTP calls that bypass the RunLog are invisible during review and debugging.

---

## Rule 11: Never Clear RunLog DB Automatically

**The runs database (`logs/runs.db`) must never be cleared automatically** — not at the start of a session, not before a test run, not as part of any automated workflow. Historical run data is valuable for comparison, regression analysis, and debugging.

The DB may only be cleared when the user **explicitly requests it** (e.g. "clear the runlog", "wipe the runs DB"). The `runlog clear` command exists for this purpose but must not be invoked proactively.

---

## Enforcement

- The **runlog-verify-runs** skill (installed via `runlog skills install`) checks these rules against actual run log output.
- The **create-e2e-test** skill (`/.opencode/skills/create-e2e-test/`) must generate tests that follow these rules.
- Code review should flag violations of these rules.
