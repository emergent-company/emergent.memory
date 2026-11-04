package agents

import (
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// eventRecorder captures the LogEvent calls the pipeline makes, which is the
// seam the chat transcript relies on to see model answers: nothing calls
// LogResponse, so a response reaches the transcript only if the runner's events
// are logged with their author and text.
type eventRecorder struct {
	NullTraceLogger
	events []loggedEvent
}

type loggedEvent struct {
	eventType string
	author    string
	content   string
}

func (l *eventRecorder) LogEvent(eventType, author, content string) {
	l.events = append(l.events, loggedEvent{eventType: eventType, author: author, content: content})
}

// TestPipelineLogEventCarriesModelAnswer pins that contract: a stage agent's
// event is forwarded whole (type, author, concatenated text parts), and an event
// without content is not forwarded at all.
func TestPipelineLogEventCarriesModelAnswer(t *testing.T) {
	rec := &eventRecorder{}
	p := &ExtractionPipeline{traceLogger: rec}

	// Event embeds the LLM response, so Content is promoted and cannot be set in
	// a struct literal.
	stageEvent := session.Event{Author: "EntityExtractor"}
	stageEvent.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{genai.NewPartFromText(`{"entities":`), genai.NewPartFromText(`[]}`)},
	}
	p.logEvent(&stageEvent)
	// An event with no content parts at all must not produce an empty entry.
	p.logEvent(&session.Event{Author: "RelationshipBuilder"})

	if len(rec.events) != 1 {
		t.Fatalf("logged %d events, want 1 (content-less events are skipped): %+v", len(rec.events), rec.events)
	}
	got := rec.events[0]
	if got.eventType != "ADK_EVENT" {
		t.Errorf("event type = %q, want ADK_EVENT", got.eventType)
	}
	if got.author != "EntityExtractor" {
		t.Errorf("author = %q, want the stage agent's name", got.author)
	}
	if got.content != `{"entities":[]}` {
		t.Errorf("content = %q, want every text part concatenated", got.content)
	}
}
