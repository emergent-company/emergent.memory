package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// lastAgentReplyTestMsg is a tiny constructor so the cases below read as the
// persisted-run-message shape the fallback consumes.
func lastAgentReplyTestMsg(role, text string) *agents.AgentRunMessage {
	m := &agents.AgentRunMessage{Role: role}
	if text != "" {
		m.Content = map[string]any{"text": text}
	}
	return m
}

// TestLastAgentReplyTextRecoversAuthorNamedReply is the regression test for
// issue #1832 at the chat fallback consumer: the executor persists the answer
// under the ADK event Author (a sanitized agent name such as "coder"), NOT the
// literal "assistant". A consumer that equality-checked role == "assistant"
// silently dropped the answer; the fallback must recover it.
func TestLastAgentReplyTextRecoversAuthorNamedReply(t *testing.T) {
	msgs := []*agents.AgentRunMessage{
		lastAgentReplyTestMsg("system", "You are a careful agent."),
		lastAgentReplyTestMsg("user", "what is the capital of Norway?"),
		lastAgentReplyTestMsg("coder", "Oslo."), // agent reply, non-"assistant" author
	}
	assert.Equal(t, "Oslo.", lastAgentReplyText(msgs))
}

// TestLastAgentReplyTextSkipsNonConversationalSides proves prompt/tool output
// can never be surfaced as the answer: a trailing tool_result (or system/tool
// row) is skipped in favour of the earlier agent-authored text.
func TestLastAgentReplyTextSkipsNonConversationalSides(t *testing.T) {
	msgs := []*agents.AgentRunMessage{
		lastAgentReplyTestMsg("user", "ask"),
		lastAgentReplyTestMsg("coder", "the real answer"),
		lastAgentReplyTestMsg("tool_result", "{\"raw\":\"tool output\"}"),
		lastAgentReplyTestMsg("system", "system prompt"),
	}
	assert.Equal(t, "the real answer", lastAgentReplyText(msgs))
}

// TestLastAgentReplyTextEmptyWhenNoAgentTurn covers the no-reply case: only
// non-agent sides present, so the caller falls through to its other sources.
func TestLastAgentReplyTextEmptyWhenNoAgentTurn(t *testing.T) {
	msgs := []*agents.AgentRunMessage{
		lastAgentReplyTestMsg("system", "prompt"),
		lastAgentReplyTestMsg("user", "hi"),
		lastAgentReplyTestMsg("tool", "tool output"),
	}
	assert.Equal(t, "", lastAgentReplyText(msgs))
	assert.Equal(t, "", lastAgentReplyText(nil))
}
