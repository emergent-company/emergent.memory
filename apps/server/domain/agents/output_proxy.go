package agents

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

const (
	// sqzCompressThreshold is the stdout size above which sqz compression is
	// attempted. Smaller outputs are cheaper to pass through verbatim.
	sqzCompressThreshold = 8000
	sqzTempPath          = "/tmp/.agent_sqz_input"
)

// rtkRewritable are the simple read-only commands rtk can proxy.
var rtkRewritable = map[string]bool{
	"ls":   true,
	"find": true,
	"grep": true,
	"rg":   true,
}

// rewriteRTKCommand rewrites a simple read-only shell command to its rtk
// equivalent. It deliberately refuses anything with shell metacharacters,
// quoting, redirection or substitution so behaviour is never changed: only a
// single plain command is rewritten. Returns cmd unchanged when not eligible.
func rewriteRTKCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" || strings.ContainsAny(trimmed, "|&;<>`$()\\\"'\n") {
		return cmd
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return cmd
	}
	if rtkRewritable[fields[0]] {
		return "rtk " + trimmed
	}
	if fields[0] == "git" && len(fields) > 1 {
		switch fields[1] {
		case "status", "diff", "log":
			return "rtk " + trimmed
		}
	}
	return cmd
}

// compressWithSQZ compresses oversized stdout inside the workspace container
// using sqz. It returns the compressed text only when sqz is available, exits
// 0, produces non-empty output, and actually shrinks the payload; otherwise the
// caller keeps the original output.
func compressWithSQZ(ctx context.Context, provider sandbox.Provider, providerID, output string) (string, bool) {
	if provider == nil || providerID == "" {
		return "", false
	}
	if err := provider.WriteFile(ctx, providerID, &sandbox.FileWriteRequest{
		FilePath: sqzTempPath,
		Content:  output,
	}); err != nil {
		return "", false
	}
	res, err := provider.Exec(ctx, providerID, &sandbox.ExecRequest{
		Command:   "sqz compress < " + sqzTempPath,
		TimeoutMs: 30000,
	})
	if err != nil || res == nil || res.ExitCode != 0 {
		return "", false
	}
	compressed := strings.TrimSpace(res.Stdout)
	if compressed == "" || len(compressed) >= len(output) {
		return "", false
	}
	return compressed, true
}
