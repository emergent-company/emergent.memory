# Skill: create-experiment

## Description

Guide for creating a new experiment in `emergent.memory.e2e` — a named group of test runs that compare variants (e.g. different models, blueprints, or configurations) on the same task. Experiments appear grouped in the **Experiments** tab of the runlog TUI.

## Trigger

Use this skill when the user wants to:
- Compare two or more models / configurations on the same agent task
- Create a named experiment that groups related test runs in the TUI
- Add model-comparison or A/B test files to the e2e suite

## What is an experiment?

An experiment is a string name set on a `RunLog` via `rl.SetExperiment("name")` (or the `EXPERIMENT` env var). All test runs sharing the same experiment name are grouped together in `logs/runs.db` and surfaced in the TUI's Experiments tab. Each run should be tagged with variant metadata (e.g. `"model:gemini-2.5-flash"`) so individual runs can be compared.

---

## Workflow

### 1. Choose the task and variants

Decide:
- **Task**: which agent workflow to test (e.g. `python-senior-coder-native` via v3 blueprint)
- **Variants**: the things being compared (e.g. two model names, two blueprint versions)
- **Experiment name**: a lowercase kebab-case string, e.g. `research-and-code-model-comparison`

### 2. Choose an existing test to copy

Pick the closest existing test file as a template. For agent workflows, prefer:
- `v3_python_senior_coder_native_test.go` — Google native search, no Brave key needed
- `v3_python_senior_coder_test.go` — uses Brave Search
- `v3_orchestrator_test.go` — multi-agent orchestrator

Read the chosen template fully before writing anything.

### 3. Name the new file

Use the pattern: `<experiment-slug>_test.go`, e.g. `v3_model_comparison_research_code_test.go`.

All test files live in the repo root, package `dockertests`.

### 4. Structure the file

Use **one shared helper function** plus **one thin test function per variant**:

```go
// Package level constant for the experiment name
const myExperimentName = "my-experiment-name"

func TestMyExperiment_VariantA(t *testing.T) {
    runMyExperiment(t, "variant-a-value")
}

func TestMyExperiment_VariantB(t *testing.T) {
    runMyExperiment(t, "variant-b-value")
}

func runMyExperiment(t *testing.T, variant string) {
    t.Helper()
    // ... full test body ...
}
```

This keeps the variant differences minimal and the step structure identical across runs.

### 5. Set the experiment name and tags

Inside the shared helper, right after `newRunLog(t)`:

```go
rl := newRunLog(t)
defer rl.Close()
rl.SetExperiment(myExperimentName)        // groups runs in TUI Experiments tab
describe(rl, "one-line summary", "bullet 1", "bullet 2")
```

After installing the blueprint and overriding the model (or other variant), tag the run:

```go
overrideAgentDefsModel(t, rl, home, projectID, model, blueprintV3Model)
postDefsOut := mustRunCLIInDirWithHome(t, "", home, "agent-definitions", "list", "--project", projectID)
rl.Tag("model:"+activeModel(postDefsOut), "blueprint:v3", "search:google-native")
```

For non-model variants, tag after resolving the effective value:
```go
rl.Tag("blueprint:"+blueprintVariant, "model:"+activeModel(defsOut))
```

**Tag format**: `"key:value"` strings. Common keys: `model`, `blueprint`, `search`, `variant`.

### 6. Hardcode variant values — do NOT read from env vars

Each test function passes its variant value directly to the helper. Do **not** read from `ORCHESTRATOR_MODEL` or other env vars inside the helper — the whole point of an experiment is to fix the variants so both test functions always run with known, stable values.

```go
// CORRECT — hardcoded
func TestMyExperiment_Flash25(t *testing.T) {
    runMyExperiment(t, "gemini-2.5-flash")
}

// WRONG — reads env var, variant is not fixed
func TestMyExperiment_Flash25(t *testing.T) {
    runMyExperiment(t, orchestratorModel())
}
```

The exception is when one variant *is* the blueprint default — then the override is a no-op (handled automatically by `overrideAgentDefsModel`).

### 7. Key helpers available

All defined in `helpers_test.go` (thin wrappers) or `orchestrator_test.go` (package-level):

| Helper | Signature | Description |
|---|---|---|
| `newRunLog` | `(t) → *runLog` | Creates per-test RunLog; auto-reads `EXPERIMENT` env var |
| `rl.SetExperiment` | `(name string)` | Sets experiment name; call right after `newRunLog` |
| `rl.Tag` | `(tags ...string)` | Tags run with `"key:value"` strings |
| `describe` | `(rl, summary, bullets...)` | Sets TUI description |
| `orchestratorModel` | `() → string` | Reads `ORCHESTRATOR_MODEL` env var (use only for non-experiment tests) |
| `overrideAgentDefsModel` | `(t, rl, home, projectID, newModel, blueprintPinnedModel)` | Patches agent defs if model differs from blueprint default |
| `activeModel` | `(defsOut string) → string` | Parses current model name from `agent-definitions list` output |
| `pollAgentUntilSuccess` | `(t, rl, home, srv, token, projectID, agentID, name, timeout)` | Clean poll loop |
| `skipIfServerDown` | `(t)` | Skip if server unreachable |
| `skipIfNoGoogleAIKey` | `(t)` | Skip if `GOOGLE_AI_API_KEY` unset |
| `logStatusPreamble` | `(t, home)` | Logs server/env context at top of test |
| `setupCLIAuth` | `(t, home)` | Configures CLI credentials in temp home |
| `projectCreateOrgArgs` | `() → []string` | Returns `["--org-id","<id>"]` when `MEMORY_ORG_ID` is set |
| `orgIDArgs` | `() → []string` | Same, for non-`projects create` calls |
| `filteredEnv` | `() → []string` | Filtered `os.Environ()` for sub-commands |
| `dumpAgentRunDetails` | `(t, rl, srv, token, projectID, names, ids)` | Dumps run detail log at end |

Blueprint constants (defined in `v3_orchestrator_test.go`, package-level):

| Constant | Value |
|---|---|
| `blueprintV3URL` | `/root/workspace-memory-blueprint-v3` |
| `blueprintV3Model` | `"gemini-3.1-flash-lite-preview"` |

Senior coder task constants (defined in `python_senior_coder_test.go`):

| Constant | Value |
|---|---|
| `seniorCoderTaskTitle` | `"Open-Meteo Weather Script"` |
| `seniorCoderTaskMessage` | Full task prompt string |
| `v3NativeSeniorCoderTimeout` | `10 * time.Minute` (defined in `v3_python_senior_coder_native_test.go`) |
| `pollInterval` | `5 * time.Second` (defined in `orchestrator_test.go`) |

### 8. Running experiments

Run both variants together:
```bash
go test -v -run TestMyExperiment -timeout 30m
```

Run just one variant:
```bash
go test -v -run TestMyExperiment_Flash25 -timeout 15m
```

Set the experiment name via env var instead of code (useful for ad-hoc grouping):
```bash
EXPERIMENT=my-experiment go test -v -run TestV3PythonSeniorCoderNativeSearch -timeout 15m
```

Against the shared test server:
```bash
./test mcj-emergent TestMyExperiment_Flash25
```

### 9. Analyzing experiment results

After runs complete, use the LLM-powered analyzer to get structured improvement suggestions:

```bash
# In the runlog TUI: press 'a' on any run to analyze it
# Or use the standalone CLI:
./analyze my-experiment                          # analyze all runs in an experiment
./analyze --test TestMyExperiment_Flash25         # analyze by test name
./analyze --run 42                               # analyze a single run by ID
```

The analyzer reads run events, explores the CLI tree via `run_cli`, and produces prioritized suggestions stored in `logs/runs.db`. Results appear in the TUI's suggestions view.

### 10. Verify compilation

After writing the file, always run:
```bash
go build ./...
go vet ./...
```

Both must pass before considering the task done.

---

## Minimal skeleton

```go
// Package dockertests — <experiment>_test.go
//
// Experiment: <describe what is being compared and why>.
//
// Required environment variables:
//
//   GOOGLE_AI_API_KEY  — skipped when absent.
//   MEMORY_TEST_SERVER — URL of the Memory server.
//   MEMORY_TEST_TOKEN  — API key for the Memory server.
package dockertests

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "testing"
    "time"
)

const myExperimentName = "my-experiment-name"

func TestMyExperiment_VariantA(t *testing.T) {
    runMyExperiment(t, "variant-a")
}

func TestMyExperiment_VariantB(t *testing.T) {
    runMyExperiment(t, "variant-b")
}

func runMyExperiment(t *testing.T, variant string) {
    t.Helper()
    skipIfServerDown(t)
    skipIfNoGoogleAIKey(t)

    googleKey := os.Getenv("GOOGLE_AI_API_KEY")
    home := t.TempDir()
    srv  := serverURL()
    token := e2eTestToken()

    rl := newRunLog(t)
    defer rl.Close()
    rl.SetExperiment(myExperimentName)
    describe(rl,
        fmt.Sprintf("Experiment %s: variant=%s", myExperimentName, variant),
        "bullet describing what this test validates",
    )

    logStatusPreamble(t, home)
    setupCLIAuth(t, home)

    // ── Step 1: Create project ───────────────────────────────────────────────
    rl.Section("Step 1 — Create project")
    projectName := fmt.Sprintf("e2e-exp-%d", time.Now().UnixMilli())
    createOut := mustRunCLIInDirWithHome(t, "", home,
        append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
    rl.CLI("memory projects create --name "+projectName, createOut)
    projectID := parseProjectID(createOut)
    if projectID == "" {
        t.Fatalf("could not parse project ID from: %q", createOut)
    }
    rl.Printf("project: %s (%s)", projectName, projectID)
    mustRunCLIInDirWithHome(t, "", home, "config", "set", "project_id", projectID)

    t.Cleanup(func() {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        cmd := exec.CommandContext(ctx, "memory", "projects", "delete", projectID)
        cmd.Env = append(filteredEnv(), "HOME="+home, "PATH="+home+"/.memory/bin:"+os.Getenv("PATH"))
        if out, err := cmd.CombinedOutput(); err != nil {
            rl.Printf("warn: failed to delete project %s: %v\n%s", projectID, err, out)
        } else {
            rl.Printf("deleted project %s", projectName)
        }
    })

    // ── Step 2: Configure Google AI provider ─────────────────────────────────
    rl.Section("Step 2 — Configure provider (variant: " + variant + ")")
    out, provErr := runCLIInDirWithHome(t, "", home,
        append([]string{
            "provider", "configure", "google",
            "--api-key", googleKey,
            "--generative-model", variant,
        }, orgIDArgs()...)...)
    rl.CLI("memory provider configure google --generative-model "+variant, out)
    if provErr != nil {
        testOut, testErr := runCLIInDirWithHome(t, "", home,
            append([]string{"provider", "test"}, orgIDArgs()...)...)
        rl.CLI("memory provider test", testOut)
        if testErr != nil {
            t.Fatalf("provider configure failed and provider test also failed: %v\n%s", testErr, testOut)
        }
    }

    // ── Step 3: Install blueprint + tag run ───────────────────────────────────
    rl.Section("Step 3 — Install v3 blueprint")
    blueprintOut := mustRunCLIInDirWithHome(t, "", home,
        "blueprints", blueprintV3URL, "--project", projectName, "--upgrade")
    rl.CLI("memory blueprints "+blueprintV3URL+" --project "+projectName+" --upgrade", blueprintOut)
    if strings.Contains(blueprintOut, "errors") && !strings.Contains(blueprintOut, "0 errors") {
        t.Fatalf("blueprint install reported errors:\n%s", blueprintOut)
    }

    overrideAgentDefsModel(t, rl, home, projectID, variant, blueprintV3Model)
    postDefsOut := mustRunCLIInDirWithHome(t, "", home,
        "agent-definitions", "list", "--project", projectID)
    rl.Tag("model:"+activeModel(postDefsOut), "blueprint:v3")

    // ── Steps 4–N: follow the template test step-by-step ─────────────────────
    // ... copy remaining steps from the chosen template test ...

    _ = googleKey
    _ = srv
    _ = token
    _ = json.Marshal // ensure json import used
}
```

---

## Key rules

- **One experiment name constant** at package level; both test functions reference it
- **Call `rl.SetExperiment` immediately after `newRunLog`** — before any sections
- **Hardcode variant values** in the top-level `TestFoo_VariantX` functions; never read from env vars inside the helper
- **Tag after the model override**, not before — `activeModel(defsOut)` gives the real effective model name
- **Always clean up the project** in `t.Cleanup` — do not use `defer` in a shared helper (cleanup runs before the test ends, not after `t.Cleanup`)
- **`go build ./...` and `go vet ./...` must pass** before finishing
- **No external Go dependencies** — standard library only

---

## Constitution

Every experiment test must comply with the **E2E Test Constitution**:

→ `constitution.md` (project root)
