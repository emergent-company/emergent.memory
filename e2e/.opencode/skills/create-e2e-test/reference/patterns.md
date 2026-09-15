# E2E Test Patterns

Canonical patterns for common e2e test scenarios. All examples use the wrappers already available in the `dockertests` package (no explicit framework import needed unless stated otherwise).

---

## Pattern 1: Project isolation

Every test must create its own project and register a cleanup. This prevents state leakage between tests.

```go
func TestMyFeature_Isolated(t *testing.T) {
    logStatusPreamble(t)
    skipIfServerDown(t)

    home := t.TempDir()   // isolated HOME — credentials never bleed between tests
    srv  := serverURL()

    // Authenticate into the isolated HOME.
    mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
    mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", e2eTestToken())

    // Create an ephemeral project.
    projectName := fmt.Sprintf("e2e-myfeature-%d", time.Now().UnixMilli())
    createOut   := mustRunCLIInDirWithHome(t, "", home, "projects", "create", "--name", projectName)
    projectID   := parseProjectID(createOut)
    if projectID == "" {
        t.Fatalf("could not parse project ID from: %q", createOut)
    }

    // Always delete on test exit.
    t.Cleanup(func() {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        cmd := exec.CommandContext(ctx, "memory", "projects", "delete", projectID)
        cmd.Env = append(filteredEnv(), "HOME="+home, "PATH="+home+"/.memory/bin:"+os.Getenv("PATH"))
        cmd.CombinedOutput() // ignore error — best effort
    })

    // ... test logic ...
}
```

---

## Pattern 2: CLI invocation

```go
// Must succeed (calls t.Fatal on non-zero exit):
out := mustRunCLIInDirWithHome(t, "", home, "projects", "list")

// May fail (returns error):
out, err := runCLIInDirWithHome(t, "", home, "agents", "runs", agentID, "--project", projectID)
if err != nil {
    t.Logf("warn: agents runs failed: %v", err)
}

// Log the invocation in the run-log:
rl.CLI("memory projects list", out)
```

---

## Pattern 3: Agent trigger and poll

```go
// Create an agent.
agentOut := mustRunCLIInDirWithHome(t, "", home,
    "agents", "create",
    "--name", "orchestrator",
    "--trigger-type", "schedule",
    "--cron", "0 0 0 1 1 *",
    "--strategy-type", "agentic",
    "--project", projectID,
)
agentID := parseAgentID(agentOut)
if agentID == "" {
    t.Fatalf("could not parse agent ID from: %q", agentOut)
}

// Trigger the agent via HTTP (use when you need a prompt message).
triggerURL  := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", srv, projectID, agentID)
triggerBody, _ := json.Marshal(map[string]string{"prompt": "do the thing"})
triggerResp := doJSON(t, "POST", triggerURL, token, projectID, triggerBody)
if triggerResp.StatusCode != 200 {
    body := readBody(t, triggerResp)
    t.Fatalf("trigger: want 200, got %d — %s", triggerResp.StatusCode, body)
}
triggerResp.Body.Close()

// Poll until a successful run appears (or timeout).
const timeout = 5 * time.Minute
ok := pollAgentUntilSuccess(t, rl, home, srv, token, projectID, agentID, "orchestrator", timeout)
if !ok {
    t.Fatalf("agent did not complete successfully within %s", timeout)
}
```

---

## Pattern 4: Graph object assertion

```go
// List objects of a given type via the HTTP API.
objs := listGraphObjectsByType(t, srv, token, projectID, "Task")
if len(objs) == 0 {
    t.Error("expected at least one Task object")
}

// Extract a property from the first object.
for _, obj := range objs {
    props, _ := obj["properties"].(map[string]any)
    title  := propString(props, "title")
    status := propString(props, "status")
    t.Logf("Task: %q [%s]", title, status)
}

// List by label (semantic type tag).
docs := listGraphObjectsByLabel(t, srv, token, projectID, "Document")

// List relationships targeting an object.
rels := listRelationshipsByTarget(t, srv, token, projectID, someObjectID, "HAS_RESULT")
if len(rels) == 0 {
    t.Error("expected at least one HAS_RESULT relationship")
}
```

---

## Pattern 5: HTTP API call

```go
// GET with JSON parsing.
resp := doJSON(t, "GET", srv+"/api/projects/"+projectID+"/agents", token, projectID, nil)
body := readBody(t, resp)
if resp.StatusCode != 200 {
    t.Fatalf("GET agents: want 200, got %d — %s", resp.StatusCode, body)
}

var parsed struct {
    Data []struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    } `json:"data"`
}
if err := json.Unmarshal([]byte(body), &parsed); err != nil {
    t.Fatalf("parse agents response: %v", err)
}

// PATCH / POST.
patchBody, _ := json.Marshal(map[string]any{"enabled": true})
patchResp := doJSON(t, "PATCH", srv+"/api/admin/some-resource/"+id, token, projectID, patchBody)
if patchResp.StatusCode != 200 {
    t.Fatalf("PATCH: want 200, got %d", patchResp.StatusCode)
}
patchResp.Body.Close()
```

---

## Pattern 6: RunLog structured logging

```go
func TestMyFeature_WithRunLog(t *testing.T) {
    rl := newRunLog(t)
    defer rl.Close()  // always defer Close so the file is flushed

    rl.Section("Step 1 — Setup")
    rl.Printf("server: %s", serverURL())

    out := mustRunCLIInDirWithHome(t, "", home, "status")
    rl.CLI("memory status", out)  // logs command + output

    rl.Section("Step 2 — Assert")
    if !strings.Contains(out, "Connected") {
        t.Errorf("expected Connected, got: %s", out)
    }
    rl.Printf("status OK")
}
```

Log files are written to `logs/e2e/<TestName>/run.log` during test execution.

---

## Pattern 7: Blueprint install

```go
// Install a blueprint from a local path.
blueprintOut := mustRunCLIInDirWithHome(t, "", home,
    "blueprints", "/root/workspace-memory-blueprint",
    "--project", projectName,
    "--upgrade",  // idempotent
)
rl.CLI("memory blueprints ...", blueprintOut)
if strings.Contains(blueprintOut, "errors") && !strings.Contains(blueprintOut, "0 errors") {
    t.Fatalf("blueprint install reported errors:\n%s", blueprintOut)
}

// Assert expected resources appear in the output.
for _, item := range []string{"multi-agent-task-pack", "orchestrator", "web-researcher"} {
    if !strings.Contains(blueprintOut, item) {
        t.Errorf("blueprint output missing expected item %q", item)
    }
}
```

---

## Pattern 8: Bookstore fixture (opencode integration tests)

```go
import fixtures "github.com/emergent-company/emergent.memory/e2e/fixtures"

func TestMyFeature_WithBookstore(t *testing.T) {
    ws := fixtures.NewBookstoreWorkspace(t)
    // ws.Dir is the temp directory containing the bookstore project files.

    srv  := serverURL()
    token := e2eTestToken()

    // Configure the workspace to point at a specific project.
    ws.WriteEnvLocal(srv, projectID, token)

    // Run opencode or CLI commands against ws.Dir.
    out := mustRunCLIInDirWithHome(t, ws.Dir, home, "status")
    // ...
}
```

---

## Skip guards

```go
skipIfServerDown(t)                         // skip if /health unreachable
skipIfNoProdToken(t)                        // skip if MEMORY_PROD_TEST_TOKEN absent (production_test.go)

// Google AI tests:
if os.Getenv("GOOGLE_AI_API_KEY") == "" {
    t.Skip("GOOGLE_AI_API_KEY not set")
}
```

---

## Polling with a deadline (manual)

When `pollAgentUntilSuccess` is not sufficient (e.g. polling for a graph object status):

```go
const timeout    = 10 * time.Minute
deadline         := time.Now().Add(timeout)
pollCount        := 0
var lastStatus string

for time.Now().Before(deadline) {
    time.Sleep(pollInterval)  // package-level 5s constant from orchestrator_test.go
    pollCount++

    out, err := runCLIInDirWithHome(t, "", home,
        "graph", "objects", "list",
        "--type", "WorkPackage",
        "--project", projectID,
        "--output", "json",
    )
    if err != nil {
        rl.Printf("poll #%d: error: %v", pollCount, err)
        continue
    }

    // parse status from out ...
    if status == "accepted" {
        rl.Printf("done after %d polls", pollCount)
        break
    }
    rl.Printf("poll #%d: status=%s", pollCount, status)
}

if lastStatus != "accepted" {
    t.Fatalf("did not reach accepted within %s (last=%s)", timeout, lastStatus)
}
```
