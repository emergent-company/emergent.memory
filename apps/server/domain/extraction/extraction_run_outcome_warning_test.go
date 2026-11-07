package extraction

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// fakeSchemaProvider serves a fixed schema set so loadSchemas is testable
// without a database, mirroring the fakePreviousRunFinder pattern.
type fakeSchemaProvider struct {
	schemas *ExtractionSchemas
	err     error
}

func (f fakeSchemaProvider) GetProjectSchemas(context.Context, string) (*ExtractionSchemas, error) {
	return f.schemas, f.err
}

func (f fakeSchemaProvider) GetInstalledSchemaSummaries(context.Context, string) ([]InstalledSchemaSummary, error) {
	return nil, nil
}

func testObjectSchema(name string, skip bool) agents.ObjectSchema {
	schema := agents.ObjectSchema{Name: name}
	schema.SkipExtraction = skip
	return schema
}

// TestLoadSchemas_ReportsSkipExtractionTypesByName is the issue's case: a pack
// type that sets skipExtraction must not vanish without a trace. It is still
// excluded from extraction, but the run is told its name, sorted so two runs
// over the same schema print the same thing.
func TestLoadSchemas_ReportsSkipExtractionTypesByName(t *testing.T) {
	w := &ObjectExtractionWorker{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		schemaProvider: fakeSchemaProvider{schemas: &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"PodcastEpisode": testObjectSchema("PodcastEpisode", true),
				"Series":         testObjectSchema("Series", true),
				"Person":         testObjectSchema("Person", false),
			},
			RelationshipSchemas: map[string]agents.RelationshipSchema{},
		}},
	}

	schemas, err := w.loadSchemas(context.Background(), &ObjectExtractionJob{ID: "job-1", ProjectID: "prj"})
	require.NoError(t, err)

	require.Equal(t, []string{"PodcastEpisode", "Series"}, schemas.SkippedTypes,
		"skipped types must be reported by name, deterministically sorted")
	require.NotContains(t, schemas.ObjectSchemas, "PodcastEpisode")
	require.NotContains(t, schemas.ObjectSchemas, "Series")
	require.Contains(t, schemas.ObjectSchemas, "Person", "the skip must stay a skip, not a removal of every type")
}

// TestLoadSchemas_NoSkipLeavesSkippedTypesEmpty keeps the ordinary path quiet.
func TestLoadSchemas_NoSkipLeavesSkippedTypesEmpty(t *testing.T) {
	w := &ObjectExtractionWorker{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		schemaProvider: fakeSchemaProvider{schemas: &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"Person": testObjectSchema("Person", false),
			},
			RelationshipSchemas: map[string]agents.RelationshipSchema{},
		}},
	}

	schemas, err := w.loadSchemas(context.Background(), &ObjectExtractionJob{ID: "job-1", ProjectID: "prj"})
	require.NoError(t, err)
	require.Empty(t, schemas.SkippedTypes)
	require.Contains(t, schemas.ObjectSchemas, "Person")
}

// TestPlanRunOutcomeWarning_SortsTypes pins the deterministic order even when
// the provider hands the map over in a different order.
func TestPlanRunOutcomeWarning_SortsTypes(t *testing.T) {
	warning := planRunOutcomeWarning([]string{"Zeta", "Alpha", "Mu"}, 5)

	require.NotNil(t, warning)
	require.Equal(t, []string{"Alpha", "Mu", "Zeta"}, warning.SkippedTypes)
	require.Equal(t, "skipped_types", warning.Kind)
	require.Equal(t, "info", warning.Severity)
	require.Contains(t, warning.Message, "Alpha, Mu, Zeta")
}

// TestPlanRunOutcomeWarning_ZeroObjectsAlwaysWarns is the other half of the
// issue: a run that produced nothing must not be reported as a bare Completed,
// even when no type was skipped and even on the first run.
func TestPlanRunOutcomeWarning_ZeroObjectsAlwaysWarns(t *testing.T) {
	warning := planRunOutcomeWarning(nil, 0)

	require.NotNil(t, warning)
	require.Equal(t, "zero_objects", warning.Kind)
	require.Equal(t, "warning", warning.Severity)
	require.Contains(t, warning.Message, "0 objects")
}

// TestPlanRunOutcomeWarning_OrdinaryRunIsQuiet is the no-false-positive case.
func TestPlanRunOutcomeWarning_OrdinaryRunIsQuiet(t *testing.T) {
	require.Nil(t, planRunOutcomeWarning(nil, 1))
	require.Nil(t, planRunOutcomeWarning(nil, 235))
}

// TestApplyRunOutcomeWarning_ZeroObjectsRecordsWarningAndNotifies drives the
// completion path end to end without a model: the result carries the skip names
// in debug_info, the run carries the warning, and the outcome notification is
// emitted at warning severity instead of being swallowed by the empty-run guard.
func TestApplyRunOutcomeWarning_ZeroObjectsRecordsWarningAndNotifies(t *testing.T) {
	notifier := &fakeProjectNotifier{}
	w := testWorker(notifier)

	job := &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 0, ObjectsMerged: 0, RelationshipsCreated: 0}

	warning := w.applyRunOutcomeWarning(context.Background(), job, "", result, []string{"PodcastEpisode"})
	require.NotNil(t, warning)
	require.Same(t, warning, result.OutcomeWarning)
	require.Equal(t, "skipped_types+zero_objects", warning.Kind)
	require.Equal(t, []string{"PodcastEpisode"}, result.SkippedTypes)
	require.Contains(t, result.DebugInfo, "skipped_types")
	require.Contains(t, result.DebugInfo, "outcome_warning")

	// A staging branch is present (the worker creates one), so the normal
	// review-ready path returns false for zero objects; the completion path must
	// still announce the warning.
	branch := uuid.New()
	w.emitExtractionOutcome(context.Background(), job, &branch, result)

	require.Len(t, notifier.calls, 1)
	call := notifier.calls[0]
	require.Equal(t, "warning", call.input.Severity, "a zero-object run must not present as an ordinary info row")
	require.Equal(t, "Document extraction finished with warnings", call.input.Title)
	require.Contains(t, call.input.Message, "PodcastEpisode")
	require.Contains(t, call.input.Message, "0 objects")

	details := decodeDetails(t, call.input.Details)
	require.Equal(t, "skipped_types+zero_objects", details["outcome_warning_kind"])
	require.Equal(t, []any{"PodcastEpisode"}, details["skipped_types"])
	require.Equal(t, float64(0), details["objects_produced"])
}

// TestApplyRunOutcomeWarning_SkipWithObjectsIsNamedButStaysInfo: skipping a
// board type is intended, so a run that still produced objects stays an info
// row — but the message and debug_info must name the skipped type.
func TestApplyRunOutcomeWarning_SkipWithObjectsIsNamedButStaysInfo(t *testing.T) {
	notifier := &fakeProjectNotifier{}
	w := testWorker(notifier)

	job := &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 20, RelationshipsCreated: 3}

	warning := w.applyRunOutcomeWarning(context.Background(), job, "", result, []string{"PodcastEpisode"})
	require.NotNil(t, warning)
	require.Equal(t, "skipped_types", warning.Kind)
	require.Equal(t, "info", warning.Severity)
	require.Equal(t, []string{"PodcastEpisode"}, result.SkippedTypes)

	branch := uuid.New()
	w.emitExtractionOutcome(context.Background(), job, &branch, result)

	require.Len(t, notifier.calls, 1)
	call := notifier.calls[0]
	require.Equal(t, "info", call.input.Severity)
	require.Contains(t, call.input.Title, "1 type(s) skipped")
	require.Contains(t, call.input.Message, "PodcastEpisode")
}

// TestApplyRunOutcomeWarning_OrdinaryRunStaysQuiet: no skip and objects written
// leaves the existing notification untouched.
func TestApplyRunOutcomeWarning_OrdinaryRunStaysQuiet(t *testing.T) {
	notifier := &fakeProjectNotifier{}
	w := testWorker(notifier)

	job := &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 12, RelationshipsCreated: 4}

	require.Nil(t, w.applyRunOutcomeWarning(context.Background(), job, "", result, nil))
	require.Nil(t, result.OutcomeWarning)
	require.NotContains(t, result.DebugInfo, "outcome_warning")
	require.NotContains(t, result.DebugInfo, "skipped_types")

	branch := uuid.New()
	w.emitExtractionOutcome(context.Background(), job, &branch, result)

	require.Len(t, notifier.calls, 1)
	require.Equal(t, "info", notifier.calls[0].input.Severity)
	require.NotContains(t, notifier.calls[0].input.Message, "skipExtraction")
}

// TestWithOutcomeWarning_FoldsIntoSeverityAndDetails pins the shared fold helper
// both notification producers use, including the no-warning identity.
func TestWithOutcomeWarning_FoldsIntoSeverityAndDetails(t *testing.T) {
	detail := map[string]any{}
	message, severity := withOutcomeWarning("file.docx · 0 objects", "info", detail,
		&RunOutcomeWarning{Kind: "skipped_types+zero_objects", Objects: 0, Severity: "warning",
			SkippedTypes: []string{"PodcastEpisode"}, Message: "This run produced 0 objects."})

	require.Equal(t, "warning", severity)
	require.Contains(t, message, "file.docx")
	require.Contains(t, message, "This run produced 0 objects.")
	require.Equal(t, []string{"PodcastEpisode"}, detail["skipped_types"])

	message, severity = withOutcomeWarning("unchanged", "info", map[string]any{}, nil)
	require.Equal(t, "info", severity)
	require.Equal(t, "unchanged", message)
}
