package main

import (
	"encoding/json"

	"github.com/emergent-company/emergent.memory/apps/web-ui/toolresult"
)

// Wire enrichment for a tool call. Two surfaces describe the same call — the live
// `mcp_tool` SSE frame while a run streams, and the `tool_call` timeline item
// when it is replayed — and both carry the same three values so the two
// renderers cannot word the same call differently:
//
//	tool_category / toolCategory  the verb and icon, from the tool's name
//	tool_subject  / toolSubject   one line saying what the call was asked to do
//	tool_view     / toolView      the readable result, from the tool's output
//
// The reading rules live in the toolresult package; this file only decides what
// goes on the wire, in which casing.

// toolCategoryJSON returns the category for a tool name as raw JSON, or nil when
// it cannot be encoded. An unknown name still produces the generic category, so
// this is never nil in practice — the nil return is only for a marshal failure.
func toolCategoryJSON(name string) json.RawMessage {
	encoded, err := marshalNoEscape(toolresult.Classify(name))
	if err != nil {
		return nil
	}
	return encoded
}

// toolSubjectJSON returns the call's one-line subject as a JSON string, or nil
// when the arguments hold nothing readable to name the call with.
func toolSubjectJSON(input json.RawMessage) json.RawMessage {
	subject := toolresult.Subject(input)
	if subject == "" {
		return nil
	}
	return rawJSONString(subject)
}

// toolViewJSON returns the readable presentation of a tool result, or nil when
// there is no result to read.
func toolViewJSON(output json.RawMessage) json.RawMessage {
	if len(output) == 0 || string(output) == "null" {
		return nil
	}
	view := toolresult.Parse(string(output))
	if view == nil {
		return nil
	}
	encoded, err := marshalNoEscape(view)
	if err != nil {
		return nil
	}
	return encoded
}

// toolAwaitingConfirmation reports whether a call's output is the executor's
// pause sentinel — the tool never ran, so what it carries is not a result and it
// names nothing. The persisted status cannot say so: afterToolCb records
// "completed" when the policy check pauses the run, so the payload is the only
// honest signal there is. Without this, a paused step replays as
// "Used memory · Note — 3 fields" over the sentinel's own keys.
func toolAwaitingConfirmation(output json.RawMessage) bool {
	if len(output) == 0 || string(output) == "null" {
		return false
	}
	var probe struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return false
	}
	if probe.Status == "awaiting_confirmation" {
		return true
	}
	// The sentinel can also arrive one envelope down.
	for _, inner := range []json.RawMessage{probe.Data, probe.Result} {
		if len(inner) == 0 {
			continue
		}
		var nested struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(inner, &nested); err == nil && nested.Status == "awaiting_confirmation" {
			return true
		}
	}
	return false
}

// toolTerminal reports whether a live tool frame is past the point where its
// `result` is the call's output. Before that the frame carries the call's
// arguments, so reading it as a result would describe the question as the
// answer.
func toolTerminal(status string) bool {
	switch status {
	case "", "running", "started", "in_progress", "pending", "queued", "awaiting_confirmation":
		return false
	default:
		return true
	}
}

// toolSettled reports whether a timeline item's output is the call's result. A
// timeline item carries no status when history wrote the output at completion, so
// an absent status means settled; a status that is present has to be terminal, or
// a call paused for approval would be replayed as if it had answered.
func toolSettled(status string) bool {
	return status == "" || toolTerminal(status)
}
