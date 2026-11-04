package agents

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestFinalResponseStreamEvents_ExtractsA2UI(t *testing.T) {
	block := "```a2ui\n" +
		"{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n" +
		"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n" +
		"```"
	parts := []*genai.Part{{Text: "Here is a proposal:\n" + block}}

	events := finalResponseStreamEvents(parts)

	var a2uiEvent *StreamEvent
	var textDelta strings.Builder
	for i := range events {
		switch events[i].Type {
		case StreamEventA2UI:
			a2uiEvent = &events[i]
		case StreamEventTextDelta:
			textDelta.WriteString(events[i].Text)
		}
	}

	if a2uiEvent == nil {
		t.Fatal("expected a StreamEventA2UI event")
	}
	if len(a2uiEvent.A2UI) != 2 {
		t.Fatalf("expected 2 a2ui messages, got %d", len(a2uiEvent.A2UI))
	}
	if a2uiEvent.SurfaceID != "s1" {
		t.Fatalf("surfaceID = %q, want s1", a2uiEvent.SurfaceID)
	}
	if textDelta.Len() == 0 {
		t.Error("expected remaining plain text delta")
	}
	if strings.Contains(textDelta.String(), "```a2ui") {
		t.Errorf("text delta should not contain the fence: %q", textDelta.String())
	}
}

func TestFinalResponseStreamEvents_NoFenceUnchanged(t *testing.T) {
	parts := []*genai.Part{
		{Text: "plain answer"},
		{Text: "chain of thought", Thought: true},
	}
	events := finalResponseStreamEvents(parts)

	for _, e := range events {
		if e.Type == StreamEventA2UI {
			t.Fatalf("did not expect A2UI event: %+v", e)
		}
	}
	var text strings.Builder
	for _, e := range events {
		if e.Type == StreamEventTextDelta {
			text.WriteString(e.Text)
		}
	}
	if text.String() != "plain answer" {
		t.Fatalf("text = %q, want plain answer", text.String())
	}
}

func TestFinalResponseStreamEvents_InvalidFenceFallsBack(t *testing.T) {
	parts := []*genai.Part{{Text: "```a2ui\nnot json\n```"}}
	events := finalResponseStreamEvents(parts)

	for _, e := range events {
		if e.Type == StreamEventA2UI {
			t.Fatalf("did not expect A2UI event for invalid fence: %+v", e)
		}
	}
}
