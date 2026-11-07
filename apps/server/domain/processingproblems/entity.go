// Package processingproblems builds a project-scoped view of work that stopped
// at a terminal failure: object extraction, document parsing, embeddings,
// source sync, email delivery, agent runs, discovery, blueprint updates and
// schema migrations.
//
// It is a read model. Each queue keeps its own tables and retry semantics; this
// package only answers "what is currently broken in this project, why, and what
// can the user do about it". Grouping every queue behind one shape is what lets
// the UI show a single attention panel instead of one bespoke failure view per
// pipeline.
package processingproblems

import "time"

// Area is the user-facing bucket a problem belongs to. The Documents page
// panel groups rows by area.
type Area string

const (
	AreaDocuments Area = "documents"
	AreaGraph     Area = "graph"
	AreaSources   Area = "sources"
	AreaEmail     Area = "email"
	AreaAgents    Area = "agents"
	AreaSchema    Area = "schema"
)

// AreaLabel is the display name of an area.
func (a Area) Label() string {
	switch a {
	case AreaDocuments:
		return "Documents"
	case AreaGraph:
		return "Knowledge graph"
	case AreaSources:
		return "Data sources"
	case AreaEmail:
		return "Email delivery"
	case AreaAgents:
		return "Agents"
	case AreaSchema:
		return "Schema"
	default:
		return string(a)
	}
}

// Queue identifiers, one per job table. They are stable because clients may
// filter or deep-link on them.
const (
	QueueObjectExtraction           = "object_extraction"
	QueueDocumentParsing            = "document_parsing"
	QueueChunkEmbedding             = "chunk_embedding"
	QueueGraphEmbedding             = "graph_embedding"
	QueueGraphRelationshipEmbedding = "graph_relationship_embedding"
	QueueSourceSync                 = "source_sync"
	QueueEmail                      = "email"
	QueueDiscovery                  = "discovery"
	QueueAgentRun                   = "agent_run"
	QueueBlueprintUpdate            = "blueprint_update"
	QueueSchemaMigration            = "schema_migration"
)

// areaOf maps a queue to the area it affects.
func areaOf(queue string) Area {
	switch queue {
	case QueueObjectExtraction, QueueDocumentParsing, QueueChunkEmbedding:
		return AreaDocuments
	case QueueGraphEmbedding, QueueGraphRelationshipEmbedding:
		return AreaGraph
	case QueueSourceSync:
		return AreaSources
	case QueueEmail:
		return AreaEmail
	case QueueAgentRun:
		return AreaAgents
	default:
		return AreaSchema
	}
}

// FailureClass is a coarse, stable cause that the UI can group and explain.
// It is deliberately not the provider's error text: summaries and remediation
// are attached per class so every client says the same thing.
type FailureClass string

const (
	ClassConfiguration FailureClass = "configuration"
	ClassProvider      FailureClass = "provider"
	ClassInvalidOutput FailureClass = "invalid_output"
	ClassInput         FailureClass = "input"
	ClassPermission    FailureClass = "permission"
	ClassConflict      FailureClass = "conflict"
	ClassLimit         FailureClass = "limit"
	ClassInternal      FailureClass = "internal"
	ClassUnknown       FailureClass = "unknown"
)

// Failure describes one classified terminal error.
type Failure struct {
	Class       FailureClass `json:"class"`
	Summary     string       `json:"summary"`
	Remediation string       `json:"remediation"`
	// Retryable means retrying the unit is worth offering. A deterministic
	// failure (missing model, unreadable document) stays false until the user
	// fixes the cause; the UI still shows the remediation.
	Retryable bool `json:"retryable"`
	// Cause narrows a class to the element that failed, when the classifier can
	// tell. It is empty for classes the copy already describes exactly.
	Cause FailureCause `json:"cause,omitempty"`
	// Model is the model this project resolves to for the failure's family
	// (routed "instance/model"): the generative model, or the embedding model
	// for an embedding queue. Empty when nothing resolves or the class is
	// unrelated.
	Model string `json:"model,omitempty"`
	// ModelType is the family Model belongs to ("generative" or "embedding").
	// The server owns the queue → family mapping, so clients use this instead
	// of inferring it from the queue name; a second copy of that map is how the
	// two drift apart.
	ModelType string `json:"modelType,omitempty"`
	// Verified reports whether the copy was checked against the project's
	// effective model configuration before being written. A configuration
	// failure with Verified=false is a guess and must read as one.
	Verified bool `json:"verified"`
	// Fix is the page where the recorded cause is fixed, when the product knows
	// one. It is nil for causes with no single destination.
	Fix *FixTarget `json:"fix,omitempty"`
}

// FailureCause is the element a failure class points at. A configuration class
// without a cause is an unverified guess; the UI must not present it as fact.
type FailureCause string

const (
	// CauseNoProvider means no provider instance is configured for the project.
	CauseNoProvider FailureCause = "no_provider"
	// CauseNoDefaultModel means a provider is connected but no generative model
	// is selected for the project.
	CauseNoDefaultModel FailureCause = "no_default_model"
	// CauseModelUnusable means a generative model resolves, but the run could
	// not use it. This is the state a "configure a model" message gets wrong
	// most damagingly: the user's provider page already looks correct.
	CauseModelUnusable FailureCause = "model_unusable"
	// CauseModelUnavailable means the provider does not offer the configured
	// model name.
	CauseModelUnavailable FailureCause = "model_unavailable"
	// CauseBaseURL means the configured provider endpoint is not usable.
	CauseBaseURL FailureCause = "base_url"
	// CauseCredential means the provider rejected or was missing the
	// credential. On a queue that needs a model this is a model problem: the
	// row can name the model and offer a live test.
	CauseCredential FailureCause = "credential"
	// CauseResourceAccess means the run was refused access to a document or
	// other resource, not to the provider. The provider may be perfectly
	// configured, so naming a model and offering to test it would point at the
	// wrong fix.
	CauseResourceAccess FailureCause = "resource_access"
)

// FixTarget is a page that fixes one recorded cause, plus the link text.
type FixTarget struct {
	Href  string `json:"href"`
	Label string `json:"label"`
}

// ActionKind describes the operation behind an action button. Only kinds a
// client actually implements appear here: a descriptor that promises an
// operation nobody performs is worse than plain navigation.
type ActionKind string

const (
	ActionRetryExtraction ActionKind = "retry_extraction"
	// ActionRetryConversion re-runs one document's failed conversion. It is a
	// separate kind from ActionRetryExtraction because the two queues have
	// different retry contracts (a conversion resets the job row; an extraction
	// starts a new linked run).
	ActionRetryConversion ActionKind = "retry_conversion"
	ActionRetryEmbeddings ActionKind = "retry_embeddings"
	ActionOpen            ActionKind = "open"
)

// Action is a server-described next step. The gateway decides whether the
// caller's role permits it; it never infers the action from queue names.
type Action struct {
	Kind ActionKind `json:"kind"`
	// Label is the button text, e.g. "Retry extraction".
	Label string `json:"label"`
	// Href is the page the action applies to (or should navigate to).
	Href string `json:"href,omitempty"`
	// RequiresRole is the minimum project role ("project_user" or
	// "project_admin"). Empty means any member.
	RequiresRole string `json:"requiresRole,omitempty"`
}

// Problem is one unit whose latest attempt is a terminal failure.
type Problem struct {
	Queue        string    `json:"queue"`
	Area         Area      `json:"area"`
	JobID        string    `json:"jobId"`
	UnitID       string    `json:"unitId"`
	Label        string    `json:"label"`
	DocumentID   string    `json:"documentId,omitempty"`
	SourceID     string    `json:"sourceId,omitempty"`
	Status       string    `json:"status"`
	Attempts     int       `json:"attempts"`
	MaxAttempts  int       `json:"maxAttempts"`
	Failure      Failure   `json:"failure"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	OccurredAt   time.Time `json:"occurredAt"`
	Action       Action    `json:"action"`
	Drilldown    string    `json:"drilldownHref,omitempty"`
}

// ClassCount is the number of problems in an area with one failure class.
type ClassCount struct {
	Class FailureClass `json:"class"`
	Count int          `json:"count"`
}

// AreaSummary is one group in the attention panel.
type AreaSummary struct {
	Area  Area   `json:"area"`
	Label string `json:"label"`
	// Count is the exact number of problems in this area, not the number of
	// rows carried in Problems: a queue returns at most problemRowLimit rows.
	Count int `json:"count"`
	// Truncated reports that Count is larger than len(Problems), so a client
	// must render the number as a lower bound ("200 of 264") instead of
	// presenting the page size as the backlog.
	Truncated bool `json:"truncated"`
	// Summary is the one-line cause breakdown, e.g.
	// "Invalid model output 4 · Internal error 3". It is computed over the
	// returned rows, so it explains the visible sample when Truncated is true.
	Summary  string       `json:"summary"`
	ByClass  []ClassCount `json:"byClass"`
	Problems []Problem    `json:"problems"`
}

// Report is the project's current processing health.
type Report struct {
	ProjectID string `json:"projectId"`
	// Total is the exact number of problems across every area. It is a sum of
	// per-area counts, so it is not capped by the per-queue row limit.
	Total int           `json:"total"`
	Areas []AreaSummary `json:"areas"`
	// GeneratedAt lets the UI show staleness for a cached response.
	GeneratedAt time.Time `json:"generatedAt"`
}

// problemRow is the uniform row each queue query projects into. TotalRows is
// the number of matching units before the query's LIMIT (a window-function
// count), shared by every row of the same queue.
type problemRow struct {
	Queue        string    `bun:"queue"`
	JobID        string    `bun:"job_id"`
	UnitID       string    `bun:"unit_id"`
	Label        string    `bun:"label"`
	DocumentID   *string   `bun:"document_id"`
	SourceID     *string   `bun:"source_id"`
	Status       string    `bun:"status"`
	Attempts     int       `bun:"attempts"`
	MaxAttempts  int       `bun:"max_attempts"`
	ErrorMessage *string   `bun:"error_message"`
	OccurredAt   time.Time `bun:"occurred_at"`
	TotalRows    int       `bun:"total_rows"`
}
