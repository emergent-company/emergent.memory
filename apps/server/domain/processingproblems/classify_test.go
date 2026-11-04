package processingproblems

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		queue     string
		message   string
		wantClass FailureClass
		wantRetry bool
	}{
		// The five shapes the Huma extraction dead letters actually took.
		{"malformed json", QueueObjectExtraction, "failed to parse entity JSON: unexpected end of JSON input", ClassLimit, true},
		{"nil adk event", QueueObjectExtraction, "RelationshipProcessor yielded a nil event", ClassInternal, false},
		{"empty assistant content", QueueObjectExtraction, "Invalid assistant message: content or tool_calls must be set", ClassInvalidOutput, true},
		{"no choices", QueueObjectExtraction, "response had no choices", ClassProvider, true},
		{"truncated discovery", QueueDiscovery, "All 1 batches failed. Errors: Batch 1: failed to parse LLM response: unexpected EOF", ClassLimit, true},
		{"truncated json", QueueObjectExtraction, "failed to parse JSON: truncated JSON: unexpected end of JSON input", ClassLimit, true},

		{"no model", QueueObjectExtraction, "no generative model configured for project", ClassConfiguration, false},
		{"invalid key", QueueObjectExtraction, "401 unauthorized: invalid api key", ClassPermission, false},
		{"rate limit", QueueObjectExtraction, "429 rate limit exceeded, retry later", ClassProvider, true},
		{"timeout", QueueGraphEmbedding, "context deadline exceeded while calling provider", ClassProvider, true},
		{"connection", QueueSourceSync, "connection refused", ClassProvider, true},
		{"context length", QueueObjectExtraction, "maximum context length exceeded", ClassLimit, true},
		{"permission", QueueSourceSync, "permission denied for drive folder", ClassPermission, false},
		{"revoked drive grant", QueueSourceSync, "enumerate source: the Google Drive connection for this source has expired or was revoked; reconnect the source to continue", ClassPermission, false},
		{"revoked gmail grant", QueueSourceSync, "the Gmail connection for this source has expired or was revoked; reconnect the source to continue", ClassPermission, false},
		{"revoked trale grant", QueueSourceSync, "trale: access token expired and refresh failed (invalid_grant); reconnect the source", ClassPermission, false},
		{"replaced oauth client", QueueSourceSync, "enumerate source: the OAuth app configured for this Google Drive source is no longer valid, so it cannot refresh access; an administrator must restore or replace the data-source OAuth client in the Google Cloud project, then reconnect the source", ClassPermission, false},
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

// TestClassifyRevokedGrantIsNotRetryable pins the panel regression that prompted
// the reconnect rule: a Google Drive source whose stored grant Google had
// revoked rendered as "Unknown / the run stopped with an unclassified error /
// Retry", when the only correct action is the source's Reconnect.
func TestClassifyRevokedGrantIsNotRetryable(t *testing.T) {
	got := Classify(QueueSourceSync, "enumerate source: the Google Drive connection for this source has expired or was revoked; reconnect the source to continue")
	if got.Class != ClassPermission {
		t.Fatalf("class = %q, want %q", got.Class, ClassPermission)
	}
	if got.Retryable {
		t.Fatal("retryable = true: a dead OAuth grant cannot be recovered by retrying")
	}
	if got.Summary != "The data source is no longer authorized." {
		t.Fatalf("summary = %q, want the source-authorization copy", got.Summary)
	}
	if !strings.Contains(got.Remediation, "Reconnect") {
		t.Fatalf("remediation = %q, want it to name Reconnect", got.Remediation)
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

// TestClassifyInvalidOutputIsQueueAware pins that schema discovery never tells
// the user to install the schema it was in the middle of producing.
func TestClassifyInvalidOutputIsQueueAware(t *testing.T) {
	discovery := Classify(QueueDiscovery, "failed to parse LLM response: unexpected EOF")
	if discovery.Class != ClassLimit {
		t.Fatalf("discovery class = %q, want %q", discovery.Class, ClassLimit)
	}
	if strings.Contains(discovery.Remediation, "object schema") {
		t.Fatalf("discovery remediation must not mention installing an object schema: %q", discovery.Remediation)
	}
	if !strings.Contains(discovery.Remediation, "batch") {
		t.Fatalf("discovery remediation should mention the batch size: %q", discovery.Remediation)
	}

	extraction := Classify(QueueObjectExtraction, `failed to parse JSON: invalid character '\n' in string literal`)
	if extraction.Class != ClassInvalidOutput {
		t.Fatalf("extraction class = %q, want %q", extraction.Class, ClassInvalidOutput)
	}
	if !strings.Contains(extraction.Remediation, "object schema") {
		t.Fatalf("extraction remediation should mention the object schema: %q", extraction.Remediation)
	}
}

// TestClassifyNilEventIsNotRetryable pins that a deterministic pipeline crash
// does not invite a retry that will fail the same way.
func TestClassifyNilEventIsNotRetryable(t *testing.T) {
	got := Classify(QueueObjectExtraction, `adk: agent "ExtractionPipeline" yielded a nil event`)
	if got.Class != ClassInternal {
		t.Fatalf("class = %q, want %q", got.Class, ClassInternal)
	}
	if got.Retryable {
		t.Fatal("retryable = true: a deterministic nil-event crash should not offer Retry")
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
			name:     "extraction deep-links the failed run",
			queue:    QueueObjectExtraction,
			row:      problemRow{DocumentID: &docID, JobID: "job-1"},
			wantKind: ActionRetryExtraction,
			wantHref: "/documents/doc-1/extractions/job-1",
		},
		{
			name:     "parsing only links to the document",
			queue:    QueueDocumentParsing,
			row:      problemRow{DocumentID: &docID},
			wantKind: ActionRetryConversion,
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
			name:  "source sync deep-links the failing source",
			queue: QueueSourceSync,
			row:   problemRow{SourceID: &sourceID},
			// Navigation to the source page: it carries the error and the
			// trigger; the panel posts that trigger from the row itself.
			wantKind: ActionOpen,
			wantHref: "/data-sources/src-1",
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

// TestClassifyRejectedOAuthClientBlamesTheApp: a rejected *client* must not wear
// the dead-grant copy. Both are permission problems, but the connector's message
// also contains "reconnect the source" as its second step, so without a rule of
// its own this shape fell into the grant branch and told the user to reconnect a
// source whose client is gone — a loop, because reconnecting cannot fix it
// (#2407).
func TestClassifyRejectedOAuthClientBlamesTheApp(t *testing.T) {
	got := Classify(QueueSourceSync, "enumerate source: the OAuth app configured for this Google Drive source is no longer valid, so it cannot refresh access; an administrator must restore or replace the data-source OAuth client in the Google Cloud project, then reconnect the source")
	if got.Class != ClassPermission {
		t.Fatalf("class = %q, want %q", got.Class, ClassPermission)
	}
	if got.Retryable {
		t.Fatal("retryable = true: a rejected OAuth client cannot be recovered by retrying")
	}
	if !strings.Contains(got.Summary, "OAuth app") {
		t.Fatalf("summary = %q, want it to name the OAuth app", got.Summary)
	}
	if !strings.Contains(got.Remediation, "administrator") {
		t.Fatalf("remediation = %q, want it to name the owner of the fix", got.Remediation)
	}
	if strings.Contains(got.Remediation, "Retry") {
		t.Fatalf("remediation = %q, want no retry offered for a terminal failure", got.Remediation)
	}
}

// TestClassifyDiscoveryNoTypesIsInput pins the follow-up to the empty-discovery
// fix: a complete answer that found nothing is user action, not a retryable
// unknown error.
func TestClassifyDiscoveryNoTypesIsInput(t *testing.T) {
	got := Classify(QueueDiscovery, "no entity types were discovered in these documents; add documents with substantive entities or choose a different model")
	if got.Class != ClassInput {
		t.Fatalf("class = %q, want %q", got.Class, ClassInput)
	}
	if got.Retryable {
		t.Fatal("retryable = true: retrying the same documents will find nothing")
	}
	if !strings.Contains(got.Remediation, "documents") {
		t.Fatalf("remediation should name the documents: %q", got.Remediation)
	}
}
