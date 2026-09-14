---
name: runlog-test-designer
description: Design and write high-quality e2e tests for the Memory CLI. Covers test structure, package layout, framework API, required practices, and common patterns. Use when creating new tests or reviewing existing ones.
metadata:
  author: emergent
  version: "1.0"
---

Design and write e2e tests for the Memory CLI that comply with the constitution and follow established patterns from the audit.

**Input**: A description of what to test — a CLI command, blueprint workflow, agent interaction, MCP tool, or API behavior.

---

## Constitution (MANDATORY)

Every test must comply with the 10 rules in the E2E Test Constitution:

→ `constitution.md` (project root)

The rules, in brief:
1. **CLI First** — use `memory` CLI for everything that has a CLI command
2. **Ask Memory First** — use `memory ask` to understand the platform before guessing
3. **Every test has a RunLog** — `newRunLog(t)` + `t.Cleanup(rl.Close)`
4. **Every section has content** — no empty `rl.Section()` blocks
5. **CLI steps are logged** — `rl.CLI(invocation, output)` after every CLI call
6. **Describe the test** — `rl.Describe(summary, bullets...)` immediately after RunLog creation
7. **Use `rl.Failf` not `t.Fatalf`** — so failures appear in `runlog inspect`
8. **Project isolation** — every test creates its own project and cleans it up
9. **Consistent section naming** — descriptive actions, not "Step 1"
10. **No silent HTTP calls** — log every HTTP call via `rl.CLIStep` or `rl.Event`

---

## Package Layout

Tests live in subdirectories under `tests/`:

| Directory | Package name | What it tests |
|---|---|---|
| `tests/cli/` | `cli_test` | CLI commands, install, auth, skills, ask endpoint |
| `tests/blueprints/` | `blueprints_test` | Blueprint install, agent orchestration, multi-agent workflows |
| `tests/tools/` | `tools_test` | MCP tools (Brave search, task CLI) |
| `tests/experiments/` | `experiments_test` | Model comparison experiments (tagged runs) |
| `tests/production/` | `production_test` | Smoke tests against live production server |

### When to add to an existing package vs create a new one

- **Same feature area?** Add to the existing package (e.g., new CLI command → `tests/cli/`)
- **New feature category?** Create `tests/<category>/` with its own `testmain_test.go` and `helpers_test.go`

### Required files in every test package

1. **`testmain_test.go`** — identical in every package:

```go
package <name>_test

import (
    "os"
    "testing"
    framework "github.com/emergent-company/emergent.memory.e2e/framework"
)

func TestMain(m *testing.M) {
    framework.LoadDotEnv()
    os.Exit(m.Run())
}
```

2. **`helpers_test.go`** — thin wrappers around `framework.*` functions. Copy from an existing package (e.g., `tests/cli/helpers_test.go`) and adjust. These wrappers exist because Go test packages cannot export functions across files.

---

## Test Anatomy — The Canonical Structure

Every test follows this structure. No exceptions.

```go
func TestFeature_Scenario(t *testing.T) {
    // ── 1. RunLog ──────────────────────────────────────────────────────
    rl := newRunLog(t)
    t.Cleanup(rl.Close)
    rl.Describe("One-line summary of what this test verifies",
        "Step: create project and configure provider",
        "Step: exercise the feature under test",
        "Step: assert expected outcomes",
    )

    // ── 2. Pre-checks (skip guards) ───────────────────────────────────
    home := t.TempDir()
    requireServerReady(t, home)       // sets up auth, skips if server down
    // skipIfNoGoogleAIKey(t)         // if LLM-dependent
    // skipIfEndpointMissing(...)     // if testing optional endpoints

    // ── 3. Environment ────────────────────────────────────────────────
    srv := serverURL()
    token := e2eTestToken()

    // ── 4. Project setup ──────────────────────────────────────────────
    rl.Section("Create project")
    projectName := uniqueProjectName("e2e-feature")
    createOut := mustRunCLIInDirWithHome(t, "", home,
        "projects", "create", "--name", projectName)
    rl.CLI("memory projects create --name "+projectName, createOut)
    projectID := parseProjectID(createOut)
    if projectID == "" {
        rl.Failf("could not parse project ID from: %q", createOut)
    }
    deleteProjectOnCleanup(t, home, projectID)

    // ── 5. Exercise the feature ───────────────────────────────────────
    rl.Section("Exercise feature")
    out := mustRunCLIInDirWithHome(t, "", home, "some", "command",
        "--project", projectID)
    rl.CLI("memory some command --project "+projectID, out)

    // ── 6. Assertions ─────────────────────────────────────────────────
    rl.Section("Verify results")
    if !strings.Contains(out, "expected") {
        t.Errorf("expected 'expected' in output, got: %s", truncate(out, 200))
    }
    rl.Printf("verification passed")
}
```

### Key ordering rules

1. **RunLog first** — before any skip guards, so the test always appears in the run database
2. **`t.Cleanup(rl.Close)`** not `defer rl.Close()` — `t.Cleanup` is more robust (runs even on panic)
3. **`rl.Describe` immediately after RunLog** — before any other code
4. **Skip guards after RunLog** — `requireServerReady`, `skipIfNoKey`, etc.
5. **`home := t.TempDir()`** — always isolated HOME directory
6. **Project creation logged** — `rl.CLI(...)` after every CLI call
7. **Project cleanup registered** — via `deleteProjectOnCleanup(t, home, projectID)`
8. **`rl.Failf` for fatal conditions** — never `t.Fatalf` after RunLog exists

---

## Auth Patterns

### Standard auth (recommended)

```go
home := t.TempDir()
requireServerReady(t, home)  // handles setupCLIAuth internally
```

`requireServerReady` calls `framework.SetupCLIAuth` which:
- In **standalone** mode: sets `server_url` + `api_key` via `memory config set`
- In **account** mode: writes `credentials.json` + sets `server_url`

### Manual auth (when you need explicit control)

```go
home := t.TempDir()
setupCLIAuth(t, home)
// or inline:
mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", token)
```

### Org ID handling

When `MEMORY_ORG_ID` is set (account mode with org context):

```go
args := []string{"projects", "create", "--name", name}
args = append(args, orgIDArgs()...)  // appends ["--org-id", id] if set
out := mustRunCLIInDirWithHome(t, "", home, args...)
```

---

## Project Lifecycle

### Creation

Use `uniqueProjectName` for guaranteed uniqueness:

```go
projectName := uniqueProjectName("e2e-myfeature")
createOut := mustRunCLIInDirWithHome(t, "", home,
    "projects", "create", "--name", projectName)
rl.CLI("memory projects create --name "+projectName, createOut)
projectID := parseProjectID(createOut)
if projectID == "" {
    rl.Failf("could not parse project ID from: %q", createOut)
}
```

Or use the framework shorthand:

```go
projectID := createProject(t, home, srv, projectName)
```

### Cleanup

Always register cleanup immediately after parsing project ID:

```go
deleteProjectOnCleanup(t, home, projectID)
```

This is a one-liner that replaces the old verbose pattern. **Never** use inline `t.Cleanup` with `exec.CommandContext` for project deletion — use the helper.

---

## CLI Invocation Patterns

### Must-succeed (fatal on error)

```go
out := mustRunCLIInDirWithHome(t, "", home, "projects", "list")
rl.CLI("memory projects list", out)
```

### May-fail (returns error)

```go
out, err := runCLIInDirWithHome(t, "", home, "agents", "runs", agentID,
    "--project", projectID)
if err != nil {
    rl.Printf("agents runs failed: %v", err)
}
```

Use the may-fail variant inside polling loops and for operations that might legitimately fail.

### Always log to RunLog

Every CLI invocation must be logged. Use the appropriate method:

| Method | When to use |
|---|---|
| `rl.CLI(invocation, output)` | After a successful CLI command |
| `rl.CLIErr(invocation, output, err)` | After a CLI command that may have failed |
| `rl.CLIStep(desc, invocation, output)` | When you want a human-friendly description |
| `rl.CLIStepErr(desc, invocation, output, err)` | Same, with error |

---

## HTTP Calls (When Permitted)

HTTP is allowed only when no CLI equivalent exists (Rule 1). When using HTTP:

```go
rl.Section("Configure tool via admin API")
resp := doJSON(t, "PATCH", srv+"/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
    token, projectID, patchBody)
body := readBody(t, resp)
rl.CLIStep("Configure Brave Search API key",
    fmt.Sprintf("PATCH /api/admin/mcp-servers/%s/tools/%s", serverID, toolID),
    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(body, 200)))

if resp.StatusCode != 200 {
    rl.Failf("PATCH tool config: want 200, got %d — %s", resp.StatusCode, body)
}
```

### Legitimate HTTP use cases

- Admin endpoints (`/api/admin/mcp-servers`, `/api/admin/orgs/*/tool-settings`)
- MCP JSON-RPC calls (`/mcp`, `/api/mcp/rpc`)
- Health/auth endpoints (`/health`, `/api/auth/issuer`)
- SSE streaming from `/api/ask` (when testing the SSE protocol itself)
- Unauthenticated requests (testing 401 rejection)
- Agent trigger with `prompt`/`context` body fields (CLI has no `--prompt` flag)

---

## Agent Orchestration Pattern

For tests that trigger agents and wait for completion:

### 1. Trigger

```go
rl.Section("Trigger agent")
framework.TriggerAgent(t, rl, home, projectID, agentID)
```

Or via HTTP when a prompt is needed:

```go
triggerURL := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", srv, projectID, agentID)
triggerBody, _ := json.Marshal(map[string]string{"prompt": "do the thing"})
triggerResp := doJSON(t, "POST", triggerURL, token, projectID, triggerBody)
rl.CLIStep("Trigger with prompt", "POST "+triggerURL, readBody(t, triggerResp))
```

### 2. Poll with `PollUntilSuccess`

```go
rl.Section("Poll for completion")
ok := pollAgentUntilSuccess(t, rl, home, srv, token, projectID,
    agentID, "orchestrator", 10*time.Minute)
if !ok {
    dumpAgents(t, rl, srv, token, projectID, agents)
    rl.Failf("agent did not complete within timeout")
}
```

### 3. Manual polling (for graph object status)

```go
rl.Section("Poll WorkPackage status")
deadline := time.Now().Add(10 * time.Minute)
var lastStatus string
for time.Now().Before(deadline) {
    time.Sleep(5 * time.Second)

    out, err := runCLIInDirWithHome(t, "", home,
        "graph", "objects", "list", "--type", "WorkPackage",
        "--project", projectID, "--output", "json")
    if err != nil {
        rl.Printf("poll error: %v", err)
        continue
    }

    status := parseWorkPackageStatus(out, wpID)
    if status != lastStatus {
        rl.Printf("status changed: %s → %s", lastStatus, status)
        lastStatus = status
    }

    if status == "complete" || status == "accepted" {
        break
    }
}
if lastStatus != "complete" && lastStatus != "accepted" {
    dumpAgents(t, rl, srv, token, projectID, agents)
    rl.Failf("WorkPackage not completed: last status=%s", lastStatus)
}
```

### 4. Auto-respond to agent questions

```go
questions, _, _ := listPendingQuestions(t, rl, home, projectID)
for _, q := range questions {
    respondToQuestion(t, rl, home, projectID, q.ID, "approved")
}
```

### 5. Always dump diagnostics before failing

```go
if !success {
    dumpAgents(t, rl, srv, token, projectID, agents)
    rl.Failf("test failed: %s", reason)
}
```

`dumpAgents` writes per-run detail files (messages, tool calls) that are invaluable for debugging.

---

## Graph Object Assertions

### List by type

```go
objs := listGraphObjectsByType(t, srv, token, projectID, "Task")
if len(objs) == 0 {
    t.Error("expected at least one Task object")
}
for _, obj := range objs {
    props, _ := obj["properties"].(map[string]any)
    title := propString(props, "title")
    rl.Printf("Task: %q status=%s", title, propString(props, "status"))
}
```

### List relationships (CLI-based)

```go
rels := listRelationshipsFromCLI(t, home, projectID, sourceID, "HAS_RESULT")
if len(rels) == 0 {
    t.Error("expected HAS_RESULT relationship")
}
```

---

## Blueprint Tests

```go
rl.Section("Install blueprint")
bpOut := mustRunCLIInDirWithHome(t, "", home,
    "blueprints", blueprintPath, "--project", projectName, "--upgrade")
rl.CLI("memory blueprints "+blueprintPath+" --project "+projectName+" --upgrade", bpOut)

if strings.Contains(bpOut, "errors") && !strings.Contains(bpOut, "0 errors") {
    rl.Failf("blueprint install reported errors:\n%s", bpOut)
}
```

Or use the framework shorthand:

```go
bpOut := framework.InstallBlueprint(t, home, blueprintURL, projectName)
rl.CLI("memory blueprints apply", bpOut)
```

---

## Skip Guards

Place skip guards **after** RunLog creation but **before** test setup:

```go
rl := newRunLog(t)
t.Cleanup(rl.Close)
rl.Describe(...)

home := t.TempDir()
requireServerReady(t, home)                        // skips if server unreachable
skipIfEndpointMissing(t, "/api/ask", token)        // skips if endpoint 404
```

### Environment-specific skips

```go
if os.Getenv("GOOGLE_AI_API_KEY") == "" {
    t.Skip("GOOGLE_AI_API_KEY not set — skipping LLM-dependent test")
}
```

---

## RunLog API Quick Reference

| Method | Event kind | When to use |
|---|---|---|
| `rl.Describe(summary, bullets...)` | (metadata) | Once, immediately after creation |
| `rl.Section(name)` | `section` | Major test phase boundary |
| `rl.CLI(invocation, output)` | `cli` | After a successful CLI command |
| `rl.CLIErr(invocation, output, err)` | `cli` | After a CLI command that may fail |
| `rl.CLIStep(desc, invocation, output)` | `cli` | CLI or HTTP with human description |
| `rl.CLIStepErr(desc, inv, output, err)` | `cli` | Same, with error |
| `rl.Printf(format, args...)` | `log` | Informational message within a section |
| `rl.Event(kind, message, details)` | custom | Structured event (API response, etc.) |
| `rl.Group(kind, title, fn)` | `group` | Collapsible sub-group in a section |
| `rl.Failf(format, args...)` | `failure` | Fatal test failure with logging |
| `rl.Tag(tags...)` | `tag` | Key:value tags for filtering/grouping |
| `rl.SetExperiment(name)` | (metadata) | Assign run to an experiment |

---

## Naming Conventions

### Test functions

| Pattern | Example | When |
|---|---|---|
| `TestCLIInstalled_<Feature>` | `TestCLIInstalled_Version` | CLI command verification |
| `Test<Blueprint>_<Scenario>` | `TestOrchestratorCompletesWorkPackage` | Blueprint/agent workflow |
| `Test<Tool>_<Scenario>` | `TestBraveSearch_ConfigureAndSearch` | MCP tool tests |
| `TestProduction_<Feature>` | `TestProduction_SetToken` | Production smoke tests |

### Section names

Good: `"Create project"`, `"Install blueprint"`, `"Poll for completion"`, `"Verify task results"`
Bad: `"Step 1"`, `"Setup"`, `"Assert"`, `"Wait"`

### Project names

Always use `uniqueProjectName("e2e-<feature>")` for timestamped uniqueness.

---

## Common Anti-Patterns (Do NOT Do These)

### 1. Using `t.Fatalf` when RunLog is active

```go
// BAD — failure invisible in runlog inspect
if projectID == "" {
    t.Fatalf("no project ID")
}

// GOOD — failure logged before test aborts
if projectID == "" {
    rl.Failf("no project ID from: %q", createOut)
}
```

### 2. Inline project cleanup with exec.CommandContext

```go
// BAD — verbose, error-prone, duplicated
t.Cleanup(func() {
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, "memory", "projects", "delete", projectID)
    cmd.Env = append(filteredEnv(), "HOME="+home, ...)
    cmd.CombinedOutput()
})

// GOOD — one-liner
deleteProjectOnCleanup(t, home, projectID)
```

### 3. Empty sections

```go
// BAD — section has no logged events
rl.Section("Verify results")
if !strings.Contains(out, "expected") {
    t.Errorf("missing expected")
}

// GOOD — log what was verified
rl.Section("Verify results")
if !strings.Contains(out, "expected") {
    t.Errorf("missing expected in: %s", truncate(out, 200))
}
rl.Printf("output contains expected content")
```

### 4. HTTP when CLI exists

```go
// BAD — HTTP for project creation
resp := doJSON(t, "POST", srv+"/api/projects", token, "", body)

// GOOD — CLI first
out := mustRunCLIInDirWithHome(t, "", home, "projects", "create", "--name", name)
```

### 5. Silent HTTP calls

```go
// BAD — no logging
resp := doJSON(t, "GET", srv+"/api/admin/mcp-servers", token, projectID, nil)

// GOOD — logged
resp := doJSON(t, "GET", srv+"/api/admin/mcp-servers", token, projectID, nil)
body := readBody(t, resp)
rl.CLIStep("List MCP servers", "GET /api/admin/mcp-servers", truncate(body, 300))
```

### 6. Missing rl.Describe

```go
// BAD — test purpose invisible in TUI
rl := newRunLog(t)
t.Cleanup(rl.Close)
rl.Section("Create project")

// GOOD — human-readable purpose
rl := newRunLog(t)
t.Cleanup(rl.Close)
rl.Describe("Verify blueprint install creates expected agents",
    "Create project and apply multi-agent blueprint",
    "Assert orchestrator and researcher agents exist",
)
rl.Section("Create project")
```

### 7. Failing without diagnostics in orchestration tests

```go
// BAD — no context on why the agent failed
if !ok {
    rl.Failf("agent did not complete")
}

// GOOD — dump full agent run details first
if !ok {
    dumpAgents(t, rl, srv, token, projectID, agents)
    rl.Failf("agent did not complete within %s", timeout)
}
```

---

## Workflow: Designing a New Test

### Step 1: Understand the feature

```bash
memory ask "How does <feature> work?"
memory <subcommand> --help
```

### Step 2: Choose the right package

Match the feature to `tests/cli/`, `tests/blueprints/`, `tests/tools/`, etc.

### Step 3: Write the test

Follow the canonical structure above. Start with RunLog, Describe, skip guards, project setup, exercise, assert.

### Step 4: Verify compilation

```bash
go build ./...
go vet ./...
go test -run '^$' ./tests/<package>/
```

### Step 5: Run the test

```bash
./test mcj-emergent TestMyNewTest
```

### Step 6: Inspect the run log

```bash
runlog runs --since 1h
runlog inspect <run-id>
```

Verify: no empty sections, all CLI steps logged, description visible, failure events present if test failed.

---

## Framework Quick Reference

### Server / Auth

| Function | Returns |
|---|---|
| `serverURL()` | `MEMORY_TEST_SERVER` |
| `e2eTestToken()` | `MEMORY_TEST_TOKEN` |
| `requireServerReady(t, home)` | Skips if down, sets up auth |
| `skipIfServerDown(t)` | Skips if `/health` unreachable |
| `setupCLIAuth(t, home)` | Configures auth in isolated home |

### CLI

| Function | Behavior |
|---|---|
| `mustRunCLIInDirWithHome(t, dir, home, args...)` | Fatal on error |
| `runCLIInDirWithHome(t, dir, home, args...)` | Returns `(string, error)` |
| `mustRunCLI(t, args...)` | Convenience (default dir/home) |

### Parse

| Function | Extracts from |
|---|---|
| `parseProjectID(output)` | `memory projects create` output |
| `parseAgentID(output)` | `memory agents create` output |
| `parseJSONField(json, field)` | Top-level JSON string field |
| `parseFrontmatterFields(content)` | SKILL.md name + description |

### Project

| Function | Does |
|---|---|
| `uniqueProjectName(prefix)` | Returns `"<prefix>-<unixmilli>"` |
| `createProject(t, home, srv, name)` | Creates via CLI, returns ID |
| `deleteProjectOnCleanup(t, home, id)` | Registers `t.Cleanup` to delete |
| `orgIDArgs()` | Returns `["--org-id", id]` or `[]` |

### Agent

| Function | Does |
|---|---|
| `pollAgentUntilSuccess(t, rl, ...)` | Polls until success/timeout |
| `listPendingQuestions(t, rl, home, projectID)` | Lists pending questions via CLI |
| `respondToQuestion(t, rl, home, projectID, qID, response)` | Responds via CLI |
| `dumpAgentRunDetails(t, rl, ...)` | Fetches full run details for debugging |

### Graph

| Function | Does |
|---|---|
| `listGraphObjectsByType(t, srv, token, projectID, type)` | HTTP query |
| `listGraphObjectsByLabel(t, srv, token, projectID, label)` | HTTP query |
| `listRelationshipsFromCLI(t, home, projectID, fromID, type)` | CLI query |
| `propString(props, keys...)` | Extracts string property |

### HTTP (when CLI unavailable)

| Function | Does |
|---|---|
| `doJSON(t, method, url, token, projectID, body)` | Authenticated JSON request |
| `doMCPJSON(t, url, token, projectID, sessionID, version, body)` | MCP JSON-RPC request |
| `readBody(t, resp)` | Reads + closes response body |

### Verification

| Function | Does |
|---|---|
| `framework.VerifyCredentialsWritten(t, rl, home)` | Checks credentials.json exists |
| `framework.VerifySkillInstalled(t, rl, skillsDir, name)` | Checks SKILL.md valid |
