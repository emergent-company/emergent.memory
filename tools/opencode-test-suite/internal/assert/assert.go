// Package assert provides test assertion helpers for opencode runner results
// and Emergent project state verification via the emergent CLI.
package assert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/tools/opencode-test-suite/internal/runner"
)

// ─────────────────────────────────────────────────────────────────────────────
// Runner result assertions
// ─────────────────────────────────────────────────────────────────────────────

// TextContains asserts that result.Text contains the given substring (case-insensitive).
func TextContains(t *testing.T, result *runner.Result, substr string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(result.Text), strings.ToLower(substr)) {
		t.Errorf("expected response text to contain %q\ngot:\n%s", substr, result.Text)
	}
}

// TextNotContains asserts that result.Text does NOT contain the given substring (case-insensitive).
func TextNotContains(t *testing.T, result *runner.Result, substr string) {
	t.Helper()
	if strings.Contains(strings.ToLower(result.Text), strings.ToLower(substr)) {
		t.Errorf("expected response text NOT to contain %q\ngot:\n%s", substr, result.Text)
	}
}

// ToolCalled asserts that a tool with the given name was called at least once.
func ToolCalled(t *testing.T, result *runner.Result, toolName string) {
	t.Helper()
	for _, tc := range result.ToolCalls {
		if tc.Tool == toolName {
			return
		}
	}
	t.Errorf("expected tool %q to be called; tool calls were: %v", toolName, toolNames(result))
}

// ToolNotCalled asserts that a tool with the given name was NOT called.
func ToolNotCalled(t *testing.T, result *runner.Result, toolName string) {
	t.Helper()
	for _, tc := range result.ToolCalls {
		if tc.Tool == toolName {
			t.Errorf("expected tool %q NOT to be called", toolName)
			return
		}
	}
}

// SkillLoaded asserts that the given skill was loaded (a tool_use with tool=="skill"
// and input.name == skillName).
func SkillLoaded(t *testing.T, result *runner.Result, skillName string) {
	t.Helper()
	for _, tc := range result.ToolCalls {
		if tc.Tool == "skill" {
			if name, ok := tc.Input["name"].(string); ok && name == skillName {
				return
			}
		}
	}
	t.Errorf("expected skill %q to be loaded; tool calls were: %v", skillName, toolNames(result))
}

// NoToolErrors asserts that no tool calls finished with an error state.
func NoToolErrors(t *testing.T, result *runner.Result) {
	t.Helper()
	for i, tc := range result.ToolCalls {
		if tc.IsErr {
			t.Errorf("tool call [%d] %q returned an error\noutput: %s",
				i, tc.Tool, tc.Output)
		}
	}
}

// BashCalled asserts that at least one bash tool call was made.
func BashCalled(t *testing.T, result *runner.Result) {
	t.Helper()
	ToolCalled(t, result, "bash")
}

// BashCommandUsed asserts that at least one bash tool call had a command
// containing the given substring. This is used to verify the agent used a
// specific CLI command (e.g. "create-batch") rather than just verifying bash
// was called at all.
func BashCommandUsed(t *testing.T, result *runner.Result, substr string) {
	t.Helper()
	for _, tc := range result.ToolCalls {
		if tc.Tool != "bash" {
			continue
		}
		cmd, ok := tc.Input["command"].(string)
		if ok && strings.Contains(cmd, substr) {
			return
		}
	}
	t.Errorf("expected a bash tool call with command containing %q; bash commands were:\n%s",
		substr, bashCommands(result))
}

// bashCommands returns a newline-separated list of all bash command inputs
// from the result (truncated for readability).
func bashCommands(result *runner.Result) string {
	var b strings.Builder
	for i, tc := range result.ToolCalls {
		if tc.Tool != "bash" {
			continue
		}
		cmd, _ := tc.Input["command"].(string)
		line := strings.ReplaceAll(cmd, "\n", " ")
		if len(line) > 200 {
			line = line[:200] + "..."
		}
		fmt.Fprintf(&b, "  [%d] %s\n", i, line)
	}
	return b.String()
}

func toolNames(result *runner.Result) []string {
	names := make([]string, len(result.ToolCalls))
	for i, tc := range result.ToolCalls {
		names[i] = tc.Tool
	}
	return names
}

// ─────────────────────────────────────────────────────────────────────────────
// Emergent state assertions via the emergent CLI
//
// These shell out to `emergent` (with --output json) to verify that the
// onboarding skill created real state — not just that the LLM claimed it did.
// The CLI handles auth and project context from the workspace .env.local,
// so we run commands from workspaceDir.
// ─────────────────────────────────────────────────────────────────────────────

// HasTemplatePack asserts that at least one template pack is installed in the
// project. It runs `emergent template-packs installed --output json` from
// workspaceDir so the CLI picks up MEMORY_SERVER and MEMORY_PROJECT from
// .env.local automatically.
func HasTemplatePack(t *testing.T, workspaceDir string) {
	t.Helper()

	out, err := runCLI(workspaceDir, "template-packs", "installed", "--output", "json")
	if err != nil {
		t.Errorf("assert.HasTemplatePack: CLI error: %v\noutput: %s", err, out)
		return
	}

	var packs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &packs); err != nil {
		t.Errorf("assert.HasTemplatePack: could not parse CLI output as JSON: %v\noutput: %s", err, out)
		return
	}

	if len(packs) == 0 {
		t.Errorf("assert.HasTemplatePack: expected at least one installed template pack, got none")
		return
	}

	names := make([]string, len(packs))
	for i, p := range packs {
		names[i] = p.Name
	}
	t.Logf("assert.HasTemplatePack: %d pack(s) installed: %v", len(packs), names)
}

// HasGraphObjects asserts that at least minCount graph objects exist in the
// project. It runs `emergent graph objects list --output json` from workspaceDir.
func HasGraphObjects(t *testing.T, workspaceDir string, minCount int) {
	t.Helper()

	out, err := runCLI(workspaceDir, "graph", "objects", "list", "--output", "json")
	if err != nil {
		t.Errorf("assert.HasGraphObjects: CLI error: %v\noutput: %s", err, out)
		return
	}

	var objects []struct {
		EntityID string `json:"entity_id"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal([]byte(out), &objects); err != nil {
		t.Errorf("assert.HasGraphObjects: could not parse CLI output as JSON: %v\noutput: %s", err, out)
		return
	}

	if len(objects) < minCount {
		t.Errorf("assert.HasGraphObjects: expected at least %d graph object(s), got %d", minCount, len(objects))
		return
	}

	t.Logf("assert.HasGraphObjects: %d object(s) found", len(objects))
	for _, o := range objects {
		t.Logf("  [%s] %s", o.Type, o.EntityID)
	}
}

// runCLI runs `emergent <args>` from dir with a 30s timeout and returns
// combined stdout+stderr output. The CLI picks up MEMORY_SERVER and
// MEMORY_PROJECT from .env.local in dir automatically — no flags needed.
func runCLI(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "emergent", args...)
	cmd.Dir = dir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return out, fmt.Errorf("emergent %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
