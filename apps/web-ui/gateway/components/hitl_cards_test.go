package components

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// --- QuestionCard ---

func questionCardOptionsFixture() []QuestionCardOption {
	return []QuestionCardOption{
		{Label: "Staging", Value: "staging", Description: "The staging cluster"},
		{Label: "Production", Value: "production"},
	}
}

func TestQuestionCardRendersOptionRowsWithIndicatorRole(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:  "Which environment?",
		Options: questionCardOptionsFixture(),
		Attrs:   templ.Attributes{"data-testid": "pending-question"},
	}))

	assertContains(t, html,
		// the card root carries the caller's testid (spread last)
		`data-testid="pending-question"`,
		"memory-question__options",
		// each option is a selectable row, not a bare button
		`role="radio"`,
		`aria-checked="false"`,
		`data-testid="chat-question-option"`,
		`data-option-value="staging"`,
		// the label and its optional description both render
		"Staging",
		"The staging cluster",
		"Production",
		"Which environment?",
	)
	// The description is omitted for an option that has none.
	if got, want := strings.Count(html, "memory-question__description"), 1; got != want {
		t.Errorf("description rows = %d, want %d:\n%s", got, want, html)
	}
}

func TestQuestionCardMultiSelectUsesCheckboxRows(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:       "Which environments?",
		Multi:        true,
		Options:      questionCardOptionsFixture(),
		FormID:       "q-respond",
		ResponseName: "response",
	}))

	assertContains(t, html,
		`role="checkbox"`,
		`type="checkbox"`,
		`form="q-respond"`,
		`name="response"`,
		`value="staging"`,
		// the multi-select instruction is the shared hint
		"Select all answers that apply",
	)
	// A native checkbox must not carry a static aria-checked.
	assertNotContains(t, html, `aria-checked`)
}

func TestQuestionCardAnsweredDisablesControls(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:   "Which environment?",
		Options:  questionCardOptionsFixture(),
		Mode:     QuestionCardAnswered,
		Selected: []string{"staging"},
	}))

	assertContains(t, html, `aria-checked="true"`, "disabled", "Answered")
	// The chosen row's indicator is filled (the selected accent + its dot).
	assertContains(t, html, "border-primary bg-primary", "bg-primary-content")
	// The answered card offers no live answer controls.
	assertNotContains(t, html, `data-testid="chat-question-submit"`, "Esc to dismiss")
}

func TestQuestionCardAnsweredMultiSelectShowsCheckedRows(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:   "Which regions?",
		Multi:    true,
		Options:  questionCardOptionsFixture(),
		Mode:     QuestionCardAnswered,
		Selected: []string{"staging"},
	}))

	assertContains(t, html, "lucide--check", "checked", "disabled", "Answered")
}

func TestQuestionCardTextInput(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:       "Which name?",
		Text:         &QuestionCardText{Placeholder: "Project name", MaxLength: 120, AriaLabel: "Which name?"},
		FormID:       "q-respond",
		ResponseName: "response",
	}))

	assertContains(t, html,
		`type="text"`,
		`name="response"`,
		`form="q-respond"`,
		`aria-label="Which name?"`,
		`placeholder="Project name"`,
		`maxlength="120"`,
		// required so a no-JS page cannot post an empty answer
		`required`,
	)
	// A text question renders no option rows.
	assertNotContains(t, html, `chat-question-option`)
}

func TestQuestionCardControlsSlotReplacesDefaultFooter(t *testing.T) {
	html := renderHTML(t, QuestionCard(QuestionCardProps{
		Prompt:   "Which environment?",
		Options:  questionCardOptionsFixture(),
		Controls: templ.Raw(`<div data-testid="custom-controls"></div>`),
	}))

	assertContains(t, html, `data-testid="custom-controls"`)
	assertNotContains(t, html, `data-testid="chat-question-submit"`)
}

// --- ApprovalCard ---

func TestApprovalCardRendersToolArgsAndActions(t *testing.T) {
	html := renderHTML(t, ApprovalCard(ApprovalCardProps{
		ToolName: "web_search",
		ArgsJSON: `{"q":"x"}`,
		Attrs:    templ.Attributes{"data-testid": "dock-approval"},
	}))

	assertContains(t, html,
		`data-testid="dock-approval"`,
		"The agent wants to run",
		"web_search",
		`data-testid="chat-approval-cancel"`,
		`data-testid="chat-approval-reject"`,
		`data-testid="chat-approval-approve"`,
		"memory-approval__args",
		"memory-approval__reject",
	)
}

func TestApprovalCardDecisionBadge(t *testing.T) {
	html := renderHTML(t, ApprovalCard(ApprovalCardProps{
		ToolName: "set_field",
		Decision: "rejected",
		Message:  "too long",
		Mode:     ApprovalCardDecided,
	}))

	assertContains(t, html, "rejected", "too long")
	// A decided card with no Actions slot offers no decision controls.
	assertNotContains(t, html, `data-testid="chat-approval-approve"`, `data-testid="chat-approval-reject"`)
}

func TestApprovalCardActionsSlotReplacesDefault(t *testing.T) {
	html := renderHTML(t, ApprovalCard(ApprovalCardProps{
		ToolName: "web_search",
		Actions:  templ.Raw(`<div data-testid="custom-actions"></div>`),
	}))

	assertContains(t, html, `data-testid="custom-actions"`)
	assertNotContains(t, html, `data-testid="chat-approval-approve"`)
}
