package main

import (
	"strings"
	"testing"
)

// questionPromptHTML returns the inner HTML of the rendered question prompt.
// The rest of the page carries its own <script> tags and the question's plain
// text (for example the input's aria-label), so assertions about the prompt
// must be scoped to the element that renders it.
func questionPromptHTML(t *testing.T, body string) string {
	t.Helper()
	const marker = `class="memory-question__body`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("question prompt element not found:\n%s", body)
	}
	start += strings.Index(body[start:], ">") + 1
	end := strings.Index(body[start:], "</div>")
	if end < 0 {
		t.Fatalf("question prompt element not closed:\n%s", body)
	}
	return body[start : start+end]
}

// TestApprovalsPageRendersQuestionMarkdown pins #2448: the pending-question
// prompt renders through the same renderMarkdown path as the chat transcript,
// so GFM (bold, lists, code, links) shows as formatting instead of literal
// asterisks on /settings/approvals.
func TestApprovalsPageRendersQuestionMarkdown(t *testing.T) {
	body := approvalsPageBody(t, &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-md",
		Question:        "Which **environment** should I use?\n\n- staging\n- production",
		Status:          "pending",
		InteractionType: "text",
	}}})

	prompt := questionPromptHTML(t, body)
	for _, want := range []string{
		"<strong>environment</strong>",
		"<ul>",
		"<li>staging</li>",
		"<li>production</li>",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("question markdown not rendered, missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "**environment**") {
		t.Errorf("question prompt rendered literal markdown asterisks:\n%s", prompt)
	}
}

// TestApprovalsPageQuestionMarkdownSanitized guards the trust boundary: the
// prompt is user-generated content rendered as HTML, so the bluemonday UGC
// sanitizer must strip script tags, event handlers, and javascript: links while
// legitimate markdown still renders.
func TestApprovalsPageQuestionMarkdownSanitized(t *testing.T) {
	body := approvalsPageBody(t, &fakeMemory{questions: []AgentQuestionItem{{
		ID:              "q-xss",
		Question:        `Hello **world** <script>alert(1)</script> <img src=x onerror=alert(2)> [click](javascript:alert(3))`,
		Status:          "pending",
		InteractionType: "text",
	}}})

	prompt := questionPromptHTML(t, body)
	for _, raw := range []string{"<script", "<img", "</script>", "javascript:", "onerror"} {
		if strings.Contains(prompt, raw) {
			t.Errorf("sanitizer left %q in the question prompt:\n%s", raw, prompt)
		}
	}
	if !strings.Contains(prompt, "<strong>world</strong>") {
		t.Errorf("sanitizer stripped legitimate markdown:\n%s", prompt)
	}
	// The payload text itself survives as inert text, with no element or
	// attribute that could execute.
	if !strings.Contains(prompt, "alert(1)") {
		t.Errorf("sanitized prompt lost its text content:\n%s", prompt)
	}
}
