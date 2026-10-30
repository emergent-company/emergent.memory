package processingproblems

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		queue     string
		message   string
		wantClass FailureClass
		wantRetry bool
	}{
		// The five shapes the Huma extraction dead letters actually took.
		{"malformed json", QueueObjectExtraction, "failed to parse entity JSON: unexpected end of JSON input", ClassInvalidOutput, true},
		{"nil adk event", QueueObjectExtraction, "RelationshipProcessor yielded a nil event", ClassInternal, true},
		{"empty assistant content", QueueObjectExtraction, "Invalid assistant message: content or tool_calls must be set", ClassInvalidOutput, true},
		{"no choices", QueueObjectExtraction, "response had no choices", ClassInvalidOutput, true},

		{"no model", QueueObjectExtraction, "no generative model configured for project", ClassConfiguration, false},
		{"invalid key", QueueObjectExtraction, "401 unauthorized: invalid api key", ClassPermission, false},
		{"rate limit", QueueObjectExtraction, "429 rate limit exceeded, retry later", ClassProvider, true},
		{"timeout", QueueGraphEmbedding, "context deadline exceeded while calling provider", ClassProvider, true},
		{"connection", QueueSourceSync, "connection refused", ClassProvider, true},
		{"context length", QueueObjectExtraction, "maximum context length exceeded", ClassLimit, true},
		{"permission", QueueSourceSync, "permission denied for drive folder", ClassPermission, false},
		{"unreadable input", QueueDocumentParsing, "unsupported file format", ClassInput, false},
		{"conflict", QueueSchemaMigration, "schema migration conflict on unique constraint", ClassConflict, false},
		{"unknown", QueueAgentRun, "something completely new happened", ClassUnknown, true},
		{"empty extraction", QueueObjectExtraction, "", ClassInternal, true},
		{"empty email", QueueEmail, "", ClassProvider, true},
		{"empty parsing", QueueDocumentParsing, "", ClassInput, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.queue, tc.message)
			if got.Class != tc.wantClass {
				t.Fatalf("class = %q, want %q (summary %q)", got.Class, tc.wantClass, got.Summary)
			}
			if got.Retryable != tc.wantRetry {
				t.Fatalf("retryable = %v, want %v", got.Retryable, tc.wantRetry)
			}
			if got.Summary == "" || got.Remediation == "" {
				t.Fatalf("empty copy: %+v", got)
			}
		})
	}
}

func TestClassifyOutputLimitBeatsInvalidJSON(t *testing.T) {
	// A deeply nested body truncated at the output cap contains "unexpected end
	// of json" but the actionable cause is the limit, not the model's format.
	got := Classify(QueueObjectExtraction, "unexpected end of JSON input (truncated at max tokens)")
	if got.Class != ClassLimit {
		t.Fatalf("class = %q, want %q", got.Class, ClassLimit)
	}
}

func TestActionFor(t *testing.T) {
	docID := "doc-1"
	sourceID := "src-1"

	tests := []struct {
		name     string
		queue    string
		row      problemRow
		wantKind ActionKind
		wantHref string
	}{
		{
			name:     "extraction retries a new run",
			queue:    QueueObjectExtraction,
			row:      problemRow{DocumentID: &docID},
			wantKind: ActionRetryExtraction,
			wantHref: "/documents/doc-1/extractions",
		},
		{
			name:     "parsing only links to the document",
			queue:    QueueDocumentParsing,
			row:      problemRow{DocumentID: &docID},
			wantKind: ActionOpen,
			wantHref: "/documents/doc-1",
		},
		{
			name:     "embeddings retrigger project-wide",
			queue:    QueueChunkEmbedding,
			row:      problemRow{DocumentID: &docID},
			wantKind: ActionRetryEmbeddings,
			wantHref: "/embeddings",
		},
		{
			name:     "relationship embeddings have no project reset yet",
			queue:    QueueGraphRelationshipEmbedding,
			row:      problemRow{},
			wantKind: ActionOpen,
			wantHref: "/embeddings",
		},
		{
			name:  "source sync links to sources",
			queue: QueueSourceSync,
			row:   problemRow{SourceID: &sourceID},
			// Navigation only: no client implements a sync trigger.
			wantKind: ActionOpen,
			wantHref: "/data-sources",
		},
		{
			name:     "email links to members",
			queue:    QueueEmail,
			row:      problemRow{},
			wantKind: ActionOpen,
			wantHref: "/settings/members",
		},
		{
			name:     "schema migration is never a blind retry",
			queue:    QueueSchemaMigration,
			row:      problemRow{},
			wantKind: ActionOpen,
			wantHref: "/schema",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := actionFor(tc.queue, tc.row)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Href != tc.wantHref {
				t.Fatalf("href = %q, want %q", got.Href, tc.wantHref)
			}
			if got.Label == "" {
				t.Fatal("empty action label")
			}
		})
	}
}

func TestAreaMapping(t *testing.T) {
	if got := areaOf(QueueObjectExtraction); got != AreaDocuments {
		t.Fatalf("extraction area = %q", got)
	}
	if got := areaOf(QueueEmail); got != AreaEmail {
		t.Fatalf("email area = %q", got)
	}
	if got := areaOf(QueueSchemaMigration); got != AreaSchema {
		t.Fatalf("migration area = %q", got)
	}
}

func TestClassSummaryCapsAtThree(t *testing.T) {
	got := ClassSummary([]ClassCount{
		{Class: ClassInvalidOutput, Count: 4},
		{Class: ClassInternal, Count: 3},
		{Class: ClassProvider, Count: 2},
		{Class: ClassInput, Count: 1},
	})
	want := "Invalid model output 4 · Internal error 3 · Provider error 2"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
