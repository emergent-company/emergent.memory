package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- agent MCP routes + dead-code guards ---
//
// These assert the agent MCP surface is registered under the new paths, and
// that the removed picker-era per-agent *share* gateway code and its old
// deep-link into the project-shares page are gone for good.

// TestAgentMCPRoutesRegistered asserts main.go wires exactly the new agent MCP
// UI + JSON handlers.
func TestAgentMCPRoutesRegistered(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	main := string(src)

	for _, want := range []string{
		`s.uiAgentMCPEndpointCreate`,
		`s.uiAgentMCPEndpointRevoke`,
		`s.uiAgentMCPKeyCreate`,
		`s.uiAgentMCPKeyRevoke`,
		`s.uiAgentMCPKeyRotate`,
		`s.uiAgentMCPSessions`,
		`s.jsonAgentMCPEndpoint`,
		`s.jsonCreateAgentMCPEndpoint`,
		`s.jsonDeleteAgentMCPEndpoint`,
		`s.jsonListAgentMCPKeys`,
		`s.jsonCreateAgentMCPKey`,
		`s.jsonListAgentMCPSessions`,
		`s.jsonDeleteAgentMCPKey`,
		`s.jsonRotateAgentMCPKey`,
		`"/agent-mcp-endpoints/:id/keys"`,
		`"/agent-mcp-keys/:id/rotate"`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go missing registration %q", want)
		}
	}
}

// TestAgentShareDeadCodeGone asserts the removed per-agent share handlers and
// files no longer exist anywhere in the gateway package.
func TestAgentShareDeadCodeGone(t *testing.T) {
	for _, name := range []string{
		"agent_mcp_shares.go",
		"agent_mcp_shares_handlers.go",
		"agent_mcp_shares.templ",
		"agent_mcp_shares_client_test.go",
		"agent_mcp_shares_handlers_test.go",
	} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("dead file %s still present", name)
		}
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, dead := range []string{
			"uiAgentMCPShare",
			"createAgentMCPShare",
			"listAgentMCPShares",
			"listProjectAgentMCPShares",
			"deleteAgentMCPShare",
			"rotateAgentMCPShare",
			"AgentMCPShare",
		} {
			if strings.Contains(string(b), dead) {
				t.Errorf("%s still references dead per-agent share symbol %q", file, dead)
			}
		}
	}
}

// TestAgentShareDeepLinkGone asserts no agent page deep-links into the
// project-shares create form any more: agent sharing lives on the agent's own
// Settings surface, and the project-shares page points users to the agents list.
func TestAgentShareDeepLinkGone(t *testing.T) {
	for _, file := range []string{"agent.templ", "mcp_shares.templ"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "shares/new?agent=") {
			t.Errorf("%s still deep-links into the project-shares picker", file)
		}
	}

	// The project-shares page still tells users to share agents from the agent,
	// and now offers a way there.
	html := renderHTML(t, MCPSharesPage(mcpSharesPageData{}))
	if !strings.Contains(html, `href="/agents"`) {
		t.Error("project-shares page must link to the agents list (the agent configuration surface)")
	}
}
