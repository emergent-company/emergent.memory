// Package experiments_test — query_sdk_vs_mcp_test.go
//
// Experiment: compare graph-query-agent using Python SDK (run_python) vs
// direct MCP tools with only the necessary tools for graph queries.
//
// Both variants use the same model (gemini-3.1-flash-lite-preview) and run
// the same 4 query scenarios against seeded graph data:
//
//  1. Find a named object (hybrid search)
//  2. List objects by type
//  3. Query empty graph
//  4. Relationship traversal
//
// The SDK variant uses the current default config (sandbox + run_python).
// The MCP variant overrides the agent to use 5 MCP tools directly, with a
// simpler system prompt and no sandbox.
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER  — URL of the Memory server.
//	MEMORY_TEST_TOKEN   — API key / token for the Memory server.
package experiments_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	queryExperimentName = "query-sdk-vs-mcp"

	// mcpQuerySystemPrompt is the old-style simple prompt without SDK reference.
	mcpQuerySystemPrompt = `You are a knowledge graph query assistant. Your role is to help users explore and understand the data in their knowledge graph.

## Rules
1. ALWAYS use the provided tools to look up data. Never answer from your training data or fabricate entities, relationships, or facts.
2. When you retrieve results, cite specific entity names, types, and relationship types in your response.
3. If a tool returns no results, clearly state that no matching data was found. Do not fabricate or hallucinate results.
4. For complex questions, chain multiple tool calls (e.g., search first, then traverse relationships).
5. Format responses using markdown for clarity. Use tables for structured data when appropriate.
6. Keep responses concise and factual. Focus on what the data shows.`

	// mcpQueryTools are the 5 MCP tools necessary for graph queries.
	mcpQueryTools = "search-hybrid,entity-query,entity-edges-get,relationship-list,entity-type-list"
)

func TestQuerySDKvsMCP_SDK(t *testing.T) {
	runQueryVariant(t, "sdk", nil)
}

func TestQuerySDKvsMCP_MCP(t *testing.T) {
	runQueryVariant(t, "mcp", &queryOverrideConfig{
		systemPrompt:   mcpQuerySystemPrompt,
		tools:          strings.Split(mcpQueryTools, ","),
		disableSandbox: true,
	})
}

// queryOverrideConfig holds the override values for the MCP variant.
type queryOverrideConfig struct {
	systemPrompt   string
	tools          []string
	disableSandbox bool
}

// queryScenarioResult captures the outcome of a single query scenario.
type queryScenarioResult struct {
	name    string
	latency time.Duration
	output  string
	passed  bool
	err     error
}

// runQueryVariant executes the full query experiment for one variant.
// When overrideCfg is nil, the agent runs with default (SDK) config.
func runQueryVariant(t *testing.T, variant string, overrideCfg *queryOverrideConfig) {
	t.Helper()

	rl := newRunLog(t)
	defer rl.Close()

	skipIfServerDown(t, rl)
	rl.SetExperiment(queryExperimentName)
	rl.Tag("variant:" + variant)

	bullets := []string{
		fmt.Sprintf("Variant: %s", variant),
		"Creates ephemeral project, seeds objects and relationships",
		"Runs 4 query scenarios: find-by-name, list-by-type, empty-graph, relationship-traversal",
		"Records latency and correctness for each scenario",
	}
	if overrideCfg != nil {
		bullets = append(bullets, fmt.Sprintf("Override: %d MCP tools, sandbox disabled, simpler prompt", len(overrideCfg.tools)))
	} else {
		bullets = append(bullets, "Default config: SDK (run_python sandbox)")
	}
	describe(rl, fmt.Sprintf("Query agent experiment: %s variant", variant), bullets...)

	home := t.TempDir()
	logStatusPreamble(t, home)
	setupCLIAuth(t, home)

	// ── Create project ──────────────────────────────────────────────────────
	rl.Section("Create project")
	projectName := fmt.Sprintf("e2e-qexp-%s-%d", variant, time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home,
		append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)
	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })

	// Set project_id in config for subsequent commands.
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "project_id", projectID)

	// ── Apply override if MCP variant ───────────────────────────────────────
	if overrideCfg != nil {
		rl.Section("Apply agent override")
		applyQueryOverride(t, rl, home, projectID, overrideCfg)
	}

	// ── Force agent definition refresh (first query call does this) ─────────
	// Trigger a dummy query to ensure the agent definition is created/updated
	// with the override applied.
	rl.Section("Warm up agent definition")
	warmOut, _ := runQueryCLI(t, home, projectID, "hello", 30*time.Second)
	rl.Printf("warmup response (%d bytes): %s", len(warmOut), truncateStr(warmOut, 200))

	// ── Seed data for scenarios ─────────────────────────────────────────────
	rl.Section("Seed graph data")

	svcName := fmt.Sprintf("QExp-Svc-%d", time.Now().UnixMilli())
	svcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcName,
		"--description", "Payment processing gateway for merchant accounts",
		"--project", projectID,
		"--output", "json")
	svcID := parseJSONField(svcOut, "id")
	rl.Printf("seeded Service: %s (id=%s)", svcName, svcID)

	compName := fmt.Sprintf("QExp-Comp-%d", time.Now().UnixMilli())
	compOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Component",
		"--name", compName,
		"--description", "PostgreSQL database for orders",
		"--project", projectID,
		"--output", "json")
	compID := parseJSONField(compOut, "id")
	rl.Printf("seeded Component: %s (id=%s)", compName, compID)

	// Create relationship if both IDs are available.
	if svcID != "" && compID != "" {
		relOut, relErr := runCLIInDirWithHome(t, "", home,
			"graph", "relationships", "create",
			"--from", svcID,
			"--to", compID,
			"--type", "DEPENDS_ON",
			"--project", projectID)
		if relErr != nil {
			rl.Printf("WARN: relationship creation failed: %v", relErr)
		} else {
			rl.Printf("created DEPENDS_ON relationship: %s -> %s", svcName, compName)
			_ = relOut
		}
	}

	// ── Create empty project for empty-graph scenario ───────────────────────
	rl.Section("Create empty project for empty-graph test")
	emptyProjectName := fmt.Sprintf("e2e-qexp-%s-empty-%d", variant, time.Now().UnixMilli())
	emptyOut := mustRunCLIInDirWithHome(t, "", home,
		append([]string{"projects", "create", "--name", emptyProjectName}, projectCreateOrgArgs()...)...)
	emptyProjectID := parseProjectID(emptyOut)
	if emptyProjectID == "" {
		rl.Failf("could not parse empty project ID")
	}
	t.Cleanup(func() { deleteProjectViaExec(t, home, emptyProjectID, emptyProjectName) })

	// Apply same override to empty project if MCP variant.
	if overrideCfg != nil {
		applyQueryOverride(t, rl, home, emptyProjectID, overrideCfg)
	}

	// ── Run scenarios ───────────────────────────────────────────────────────
	timeout := 60 * time.Second
	var results []queryScenarioResult

	// Scenario 1: Find seeded object by name
	rl.Section("Scenario 1: Find by name")
	r1 := runQueryScenario(t, home, projectID, "find-by-name",
		fmt.Sprintf("What do you know about %s?", svcName), timeout)
	r1.passed = r1.err == nil && containsAnyStr(r1.output, []string{svcName, "payment", "merchant", "Service"})
	results = append(results, r1)
	rl.Printf("[%s] latency=%s passed=%v output=%s",
		r1.name, r1.latency.Round(time.Millisecond), r1.passed, truncateStr(r1.output, 300))

	// Scenario 2: List by type
	rl.Section("Scenario 2: List by type")
	r2 := runQueryScenario(t, home, projectID, "list-by-type",
		"List all Service objects in this project", timeout)
	r2.passed = r2.err == nil && containsAnyStr(r2.output, []string{svcName, "Service", "service"})
	results = append(results, r2)
	rl.Printf("[%s] latency=%s passed=%v output=%s",
		r2.name, r2.latency.Round(time.Millisecond), r2.passed, truncateStr(r2.output, 300))

	// Scenario 3: Empty graph
	rl.Section("Scenario 3: Empty graph")
	r3 := runQueryScenario(t, home, emptyProjectID, "empty-graph",
		"What objects exist in this project?", timeout)
	noDataIndicators := []string{"no ", "empty", "doesn't have", "does not have", "no objects",
		"no data", "no results", "none", "not find", "couldn't find", "no entities"}
	r3.passed = r3.err == nil && containsAnyStr(r3.output, noDataIndicators)
	results = append(results, r3)
	rl.Printf("[%s] latency=%s passed=%v output=%s",
		r3.name, r3.latency.Round(time.Millisecond), r3.passed, truncateStr(r3.output, 300))

	// Scenario 4: Relationship traversal
	rl.Section("Scenario 4: Relationship traversal")
	r4 := runQueryScenario(t, home, projectID, "relationship-traversal",
		fmt.Sprintf("How is %s related to other objects in this project?", svcName), timeout)
	relKeywords := []string{svcName, compName, "DEPENDS_ON", "depends", "relationship", "related", "connect"}
	r4.passed = r4.err == nil && containsAnyStr(r4.output, relKeywords)
	results = append(results, r4)
	rl.Printf("[%s] latency=%s passed=%v output=%s",
		r4.name, r4.latency.Round(time.Millisecond), r4.passed, truncateStr(r4.output, 300))

	// ── Summary ─────────────────────────────────────────────────────────────
	rl.Section("Summary")
	totalLatency := time.Duration(0)
	passCount := 0
	for _, r := range results {
		totalLatency += r.latency
		if r.passed {
			passCount++
		}
	}
	rl.Printf("variant:       %s", variant)
	rl.Printf("scenarios:     %d/%d passed", passCount, len(results))
	rl.Printf("total latency: %s", totalLatency.Round(time.Millisecond))
	rl.Printf("avg latency:   %s", (totalLatency / time.Duration(len(results))).Round(time.Millisecond))

	rl.Printf("")
	rl.Printf("%-25s  %10s  %6s  %s", "SCENARIO", "LATENCY", "PASS", "ERROR")
	rl.Printf("%-25s  %10s  %6s  %s", strings.Repeat("-", 25), strings.Repeat("-", 10), strings.Repeat("-", 6), strings.Repeat("-", 30))
	for _, r := range results {
		errStr := ""
		if r.err != nil {
			errStr = r.err.Error()
			if len(errStr) > 30 {
				errStr = errStr[:30]
			}
		}
		rl.Printf("%-25s  %10s  %6v  %s", r.name, r.latency.Round(time.Millisecond), r.passed, errStr)
	}

	// Fail test if fewer than 2 scenarios passed (some tolerance for flakiness).
	if passCount < 2 {
		t.Errorf("only %d/%d scenarios passed for variant %q — expected at least 2", passCount, len(results), variant)
	}
}

// runQueryScenario executes a single memory query and captures timing.
func runQueryScenario(t *testing.T, home, projectID, name, question string, timeout time.Duration) queryScenarioResult {
	t.Helper()
	start := time.Now()
	out, err := runQueryCLI(t, home, projectID, question, timeout)
	elapsed := time.Since(start)
	return queryScenarioResult{
		name:    name,
		latency: elapsed,
		output:  out,
		err:     err,
	}
}

// runQueryCLI runs `memory query "<question>" --project <id>` with custom timeout.
func runQueryCLI(t *testing.T, home, projectID, question string, timeout time.Duration) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{"query", question, "--project", projectID}
	cmd := exec.CommandContext(ctx, "memory", args...)
	env := filteredEnv()
	env = append(env, "HOME="+home)
	env = append(env, "PATH="+home+"/.memory/bin:"+os.Getenv("PATH"))
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}

// applyQueryOverride sets the agent override via the CLI.
func applyQueryOverride(t *testing.T, rl *runLog, home, projectID string, cfg *queryOverrideConfig) {
	t.Helper()

	args := []string{"defs", "override", "graph-query-agent", "--project", projectID}

	if cfg.tools != nil {
		args = append(args, "--tools", strings.Join(cfg.tools, ","))
	}
	if cfg.disableSandbox {
		args = append(args, "--sandbox-enabled", "false")
	}
	if cfg.systemPrompt != "" {
		// Write prompt to a temp file to avoid shell quoting issues with long prompts.
		promptFile := filepath.Join(t.TempDir(), "override-prompt.txt")
		if err := os.WriteFile(promptFile, []byte(cfg.systemPrompt), 0644); err != nil {
			rl.Failf("write prompt file: %v", err)
		}
		args = append(args, "--system-prompt-file", promptFile)
	}

	out := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory "+strings.Join(args, " "), out)
	rl.Printf("override applied for project %s", projectID)
}

// containsAnyStr returns true if lower-cased text contains any of the keywords.
func containsAnyStr(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// truncateStr truncates s to n chars, appending "..." if truncated.
func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
