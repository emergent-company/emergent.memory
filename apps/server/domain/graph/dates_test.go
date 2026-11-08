package graph

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// dateTestSchema declares the two shapes that must never mix: a calendar date
// and an instant.
func dateTestSchema() agents.ObjectSchema {
	return agents.ObjectSchema{
		Name: "Obligation",
		Properties: map[string]agents.PropertyDef{
			"next_due":    {Type: "date"},
			"reviewed_at": {Type: "datetime"},
			"title":       {Type: "string"},
			// JSON-Schema spelling: packs may declare a date as string+format.
			"due_on": {Type: "string", Format: "date"},
		},
	}
}

// TestCoerceToDateStoresCalendarDate is the #402 write-path regression test:
// before the fix a date value was stamped as RFC 3339 ("...T00:00:00Z"), which
// is what made MCP writes differ from seed/import writes.
func TestCoerceToDateStoresCalendarDate(t *testing.T) {
	cases := map[string]string{
		"2026-12-01":                "2026-12-01",
		"2026-12-01T00:00:00Z":      "2026-12-01",
		"2026-12-01T12:34:56Z":      "2026-12-01",
		"2026-12-01T00:00:00-05:00": "2026-12-01",
		"2026-12-01 15:04:05":       "2026-12-01",
		"2026-12-01T15:04:05":       "2026-12-01",
		"01/02/2026":                "2026-01-02",
	}
	for input, want := range cases {
		got, err := coerceToDate(input)
		require.NoError(t, err, "input %q", input)
		assert.Equal(t, want, got, "input %q must normalise to a calendar date", input)
	}

	// A time.Time value is also reduced to its calendar date.
	got, err := coerceToDate(time.Date(2026, 12, 3, 18, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, "2026-12-03", got)

	_, err = coerceToDate("not-a-date")
	require.Error(t, err)
}

// TestCoercePropertyValueRespectsDateFormat covers packs that declare a date as
// a string with format=date, and proves datetime stays RFC 3339.
func TestCoercePropertyValueRespectsDateFormat(t *testing.T) {
	got, err := coercePropertyValue("2026-12-02T00:00:00Z", agents.PropertyDef{Type: "string", Format: "date"})
	require.NoError(t, err)
	assert.Equal(t, "2026-12-02", got)

	dt, err := coercePropertyValue("2026-12-02T09:30:00+02:00", agents.PropertyDef{Type: "datetime"})
	require.NoError(t, err)
	assert.Equal(t, "2026-12-02T09:30:00+02:00", dt, "datetime must stay RFC 3339")
}

// TestNormaliseDatePropertiesConvergesMixedShapes proves read normalisation
// rewrites an RFC 3339 date left by an old write while leaving datetime and
// plain strings alone.
func TestNormaliseDatePropertiesConvergesMixedShapes(t *testing.T) {
	props := map[string]any{
		"next_due":    "2026-12-02T00:00:00Z",
		"due_on":      "2026-12-02T00:00:00Z",
		"reviewed_at": "2026-12-02T00:00:00Z",
		"title":       "Pay the fee by 2026-12-02T00:00:00Z",
	}

	got := normaliseDateProperties(props, dateTestSchema())

	assert.Equal(t, "2026-12-02", got["next_due"])
	assert.Equal(t, "2026-12-02", got["due_on"])
	assert.Equal(t, "2026-12-02T00:00:00Z", got["reviewed_at"], "datetime must not be rewritten")
	assert.Equal(t, "Pay the fee by 2026-12-02T00:00:00Z", got["title"], "free text must not be rewritten")
}

func TestNormaliseDatePropertiesLeavesUnparseableValues(t *testing.T) {
	props := map[string]any{"next_due": "sometime next week"}
	got := normaliseDateProperties(props, dateTestSchema())
	assert.Equal(t, "sometime next week", got["next_due"])
}

// TestNormaliseDatePropertiesArrayElements proves an array-of-date property left
// in the old RFC 3339 shape converges on read and the caller's slice is not
// mutated in place.
func TestNormaliseDatePropertiesArrayElements(t *testing.T) {
	schema := agents.ObjectSchema{Properties: map[string]agents.PropertyDef{
		"milestones": {Type: "array", Items: &agents.PropertyDef{Type: "date"}},
	}}
	original := []any{"2026-12-02T00:00:00Z", "2026-12-03"}
	props := map[string]any{"milestones": original}

	got := normaliseDateProperties(props, schema)

	assert.Equal(t, []any{"2026-12-02", "2026-12-03"}, got["milestones"])
	assert.Equal(t, "2026-12-02T00:00:00Z", original[0], "input slice must not be mutated")
}

// TestApplySchemaDefaultsCoercesDates proves a date default is stored in the
// canonical shape instead of escaping coercion as RFC 3339.
func TestApplySchemaDefaultsCoercesDates(t *testing.T) {
	schema := agents.ObjectSchema{Properties: map[string]agents.PropertyDef{
		"next_due": {Type: "date", Default: "2026-12-01T00:00:00Z"},
	}}
	props := map[string]any{}
	applySchemaDefaults(props, schema)
	assert.Equal(t, "2026-12-01", props["next_due"])
}

// fakeDateSchemaProvider serves one object schema without a database.
type fakeDateSchemaProvider struct {
	schemas *ExtractionSchemas
	err     error
}

func (f *fakeDateSchemaProvider) GetProjectSchemas(context.Context, string) (*ExtractionSchemas, error) {
	return f.schemas, f.err
}

func (f *fakeDateSchemaProvider) InvalidateProjectCache(string) {}

// TestNormaliseDatePropertyValuesReadPath exercises the Service helper used by
// read paths: a pre-existing mixed row converges on read, and unknown types or
// a nil provider pass through unchanged.
func TestNormaliseDatePropertyValuesReadPath(t *testing.T) {
	svc := &Service{schemaProvider: &fakeDateSchemaProvider{
		schemas: &ExtractionSchemas{ObjectSchemas: map[string]agents.ObjectSchema{
			"Obligation": dateTestSchema(),
		}},
	}}
	projectID := uuid.New()

	got := svc.NormaliseDatePropertyValues(context.Background(), projectID, "Obligation", map[string]any{
		"next_due": "2026-12-02T00:00:00Z",
	})
	assert.Equal(t, map[string]any{"next_due": "2026-12-02"}, got)

	unknown := svc.NormaliseDatePropertyValues(context.Background(), projectID, "Unknown", map[string]any{
		"next_due": "2026-12-02T00:00:00Z",
	})
	assert.Equal(t, map[string]any{"next_due": "2026-12-02T00:00:00Z"}, unknown)

	bare := &Service{}
	unchanged := bare.NormaliseDatePropertyValues(context.Background(), projectID, "Obligation", map[string]any{
		"next_due": "2026-12-02T00:00:00Z",
	})
	assert.Equal(t, map[string]any{"next_due": "2026-12-02T00:00:00Z"}, unchanged)
}

// TestChangesetWriteStoresCalendarDate covers the source-push path: it used to
// validate properties and then store the raw map, so a date pushed as RFC 3339
// stayed RFC 3339.
func TestChangesetWriteStoresCalendarDate(t *testing.T) {
	schemas := &ExtractionSchemas{ObjectSchemas: map[string]agents.ObjectSchema{
		"Obligation": dateTestSchema(),
	}}
	key := "obl-1"
	item := &ChangesetObjectItem{
		Type:       "Obligation",
		Key:        &key,
		Properties: map[string]any{"next_due": "2026-12-01T00:00:00Z"},
	}

	obj, err := buildChangesetObject(uuid.New(), nil, uuid.New(), item, schemas, "source", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-12-01", obj.Properties["next_due"])
}

// TestChangesetVersionNormalisesInheritedDates proves a source push that only
// touches an unrelated field still converges an inherited legacy date value.
func TestChangesetVersionNormalisesInheritedDates(t *testing.T) {
	schemas := &ExtractionSchemas{ObjectSchemas: map[string]agents.ObjectSchema{
		"Obligation": dateTestSchema(),
	}}
	head := &GraphObject{
		Type:       "Obligation",
		Properties: map[string]any{"next_due": "2026-12-01T00:00:00Z", "title": "old"},
	}
	item := &ChangesetObjectItem{Properties: map[string]any{
		"next_due": "2026-12-01T00:00:00Z", // legacy shape supplied by the source
		"title":    "new",
	}}

	version := buildChangesetObjectVersion(head, item, uuid.New(), schemas, "source", nil, false, nil)
	assert.Equal(t, "2026-12-01", version.Properties["next_due"])
	assert.Equal(t, "new", version.Properties["title"])
}

// TestNormaliseRelationshipDatePropertyValues proves edge properties converge
// on read using the relationship schema.
func TestNormaliseRelationshipDatePropertyValues(t *testing.T) {
	svc := &Service{schemaProvider: &fakeDateSchemaProvider{
		schemas: &ExtractionSchemas{RelationshipSchemas: map[string]agents.RelationshipSchema{
			"has_subject": {Properties: map[string]agents.PropertyDef{
				"since": {Type: "date"},
			}},
		}},
	}}
	projectID := uuid.New()

	got := svc.NormaliseRelationshipDatePropertyValues(context.Background(), projectID, "has_subject", "Obligation", "Person",
		map[string]any{"since": "2026-12-02T00:00:00Z"})
	assert.Equal(t, map[string]any{"since": "2026-12-02"}, got)

	// Unknown relationship type passes through untouched.
	unknown := svc.NormaliseRelationshipDatePropertyValues(context.Background(), projectID, "unknown_rel", "", "",
		map[string]any{"since": "2026-12-02T00:00:00Z"})
	assert.Equal(t, map[string]any{"since": "2026-12-02T00:00:00Z"}, unknown)
}
