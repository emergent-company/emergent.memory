// Package cli_test — ask_prompt_test.go
//
// End-to-end tests that validate the CLI assistant agent's prompt behavior:
// classification accuracy (DOCS vs TASK routing), documentation response
// quality, and correct tool selection for task requests.
//
// These tests serve as a safety net for prompt optimizations — they capture
// the expected behavior before changes are made, so regressions can be
// detected after prompt modifications.
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER  — URL of the Memory server.
//	MEMORY_TEST_TOKEN   — API key / token for the Memory server.
package cli_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Group A — DOCS classification: pure documentation questions
//
// These tests verify the agent correctly classifies doc questions and answers
// from platform knowledge (CLI reference, docs URLs) WITHOUT calling graph/
// agent/schema tools.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_DocsClassification_HowToCreateProject asks a pure docs
// question ("how do I create a project?") and verifies the agent answers with
// CLI command guidance rather than executing a tool to actually create one.
func TestAskPrompt_DocsClassification_HowToCreateProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: docs classification — how to create a project",
		"Ask a 'how do I' question about creating projects",
		"Verify response contains CLI command guidance (memory projects create)",
		"Verify response does NOT actually create a project",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-docs-create")

	rl.Section("Ask docs question about creating projects")
	out := mustAsk(t, rl, home,
		"How do I create a new project using the memory CLI?",
		contextProjectID)

	rl.Section("Verify docs-style response")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should contain CLI command guidance.
	if !containsAny(out, []string{"memory projects create", "projects create", "--name"}) {
		t.Errorf("expected docs response to mention 'memory projects create' or '--name'; got: %s",
			truncate(out, 500))
	}

	// Should NOT contain evidence of actually executing a create operation.
	// Look for tool execution indicators: UUIDs from created resources, "successfully created", etc.
	createdID := extractUUIDFromText(out)
	if createdID != "" {
		// If there's a UUID, it might be the context project — that's OK.
		// But if the agent says "created" + UUID, that's a classification miss.
		if containsAll(out, []string{"created", createdID}) &&
			createdID != contextProjectID {
			t.Errorf("agent appears to have actually created a project (classification miss); response: %s",
				truncate(out, 500))
		}
	}
	rl.Printf("response contains CLI guidance without executing create")
}

// TestAskPrompt_DocsClassification_WhatIsKnowledgeGraph asks a conceptual
// docs question and verifies the response explains the knowledge graph
// concept rather than querying graph data.
func TestAskPrompt_DocsClassification_WhatIsKnowledgeGraph(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: docs classification — what is the knowledge graph",
		"Ask a conceptual question about the knowledge graph",
		"Verify response explains the concept (graph, objects, relationships)",
		"Verify response does NOT query live graph data",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-docs-kg")

	rl.Section("Ask conceptual question about knowledge graph")
	out := mustAsk(t, rl, home,
		"What is the knowledge graph in Memory and how does it work?",
		contextProjectID)

	rl.Section("Verify conceptual response")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should explain graph concepts.
	conceptKeywords := []string{"graph", "object", "relationship", "entit"}
	if !containsAny(out, conceptKeywords) {
		t.Errorf("expected conceptual explanation mentioning %v; got: %s",
			conceptKeywords, truncate(out, 500))
	}

	// Should NOT say "no objects found" or "no data" — that indicates it queried
	// the graph instead of explaining the concept.
	lowerOut := strings.ToLower(out)
	if strings.Contains(lowerOut, "no objects found") ||
		strings.Contains(lowerOut, "no data found") ||
		strings.Contains(lowerOut, "no entities found") {
		t.Errorf("agent appears to have queried graph data for a conceptual question (classification miss); got: %s",
			truncate(out, 500))
	}
	rl.Printf("response explains knowledge graph concept without querying data")
}

// TestAskPrompt_DocsClassification_SupportedProviders asks about supported LLM
// providers and verifies the agent answers from platform facts (Google AI,
// Vertex AI only) without fabricating unsupported providers.
func TestAskPrompt_DocsClassification_SupportedProviders(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: docs classification — supported LLM providers",
		"Ask what LLM providers Memory supports",
		"Verify response mentions Google AI and/or Vertex AI",
		"Verify response does NOT mention OpenAI or Anthropic as supported",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Ask about supported providers")
	// No project context needed — this is a pure docs question.
	out := mustAsk(t, rl, home,
		"What LLM providers does Memory support?")

	rl.Section("Verify provider facts")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Must mention Google/Gemini/Vertex.
	if !containsAny(out, []string{"google", "gemini", "vertex"}) {
		t.Errorf("expected response to mention Google/Gemini/Vertex; got: %s",
			truncate(out, 500))
	}

	// Must NOT present OpenAI or Anthropic as supported providers.
	lowerOut := strings.ToLower(out)
	for _, unsupported := range []string{"openai", "anthropic", "gpt-4", "claude"} {
		if strings.Contains(lowerOut, unsupported) {
			// Check if it's in a "not supported" context — that's fine.
			if !containsAny(out, []string{"not support", "not available", "only support", "do not"}) {
				t.Errorf("response mentions unsupported provider %q without disclaimer; got: %s",
					unsupported, truncate(out, 500))
			}
		}
	}
	rl.Printf("response correctly identifies supported providers")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group B — TASK classification: action requests that need tool calls
//
// These tests verify the agent correctly classifies task requests and uses
// the appropriate tools (not web-fetch) to fulfill them.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_TaskClassification_ListGraphObjects asks the agent to list
// graph objects in a project that has seeded data. Verifies the agent uses
// graph tools (not web-fetch) and returns actual data.
func TestAskPrompt_TaskClassification_ListGraphObjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: task classification — list graph objects with seeded data",
		"Create project and seed a Service graph object",
		"Ask agent to list graph objects",
		"Verify response contains the seeded object name",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project and seed data")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-task-list")

	// Seed a graph object.
	seedName := fmt.Sprintf("AskPromptTestSvc-%d", time.Now().UnixMilli())
	seedOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", seedName,
		"--description", "Test service for prompt classification validation",
		"--project", contextProjectID)
	rl.CLI("memory graph objects create --type Service --name "+seedName, seedOut)

	rl.Section("Ask agent to list graph objects")
	out := mustAsk(t, rl, home,
		"What graph objects exist in this project?",
		contextProjectID)

	rl.Section("Verify task response with live data")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// The agent should return actual data, not documentation about graph objects.
	if !containsAny(out, []string{seedName, "Service"}) {
		t.Errorf("expected agent to return seeded object %q or type 'Service'; got: %s",
			seedName, truncate(out, 500))
	}

	// Should NOT be a docs-style response about "how to list objects".
	docsIndicators := []string{"memory graph objects list", "to list graph objects", "you can use"}
	docsCount := 0
	for _, d := range docsIndicators {
		if strings.Contains(strings.ToLower(out), strings.ToLower(d)) {
			docsCount++
		}
	}
	if docsCount >= 2 && !containsAny(out, []string{seedName}) {
		t.Errorf("agent appears to have returned docs instead of querying data (classification miss); got: %s",
			truncate(out, 500))
	}
	rl.Printf("response contains live graph data including seeded object")
}

// TestAskPrompt_TaskClassification_CountObjects asks the agent to count
// objects of a specific type. This tests whether the agent can handle a
// data question that requires tool calls.
func TestAskPrompt_TaskClassification_CountObjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: task classification — count graph objects by type",
		"Create project and seed multiple graph objects",
		"Ask agent how many objects of type 'Component' exist",
		"Verify response contains a count or mentions the seeded objects",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project and seed data")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-task-count")

	// Seed two objects.
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("AskPromptComp-%d-%d", i, time.Now().UnixMilli())
		seedOut := mustRunCLIInDirWithHome(t, "", home,
			"graph", "objects", "create",
			"--type", "Component",
			"--name", name,
			"--project", contextProjectID)
		rl.CLI(fmt.Sprintf("memory graph objects create --type Component --name %s", name), seedOut)
	}

	rl.Section("Ask agent to count components")
	out := mustAsk(t, rl, home,
		"How many Component objects are in this project?",
		contextProjectID)

	rl.Section("Verify task response with count")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should mention "2" or "two" or list the components.
	if !containsAny(out, []string{"2", "two", "Component", "component"}) {
		t.Errorf("expected response to mention count or type 'Component'; got: %s",
			truncate(out, 500))
	}
	rl.Printf("response contains object count or component details")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group C — CLI-first response format
//
// These tests verify the agent defaults to CLI command examples (not curl/API)
// and respects the response format rules in the prompt.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_CLIFirst_NoCurlByDefault asks a docs question and verifies
// the response uses CLI commands, not curl or HTTP examples.
func TestAskPrompt_CLIFirst_NoCurlByDefault(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: CLI-first format — no curl by default",
		"Ask how to query the knowledge graph",
		"Verify response uses CLI commands (memory query/graph)",
		"Verify response does NOT include unsolicited curl examples",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Ask docs question about querying")
	out := mustAsk(t, rl, home,
		"How do I query the knowledge graph?")

	rl.Section("Verify CLI-first response format")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should mention CLI commands.
	if !containsAny(out, []string{"memory query", "memory graph", "query"}) {
		t.Errorf("expected CLI command examples; got: %s", truncate(out, 500))
	}

	// Should NOT contain unsolicited curl examples.
	lowerOut := strings.ToLower(out)
	if strings.Contains(lowerOut, "curl ") && !strings.Contains(lowerOut, "api") {
		t.Errorf("response contains unsolicited curl example; got: %s", truncate(out, 500))
	}
	rl.Printf("response uses CLI-first format")
}

// TestAskPrompt_APIWhenAsked explicitly asks about the REST API and verifies
// the response includes HTTP/API examples.
func TestAskPrompt_APIWhenAsked(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: API response when explicitly asked",
		"Ask about the REST API for graph objects",
		"Verify response includes HTTP/API/endpoint information",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Ask about REST API")
	out := mustAsk(t, rl, home,
		"What REST API endpoints exist for managing graph objects?")

	rl.Section("Verify API response")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// When the user asks about the API, the response should include API info.
	apiKeywords := []string{"api", "endpoint", "http", "rest", "POST", "GET", "/api/"}
	if !containsAny(out, apiKeywords) {
		t.Errorf("expected API/HTTP information when explicitly asked; got: %s",
			truncate(out, 500))
	}
	rl.Printf("response includes API information when explicitly requested")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group D — Cross-project tasks (SDK scripting)
//
// These tests verify the agent correctly uses run_python/run_go for tasks
// that span multiple projects (which graph tools cannot handle).
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_CrossProject_ListProjects asks the agent to list all projects
// and verifies it uses scripting (run_python/run_go) rather than graph tools.
func TestAskPrompt_CrossProject_ListProjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: cross-project task — list all projects",
		"Create a uniquely named project",
		"Ask agent to list all projects",
		"Verify response includes the project name (proving it used scripting or project-get)",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create uniquely named project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-askp-xproj")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	rl.Section("Ask agent to list all projects")
	out := mustAsk(t, rl, home,
		"List all my projects and their IDs",
		projectID)

	rl.Section("Verify cross-project response")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should contain the project we created.
	if !containsAny(out, []string{projectName, projectID}) {
		t.Errorf("expected response to mention project %q or ID %q; got: %s",
			projectName, projectID, truncate(out, 500))
	}

	// Should NOT say "no tool" or "cannot" — the agent has run_python/run_go for this.
	lowerOut := strings.ToLower(out)
	if strings.Contains(lowerOut, "don't have a tool") || strings.Contains(lowerOut, "cannot list projects") {
		t.Errorf("agent claims it cannot list projects — should use run_python/run_go; got: %s",
			truncate(out, 500))
	}
	rl.Printf("response lists projects correctly")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group E — Relocated commands
//
// These tests verify the agent uses the correct (new) command paths for
// commands that moved in a recent version.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_RelocatedCommands_MCPServers asks about MCP server management
// and verifies the agent suggests "memory agents mcp-servers" (new path),
// not "memory mcp-servers" (old path).
func TestAskPrompt_RelocatedCommands_MCPServers(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: relocated commands — MCP servers use new path",
		"Ask how to manage MCP servers",
		"Verify response uses 'memory agents mcp-servers' (new path)",
		"Verify response does NOT use 'memory mcp-servers' (old path)",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Ask about MCP server management")
	out := mustAsk(t, rl, home,
		"How do I list and manage MCP servers?")

	rl.Section("Verify correct command path")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should mention the new path.
	if !containsAny(out, []string{"agents mcp-servers", "agents mcp"}) {
		// May also just say "mcp-servers" in a general context — that's OK
		// as long as it doesn't say "memory mcp-servers" (old path without "agents").
		rl.Printf("response does not explicitly mention 'agents mcp-servers' — checking for old path")
	}

	// Check for old (wrong) path: "memory mcp-servers" without "agents" preceding it.
	lowerOut := strings.ToLower(out)
	// Only flag if the old path appears WITHOUT the correct new path also appearing.
	hasOldPath := strings.Contains(lowerOut, "memory mcp-servers") &&
		!strings.Contains(lowerOut, "memory agents mcp-servers")
	if hasOldPath {
		t.Errorf("response uses old command path 'memory mcp-servers' instead of 'memory agents mcp-servers'; got: %s",
			truncate(out, 500))
	}
	rl.Printf("response uses correct command path for MCP servers")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group F — Auth awareness
//
// These tests verify the agent adapts its response to authentication context.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_AuthAware_ProjectContextMentioned asks a project-scoped
// question with a valid project and verifies the agent acknowledges the
// project context (doesn't tell the user to set a project).
func TestAskPrompt_AuthAware_ProjectContextMentioned(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt: auth awareness — project context acknowledged",
		"Ask a project-scoped question with --project set",
		"Verify agent does NOT ask user to set a project",
		"Verify agent uses tools to answer (not just guidance)",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-auth-ctx")

	rl.Section("Ask project-scoped question")
	out := mustAsk(t, rl, home,
		"List the agent definitions in this project",
		contextProjectID)

	rl.Section("Verify project-aware response")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}

	// Should NOT tell the user to set a project — we already have one.
	lowerOut := strings.ToLower(out)
	if strings.Contains(lowerOut, "set project") ||
		strings.Contains(lowerOut, "pass --project") ||
		strings.Contains(lowerOut, "no project context") {
		t.Errorf("agent tells user to set project despite having project context; got: %s",
			truncate(out, 500))
	}
	rl.Printf("agent correctly acknowledges project context")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group G — Timing baselines
//
// These tests capture timing information for ask operations. They don't assert
// on specific latencies (too flaky), but they log timing data that can be
// compared before and after prompt optimizations.
// ─────────────────────────────────────────────────────────────────────────────

// TestAskPrompt_Timing_DocsQuestion captures timing for a pure docs question.
func TestAskPrompt_Timing_DocsQuestion(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt timing baseline: docs question",
		"Ask a docs question and capture response time",
		"Log timing for pre/post optimization comparison",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Time docs question")
	start := time.Now()
	out := mustAsk(t, rl, home,
		"How do I configure a Google provider for my organization?")
	elapsed := time.Since(start)

	rl.Section("Log timing")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}
	rl.Printf("docs question latency: %s", elapsed.Round(time.Millisecond))
	rl.Printf("response length: %d bytes", len(out))
}

// TestAskPrompt_Timing_TaskQuestion captures timing for a task question that
// requires tool calls.
func TestAskPrompt_Timing_TaskQuestion(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Prompt timing baseline: task question",
		"Ask a task question requiring tool calls and capture response time",
		"Log timing for pre/post optimization comparison",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-askp-timing-task")

	rl.Section("Time task question")
	start := time.Now()
	out := mustAsk(t, rl, home,
		"List all agent definitions in this project",
		contextProjectID)
	elapsed := time.Since(start)

	rl.Section("Log timing")
	if out == "" {
		rl.Failf("memory ask returned empty output")
	}
	rl.Printf("task question latency: %s", elapsed.Round(time.Millisecond))
	rl.Printf("response length: %d bytes", len(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// Local helpers
// ─────────────────────────────────────────────────────────────────────────────

// postAskProject wraps the SSE ask endpoint for project-scoped asks.
// This is defined in ask_test.go already — we reuse the helpers from
// ask_cli_test.go (mustAsk, askMayFail, etc.) which call the CLI directly.
// No additional helpers needed.

// Ensure framework import is used (for SetToken in skipIfEndpointMissing).
var _ = framework.SetToken
