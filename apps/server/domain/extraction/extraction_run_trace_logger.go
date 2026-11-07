package extraction

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// runMessageRecorder is the transcript sink the bridge writes to. *RunRecorder
// implements it against the database; tests use a fake so the mapping from
// pipeline callbacks to chat messages is verifiable without a database.
type runMessageRecorder interface {
	RecordMessage(ctx context.Context, runID string, msg RunMessage) error
}

// Stage kinds recorded on a message so the transcript can label it without
// embedding markup in the text (which plain-text surfaces rendered literally as
// "**EntityProcessor** · stage output").
const (
	stageKindHeader = "header" // the run-level opening message
	stageKindPrompt = "prompt" // the stage's input
	stageKindOutput = "output" // the stage's answer, or a processor's status line
	stageKindError  = "error"  // a stage failure
	// stageKindWarning labels a run-level warning (e.g. a run whose object
	// count deviates strongly from the previous run, #240). It is a status
	// line, not a stage failure, so it renders under "Extraction · warning".
	stageKindWarning = "warning"
)

// runTraceLogger bridges the extraction pipeline's TraceLogger callbacks to the
// job's agent run, so the stage prompts and model responses appear as a chat
// transcript. It is a decorator: every callback is still forwarded to the
// underlying logger, which keeps the full-fidelity per-job log file.
//
// Only content a reader can use becomes a message: the prompt (what the stage
// was asked), the stage's output (the model's answer, or a processor's own
// status line) and errors. Parsed results,
// schemas, spans-level counters and events stay in the log file — they are
// structured diagnostics, not conversation.
type runTraceLogger struct {
	delegate agents.TraceLogger
	rec      runMessageRecorder
	runID    string
	log      *slog.Logger

	mu   sync.Mutex
	step int
	// lastAnswer dedupes repeated output per author within one turn: a stage that
	// emits the same line twice must not fill the transcript with copies, while
	// two stages — or the same stage after a new prompt — that legitimately
	// return the same payload both keep theirs.
	lastAnswer map[string]string
}

// newRunTraceLogger wraps delegate (which may be nil) and records a header
// message describing the extraction, so an opened transcript starts with what
// was being extracted rather than a bare prompt.
func newRunTraceLogger(rec runMessageRecorder, runID string, delegate agents.TraceLogger, header string, log *slog.Logger) *runTraceLogger {
	if delegate == nil {
		delegate = &agents.NullTraceLogger{}
	}
	if log == nil {
		log = slog.Default()
	}
	l := &runTraceLogger{delegate: delegate, rec: rec, runID: runID, log: log, lastAnswer: map[string]string{}}
	if header != "" {
		l.record("user", extractionRunAgentName, stageKindHeader, header)
	}
	return l
}

// record persists one message, carrying its stage provenance, and advancing the
// step counter. Failures are logged and swallowed: the transcript is
// diagnostics and must never fail extraction.
func (l *runTraceLogger) record(role, stage, stageKind, text string) {
	if l.rec == nil || l.runID == "" {
		return
	}
	l.mu.Lock()
	l.step++
	step := l.step
	l.mu.Unlock()
	msg := RunMessage{Role: role, Step: step, Text: text, Stage: stage, StageKind: stageKind}
	if err := l.rec.RecordMessage(context.Background(), l.runID, msg); err != nil {
		l.log.Warn("failed to record extraction transcript message",
			slog.String("run_id", l.runID),
			logger.Error(err))
	}
}

func (l *runTraceLogger) Close() error { return l.delegate.Close() }

func (l *runTraceLogger) LogFilePath() string { return l.delegate.LogFilePath() }

func (l *runTraceLogger) LogStageStart(stageName string) {
	l.delegate.LogStageStart(stageName)
}

func (l *runTraceLogger) LogPrompt(agentName string, prompt string) {
	l.delegate.LogPrompt(agentName, prompt)
	// A new prompt starts a new turn for that stage, so the repeat guard forgets
	// the stage's previous answer. Without this, a later batch (or a quality
	// retry) that legitimately returns byte-identical output — two empty-result
	// extractions are the common case — would lose its answer even though many
	// messages were recorded in between.
	l.forgetAnswer(agentName)
	l.record("user", agentName, stageKindPrompt, prompt)
}

func (l *runTraceLogger) LogResponse(agentName string, response string) {
	l.delegate.LogResponse(agentName, response)
	// Nothing in the pipeline calls this today (answers arrive as ADK events), so
	// the guard is a forward-compatible no-op; it keeps a stage that ever calls
	// both from recording its answer twice.
	if !l.claimResponse(agentName, response) {
		return
	}
	l.record("assistant", agentName, stageKindOutput, response)
}

func (l *runTraceLogger) LogParsedResult(stageName string, result any) {
	l.delegate.LogParsedResult(stageName, result)
}

func (l *runTraceLogger) LogSchemas(objectSchemas map[string]agents.ObjectSchema, relationshipSchemas map[string]agents.RelationshipSchema) {
	l.delegate.LogSchemas(objectSchemas, relationshipSchemas)
}

func (l *runTraceLogger) LogDocumentText(text string) {
	l.delegate.LogDocumentText(text)
}

func (l *runTraceLogger) LogEntities(entities []agents.InternalEntity) {
	l.delegate.LogEntities(entities)
}

func (l *runTraceLogger) LogRelationships(relationships []agents.ExtractedRelationship) {
	l.delegate.LogRelationships(relationships)
}

func (l *runTraceLogger) LogQualityCheck(iteration int, orphanRate float64, threshold float64, orphanIDs []string) {
	l.delegate.LogQualityCheck(iteration, orphanRate, threshold, orphanIDs)
}

func (l *runTraceLogger) LogError(stage string, err error) {
	l.delegate.LogError(stage, err)
	if err != nil {
		l.record("assistant", stage, stageKindError, err.Error())
	}
}

func (l *runTraceLogger) LogInfo(message string) { l.delegate.LogInfo(message) }

// LogEvent records the model's answer. Responses do not arrive through
// LogResponse — nothing in the pipeline calls it, because a stage's instruction
// provider can log only the prompt it builds. The answer comes back as an ADK
// event: ExtractionPipeline.logEvent logs every event from the runner with its
// author, and the stage agent's event carries the model's text. Events authored
// by the user are the synthetic kickoff message (already represented by the
// prompts) and non-text events (function calls) carry no usable text, so both are
// skipped.
func (l *runTraceLogger) LogEvent(eventType string, author string, content string) {
	l.delegate.LogEvent(eventType, author, content)
	if eventType != "ADK_EVENT" {
		return
	}
	text := strings.TrimSpace(content)
	if text == "" || author == "" || strings.EqualFold(author, "user") || strings.EqualFold(author, "system") {
		return
	}
	if !l.claimResponse(author, text) {
		return
	}
	// Recorded as kind "output" rather than something answer-specific: the
	// runner's events carry both the model's answer and a stage's own synthetic
	// lines (the processors' "Processed N entities", the quality check's
	// pass/fail), and only the author distinguishes them. A neutral kind is
	// accurate for both.
	l.record("assistant", author, stageKindOutput, content)
}

// forgetAnswer clears the repeat guard for a stage that is starting a new turn.
func (l *runTraceLogger) forgetAnswer(author string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.lastAnswer, author)
}

// claimResponse reports whether text is new for that author in the current turn.
func (l *runTraceLogger) claimResponse(author, text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || l.lastAnswer[author] == trimmed {
		return false
	}
	l.lastAnswer[author] = trimmed
	return true
}

var _ agents.TraceLogger = (*runTraceLogger)(nil)
