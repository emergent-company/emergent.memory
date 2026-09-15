# Skill: create-e2e-test

## Description

Quick-start guide for writing a new e2e test. For the comprehensive reference (framework API, patterns, anti-patterns), see the **runlog-test-designer** skill.

## Trigger

Use this skill when the user wants to:
- Add a new e2e test to the suite
- Understand how to structure an e2e test file
- Add a test for a new CLI command, blueprint, or agent workflow

## Full Reference

For detailed patterns, framework API, anti-patterns, and the complete test design guide:

→ The **runlog-test-designer** skill (installed via `runlog skills install`)

## Pre-requisites

- Go module: `github.com/emergent-company/emergent.memory/e2e`
- No external Go dependencies — standard library only (except `framework/analyzer.go` which uses Google AI)
- Tests live in subdirectories under `tests/`: `tests/cli/`, `tests/blueprints/`, `tests/tools/`, `tests/experiments/`, `tests/production/`
- Each test package has its own `testmain_test.go` and `helpers_test.go` (thin wrappers around `framework.*`)
- Framework: `github.com/emergent-company/runlog` (package `runlog`, imported as `framework`)
- Fixtures: `github.com/emergent-company/emergent.memory/e2e/fixtures` (package `e2efixtures`)

## Workflow

1. **Choose the package** — match feature to `tests/cli/`, `tests/blueprints/`, `tests/tools/`, etc.
2. **Name the file** — `<feature>_test.go` in the chosen directory
3. **Declare the package** — `package <dirname>_test` (e.g., `package cli_test`)
4. **Follow the canonical structure** from the skeleton below
5. **Verify**: `go build ./...` && `go test -run '^$' ./tests/<package>/`
6. **Run**: `./test mcj-emergent TestMyNewTest`
7. **Inspect**: `./runlog inspect <run-id>` to verify log quality

## Skeleton

```go
package cli_test

import (
    "strings"
    "testing"
)

func TestMyFeature_Scenario(t *testing.T) {
    // 1. RunLog FIRST — always
    rl := newRunLog(t)
    t.Cleanup(rl.Close)
    rl.Describe("Verify <what this test checks>",
        "<step 1 description>",
        "<step 2 description>",
    )

    // 2. Skip guards
    home := t.TempDir()
    requireServerReady(t, home)

    // 3. Project setup
    rl.Section("Create project")
    projectName := uniqueProjectName("e2e-myfeature")
    createOut := mustRunCLIInDirWithHome(t, "", home,
        "projects", "create", "--name", projectName)
    rl.CLI("memory projects create --name "+projectName, createOut)
    projectID := parseProjectID(createOut)
    if projectID == "" {
        rl.Failf("could not parse project ID from: %q", createOut)
    }
    deleteProjectOnCleanup(t, home, projectID)

    // 4. Exercise the feature
    rl.Section("Exercise feature")
    out := mustRunCLIInDirWithHome(t, "", home, "some", "command",
        "--project", projectID)
    rl.CLI("memory some command", out)

    // 5. Assert
    rl.Section("Verify results")
    if !strings.Contains(out, "expected") {
        t.Errorf("expected 'expected' in output, got: %s", truncate(out, 200))
    }
    rl.Printf("verification passed")
}
```

## Constitution (MANDATORY)

Every test **must** comply with the E2E Test Constitution:

→ `constitution.md` (project root)

Key rules:
- **RunLog first** — `newRunLog(t)` + `t.Cleanup(rl.Close)` before anything else
- **Describe immediately** — `rl.Describe(summary, bullets...)` right after RunLog
- **CLI First** — use `memory` CLI for everything that has a CLI command
- **`rl.Failf` not `t.Fatalf`** — so failures appear in `runlog inspect`
- **Log every CLI call** — `rl.CLI(invocation, output)` after every command
- **No empty sections** — every `rl.Section()` must have at least one logged event
- **Project isolation** — `uniqueProjectName` + `deleteProjectOnCleanup`
- **Log HTTP calls** — `rl.CLIStep` for any HTTP that's permitted (admin endpoints, etc.)
