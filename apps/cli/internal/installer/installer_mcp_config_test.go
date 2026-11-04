package installer

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything it wrote. The pipe is drained concurrently so a message larger
// than the pipe buffer cannot deadlock the test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w

	var (
		wg  sync.WaitGroup
		out []byte
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, _ = io.ReadAll(r)
	}()

	fn()

	if err := w.Close(); err != nil {
		os.Stdout = orig
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stdout = orig
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}
	return string(out)
}

// The completion message used to print a stdio MCP config (`command` + `args:
// ["mcp"]`) for a subcommand that does not exist, so every user who pasted it
// got `unknown command "mcp"` (#2204). Memory is a remote Streamable-HTTP
// server; the snippet must carry the URL and headers instead.
func TestPrintCompletionMessagePrintsRemoteMCPConfig(t *testing.T) {
	inst := New(Config{InstallDir: t.TempDir(), ServerPort: 3002})

	got := captureStdout(t, func() {
		inst.printCompletionMessage("emt_test_key", true, true)
	})

	wantAll := []string{
		`"type": "http"`,
		`"url": "http://localhost:3002/api/mcp"`,
		`"X-Project-ID"`,
		`"X-API-Key"`,
		"memory mcp-guide",
	}
	for _, want := range wantAll {
		if !strings.Contains(got, want) {
			t.Errorf("completion message is missing %q\n--- output ---\n%s", want, got)
		}
	}

	// The stdio shape must be gone for good: a `command` key sends clients down
	// a path the CLI cannot serve.
	for _, unwanted := range []string{`"args": ["mcp"]`, `"command"`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("completion message still prints the stdio MCP form (%q)\n--- output ---\n%s", unwanted, got)
		}
	}
}
