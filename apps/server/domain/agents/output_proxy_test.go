package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

func TestRewriteRTKCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ls -la", "rtk ls -la"},
		{"find . -name main.go", "rtk find . -name main.go"},
		{"find . -name '*.go'", "find . -name '*.go'"},
		{"grep foo bar.go", "rtk grep foo bar.go"},
		{"rg TODO", "rtk rg TODO"},
		{"git status", "rtk git status"},
		{"git diff HEAD", "rtk git diff HEAD"},
		{"git log --oneline -5", "rtk git log --oneline -5"},
		// Not eligible: writes, arbitrary commands, shell metacharacters, quoting.
		{"git commit -m x", "git commit -m x"},
		{"go test ./...", "go test ./..."},
		{"cat file", "cat file"},
		{"ls | head", "ls | head"},
		{"grep foo | wc -l", "grep foo | wc -l"},
		{"ls *.go", "rtk ls *.go"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, rewriteRTKCommand(c.in), "rewrite %q", c.in)
	}
}

func TestCompressWithSQZ_Success(t *testing.T) {
	provider := newTestProvider()
	provider.execResult = &sandbox.ExecResult{ExitCode: 0, Stdout: "compressed"}
	out := strings.Repeat("x", sqzCompressThreshold+100)

	got, ok := compressWithSQZ(context.Background(), provider, "test-provider", out)
	require.True(t, ok)
	assert.Equal(t, "compressed", got)

	// The helper writes the input into the container before running sqz.
	assert.Equal(t, 1, provider.execCallCount)
	require.NotNil(t, provider.lastExecReq)
	assert.Contains(t, provider.lastExecReq.Command, "sqz compress --no-cache --no-abbrev")
	assert.Contains(t, provider.lastExecReq.Command, "rm -f")
}

func TestCompressWithSQZ_FallsBack(t *testing.T) {
	out := strings.Repeat("x", sqzCompressThreshold+100)

	provider := newTestProvider()
	provider.execResult = &sandbox.ExecResult{ExitCode: 0, Stdout: out}
	if got, ok := compressWithSQZ(context.Background(), provider, "p", out); ok {
		t.Fatalf("expected fallback when sqz output is not smaller, got %q", got)
	}

	provider = newTestProvider()
	provider.execResult = &sandbox.ExecResult{ExitCode: 1, Stdout: "err"}
	if _, ok := compressWithSQZ(context.Background(), provider, "p", out); ok {
		t.Fatal("expected fallback on non-zero exit")
	}

	// A dedup ref is never acceptable output, even when shorter.
	provider = newTestProvider()
	provider.execResult = &sandbox.ExecResult{ExitCode: 0, Stdout: "§ref:b2c52d0a417a734f§"}
	if _, ok := compressWithSQZ(context.Background(), provider, "p", out); ok {
		t.Fatal("expected fallback on sqz dedup ref")
	}

	// A ref embedded in otherwise valid output is also rejected.
	provider = newTestProvider()
	provider.execResult = &sandbox.ExecResult{ExitCode: 0, Stdout: "prefix §ref:abc§ suffix"}
	if _, ok := compressWithSQZ(context.Background(), provider, "p", out); ok {
		t.Fatal("expected fallback on embedded sqz ref")
	}
}
