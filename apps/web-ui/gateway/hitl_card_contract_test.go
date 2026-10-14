package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
)

// This file is the contract between the canonical gateway cards
// (components.QuestionCard / components.ApprovalCard) and the chat's
// client-side renderer (chat-stream.js renderQuestion / renderApproval). The
// chat builds its cards in JavaScript because the transcript is live, so it
// cannot render the Go components directly; the two must instead agree on the
// card's contract, and this test fails when either side drifts.
//
// The contract is the `data-testid` set and the `memory-<block>__<element>` hook
// classes each renderer emits — the identifiers the CSS and the test suites key
// on. It deliberately does not compare class order or colour utilities, which
// are not part of the contract (web-ui-component-conventions: "Component tests
// assert the contract, not class order").

const chatStreamJS = "webui/static/js/chat-stream.js"

// chatComponentsJS is the A2UI card renderer source (chat-components.js), which
// carries the second question/approval renderer (a2uiQuestion / a2uiApproval).
const chatComponentsJS = "webui/static/js/chat-components.js"

var (
	// A testid set through setAttribute("data-testid", "x").
	jsSetAttrTestIDRE = regexp.MustCompile(`setAttribute\("data-testid",\s*"([^"]+)"\)`)
	// A testid written inline in an HTML string: data-testid="x".
	inlineTestIDRE = regexp.MustCompile(`data-testid="([^"]+)"`)
	// A gateway card element hook: memory-question__body, memory-approval__args.
	hookRE = regexp.MustCompile(`memory-(?:question|approval)__[a-z0-9-]+`)
	// The bare card root marker: memory-question / memory-approval, but not the
	// __element hooks (a word boundary does not fall before "_").
	rootHookRE = regexp.MustCompile(`memory-(?:question|approval)\b`)
)

// chatStreamSource reads the chat renderer's source.
func chatStreamSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(chatStreamJS)
	if err != nil {
		t.Fatalf("read %s: %v", chatStreamJS, err)
	}
	return string(src)
}

// chatComponentsSource reads the A2UI card renderer's source.
func chatComponentsSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(chatComponentsJS)
	if err != nil {
		t.Fatalf("read %s: %v", chatComponentsJS, err)
	}
	return string(src)
}

// jsFunctionBody returns the source of a top-level function in chat-stream.js,
// from its declaration to the next 4-space-indented declaration. The renderers
// are top-level functions; their helpers are nested at 6 spaces, so the slice
// keeps every helper a renderer owns.
func jsFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "\n    function "+name+"(")
	if start < 0 {
		t.Fatalf("chat-stream.js: function %s not found", name)
	}
	rest := src[start+1:]
	next := regexp.MustCompile(`\n    (?:async )?function `).FindStringIndex(rest[1:])
	if next == nil {
		return rest
	}
	return rest[:next[0]+1]
}

// chatComponentsFunctionBody returns the source of a top-level function in
// chat-components.js, which is declared at two-space indent inside the IIFE
// (chat-stream.js renderers are at four-space indent, handled by
// jsFunctionBody). Nested helpers sit at four+ spaces, so the slice keeps every
// helper a renderer owns.
func chatComponentsFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "\n  function "+name+"(")
	if start < 0 {
		t.Fatalf("chat-components.js: function %s not found", name)
	}
	rest := src[start+1:]
	next := regexp.MustCompile(`\n  (?:async )?function `).FindStringIndex(rest[1:])
	if next == nil {
		return rest
	}
	return rest[:next[0]+1]
}

// uniqueSorted dedupes and sorts in place, returning the slice.
func uniqueSorted(in []string) []string {
	slices.Sort(in)
	return slices.Compact(in)
}

// testIDs extracts every data-testid a renderer emits, whether set through
// setAttribute or written inline in an HTML string.
func testIDs(s string) []string {
	var out []string
	for _, m := range jsSetAttrTestIDRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	for _, m := range inlineTestIDRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return uniqueSorted(out)
}

// cardHooks extracts the card's own class hooks: the bare root marker plus every
// memory-<block>__<element> hook.
func cardHooks(s string) []string {
	out := rootHookRE.FindAllString(s, -1)
	out = append(out, hookRE.FindAllString(s, -1)...)
	return uniqueSorted(out)
}

// assertSameSet fails when the two sets differ, naming the missing and extra
// entries so the drift is obvious.
func assertSameSet(t *testing.T, what string, component, js []string) {
	t.Helper()
	for _, want := range js {
		if !slices.Contains(component, want) {
			t.Errorf("%s: component is missing %q (present in the chat renderer: %v)", what, want, js)
		}
	}
	for _, got := range component {
		if !slices.Contains(js, got) {
			t.Errorf("%s: component emits %q, absent from the chat renderer (%v)", what, got, js)
		}
	}
}

// TestQuestionCardMatchesChatRenderer pins the question card's contract: the
// chat's renderQuestion must emit the same data-testids and card hooks as the
// canonical component, in both its interactive and answered states.
func TestQuestionCardMatchesChatRenderer(t *testing.T) {
	js := jsFunctionBody(t, chatStreamSource(t), "renderQuestion")

	option := components.QuestionCardOption{Label: "Alpha", Value: "alpha", Description: "The first one"}
	interactive := renderHTML(t, components.QuestionCard(components.QuestionCardProps{
		Prompt:  "Pick one",
		Options: []components.QuestionCardOption{option},
		Attrs:   templ.Attributes{"data-testid": "chat-question-card"},
	}))
	answered := renderHTML(t, components.QuestionCard(components.QuestionCardProps{
		Prompt:   "Pick one",
		Options:  []components.QuestionCardOption{option},
		Mode:     components.QuestionCardAnswered,
		Selected: []string{"alpha"},
		Attrs:    templ.Attributes{"data-testid": "chat-question-card"},
	}))
	both := interactive + "\n" + answered

	assertSameSet(t, "question data-testid", testIDs(both), testIDs(js))
	assertSameSet(t, "question card hooks", cardHooks(both), cardHooks(js))

	// The option row carries the interaction's indicator role, and both
	// renderers pick the role from the same axis.
	for _, want := range []string{`role="radio"`, `aria-checked="false"`, `data-option-value="alpha"`} {
		if !strings.Contains(interactive, want) {
			t.Errorf("interactive card missing %q:\n%s", want, interactive)
		}
	}
	multi := renderHTML(t, components.QuestionCard(components.QuestionCardProps{
		Prompt:  "Pick some",
		Multi:   true,
		Options: []components.QuestionCardOption{option},
		Attrs:   templ.Attributes{"data-testid": "chat-question-card"},
	}))
	if !strings.Contains(multi, `role="checkbox"`) || !strings.Contains(multi, `type="checkbox"`) {
		t.Errorf("multi-select card must render a checkbox option row:\n%s", multi)
	}
	if !strings.Contains(js, `isMulti ? "checkbox" : "radio"`) {
		t.Errorf("chat renderer no longer selects the option role from the multi axis:\n%s", js)
	}

	// The answered state reflects the choice and offers no interactive controls.
	for _, want := range []string{`aria-checked="true"`, "disabled", "Answered"} {
		if !strings.Contains(answered, want) {
			t.Errorf("answered card missing %q:\n%s", want, answered)
		}
	}
	for _, unwanted := range []string{"chat-question-submit", "Esc to dismiss"} {
		if strings.Contains(answered, unwanted) {
			t.Errorf("answered card must not offer %q:\n%s", unwanted, answered)
		}
	}
}

// TestApprovalCardMatchesChatRenderer pins the approval card's contract: the
// chat's renderApproval must emit the same data-testids and card hooks as the
// canonical component's default (pending) render.
func TestApprovalCardMatchesChatRenderer(t *testing.T) {
	js := jsFunctionBody(t, chatStreamSource(t), "renderApproval")

	card := renderHTML(t, components.ApprovalCard(components.ApprovalCardProps{
		ToolName: "web_search",
		ArgsJSON: "{}",
		Attrs:    templ.Attributes{"data-testid": "chat-approval-card"},
	}))

	assertSameSet(t, "approval data-testid", testIDs(card), testIDs(js))
	assertSameSet(t, "approval card hooks", cardHooks(card), cardHooks(js))

	// The tool name is presented and the response controls are the chat's own.
	for _, want := range []string{"web_search", `data-testid="chat-approval-cancel"`, `data-testid="chat-approval-reject"`, `data-testid="chat-approval-approve"`} {
		if !strings.Contains(card, want) {
			t.Errorf("approval card missing %q:\n%s", want, card)
		}
	}
}

// The card-skeleton contract the A2UI question renderer must share with the
// canonical chat renderer. chat-question-submit is deliberately excluded: the
// A2UI question answers on option click and has no two-step select/submit.
var questionCardContract = []string{
	"chat-question-card",
	"chat-question-option",
	"memory-question",
	"memory-question__body",
	"memory-question__options",
	"memory-question__option",
	"memory-question__indicator",
	"memory-question__label",
	"memory-question__description",
}

// The card-skeleton contract the A2UI approval renderer must share with the
// canonical chat renderer (approve/reject/cancel + the memory-approval hooks).
var approvalCardContract = []string{
	"chat-approval-card",
	"chat-approval-approve",
	"chat-approval-reject",
	"chat-approval-cancel",
	"memory-approval",
	"memory-approval__title",
	"memory-approval__sentence",
	"memory-approval__args",
	"memory-approval__actions",
	"memory-approval__cancel",
	"memory-approval__reject-action",
	"memory-approval__approve",
}

// assertCardContract fails when a renderer's source is missing any of the
// contract tokens (data-testids + memory-* hooks) the card must emit.
func assertCardContract(t *testing.T, name, src string, contract []string) {
	t.Helper()
	have := append(testIDs(src), cardHooks(src)...)
	for _, want := range contract {
		if !slices.Contains(have, want) {
			t.Errorf("%s: missing card-contract token %q", name, want)
		}
	}
}

// TestA2UICardsMatchCanonical pins the A2UI question/approval renderers
// (chat-components.js a2uiQuestion / a2uiApproval) to the same card-skeleton
// contract — data-testids + memory-* hooks — as the canonical chat renderers
// (chat-stream.js renderQuestion / renderApproval). Both sides must emit every
// token. The A2UI renderers answer on option click, so chat-question-submit is
// not part of the contract; the canonical side is re-asserted here so the
// contract is explicit on both sides.
func TestA2UICardsMatchCanonical(t *testing.T) {
	stream := chatStreamSource(t)
	comps := chatComponentsSource(t)

	canonicalQ := jsFunctionBody(t, stream, "renderQuestion")
	canonicalA := jsFunctionBody(t, stream, "renderApproval")
	a2uiQ := chatComponentsFunctionBody(t, comps, "a2uiQuestion")
	a2uiA := chatComponentsFunctionBody(t, comps, "a2uiApproval")

	assertCardContract(t, "canonical renderQuestion", canonicalQ, questionCardContract)
	assertCardContract(t, "a2uiQuestion", a2uiQ, questionCardContract)
	assertCardContract(t, "canonical renderApproval", canonicalA, approvalCardContract)
	assertCardContract(t, "a2uiApproval", a2uiA, approvalCardContract)
}
