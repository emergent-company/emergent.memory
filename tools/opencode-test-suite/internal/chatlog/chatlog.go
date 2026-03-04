// Package chatlog writes human-readable chat log files for opencode test runs.
//
// Each test run produces a single plaintext file under the logs/ directory at
// the root of the test suite. Files are named:
//
//	logs/<testName>_<timestamp>.txt
//
// The log includes metadata (test name, date, model, cost, elapsed, pass/fail),
// then each conversation turn with its prompt, tool calls (inputs + outputs),
// and the model's response.
//
// Usage:
//
//	result, err := srv.RunUntilDone(...)
//	chatlog.Write(t, result, t.Name())
//
// Write registers a t.Cleanup handler so the log is written after all
// assertions complete — this ensures STATUS reflects the real pass/fail
// outcome.
package chatlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent/tools/opencode-test-suite/internal/runner"
)

// Write schedules a log file write for after all test assertions complete.
// This ensures STATUS reflects t.Failed() accurately.
// Errors writing the file are non-fatal — they are logged via t.Logf only.
func Write(t *testing.T, result *runner.Result, testName string) {
	t.Helper()
	// Capture result reference immediately (result is already populated before
	// assertions run since RunUntilDone has returned).
	t.Cleanup(func() {
		writeFile(t, result, testName, !t.Failed())
	})
}

// writeFile formats and writes the log file.
func writeFile(t *testing.T, result *runner.Result, testName string, passed bool) {
	t.Helper()

	logsDir := logsDirectory()
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Logf("chatlog: failed to create logs dir %s: %v", logsDir, err)
		return
	}

	ts := time.Now().UTC().Format("2006-01-02T15-04-05")
	// sanitize testName for use in a filename
	safeName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, testName)
	filename := fmt.Sprintf("%s_%s.txt", safeName, ts)
	path := filepath.Join(logsDir, filename)

	content := format(result, testName, passed)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Logf("chatlog: failed to write log to %s: %v", path, err)
		return
	}
	t.Logf("chatlog: wrote log to %s", path)
}

// logsDirectory returns the absolute path to the logs/ directory next to the
// test suite root. It locates the root by walking up from this source file.
func logsDirectory() string {
	// Use the source file's location to find the test suite root.
	_, srcFile, _, ok := runtime.Caller(0)
	if !ok {
		return "logs"
	}
	// srcFile = .../opencode-test-suite/internal/chatlog/chatlog.go
	// root    = .../opencode-test-suite/
	root := filepath.Dir(filepath.Dir(filepath.Dir(srcFile)))
	return filepath.Join(root, "logs")
}

// ─────────────────────────────────────────────────────────────────────────────
// Formatting
// ─────────────────────────────────────────────────────────────────────────────

const (
	outerLine = "================================================================================"
	turnLine  = "════════════════════════════════════════════════════════════════════════════════"
	innerLine = "────────────────────────────────────────────────────────────────────────────────"
)

func format(result *runner.Result, testName string, passed bool) string {
	var b strings.Builder

	// ── Header ──────────────────────────────────────────────────────────────
	status := "PASS"
	if !passed {
		status = "FAIL"
	}
	startStr := "—"
	if !result.StartedAt.IsZero() {
		startStr = result.StartedAt.UTC().Format("2006-01-02 15:04:05 UTC")
	}

	b.WriteString(outerLine + "\n")
	b.WriteString(fmt.Sprintf("TEST:    %s\n", testName))
	b.WriteString(fmt.Sprintf("DATE:    %s\n", startStr))
	b.WriteString(fmt.Sprintf("MODEL:   %s\n", orDash(result.Model)))
	b.WriteString(fmt.Sprintf("SESSION: %s\n", orDash(result.SessionID)))
	b.WriteString(fmt.Sprintf("STATUS:  %s\n", status))
	b.WriteString(fmt.Sprintf("COST:    $%.4f\n", result.Cost))
	b.WriteString(fmt.Sprintf("ELAPSED: %s\n", result.Elapsed.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("TURNS:   %d\n", len(result.Turns)))
	b.WriteString(outerLine + "\n")

	// ── Turns ────────────────────────────────────────────────────────────────
	if len(result.Turns) == 0 {
		// Fallback: no per-turn data, dump the flat result.
		b.WriteString("\n(no per-turn data — showing flat result)\n\n")
		b.WriteString("A:\n")
		b.WriteString(indent(result.Text, "  "))
		b.WriteString("\n")
		b.WriteString(outerLine + "\n")
		return b.String()
	}

	for _, turn := range result.Turns {
		b.WriteString("\n")
		b.WriteString(turnLine + "\n")
		timeStr := ""
		if !turn.StartedAt.IsZero() {
			timeStr = turn.StartedAt.UTC().Format("15:04:05")
		}
		b.WriteString(fmt.Sprintf("TURN %d  [%s]  elapsed: %s  cost: $%.4f\n",
			turn.Number, timeStr, turn.Elapsed.Round(time.Millisecond), turn.Cost))
		b.WriteString(turnLine + "\n")

		// User prompt
		b.WriteString("\nUSER:\n")
		b.WriteString(indent(turn.Prompt, "  "))
		b.WriteString("\n")

		// Tool calls
		if len(turn.ToolCalls) > 0 {
			b.WriteString("\nTOOLS:\n")
			for i, tc := range turn.ToolCalls {
				formatToolCall(&b, i+1, tc)
			}
		}

		// Model response
		b.WriteString("\nA:\n")
		b.WriteString(indent(turn.Text, "  "))
		b.WriteString("\n")

		// Turn footer
		b.WriteString("\n" + innerLine + "\n")
		b.WriteString(fmt.Sprintf("TOTAL: %d tool calls  |  $%.4f  |  %s\n",
			len(turn.ToolCalls), turn.Cost, turn.Elapsed.Round(time.Millisecond)))
	}

	b.WriteString(outerLine + "\n")
	return b.String()
}

func formatToolCall(b *strings.Builder, n int, tc runner.ToolCall) {
	errLabel := ""
	if tc.IsErr {
		errLabel = "  ERROR"
	}
	b.WriteString(fmt.Sprintf("  [%d] %s%s\n", n, tc.Tool, errLabel))

	// Input: show the most useful field(s) per tool type.
	if len(tc.Input) > 0 {
		inputStr := formatInput(tc.Tool, tc.Input)
		if inputStr != "" {
			b.WriteString(fmt.Sprintf("      INPUT:  %s\n", inputStr))
		}
	}

	// Output: always show full output (this is for analysis).
	if tc.Output != "" {
		b.WriteString("      OUTPUT:\n")
		b.WriteString(indentBlock(tc.Output, "        "))
		b.WriteString("\n")
	}
}

// formatInput returns a human-readable summary of a tool's input map.
func formatInput(tool string, inp map[string]interface{}) string {
	if inp == nil {
		return ""
	}
	switch tool {
	case "bash":
		cmd, _ := inp["command"].(string)
		return cmd
	case "read":
		path, _ := inp["filePath"].(string)
		return path
	case "write":
		path, _ := inp["filePath"].(string)
		content, _ := inp["content"].(string)
		if content != "" {
			return fmt.Sprintf("%s\n        CONTENT:\n%s", path, indentBlock(content, "          "))
		}
		return path
	case "edit":
		path, _ := inp["filePath"].(string)
		return path
	case "glob":
		pattern, _ := inp["pattern"].(string)
		dir, _ := inp["path"].(string)
		if dir != "" {
			return fmt.Sprintf("%s  (in %s)", pattern, dir)
		}
		return pattern
	case "grep":
		pattern, _ := inp["pattern"].(string)
		path, _ := inp["path"].(string)
		if path != "" {
			return fmt.Sprintf("%s  (in %s)", pattern, path)
		}
		return pattern
	case "skill":
		name, _ := inp["name"].(string)
		return fmt.Sprintf("name = %q", name)
	default:
		// Render all key=value pairs.
		parts := make([]string, 0, len(inp))
		for k, v := range inp {
			parts = append(parts, fmt.Sprintf("%s = %v", k, v))
		}
		return strings.Join(parts, "  |  ")
	}
}

// indent prefixes every non-empty line of s with prefix.
func indent(s, prefix string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return prefix + "(empty)\n"
	}
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(prefix)
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// indentBlock is like indent but does not add a trailing newline.
func indentBlock(s, prefix string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(prefix)
		b.WriteString(l)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
