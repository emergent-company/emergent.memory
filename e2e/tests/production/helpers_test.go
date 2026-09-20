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
	return framework.NewRunLog(t)
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
	emitCLI(t, invocation, out)
	return out
}
