package processingproblems

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"golang.org/x/sync/errgroup"

	"github.com/emergent-company/emergent.memory/domain/modelstate"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// areaOrder is the display order of the attention panel. Documents comes first
// because that is where the panel lives.
var areaOrder = []Area{
	AreaDocuments,
	AreaGraph,
	AreaSources,
	AreaEmail,
	AreaAgents,
	AreaSchema,
}

// Service assembles the project's processing-problem report.
type Service struct {
	repo  *Repository
	model modelstate.Reader
	log   *slog.Logger
}

// NewService creates the read-model service. model resolves the project's
// effective model configuration so a configuration failure can name the element
// that is missing; it may be nil in tests, which leaves the copy unverified.
func NewService(repo *Repository, model modelstate.Reader, log *slog.Logger) *Service {
	return &Service{repo: repo, model: model, log: log.With(logger.Scope("processingproblems"))}
}

// DB exposes the database handle for handler-level membership checks.
func (s *Service) DB() bun.IDB { return s.repo.DB() }

// List returns the current terminal failures for a project, grouped by area.
//
// One queue failing to read must not blank the whole report: each query
// degrades to an empty result and is logged, so a deployment that predates a
// table still renders the queues it can answer for.
//
// Each area's Count is the exact number of matching units, taken from the
// queue's pre-LIMIT row count; Problems holds at most problemRowLimit rows per
// queue. Truncated marks the difference so the UI can say "200 of 264" instead
// of reporting the page size as the backlog.
func (s *Service) List(ctx context.Context, projectID string) (*Report, error) {
	queries := s.repo.queries()
	results := make([]queueResult, len(queries))

	g, gctx := errgroup.WithContext(ctx)
	for i, q := range queries {
		i, q := i, q
		g.Go(func() error {
			rows, err := q.run(gctx, projectID)
			if err != nil {
				s.log.Warn("failed to read processing problems for queue",
					slog.String("queue", q.name),
					slog.String("project_id", projectID),
					logger.Error(err))
				return nil
			}
			results[i] = newQueueResult(rows)
			return nil
		})
	}
	_ = g.Wait()

	// areaTotal is the exact size of each area; areaProblems holds the rows
	// actually returned for it.
	areaTotal := make(map[Area]int, len(areaOrder))
	byArea := make(map[Area][]Problem)
	for i, result := range results {
		queue := queries[i].name
		area := areaOf(queue)
		areaTotal[area] += result.total
		for _, row := range result.rows {
			problem := Problem{
				Queue:       queue,
				Area:        area,
				JobID:       row.JobID,
				UnitID:      row.UnitID,
				Label:       row.Label,
				DocumentID:  deref(row.DocumentID),
				SourceID:    deref(row.SourceID),
				Status:      row.Status,
				Attempts:    row.Attempts,
				MaxAttempts: row.MaxAttempts,
				Failure:     Classify(queue, deref(row.ErrorMessage)),
				OccurredAt:  row.OccurredAt,
				Action:      actionFor(queue, row),
				Drilldown:   drilldownFor(queue, row),
			}
			if row.ErrorMessage != nil {
				problem.ErrorMessage = *row.ErrorMessage
			}
			byArea[area] = append(byArea[area], problem)
		}
	}

	// A configuration failure's copy depends on what the project actually has
	// configured, not on the error text: the same message means "connect a
	// provider", "select a default model" or "your model is fine, the run failed
	// anyway". Resolve the model state once per report and rewrite those
	// failures. When the state cannot be read the fallback copy stays, and it
	// does not assert a cause it has not checked.
	if needsModelState(byArea) {
		if state, ok := s.modelState(ctx, projectID); ok {
			applyModelState(byArea, state)
		}
	}

	report := &Report{
		ProjectID:   projectID,
		Areas:       make([]AreaSummary, 0, len(areaOrder)),
		GeneratedAt: time.Now().UTC(),
	}

	for _, area := range areaOrder {
		problems := byArea[area]
		total := areaTotal[area]
		if total == 0 && len(problems) == 0 {
			continue
		}
		sort.SliceStable(problems, func(i, j int) bool {
			return problems[i].OccurredAt.After(problems[j].OccurredAt)
		})

		counts := make(map[FailureClass]int, len(problems))
		for _, p := range problems {
			counts[p.Failure.Class]++
		}
		byClass := make([]ClassCount, 0, len(counts))
		for class, count := range counts {
			byClass = append(byClass, ClassCount{Class: class, Count: count})
		}
		sort.SliceStable(byClass, func(i, j int) bool {
			if byClass[i].Count == byClass[j].Count {
				return byClass[i].Class < byClass[j].Class
			}
			return byClass[i].Count > byClass[j].Count
		})

		report.Areas = append(report.Areas, AreaSummary{
			Area:      area,
			Label:     area.Label(),
			Count:     total,
			Truncated: total > len(problems),
			Summary:   ClassSummary(byClass),
			ByClass:   byClass,
			Problems:  problems,
		})
		report.Total += total
	}

	return report, nil
}

// needsModelState reports whether any row depends on the project's model state:
// a configuration failure's copy, or a credential failure on a queue that needs
// a model (so the row can name it and offer a live test). A resource-access
// permission failure does not depend on it and must not pay for the read.
func needsModelState(byArea map[Area][]Problem) bool {
	for _, problems := range byArea {
		for _, p := range problems {
			if p.Failure.Class == ClassConfiguration {
				return true
			}
			if p.Failure.Class == ClassPermission &&
				p.Failure.Cause == CauseCredential && isModelQueue(p.Queue) {
				return true
			}
		}
	}
	return false
}

// modelState resolves the project's effective model configuration. A failure is
// logged and reported as unresolved so the report still renders: a metadata
// read error must not turn into a false claim about the user's configuration.
func (s *Service) modelState(ctx context.Context, projectID string) (modelstate.State, bool) {
	if s.model == nil {
		return modelstate.State{}, false
	}
	state, err := s.model.State(ctx, projectID)
	if err != nil {
		s.log.Warn("failed to resolve model state for configuration failure copy",
			slog.String("project_id", projectID),
			logger.Error(err))
		return modelstate.State{}, false
	}
	return state, true
}

// queueResult is one queue's rows plus the exact number of matching units the
// query counted before applying its row limit.
type queueResult struct {
	rows  []problemRow
	total int
}

// newQueueResult reads the shared pre-LIMIT count off the rows. Every row of a
// queue carries the same value (COUNT(*) OVER ()), so the first row is enough.
func newQueueResult(rows []problemRow) queueResult {
	if len(rows) == 0 {
		return queueResult{rows: rows}
	}
	return queueResult{rows: rows, total: rows[0].TotalRows}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// actionFor describes the next step for a queue. Queues without a safe retry
// expose navigation only: a Retry button that corrupts a schema is worse than
// no button.
func actionFor(queue string, row problemRow) Action {
	documentID := deref(row.DocumentID)

	switch queue {
	case QueueObjectExtraction:
		href := "/documents"
		if documentID != "" {
			href = "/documents/" + url.PathEscape(documentID) + "/extractions"
			if row.JobID != "" {
				// Deep-link the failed run so the user lands on its trace,
				// model, token usage, and structured error details instead of
				// the whole extraction history.
				href += "/" + url.PathEscape(row.JobID)
			}
		}
		return Action{Kind: ActionRetryExtraction, Label: "Retry extraction", Href: href}

	case QueueDocumentParsing:
		href := "/documents"
		if documentID != "" {
			href = "/documents/" + url.PathEscape(documentID)
		}
		// A conversion retry resets the failed job row, so it is a different
		// contract from extraction's "start a new linked run". The descriptor
		// carries the kind the gateway's per-row button acts on.
		return Action{Kind: ActionRetryConversion, Label: "Retry conversion", Href: href}

	case QueueChunkEmbedding, QueueGraphEmbedding:
		return Action{
			Kind:         ActionRetryEmbeddings,
			Label:        "Retry failed embeddings",
			Href:         "/embeddings",
			RequiresRole: "project_admin",
		}

	case QueueGraphRelationshipEmbedding:
		// The project retrigger covers object and chunk embeddings; relationship
		// embeddings have no project-wide reset yet. Link to the status page
		// rather than offer a button that would silently skip them.
		return Action{Kind: ActionOpen, Label: "Review embeddings", Href: "/embeddings"}

	case QueueSourceSync:
		// Deep-link the failing source, not the list: its page carries the
		// "Trigger sync" action and the job's own error. The panel's per-row
		// sync button posts to the same handler.
		href := "/data-sources"
		if sourceID := deref(row.SourceID); sourceID != "" {
			href = "/data-sources/" + url.PathEscape(sourceID)
		}
		return Action{Kind: ActionOpen, Label: "Open data source", Href: href}

	case QueueEmail:
		return Action{Kind: ActionOpen, Label: "Review members", Href: "/settings/members"}

	case QueueAgentRun:
		return Action{Kind: ActionOpen, Label: "Open sessions", Href: "/sessions"}

	case QueueBlueprintUpdate:
		return Action{Kind: ActionOpen, Label: "Open blueprints", Href: "/blueprints"}

	case QueueDiscovery:
		// Anchor the discovery card: the schema page as a whole is the compiled
		// type browser, and landing at the top left the user hunting for the
		// run this row is about.
		return Action{Kind: ActionOpen, Label: "Open discovery", Href: "/schema#discovery"}
	case QueueSchemaMigration:
		return Action{Kind: ActionOpen, Label: "Open schema", Href: "/schema"}

	default:
		return Action{Kind: ActionOpen, Label: "Open"}
	}
}

func drilldownFor(queue string, row problemRow) string {
	documentID := deref(row.DocumentID)
	base := "/documents"
	if documentID != "" {
		base = "/documents/" + url.PathEscape(documentID)
	}

	switch queue {
	case QueueObjectExtraction:
		href := base + "/extractions"
		if row.JobID != "" {
			href += "/" + url.PathEscape(row.JobID)
		}
		return href
	case QueueDocumentParsing, QueueChunkEmbedding:
		return base
	case QueueGraphEmbedding, QueueGraphRelationshipEmbedding:
		return "/embeddings"
	case QueueSourceSync:
		if sourceID := deref(row.SourceID); sourceID != "" {
			return "/data-sources/" + url.PathEscape(sourceID)
		}
		return "/data-sources"
	case QueueEmail:
		return "/settings/members"
	case QueueAgentRun:
		return "/sessions"
	case QueueBlueprintUpdate:
		return "/blueprints"
	case QueueDiscovery:
		return "/schema#discovery"
	default:
		return "/schema"
	}
}

// ClassLabel is the human name of a failure class for the panel chips.
func ClassLabel(class FailureClass) string {
	switch class {
	case ClassConfiguration:
		return "Configuration"
	case ClassProvider:
		return "Provider error"
	case ClassInvalidOutput:
		return "Invalid model output"
	case ClassInput:
		return "Unreadable input"
	case ClassPermission:
		return "Permission"
	case ClassConflict:
		return "Conflict"
	case ClassLimit:
		return "Output limit"
	case ClassInternal:
		return "Internal error"
	default:
		return "Unknown"
	}
}

// classSummary renders "Invalid model output 4 · Internal error 3" for the
// panel's one-line cause, capped to the three largest classes.
func ClassSummary(counts []ClassCount) string {
	parts := make([]string, 0, 3)
	for i, c := range counts {
		if i == 3 {
			break
		}
		parts = append(parts, ClassLabel(c.Class)+" "+itoa(c.Count))
	}
	return strings.Join(parts, " · ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
