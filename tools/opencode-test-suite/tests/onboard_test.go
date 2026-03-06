// Package tests contains end-to-end tests that drive opencode via its HTTP API
// against a live Emergent server to verify the emergent-onboard skill works correctly.
//
// Environment variables:
//
//	EMERGENT_TEST_SERVER   - Emergent API base URL (default: http://localhost:5300)
//
// Requirements:
//   - `opencode` binary in PATH
//   - `emergent` CLI binary in PATH (built with `task cli:install`)
//   - A running Emergent server at EMERGENT_TEST_SERVER
package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/assert"
	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/chatlog"
	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/fixture"
	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/harness"
	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/runner"
)

const (
	testModel   = "google-vertex/gemini-3-flash-preview"
	testTimeout = 5 * time.Minute
	maxTurns    = 8
)

// initialPrompt triggers the emergent-onboard skill via slash command syntax.
// We explicitly tell the LLM the project and provider are already configured
// so it skips the setup steps and goes straight to designing the template pack.
// RunUntilDone auto-replies to any confirmation questions.
const initialPrompt = `start emergent onboarding

Important context:
- This project is already configured — EMERGENT_PROJECT_ID is set in .env.local.
- Do NOT create a new project or list existing projects. Use the project from .env.local.
- Complete all remaining steps automatically without asking for confirmation: design the template pack, install it, upload the documents, create graph objects, then run a search query to verify.
- IMPORTANT: When creating graph objects, use the batch command: emergent graph objects create-batch --file <path>
- IMPORTANT: When creating graph relationships, use the batch command: emergent graph relationships create-batch --file <path>
- Do NOT use single-item create commands (emergent graph objects create or emergent graph relationships create) — always use the batch variants.`

// TestOnboardSkill is the primary e2e test. It:
//  1. Creates a fresh Emergent project via the CLI.
//  2. Creates a fake bookstore-API workspace (with opencode.json auto-allow permissions).
//  3. Writes .env.local pointing opencode at the project.
//  4. Installs emergent skills into the workspace (.agents/skills/).
//  5. Starts an opencode serve process (HTTP API mode).
//  6. Sends a single prompt that loads the skill and instructs the agent to
//     complete all steps without asking for confirmation.
//  7. Asserts that the skill was loaded, bash was called, and no tool errors occurred.
//  8. Cleans up the project (via t.Cleanup).
func TestOnboardSkill(t *testing.T) {
	harness.SkipIfServerDown(t)

	harness.SetupAuth(t, e2eTestToken)
	proj := harness.CreateProject(t, fmt.Sprintf("oc-test-onboard-%d", time.Now().Unix()))
	projToken := harness.CreateProjectToken(t, proj.ID)
	ws := fixture.NewWorkspace(t)
	ws.EnvFile(harness.ServerURL(), proj.ID, projToken)
	harness.InstallSkills(t, ws.Dir)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	srv, err := runner.StartServer(ctx, ws.Dir, 0)
	if err != nil {
		t.Fatalf("start opencode server: %v", err)
	}
	t.Cleanup(srv.Close)
	t.Logf("opencode server: %s", srv.URL)

	sessionID, err := srv.NewSession("onboard-test")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Logf("session: %s", sessionID)

	result, err := srv.RunUntilDone(ctx, sessionID, initialPrompt, testModel, maxTurns)
	if err != nil {
		t.Fatalf("conversation failed: %v", err)
	}
	chatlog.Write(t, result, t.Name())

	t.Logf("opencode cost:    $%.4f", result.Cost)
	t.Logf("opencode elapsed: %s", result.Elapsed)
	t.Logf("opencode text (last 500 chars):\n...%s", last500(result.Text))
	t.Logf("tool calls (%d): %v", len(result.ToolCalls), toolCallNames(result))

	// Core process assertions — did the LLM actually run the skill and use bash?
	assert.SkillLoaded(t, result, "emergent-onboard")
	assert.BashCalled(t, result)
	assert.NoToolErrors(t, result)

	// Emergent state assertions via CLI — did the skill create real state?
	assert.HasTemplatePack(t, ws.Dir)
	assert.HasGraphObjects(t, ws.Dir, 3) // at minimum the 3 services

	// Batch command assertion — agent must use create-batch, not single creates.
	assert.BashCommandUsed(t, result, "create-batch")
}

// e2eTestToken is the static Bearer token accepted by the local dev server.
// It maps to the AdminUser fixture (test-admin-user) and is the same token
// used by the server-go E2E test suite.
const e2eTestToken = "e2e-test-user"

// and logs all tool calls and text output. Use this to understand what opencode
// actually does before relying on the full assertions in TestOnboardSkill.
//
// Run with: go test -v -run TestOnboardSkillDebug -timeout 5m ./tests/
func TestOnboardSkillDebug(t *testing.T) {
	harness.SkipIfServerDown(t)

	harness.SetupAuth(t, e2eTestToken)
	proj := harness.CreateProject(t, fmt.Sprintf("oc-dbg-onboard-%d", time.Now().Unix()))
	projToken := harness.CreateProjectToken(t, proj.ID)
	ws := fixture.NewWorkspace(t)
	ws.EnvFile(harness.ServerURL(), proj.ID, projToken)
	harness.InstallSkills(t, ws.Dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	srv, err := runner.StartServer(ctx, ws.Dir, 0)
	if err != nil {
		t.Fatalf("start opencode server: %v", err)
	}
	t.Cleanup(srv.Close)
	t.Logf("opencode server: %s", srv.URL)

	sessionID, err := srv.NewSession("onboard-debug")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Logf("session: %s", sessionID)

	result, err := srv.RunUntilDone(ctx, sessionID, initialPrompt, testModel, maxTurns)
	if err != nil {
		t.Logf("conversation error: %v", err)
	}
	if result != nil {
		chatlog.Write(t, result, t.Name())
		t.Logf("total: $%.4f  %s  %d tool calls", result.Cost, result.Elapsed.Round(time.Second), len(result.ToolCalls))
	}
}

func last500(s string) string {
	if len(s) <= 500 {
		return s
	}
	return s[len(s)-500:]
}

// toolCallNames returns a slice of tool names from result for log messages.
func toolCallNames(result *runner.Result) []string {
	names := make([]string, len(result.ToolCalls))
	for i, tc := range result.ToolCalls {
		names[i] = tc.Tool
	}
	return names
}
