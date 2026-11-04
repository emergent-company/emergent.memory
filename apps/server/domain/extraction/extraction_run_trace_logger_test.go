package extraction

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// recordedMessage is one transcript write captured by recordingSink.
type recordedMessage struct {
	runID     string
	role      string
	step      int
	text      string
	stage     string
	stageKind string
}

// recordingSink is the runMessageRecorder seam without a database, so the
// pipeline-callback → chat-message mapping is testable in isolation.
type recordingSink struct {
	mu   sync.Mutex
	msgs []recordedMessage
}

func (s *recordingSink) RecordMessage(_ context.Context, runID string, msg RunMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, recordedMessage{
		runID:     runID,
		role:      msg.Role,
		step:      msg.Step,
		text:      msg.Text,
		stage:     msg.Stage,
		stageKind: msg.StageKind,
	})
	return nil
}

func (s *recordingSink) messages() []recordedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedMessage(nil), s.msgs...)
}

// countingLogger is a TraceLogger that records the calls the bridge is expected
// to forward verbatim. Embedding NullTraceLogger keeps the noise down while
// still satisfying the interface.
type countingLogger struct {
	agents.NullTraceLogger
	prompts  int
	closed   bool
	stageHit string
}

func (l *countingLogger) LogPrompt(_ string, _ string) { l.prompts++ }
func (l *countingLogger) LogStageStart(name string)    { l.stageHit = name }
func (l *countingLogger) Close() error                 { l.closed = true; return nil }

// TestRunTraceLoggerRecordsPromptAndResponse pins what becomes chat: a header
// describing the extraction, one user message per stage prompt and one assistant
// message per model response, with a monotonic step counter (the run timeline
// orders by time and breaks ties on step).
func TestRunTraceLoggerRecordsPromptAndResponse(t *testing.T) {
	sink := &recordingSink{}
	lg := newRunTraceLogger(sink, "run-1", nil, "Extraction of document doc-1 · job `job-1`", nil)

	lg.LogStageStart("ENTITY EXTRACTION")
	lg.LogPrompt("EntityExtractor", "extract entities from this text")
	lg.LogResponse("EntityExtractor", `{"entities":[]}`)
	lg.LogError("RELATIONSHIP EXTRACTION", errors.New("model overloaded"))

	msgs := sink.messages()
	if len(msgs) != 4 {
		t.Fatalf("recorded %d messages, want 4 (header, prompt, response, error): %+v", len(msgs), msgs)
	}
	// The header opens the transcript: an opened run must say what was extracted.
	if msgs[0].role != "user" || msgs[0].stageKind != stageKindHeader || !strings.Contains(msgs[0].text, "job `job-1`") {
		t.Errorf("first message = %+v, want the user-role header naming the job", msgs[0])
	}
	if msgs[1].role != "user" || msgs[1].stage != "EntityExtractor" || msgs[1].stageKind != stageKindPrompt {
		t.Errorf("prompt message = %+v, want a user message authored by EntityExtractor", msgs[1])
	}
	if !strings.Contains(msgs[1].text, "extract entities from this text") {
		t.Errorf("prompt message text = %q, want the bare prompt (no embedded label markup)", msgs[1].text)
	}
	// runrole:allow — this asserts what the recorder WRITES (the bridge's fixed
	// speaker roles for the chat timeline), not a classification of persisted
	// agent-authored roles, so the literal is the contract under test.
	if msgs[2].role != "assistant" || msgs[2].stageKind != stageKindOutput || !strings.Contains(msgs[2].text, `{"entities":[]}`) {
		t.Errorf("response message = %+v, want an assistant message carrying the response", msgs[2])
	}
	// runrole:allow — same as above: the written role, not a read-side classifier.
	if msgs[3].role != "assistant" || msgs[3].stage != "RELATIONSHIP EXTRACTION" || msgs[3].stageKind != stageKindError || !strings.Contains(msgs[3].text, "model overloaded") {
		t.Errorf("error message = %+v, want an assistant message carrying the error", msgs[3])
	}
	for i, m := range msgs {
		if m.runID != "run-1" {
			t.Errorf("message %d run = %q, want run-1", i, m.runID)
		}
		if i > 0 && m.step <= msgs[i-1].step {
			t.Errorf("step %d is not greater than %d — the timeline tie-break needs monotonic steps", m.step, msgs[i-1].step)
		}
	}
}

// TestRunTraceLoggerForwardsToDelegate proves the bridge is a decorator: every
// callback still reaches the file logger, so the transcript never replaces the
// full-fidelity per-job log (which keeps schemas, parsed results and counters the
// transcript leaves out).
func TestRunTraceLoggerForwardsToDelegate(t *testing.T) {
	delegate := &countingLogger{}
	lg := newRunTraceLogger(&recordingSink{}, "run-1", delegate, "", nil)

	lg.LogStageStart("FINAL RESULTS")
	lg.LogPrompt("EntityExtractor", "prompt")

	if delegate.stageHit != "FINAL RESULTS" {
		t.Errorf("stage start not forwarded, got %q", delegate.stageHit)
	}
	if delegate.prompts != 1 {
		t.Errorf("prompt forwarded %d times, want 1", delegate.prompts)
	}
	if err := lg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !delegate.closed {
		t.Error("Close must close the delegate so the trace log file is flushed")
	}
	if got := lg.LogFilePath(); got != "" {
		t.Errorf("LogFilePath = %q, want the delegate's empty path", got)
	}
}

// TestRunTraceLoggerWithoutSinkIsSafe covers the degraded paths: recording is
// disabled (no sink) or the run could not be opened (empty id), and the pipeline
// must run unchanged.
func TestRunTraceLoggerWithoutSinkIsSafe(t *testing.T) {
	for name, lg := range map[string]*runTraceLogger{
		"no sink":     newRunTraceLogger(nil, "run-1", nil, "header", nil),
		"no run id":   newRunTraceLogger(&recordingSink{}, "", nil, "header", nil),
		"no delegate": newRunTraceLogger(&recordingSink{}, "run-1", nil, "header", nil),
	} {
		lg.LogStageStart("stage")
		lg.LogPrompt("EntityExtractor", "prompt")
		lg.LogResponse("EntityExtractor", "response")
		lg.LogError("stage", errors.New("boom"))
		lg.LogInfo("info")
		lg.LogEvent("event", "author", "content")
		if err := lg.Close(); err != nil {
			t.Errorf("%s: Close: %v", name, err)
		}
	}
}

// TestTruncateForRunHardLimit pins the runaway-output guard: text is stored
// whole up to the bound, and a message past it is cut on a rune boundary and
// marks exactly where the rest lives. The bound is a safety valve, not the
// readability policy — the transcript is meant to show the whole prompt and
// answer (see runMessageHardLimitChars).
func TestTruncateForRunHardLimit(t *testing.T) {
	if got := truncateForRun("short", 100); got != "short" {
		t.Errorf("short text = %q, want it unchanged", got)
	}

	long := strings.Repeat("x", 500)
	got := truncateForRun(long, 100)
	if n := utf8.RuneCountInString(got); n != 100 {
		t.Errorf("truncated length = %d runes, want exactly 100", n)
	}
	if !strings.Contains(got, "truncated at 100 characters") || !strings.Contains(got, "trace log") {
		t.Errorf("truncated text %q must name the bound and say where the full text is", got)
	}

	// Multi-byte text must be cut on a rune boundary, never mid-character.
	multi := strings.Repeat("héllo—wörld🙂", 200)
	cut := truncateForRun(multi, 100)
	if !utf8.ValidString(cut) {
		t.Fatalf("truncating multi-byte text produced invalid UTF-8: %q", cut)
	}
	if n := utf8.RuneCountInString(cut); n != 100 {
		t.Errorf("multi-byte truncated length = %d runes, want exactly 100", n)
	}

	// A max smaller than the marker must not panic or return trailing bytes of
	// the marker itself.
	if got := truncateForRun(long, 5); utf8.RuneCountInString(got) != 5 {
		t.Errorf("tiny budget length = %d runes, want 5", utf8.RuneCountInString(got))
	}
}

// TestRunTraceLoggerRecordsModelResponsesFromEvents covers how answers actually
// reach the transcript: the pipeline logs runner events (author + text), not
// LogResponse. A stage agent's event becomes an assistant message; the synthetic
// user kickoff message, empty text, non-ADK events and an immediate repeat of the
// previous answer do not.
func TestRunTraceLoggerRecordsModelResponsesFromEvents(t *testing.T) {
	sink := &recordingSink{}
	lg := newRunTraceLogger(sink, "run-1", nil, "Extraction of document doc-1", nil)

	lg.LogEvent("ADK_EVENT", "EntityExtractor", `{"entities":[{"name":"A"}]}`)
	lg.LogEvent("ADK_EVENT", "user", "Extract entities and relationships from the document.")
	lg.LogEvent("ADK_EVENT", "RelationshipBuilder", "   ")
	lg.LogEvent("ADK_EVENT", "EntityExtractor", `{"entities":[{"name":"A"}]}`)
	lg.LogEvent("SOMETHING_ELSE", "EntityExtractor", "not an ADK event")

	msgs := sink.messages()
	if len(msgs) != 2 {
		t.Fatalf("recorded %d messages, want 2 (header + one model answer): %+v", len(msgs), msgs)
	}
	if msgs[0].role != "user" {
		t.Errorf("first message role = %q, want the user-role header (runrole:allow — written role contract)", msgs[0].role)
	}
	// runrole:allow — this asserts what the bridge WRITES for a model answer, not a
	// read-side classification of persisted agent-authored roles.
	if msgs[1].role != "assistant" {
		t.Errorf("answer message role = %q, want assistant", msgs[1].role)
	}
	if msgs[1].stage != "EntityExtractor" || msgs[1].stageKind != stageKindOutput {
		t.Errorf("answer message = %+v, want it authored by EntityExtractor as stage output", msgs[1])
	}
	if msgs[1].text != `{"entities":[{"name":"A"}]}` {
		t.Errorf("answer message text = %q, want the model's text alone (the stage lives in metadata)", msgs[1].text)
	}
	if msgs[1].step <= msgs[0].step {
		t.Errorf("answer step %d must follow the header step %d", msgs[1].step, msgs[0].step)
	}
}

// TestRunTraceLoggerDedupesOnlyWithinATurn pins the repeat guard's scope. Two
// stages that return the same payload both keep theirs, and a stage that returns
// the same payload again in a later turn (a second document batch, a quality
// retry — each starts with a fresh prompt) keeps its answer too. Only an
// immediate repeat from the same stage inside one turn is a duplicate.
func TestRunTraceLoggerDedupesOnlyWithinATurn(t *testing.T) {
	sink := &recordingSink{}
	lg := newRunTraceLogger(sink, "run-1", nil, "", nil)

	// Turn one: prompt, answer, and a second identical answer with no new prompt
	// in between — the duplicate is dropped.
	lg.LogPrompt("EntityExtractor", "batch one")
	lg.LogEvent("ADK_EVENT", "EntityExtractor", `{"entities":[]}`)
	lg.LogEvent("ADK_EVENT", "EntityExtractor", `{"entities":[]}`)
	// A different stage returning the same text is not a duplicate.
	lg.LogEvent("ADK_EVENT", "RelationshipBuilder", `{"entities":[]}`)
	// Turn two for the same stage: the identical answer must be recorded.
	lg.LogPrompt("EntityExtractor", "batch two")
	lg.LogEvent("ADK_EVENT", "EntityExtractor", `{"entities":[]}`)

	var answers []recordedMessage
	for _, m := range sink.messages() {
		if m.stageKind == stageKindOutput {
			answers = append(answers, m)
		}
	}
	if len(answers) != 3 {
		t.Fatalf("recorded %d answers, want 3 (one repeat dropped, the second turn kept): %+v", len(answers), answers)
	}
	wantAuthors := []string{"EntityExtractor", "RelationshipBuilder", "EntityExtractor"}
	for i, want := range wantAuthors {
		if answers[i].stage != want {
			t.Errorf("answer %d = %+v, want it authored by %s", i, answers[i], want)
		}
	}
}
