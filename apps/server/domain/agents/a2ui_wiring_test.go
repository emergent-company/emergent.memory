package agents

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// renderA2UISet matches a RenderA2UI:true assignment, tolerating the gofmt
// column alignment of composite-literal fields (single or multiple spaces).
var renderA2UISet = regexp.MustCompile(`RenderA2UI:\s+true`)

// TestRenderA2UIWiring pins the surface wiring for issue #414: every transport
// that renders A2UI cards — or resumes a run that lives on such a surface — must
// set RenderA2UI:true on the ExecuteRequest it hands the executor, and the
// text-only surfaces (agentcompat, non-stream A2A) must not. Deleting any one of
// the positive assignments (or adding one to a negative surface) makes the
// corresponding function-body check fail, so a surface cannot silently lose the
// catalog instruction (and resume legs cannot drop it, nor a carrier-less
// transport gain it). It mirrors the fail-first wiring tests
// (transport_enforced_wiring_test.go, webhook_trust_test.go): it fails when the
// wiring regresses, not when behavior drifts.
func TestRenderA2UIWiring(t *testing.T) {
	// file → functions whose ExecuteRequest must carry RenderA2UI: true.
	sites := map[string][]string{
		filepath.Join("..", "chat", "handler.go"): {"streamAgentChat", "resumeForAnswer"},
		"a2a_stream.go":    {"streamNewTask", "streamResumeTask"},
		"share_service.go": {"buildShareExecuteRequest"},
		"executor.go":      {"maybeWakeParent"},
		"handler.go":       {"resumeQuestionRun"},
	}

	for file, funcs := range sites {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, fn := range funcs {
			body := a2uiRenderFuncBody(t, file, string(src), fn)
			if body == "" {
				t.Errorf("%s: function %s not found", file, fn)
				continue
			}
			if !renderA2UISet.MatchString(body) {
				t.Errorf("%s: %s must set RenderA2UI: true on its ExecuteRequest (deleting it drops the catalog instruction on an A2UI surface)", file, fn)
			}
		}
	}

	// Negative pins: surfaces with no A2UI carrier must never set the flag.
	// - agentcompat forwards only text deltas, so a card-only reply would strand
	//   an empty content delta.
	// - the non-stream A2A transport (startA2ATask/resumeA2ATask) has no
	//   a2uiDataPart carrier: RunToA2ATask/finalTextArtifact read Content["text"],
	//   which replyForStorage strips, so a card-only continuation would strand a
	//   completed task with no result artifact. Only the streaming A2A translator
	//   carries cards (a2a_stream.go), which is why those are positive pins above.
	compat, err := os.ReadFile(filepath.Join("..", "agentcompat", "service.go"))
	if err != nil {
		t.Fatalf("read agentcompat/service.go: %v", err)
	}
	if renderA2UISet.MatchString(string(compat)) {
		t.Errorf("agentcompat/service.go must NOT set RenderA2UI: true (it is a text-only surface)")
	}

	a2aMsgSrc, err := os.ReadFile("a2a_message.go")
	if err != nil {
		t.Fatalf("read a2a_message.go: %v", err)
	}
	for _, fn := range []string{"startA2ATask", "resumeA2ATask"} {
		body := a2uiRenderFuncBody(t, "a2a_message.go", string(a2aMsgSrc), fn)
		if body == "" {
			t.Errorf("a2a_message.go: function %s not found", fn)
			continue
		}
		if renderA2UISet.MatchString(body) {
			t.Errorf("a2a_message.go: %s must NOT set RenderA2UI: true (non-stream A2A has no A2UI carrier)", fn)
		}
	}
}

// a2uiRenderFuncBody returns the gofmt'd source of the named function's body, or
// "" if the function is not present in src.
func a2uiRenderFuncBody(t *testing.T, file, src, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Body == nil {
			continue
		}
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, fd.Body); err != nil {
			t.Fatalf("format %s body: %v", file, err)
		}
		return buf.String()
	}
	return ""
}
