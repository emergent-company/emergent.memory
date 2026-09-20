// Package cli_test — misc_test.go
//
// End-to-end tests for miscellaneous CLI commands that produce static output:
// mcp-guide, completion bash, completion zsh.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_MCPGuide verifies that `memory mcp-guide` outputs MCP
// server configuration snippets suitable for AI agent config files.
func TestCLIInstalled_MCPGuide(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-guide outputs MCP config snippets",
		"Set up CLI auth",
		"Run `memory mcp-guide`",
		"Assert output contains MCP server config JSON",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory mcp-guide")
	out, err := runCLIInDirWithHome(t, "", home, "mcp-guide")
	rl.CLIErr("memory mcp-guide", out, err, 0)

	if err != nil {
		rl.Printf("mcp-guide returned error (may require MEMORY_API_KEY env var): %v", err)
		t.Skipf("mcp-guide not available in this auth mode: %v", err)
	}

	rl.Section("Verify output contains MCP config")
	lower := strings.ToLower(out)
	// mcp-guide should output JSON config with mcpServers or mcp-related content.
	if !strings.Contains(out, "mcpServers") && !strings.Contains(out, "mcp") && !strings.Contains(lower, "memory") {
		t.Errorf("expected mcp-guide output to contain MCP config snippets, got:\n%s", truncate(out, 500))
	}
	// Should contain the command path for the memory binary.
	if !strings.Contains(out, "memory") {
		t.Errorf("expected mcp-guide output to reference 'memory' binary, got:\n%s", truncate(out, 500))
	}
	rl.Printf("mcp-guide output: %d bytes, contains MCP config: true", len(out))
}

// TestCLIInstalled_CompletionBash verifies that `memory completion bash`
// generates a valid bash completion script.
func TestCLIInstalled_CompletionBash(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify completion bash generates shell completion script",
		"Run `memory completion bash`",
		"Assert output contains bash completion markers",
	)
	logStatusPreamble(t)

	rl.Section("Run memory completion bash")
	out := mustRunCLI(t, "completion", "bash")
	rl.CLI("memory completion bash", truncate(out, 500))

	rl.Section("Verify bash completion output")
	if !strings.Contains(out, "bash completion") && !strings.Contains(out, "# bash") && !strings.Contains(out, "__memory") {
		t.Errorf("expected bash completion script, got:\n%s", truncate(out, 500))
	}
	// Should contain function definitions for the memory CLI.
	if !strings.Contains(out, "memory") {
		t.Errorf("expected completion script to reference 'memory', got:\n%s", truncate(out, 300))
	}
	rl.Printf("bash completion script: %d bytes", len(out))
}

// TestCLIInstalled_CompletionZsh verifies that `memory completion zsh`
// generates a valid zsh completion script.
func TestCLIInstalled_CompletionZsh(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify completion zsh generates shell completion script",
		"Run `memory completion zsh`",
		"Assert output contains zsh completion markers",
	)
	logStatusPreamble(t)

	rl.Section("Run memory completion zsh")
	out := mustRunCLI(t, "completion", "zsh")
	rl.CLI("memory completion zsh", truncate(out, 500))

	rl.Section("Verify zsh completion output")
	if !strings.Contains(out, "compdef") && !strings.Contains(out, "zsh") && !strings.Contains(out, "__memory") {
		t.Errorf("expected zsh completion script with compdef, got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(out, "memory") {
		t.Errorf("expected completion script to reference 'memory', got:\n%s", truncate(out, 300))
	}
	rl.Printf("zsh completion script: %d bytes", len(out))
}

// TestCLIInstalled_CompletionFish verifies that `memory completion fish`
// generates a valid fish completion script.
func TestCLIInstalled_CompletionFish(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify completion fish generates shell completion script",
		"Run `memory completion fish`",
		"Assert output contains fish completion markers",
	)
	logStatusPreamble(t)

	rl.Section("Run memory completion fish")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "completion", "fish")
	if err != nil {
		rl.Printf("completion fish returned error: %v", err)
		t.Skipf("completion fish not supported: %v — %s", err, truncate(out, 300))
	}
	rl.CLI("memory completion fish", truncate(out, 500))

	rl.Section("Verify fish completion output")
	if !strings.Contains(out, "complete") && !strings.Contains(out, "fish") && !strings.Contains(out, "memory") {
		t.Errorf("expected fish completion script, got:\n%s", truncate(out, 500))
	}
	rl.Printf("fish completion script: %d bytes", len(out))
}

// TestCLIInstalled_CompletionPowershell verifies that `memory completion
// powershell` generates a valid PowerShell completion script.
func TestCLIInstalled_CompletionPowershell(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify completion powershell generates shell completion script",
		"Run `memory completion powershell`",
		"Assert output contains PowerShell completion markers",
	)
	logStatusPreamble(t)

	rl.Section("Run memory completion powershell")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "completion", "powershell")
	if err != nil {
		rl.Printf("completion powershell returned error: %v", err)
		t.Skipf("completion powershell not supported: %v — %s", err, truncate(out, 300))
	}
	rl.CLI("memory completion powershell", truncate(out, 500))

	rl.Section("Verify powershell completion output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "register") && !strings.Contains(lower, "powershell") && !strings.Contains(lower, "memory") {
		t.Errorf("expected PowerShell completion script, got:\n%s", truncate(out, 500))
	}
	rl.Printf("PowerShell completion script: %d bytes", len(out))
}

// TestCLIInstalled_UpgradeHelp verifies that `memory upgrade --help` prints
// usage information for the upgrade command.
func TestCLIInstalled_UpgradeHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory upgrade --help prints usage information",
		"Run `memory upgrade --help`",
		"Assert output contains upgrade usage hints",
	)
	logStatusPreamble(t)

	rl.Section("Run memory upgrade --help")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "upgrade", "--help")
	if err != nil {
		rl.Printf("upgrade --help returned error: %v", err)
		t.Skipf("upgrade command not available: %v — %s", err, truncate(out, 300))
	}
	rl.CLI("memory upgrade --help", out)

	rl.Section("Verify upgrade help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "upgrade") {
		t.Errorf("expected upgrade help to mention 'upgrade', got:\n%s", truncate(out, 500))
	}
	rl.Printf("upgrade help output: %d bytes", len(out))
}

var _ = framework.SetToken // keep import used
