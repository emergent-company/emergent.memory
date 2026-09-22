// Package production_test — helpers_test.go
//
// Thin wrappers around framework exported functions for production tests.
package production_test

import (
	"fmt"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases
// ─────────────────────────────────────────────────────────────────────────────

type runLog = framework.RunLog

// ─────────────────────────────────────────────────────────────────────────────
// Run log helpers
// ─────────────────────────────────────────────────────────────────────────────

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	rl := framework.NewRunLog(t)
	category := deriveCategory(t.Name())
	if category != "" {
		rl.SetCategory(category)
	}
	return rl
}

// deriveCategory returns a category name based on the test function name.
// Only 4 tests exist in this package (SetToken, AuthAndList, ServerHealth,
// IssuerEndpoint) — all share a single "production" category rather than
// sub-bucketing further.
func deriveCategory(testName string) string {
	switch {
	case matchCategory(testName, "TestProduction_"):
		return "production"
	}
	return ""
}

func matchCategory(testName string, patterns ...string) bool {
	for _, p := range patterns {
		if len(testName) >= len(p) && testName[:len(p)] == p {
			return true
		}
	}
	return false
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

func logSession(t *testing.T, invocation, output string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if _, ok := v.(*framework.RunLog); ok {
			return
		}
	}
	framework.LogSession(t, invocation, output)
}

func mustRunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) string {
	t.Helper()
	out := framework.MustRunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	// Test calls rl.CLI/rl.CLIErr explicitly — helper does NOT record to avoid double events.
	// emitCLI is intentionally removed; recording happens in test body.
	_ = invocation
	return out
}
