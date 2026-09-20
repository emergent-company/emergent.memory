// Package experiments_test — helpers_test.go
//
// Thin wrappers around framework exported functions, plus experiment-specific
// constants shared across all files in this package.
package experiments_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants shared across experiment tests
// ─────────────────────────────────────────────────────────────────────────────

const (
	// researchCodeExperimentName groups related model comparison runs in the TUI.
	researchCodeExperimentName = "research-and-code-model-comparison"

	// blueprintV3URL is the local path to the v3 blueprint directory.
	blueprintV3URL = "/root/workspace-memory-blueprint-v3"

	// blueprintV3Model is the model name pinned inside the v3 blueprint YAML files.
	blueprintV3Model = "gemini-3.1-flash-lite-preview"

	// v3NativeSeniorCoderTimeout is the maximum time to wait for the native senior coder agent.
	v3NativeSeniorCoderTimeout = 10 * time.Minute

	// pollInterval is how often to re-check agent run status.
	pollInterval = 5 * time.Second

	// seniorCoderTaskTitle and seniorCoderTaskMessage define the standard
	// task used for model comparison experiments.
	seniorCoderTaskTitle = "Open-Meteo Weather Script"

	seniorCoderTaskMessage = "Title: Open-Meteo Weather Script\n" +
		"Description: Search the web for the Open-Meteo API documentation. " +
		"Then write a Python script that fetches the current weather for London " +
		"(latitude 51.5, longitude -0.1) using the Open-Meteo API (no API key needed). " +
		"The script must print the current temperature in Celsius. " +
		"Use only the standard library plus the requests package."
)

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases
// ─────────────────────────────────────────────────────────────────────────────

type runLog = framework.RunLog
type agentInfo = framework.AgentInfo

// ─────────────────────────────────────────────────────────────────────────────
// Run log helpers
// ─────────────────────────────────────────────────────────────────────────────

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	return framework.NewRunLog(t)
}

func describe(rl *runLog, summary string, bullets ...string) {
	rl.Describe(summary, bullets...)
}

func compactRunsOutput(runsOut string) string {
	return framework.CompactRunsOutput(runsOut)
}

// ─────────────────────────────────────────────────────────────────────────────
// Server helpers
// ─────────────────────────────────────────────────────────────────────────────

func serverURL() string     { return framework.ServerURL() }
func e2eTestToken() string  { return framework.E2ETestToken() }
func filteredEnv() []string { return framework.FilteredEnv() }

func skipIfServerDown(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	framework.SkipIfServerDown(t, rl)
}

func setupCLIAuth(t *testing.T, home string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.Section("Setup CLI auth")
		}
	}
	framework.SetupCLIAuth(t, home)
}

func logStatusPreamble(t *testing.T, home ...string) {
	t.Helper()
	framework.LogStatusPreamble(t, home...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Skip guards
// ─────────────────────────────────────────────────────────────────────────────

// skipIfNoLLMProvider skips t when no LLM provider env var is set.
func skipIfNoLLMProvider(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	provider, _, _ := framework.ProviderFromEnv()
	if provider == "" {
		framework.DoSkipf(t, rl,
			"no LLM provider configured — set DEEPSEEK_API_KEY, GOOGLE_AI_API_KEY, or OPENAI_API_KEY")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CLI helpers
// ─────────────────────────────────────────────────────────────────────────────

func emitCLI(t *testing.T, invocation, out string) {
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.CLI(invocation, out)
			return
		}
	}
	logSession(t, invocation, out)
}

func emitCLIErr(t *testing.T, invocation, out string, err error) {
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.CLIErr(invocation, out, err, 0)
		}
	}
}

func mustRunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) string {
	t.Helper()
	out := framework.MustRunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	emitCLI(t, invocation, out)
	return out
}

// runCLIInDirWithHome is like mustRunCLIInDirWithHome but returns an error
// instead of failing the test — used for polling where transient failures are OK.
func runCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error) {
	t.Helper()
	out, err := framework.RunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	emitCLIErr(t, invocation, out, err)
	return out, err
}

func logSession(t *testing.T, invocation, output string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if _, ok := v.(*framework.RunLog); ok {
			return
		}
	}
	framework.LogSession(t, invocation, output)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func doJSON(t *testing.T, method, url, token, projectID string, body []byte) *http.Response {
	return framework.DoJSON(t, method, url, token, projectID, body)
}

func readBody(t *testing.T, resp *http.Response) string {
	return framework.ReadBody(t, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// Parse helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseProjectID(output string) string         { return framework.ParseProjectID(output) }
func parseAgentID(output string) string           { return framework.ParseAgentID(output) }
func parseAgentDefsModel(output string) string    { return framework.ParseAgentDefsModel(output) }
func parseJSONField(jsonStr, field string) string { return framework.ParseJSONField(jsonStr, field) }

func projectCreateOrgArgs() []string { return framework.ProjectCreateOrgArgs() }
func orgIDArgs() []string            { return framework.OrgIDArgs() }

// ─────────────────────────────────────────────────────────────────────────────
// Agent helpers
// ─────────────────────────────────────────────────────────────────────────────

func dumpAgentRunDetails(t *testing.T, rl *runLog, srv, token, projectID string, agentNames, agentIDs []string) {
	framework.DumpAgentRunDetails(t, rl, srv, token, projectID, agentNames, agentIDs)
}

// ─────────────────────────────────────────────────────────────────────────────
// Model helpers
// ─────────────────────────────────────────────────────────────────────────────

// activeModel returns the model actually in use by the installed agent definitions.
func activeModel(defsOut string) string {
	if m := parseAgentDefsModel(defsOut); m != "" {
		return m
	}
	return blueprintV3Model
}

// ─────────────────────────────────────────────────────────────────────────────
// Gantt / token summary helpers
// ─────────────────────────────────────────────────────────────────────────────

type agentRunInterval = framework.AgentRunInterval

func buildAgentRunIntervals(t *testing.T, rl *runLog, srv, token, projectID string, agents []agentInfo) []agentRunInterval {
	return framework.BuildAgentRunIntervals(t, rl, srv, token, projectID, agents)
}

func printGanttTimeline(rl *runLog, intervals []agentRunInterval) {
	framework.PrintGantt(rl, intervals)
}

func printTokenUsageSummary(rl *runLog, intervals []agentRunInterval) {
	framework.PrintTokenSummary(rl, intervals)
}

// ─────────────────────────────────────────────────────────────────────────────
// Project cleanup helper
// ─────────────────────────────────────────────────────────────────────────────

// deleteProjectViaExec deletes a project using the memory CLI binary directly.
// Does not call t.Fatal — safe for t.Cleanup.
func deleteProjectViaExec(t *testing.T, home, projectID, projectName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "memory", "projects", "delete", projectID)
	env := filteredEnv()
	env = append(env, "HOME="+home)
	env = append(env, "PATH="+home+"/.memory/bin:"+os.Getenv("PATH"))
	cmd.Env = env

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("warn: failed to delete test project %s: %v\n%s", projectID, err, out)
	} else {
		t.Logf("deleted test project %s (%s)", projectName, projectID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Step-level helpers — high-level building blocks for experiment tests
// ─────────────────────────────────────────────────────────────────────────────

// setupProject creates an ephemeral project, sets project_id in the CLI config,
// and registers a t.Cleanup that deletes it. Returns (projectName, projectID).
func setupProject(t *testing.T, rl *runLog, home, namePrefix string) (string, string) {
	t.Helper()
	rl.Section("Create project")
	projectName := fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "project_id", projectID)

	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })
	return projectName, projectID
}

// setupTestProvider configures whichever LLM provider is available from env vars.
// model overrides the env-var model when non-empty (e.g. for blueprint-pinned models).
// If no provider env vars are set the test is skipped.
func setupTestProvider(t *testing.T, rl *runLog, home, model string) {
	t.Helper()
	provider, apiKey, envModel := framework.ProviderFromEnv()
	if provider == "" {
		framework.DoSkipf(t, rl,
			"no LLM provider configured — set DEEPSEEK_API_KEY, GOOGLE_AI_API_KEY, or OPENAI_API_KEY")
	}
	if model == "" {
		model = envModel
	}
	rl.Section("Configure provider")
	args := []string{"provider", "configure", provider, "--api-key", apiKey}
	if model != "" {
		args = append(args, "--generative-model", model)
	}
	args = append(args, orgIDArgs()...)
	out, provErr := runCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider configure "+provider, out)
	if provErr != nil {
		testOut, testErr := runCLIInDirWithHome(t, "", home,
			append([]string{"provider", "test"}, orgIDArgs()...)...)
		rl.CLI("memory provider test", testOut)
		if testErr != nil {
			rl.Failf("provider configure failed and provider test also failed: %v\n%s", testErr, testOut)
		}
	}
}

// installBlueprint installs a blueprint, verifies requiredDefs are present,
// overrides agent definition models when model != blueprintModel, and tags the
// run log. extraTags are appended after the "model:..." tag. Returns the
// post-override agent-definitions list output.
func installBlueprint(t *testing.T, rl *runLog, home, projectName, projectID, url, model, bpModel string, requiredDefs []string, extraTags ...string) string {
	t.Helper()
	rl.Section("Install blueprint")
	blueprintOut := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", url,
		"--project", projectName,
		"--upgrade",
	)
	rl.CLI("memory blueprints "+url+" --project "+projectName+" --upgrade", blueprintOut)
	if strings.Contains(blueprintOut, "errors") && !strings.Contains(blueprintOut, "0 errors") {
		rl.Failf("blueprint install reported errors:\n%s", blueprintOut)
	}

	defsOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "list",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions list --project "+projectID, defsOut)
	for _, required := range requiredDefs {
		if !strings.Contains(defsOut, required) {
			rl.Failf("agent definition %q not found after blueprint install", required)
		}
	}

	overrideAgentDefsModel(t, rl, home, projectID, model, bpModel)
	postOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "list", "--project", projectID)
	tags := append([]string{"model:" + activeModel(postOut)}, extraTags...)
	rl.Tag(tags...)
	return postOut
}

// createRuntimeAgents creates runtime agent entities for each entry in agents.
// Each entry's Name is required; Cron defaults to "0 0 0 1 1 *" when empty.
// The ID field of each entry is populated and the updated slice is returned.
func createRuntimeAgents(t *testing.T, rl *runLog, home, projectID string, agents []agentInfo) []agentInfo {
	t.Helper()
	rl.Section("Create runtime agents")
	const defaultCron = "0 0 0 1 1 *"
	result := make([]agentInfo, len(agents))
	for i, a := range agents {
		cron := a.Cron
		if cron == "" {
			cron = defaultCron
		}
		agentOut := mustRunCLIInDirWithHome(t, "", home,
			"agents", "create",
			"--name", a.Name,
			"--trigger-type", "schedule",
			"--cron", cron,
			"--strategy-type", "agentic",
			"--project", projectID,
		)
		rl.CLI("memory agents create --name "+a.Name, agentOut)
		id := parseAgentID(agentOut)
		if id == "" {
			rl.Failf("could not parse agent ID for %q from: %q", a.Name, agentOut)
		}
		result[i] = agentInfo{Name: a.Name, ID: id, Cron: cron}
	}
	return result
}

// createWorkPackage pre-creates a WorkPackage graph object with the given title
// and returns its canonical ID.
func createWorkPackage(t *testing.T, rl *runLog, home, projectID, title string) string {
	t.Helper()
	rl.Section("Create WorkPackage")
	wpPropsJSON, _ := json.Marshal(map[string]string{
		"title":  title,
		"status": "created",
	})
	wpCreateOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "WorkPackage",
		"--properties", string(wpPropsJSON),
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type WorkPackage", wpCreateOut)
	wpID := parseJSONField(wpCreateOut, "id")
	if wpID == "" {
		wpID = parseJSONField(wpCreateOut, "canonical_id")
	}
	if wpID == "" {
		rl.Failf("could not parse WorkPackage ID from: %q", wpCreateOut)
	}
	return wpID
}

// dumpAgents writes per-run detail logs for every agent in the slice.
func dumpAgents(t *testing.T, rl *runLog, srv, token, projectID string, agents []agentInfo) {
	t.Helper()
	names := make([]string, len(agents))
	ids := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
		ids[i] = a.ID
	}
	dumpAgentRunDetails(t, rl, srv, token, projectID, names, ids)
}

// printSummary opens a Summary section and prints the Gantt timeline and token
// usage summary for the given agents.
func printSummary(t *testing.T, rl *runLog, srv, token, projectID string, agents []agentInfo) {
	t.Helper()
	rl.Section("Summary")
	intervals := buildAgentRunIntervals(t, rl, srv, token, projectID, agents)
	printGanttTimeline(rl, intervals)
	printTokenUsageSummary(rl, intervals)
}

// overrideAgentDefsModel updates every agent definition in the project to use
// the given model name. It is a no-op when model == currentBlueprintModel.
func overrideAgentDefsModel(t *testing.T, rl *runLog, home, projectID, model, currentBlueprintModel string) {
	t.Helper()
	if model == currentBlueprintModel {
		return
	}

	listOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "list",
		"--project", projectID,
	)
	if err != nil {
		rl.Failf("overrideAgentDefsModel: list failed: %v\n%s", err, listOut)
	}

	type agentDef struct{ id, name string }
	var defs []agentDef
	var currentName string
	for _, line := range strings.Split(listOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if idx := strings.Index(trimmed, ". "); idx != -1 {
				currentName = strings.TrimSpace(trimmed[idx+2:])
			}
		}
		if strings.HasPrefix(trimmed, "ID:") {
			id := strings.TrimSpace(strings.TrimPrefix(trimmed, "ID:"))
			if id != "" {
				defs = append(defs, agentDef{id: id, name: currentName})
				currentName = ""
			}
		}
	}

	if len(defs) == 0 {
		rl.Failf("overrideAgentDefsModel: found 0 agent definitions in output:\n%s", listOut)
	}

	rl.Printf("overriding model to %q on %d agent definition(s)", model, len(defs))
	for _, def := range defs {
		updateOut, updateErr := runCLIInDirWithHome(t, "", home,
			"agent-definitions", "update", def.id,
			"--project", projectID,
			"--model", model,
		)
		if updateErr != nil {
			rl.Failf("overrideAgentDefsModel: update %s (%s) failed: %v\n%s", def.name, def.id, updateErr, updateOut)
		}
		rl.Printf("  updated %s (%s) → model=%s", def.name, def.id, model)
	}
}
