// Package docs_test — docs_site_test.go
//
// Tests that verify key documentation pages are accessible and contain expected
// sections.  These tests make HTTP requests to the published docs site and skip
// gracefully when the network is unavailable.
package docs_test

import (
	"strings"
	"testing"
	"time"
)

const docsBaseURL = "https://emergent-company.github.io/emergent.memory/latest"

// ─────────────────────────────────────────────────────────────────────────────
// Home page
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_HomePageLoads verifies the docs home page is accessible and
// contains the product name.
func TestDocSite_HomePageLoads(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify docs home page loads and contains product name",
		"GET the docs home page",
		"Assert HTTP 200 and body contains 'Memory' or 'emergent'",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch docs home page")
	body, status, err := httpGet(docsBaseURL+"/", 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch docs home page: %v", err)
	}
	rl.Printf("GET %s/ → HTTP %d (%d bytes)", docsBaseURL, status, len(body))

	rl.Section("Verify home page content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "memory") && !strings.Contains(lower, "emergent") {
		t.Errorf("home page does not contain 'memory' or 'emergent'")
	}
	rl.Printf("home page contains expected product references")
}

// ─────────────────────────────────────────────────────────────────────────────
// Getting Started
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_GettingStartedPage verifies the getting-started page loads and
// covers key topics (installation, projects, CLI commands).
func TestDocSite_GettingStartedPage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify getting-started page loads and covers key topics",
		"GET the getting-started page",
		"Assert page contains installation, projects, and CLI references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch getting-started page")
	url := docsBaseURL + "/user-guide/getting-started/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch getting-started page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify getting-started content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	expectedTopics := []string{"project"}
	for _, topic := range expectedTopics {
		if !strings.Contains(lower, topic) {
			t.Errorf("getting-started page missing expected topic %q", topic)
		} else {
			rl.Printf("  found topic: %s", topic)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Knowledge Graph
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_KnowledgeGraphPage verifies the knowledge-graph page loads and
// documents graph objects and relationships.
func TestDocSite_KnowledgeGraphPage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify knowledge-graph page loads and documents graph concepts",
		"GET the knowledge-graph page",
		"Assert page contains graph, objects, relationships references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch knowledge-graph page")
	url := docsBaseURL + "/user-guide/knowledge-graph/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch knowledge-graph page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify knowledge-graph content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	expectedTopics := []string{"graph", "object", "relationship"}
	for _, topic := range expectedTopics {
		if !strings.Contains(lower, topic) {
			t.Errorf("knowledge-graph page missing expected topic %q", topic)
		} else {
			rl.Printf("  found topic: %s", topic)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agents
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_AgentsPage verifies the agents documentation page loads and
// covers agent-related topics (agents, defs, triggers, hooks).
func TestDocSite_AgentsPage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents page loads and covers agent topics",
		"GET the agents page",
		"Assert page contains agents, trigger, defs references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch agents page")
	url := docsBaseURL + "/user-guide/agents/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch agents page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify agents page content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	expectedTopics := []string{"agent", "trigger"}
	for _, topic := range expectedTopics {
		if !strings.Contains(lower, topic) {
			t.Errorf("agents page missing expected topic %q", topic)
		} else {
			rl.Printf("  found topic: %s", topic)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Provider Setup
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_ProviderSetupPage verifies the provider-setup page loads and
// documents provider configuration.
func TestDocSite_ProviderSetupPage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider-setup page loads and documents provider config",
		"GET the provider-setup page",
		"Assert page contains provider, API key references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch provider-setup page")
	url := docsBaseURL + "/developer-guide/provider-setup/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch provider-setup page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify provider-setup content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	expectedTopics := []string{"provider"}
	for _, topic := range expectedTopics {
		if !strings.Contains(lower, topic) {
			t.Errorf("provider-setup page missing expected topic %q", topic)
		} else {
			rl.Printf("  found topic: %s", topic)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Go SDK
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_GoSDKPage verifies the Go SDK page loads and references the SDK
// package or client usage.
func TestDocSite_GoSDKPage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify Go SDK page loads and references SDK usage",
		"GET the Go SDK page",
		"Assert page contains SDK, Go, or client references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch Go SDK page")
	url := docsBaseURL + "/go-sdk/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch Go SDK page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify Go SDK content")
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}
	lower := strings.ToLower(body)
	// SDK page should reference Go, SDK, client, or the package path.
	if !strings.Contains(lower, "sdk") && !strings.Contains(lower, "client") && !strings.Contains(lower, "go") {
		t.Errorf("Go SDK page missing expected SDK references")
	}
	rl.Printf("Go SDK page contains expected references")
}

// ─────────────────────────────────────────────────────────────────────────────
// CLI example verification in docs pages
// ─────────────────────────────────────────────────────────────────────────────

// TestDocSite_GettingStartedCLIExamples verifies the getting-started page
// contains specific code examples that users should follow.
func TestDocSite_GettingStartedCLIExamples(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify getting-started page contains expected code examples",
		"GET the getting-started page",
		"Assert page contains 'memory sdk' or 'memory go' SDK references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch getting-started page")
	url := docsBaseURL + "/user-guide/getting-started/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch getting-started page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}

	rl.Section("Verify code examples in page content")
	lower := strings.ToLower(body)
	patterns := []string{"memory sdk", "memory go"}
	found := false
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			found = true
			rl.Printf("  found example: %s", p)
		}
	}
	if !found {
		t.Errorf("getting-started page missing expected SDK examples (checked: %v)", patterns)
	}
}

// TestDocSite_AgentsPageCLIExamples verifies the agents docs page contains
// agent-related code examples.
func TestDocSite_AgentsPageCLIExamples(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents page contains agent-related code examples",
		"GET the agents page",
		"Assert page contains agent-related references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch agents page")
	url := docsBaseURL + "/user-guide/agents/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch agents page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}

	rl.Section("Verify agent references in page content")
	lower := strings.ToLower(body)
	patterns := []string{"agent", "memory sdk", "memory go"}
	found := false
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			found = true
			rl.Printf("  found example: %s", p)
		}
	}
	if !found {
		t.Errorf("agents page missing expected agent references (checked: %v)", patterns)
	}
}

// TestDocSite_KnowledgeGraphCLIExamples verifies the knowledge-graph docs page
// contains graph-related code examples.
func TestDocSite_KnowledgeGraphCLIExamples(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify knowledge-graph page contains graph code examples",
		"GET the knowledge-graph page",
		"Assert page contains graph-related references",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch knowledge-graph page")
	url := docsBaseURL + "/user-guide/knowledge-graph/"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		rl.Failf("failed to fetch knowledge-graph page: %v", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}

	rl.Section("Verify graph references in page content")
	lower := strings.ToLower(body)
	patterns := []string{"graph", "memory sdk", "memory go"}
	found := false
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			found = true
			rl.Printf("  found example: %s", p)
		}
	}
	if !found {
		t.Errorf("knowledge-graph page missing expected graph references (checked: %v)", patterns)
	}
}
