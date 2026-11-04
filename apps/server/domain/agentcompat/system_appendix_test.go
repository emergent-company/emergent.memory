package agentcompat

import (
	"strings"
	"testing"
)

// TestBuildSystemAppendixNoProjectContext locks the #1694 change: the
// agentcompat surface no longer injects project_info. History and the tool
// naming block are preserved for callers.
func TestBuildSystemAppendixNoProjectContext(t *testing.T) {
	messages := []ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "how are you?"},
	}

	got := buildSystemAppendix(messages, true)
	if strings.Contains(got, "Knowledge base context") {
		t.Fatalf("buildSystemAppendix() must not inject project context: %q", got)
	}
	if !strings.Contains(got, "## Conversation history") {
		t.Fatalf("buildSystemAppendix() should keep conversation history: %q", got)
	}
	if !strings.Contains(got, "## Tool naming convention") {
		t.Fatalf("buildSystemAppendix() should keep the tool naming block: %q", got)
	}
}

func TestBuildSystemAppendixEmptyConversation(t *testing.T) {
	// A single user message has nothing to replay and no client tools.
	if got := buildSystemAppendix([]ChatMessage{{Role: "user", Content: "hi"}}, false); got != "" {
		t.Fatalf("buildSystemAppendix() = %q, want empty", got)
	}
}

// TestSystemPromptAppendixUnchanged guards requirement 5: the shared appendix
// helper keeps its behaviour for other callers (it never carried project_info;
// it only describes the memory_ prefix convention).
func TestSystemPromptAppendixUnchanged(t *testing.T) {
	if got := SystemPromptAppendix(false); got != "" {
		t.Fatalf("SystemPromptAppendix(false) = %q, want empty", got)
	}
	got := SystemPromptAppendix(true)
	if !strings.Contains(got, "## Tool naming convention") {
		t.Fatalf("SystemPromptAppendix(true) = %q, want tool naming convention", got)
	}
}
