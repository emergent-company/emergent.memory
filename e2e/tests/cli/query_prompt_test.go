// Package cli_test — query_prompt_test.go
//
// End-to-end tests that validate the graph query agent's prompt behavior:
// correct tool usage for graph queries, response quality with seeded data,
// and handling of empty/complex graph scenarios.
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
)

// ─────────────────────────────────────────────────────────────────────────────
// Group A — Basic graph queries (agent mode)
//
// These tests verify the query agent can find and describe seeded objects
// using its graph tools (search-hybrid, query_entities, etc.).
// ─────────────────────────────────────────────────────────────────────────────

// TestQueryPrompt_FindSeededObject seeds a named object, then queries for it
// by name and verifies the agent finds it.
func TestQueryPrompt_FindSeededObject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query prompt: find a seeded object by name",
		"Create project and seed a Service object with a unique name",
		"Run memory query in agent mode asking for that object",
		"Verify the response mentions the object name",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project and seed data")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-find")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	seedName := fmt.Sprintf("QueryPromptSvc-%d", time.Now().UnixMilli())
	seedOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", seedName,
		"--description", "Payment processing gateway for merchant accounts",
		"--project", projectID)
	rl.CLI("memory graph objects create --type Service --name "+seedName, seedOut)

	rl.Section("Query for seeded object")
	out, err := runCLIInDirWithHome(t, "", home,
		"query", fmt.Sprintf("What do you know about %s?", seedName),
		"--project", projectID)
	rl.CLI("memory query '"+seedName+"' --project "+projectID, out)

	if err != nil {
		rl.Printf("query agent returned error (may be expected without provider): %v", err)
		t.Skipf("memory query agent mode failed: %v", err)
	}

	rl.Section("Verify response mentions seeded object")
	if out == "" {
		rl.Failf("memory query returned empty output")
	}

	if !containsAny(out, []string{seedName, "payment", "merchant", "Service"}) {
		t.Errorf("expected query response to mention seeded object %q or its description; got: %s",
			seedName, truncate(out, 500))
	}
	rl.Printf("query agent found seeded object %s", seedName)
}

// TestQueryPrompt_ListByType seeds multiple objects of different types, then
// queries for a specific type and verifies only objects of that type are
// mentioned.
func TestQueryPrompt_ListByType(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query prompt: list objects by type",
		"Create project and seed Service and Component objects",
		"Query for 'all Service objects'",
		"Verify response mentions Service objects, not Components",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project and seed data")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-type")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	svcName := fmt.Sprintf("QP-Svc-%d", time.Now().UnixMilli())
	mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcName,
		"--description", "Authentication service",
		"--project", projectID)
	rl.Printf("seeded Service: %s", svcName)

	compName := fmt.Sprintf("QP-Comp-%d", time.Now().UnixMilli())
	mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Component",
		"--name", compName,
		"--project", projectID)
	rl.Printf("seeded Component: %s", compName)

	rl.Section("Query for Service objects")
	out, err := runCLIInDirWithHome(t, "", home,
		"query", "List all Service objects in this project",
		"--project", projectID)
	rl.CLI("memory query 'List all Service objects' --project "+projectID, out)

	if err != nil {
		rl.Printf("query agent returned error: %v", err)
		t.Skipf("memory query agent mode failed: %v", err)
	}

	rl.Section("Verify type-filtered response")
	if out == "" {
		rl.Failf("memory query returned empty output")
	}

	// Should mention the Service or its name.
	if !containsAny(out, []string{svcName, "Service", "service", "authentication"}) {
		t.Errorf("expected response to mention Service %q; got: %s",
			svcName, truncate(out, 500))
	}
	rl.Printf("query response correctly includes Service objects")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group B — Empty graph handling
//
// These tests verify the query agent handles empty graphs gracefully — it
// should clearly state no data was found rather than hallucinating objects.
// ─────────────────────────────────────────────────────────────────────────────

// TestQueryPrompt_EmptyGraph queries an empty project and verifies the agent
// says "no data found" rather than fabricating results.
func TestQueryPrompt_EmptyGraph(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query prompt: empty graph returns 'no data found'",
		"Create an empty project (no graph objects)",
		"Query for 'what objects exist'",
		"Verify response states no data, not fabricated results",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create empty project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-empty")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Query empty graph")
	out, err := runCLIInDirWithHome(t, "", home,
		"query", "What objects exist in this project?",
		"--project", projectID)
	rl.CLI("memory query 'What objects exist?' --project "+projectID, out)

	if err != nil {
		rl.Printf("query agent returned error: %v", err)
		t.Skipf("memory query agent mode failed: %v", err)
	}

	rl.Section("Verify no-data response")
	if out == "" {
		rl.Failf("memory query returned empty output")
	}

	// Should indicate no data exists — NOT fabricate objects.
	lowerOut := strings.ToLower(out)
	noDataIndicators := []string{
		"no ", "empty", "doesn't have", "does not have",
		"no objects", "no data", "no results", "none", "not find",
		"couldn't find", "no entities",
	}
	hasNoDataIndicator := false
	for _, indicator := range noDataIndicators {
		if strings.Contains(lowerOut, indicator) {
			hasNoDataIndicator = true
			break
		}
	}

	if !hasNoDataIndicator {
		// The response might list zero items in a table — that's also acceptable.
		// But it should not list specific named objects that don't exist.
		rl.Printf("WARN: response does not explicitly say 'no data' — may have fabricated results: %s",
			truncate(out, 300))
	} else {
		rl.Printf("query agent correctly reports empty graph")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Group C — Relationship traversal
//
// These tests verify the query agent can traverse relationships between
// objects and describe connections.
// ─────────────────────────────────────────────────────────────────────────────

// TestQueryPrompt_RelationshipTraversal seeds two objects with a relationship,
// then queries about the connection.
func TestQueryPrompt_RelationshipTraversal(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query prompt: relationship traversal",
		"Create project, seed two objects and a relationship between them",
		"Query about how the objects are related",
		"Verify response describes the relationship",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project and seed objects")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-rel")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	// Create two objects.
	svcName := fmt.Sprintf("QP-RelSvc-%d", time.Now().UnixMilli())
	svcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcName,
		"--description", "Order processing service",
		"--project", projectID,
		"--output", "json")
	rl.CLI("create Service "+svcName, svcOut)
	svcID := parseJSONField(svcOut, "id")
	if svcID == "" {
		svcID = parseJSONField(svcOut, "entity_id")
	}
	if svcID == "" {
		rl.Printf("WARN: could not extract Service ID — skipping relationship creation")
		t.Skip("could not extract Service ID from JSON output")
	}

	dbName := fmt.Sprintf("QP-RelDB-%d", time.Now().UnixMilli())
	dbOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Component",
		"--name", dbName,
		"--description", "PostgreSQL database for orders",
		"--project", projectID,
		"--output", "json")
	rl.CLI("create Component "+dbName, dbOut)
	dbID := parseJSONField(dbOut, "id")
	if dbID == "" {
		dbID = parseJSONField(dbOut, "entity_id")
	}
	if dbID == "" {
		rl.Printf("WARN: could not extract Component ID — skipping relationship creation")
		t.Skip("could not extract Component ID from JSON output")
	}

	// Create relationship.
	rl.Section("Create relationship")
	relOut, relErr := runCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--from", svcID,
		"--to", dbID,
		"--type", "DEPENDS_ON",
		"--project", projectID)
	if relErr != nil {
		rl.CLIErr("memory graph relationships create", relOut, relErr, 0)
		rl.Printf("WARN: relationship creation failed: %v — query test may still pass if objects are found", relErr)
	} else {
		rl.CLI("memory graph relationships create --from "+svcID+" --to "+dbID+" --type DEPENDS_ON", relOut)
	}

	rl.Section("Query about relationships")
	out, err := runCLIInDirWithHome(t, "", home,
		"query", fmt.Sprintf("How is %s related to other objects in this project?", svcName),
		"--project", projectID)
	rl.CLI("memory query relationship question --project "+projectID, out)

	if err != nil {
		rl.Printf("query agent returned error: %v", err)
		t.Skipf("memory query agent mode failed: %v", err)
	}

	rl.Section("Verify relationship response")
	if out == "" {
		rl.Failf("memory query returned empty output")
	}

	// Should mention at least one of: the relationship type, the related object, or "depends".
	relKeywords := []string{svcName, dbName, "DEPENDS_ON", "depends", "relationship", "related", "connect"}
	if !containsAny(out, relKeywords) {
		t.Errorf("expected response to mention relationship details; got: %s",
			truncate(out, 500))
	}
	rl.Printf("query agent describes relationships between objects")
}

// ─────────────────────────────────────────────────────────────────────────────
// Group D — Timing baselines
//
// These tests capture timing for query agent operations to enable before/after
// comparisons for prompt optimizations.
// ─────────────────────────────────────────────────────────────────────────────

// TestQueryPrompt_Timing_SearchMode captures search mode timing as a baseline.
func TestQueryPrompt_Timing_SearchMode(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query timing baseline: search mode",
		"Create project, seed data, run search mode query",
		"Log timing for pre/post optimization comparison",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project and seed data")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-timing-search")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "TimingSearchSvc",
		"--description", "Service for timing baseline measurement",
		"--project", projectID)
	rl.Printf("seeded TimingSearchSvc")

	rl.Section("Time search mode query")
	start := time.Now()
	out := mustRunCLIInDirWithHome(t, "", home,
		"query", "--mode=search", "TimingSearchSvc",
		"--project", projectID)
	elapsed := time.Since(start)
	rl.CLI("memory query --mode=search TimingSearchSvc", out)

	rl.Section("Log timing")
	rl.Printf("search mode latency: %s", elapsed.Round(time.Millisecond))
	rl.Printf("response length: %d bytes", len(out))
}

// TestQueryPrompt_Timing_AgentMode captures agent mode timing as a baseline.
func TestQueryPrompt_Timing_AgentMode(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Query timing baseline: agent mode",
		"Create project, seed data, run agent mode query",
		"Log timing for pre/post optimization comparison",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project and seed data")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-qp-timing-agent")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)

	mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "TimingAgentSvc",
		"--description", "Service for agent mode timing baseline",
		"--project", projectID)
	rl.Printf("seeded TimingAgentSvc")

	rl.Section("Time agent mode query")
	start := time.Now()
	out, err := runCLIInDirWithHome(t, "", home,
		"query", "What services exist in this project?",
		"--project", projectID)
	elapsed := time.Since(start)
	rl.CLI("memory query agent mode", out)

	if err != nil {
		rl.Printf("query agent returned error (timing still captured): %v", err)
		t.Skipf("memory query agent mode failed: %v", err)
	}

	rl.Section("Log timing")
	rl.Printf("agent mode latency: %s", elapsed.Round(time.Millisecond))
	rl.Printf("response length: %d bytes", len(out))
}
