package graph

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// dateLayout is the canonical storage shape of a `date` property: a plain
// calendar date with no time and no zone. `datetime` properties keep RFC 3339.
const dateLayout = "2006-01-02"

// acceptedDateLayouts are the shapes a `date` property may arrive in. RFC 3339
// values are accepted because a value previously stamped by this service
// ("2026-12-01T00:00:00Z") is otherwise indistinguishable from a datetime and
// must keep round-tripping into the canonical date shape (issue #402).
var acceptedDateLayouts = []string{
	time.RFC3339Nano, // accepts the no-fraction RFC 3339 form too
	dateLayout,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"01/02/2006",
	"02-01-2006",
}

// isDateProperty reports whether a property definition declares a calendar
// date. Pack authors write it both as `type: date` and as the JSON-Schema
// spelling `type: string, format: date`; both must normalise identically.
func isDateProperty(def agents.PropertyDef) bool {
	if strings.EqualFold(strings.TrimSpace(def.Type), "date") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(def.Type), "string") &&
		strings.EqualFold(strings.TrimSpace(def.Format), "date")
}

// normaliseDateValue renders a date-like value as YYYY-MM-DD. The bool is false
// when the value is not a recognised date shape.
//
// The calendar date is taken in the location the value was written in, so
// "2026-12-01T00:00:00-05:00" stays 2026-12-01 rather than sliding to the next
// day in UTC. A time component is dropped: a `date` is a day, not an instant.
func normaliseDateValue(value any) (string, bool) {
	switch v := value.(type) {
	case time.Time:
		return v.Format(dateLayout), true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", false
		}
		for _, layout := range acceptedDateLayouts {
			t, err := time.Parse(layout, trimmed)
			if err == nil {
				return t.Format(dateLayout), true
			}
		}
		return "", false
	default:
		return "", false
	}
}

// normaliseDateProperties rewrites every date-typed property present in props
// to YYYY-MM-DD, in place, and returns props. Values that do not parse are left
// untouched so a pre-existing malformed row is surfaced rather than silently
// rewritten. Unknown types and properties pass through unchanged.
func normaliseDateProperties(props map[string]any, schema agents.ObjectSchema) map[string]any {
	if len(props) == 0 || len(schema.Properties) == 0 {
		return props
	}
	for key, value := range props {
		def, ok := schema.Properties[key]
		if !ok {
			continue
		}
		switch {
		case isDateProperty(def):
			if normalised, ok := normaliseDateValue(value); ok {
				props[key] = normalised
			}
		case strings.EqualFold(strings.TrimSpace(def.Type), "array") && def.Items != nil && isDateProperty(*def.Items):
			// Write-time coercion already reduces array elements to calendar
			// dates; normalise pre-existing rows so one array cannot hold both
			// shapes (issue #402).
			switch elements := value.(type) {
			case []any:
				// Copy rather than mutate: the caller's value may alias a cached
				// model or a request body.
				normalised := make([]any, len(elements))
				copy(normalised, elements)
				changed := false
				for i, element := range normalised {
					if date, ok := normaliseDateValue(element); ok && date != element {
						normalised[i] = date
						changed = true
					}
				}
				if changed {
					props[key] = normalised
				}
			case []string:
				// In-memory props (e.g. a Go-built seed default) may hold a typed
				// slice; convert only when a date actually changes.
				normalised := make([]any, len(elements))
				changed := false
				for i, element := range elements {
					normalised[i] = element
					if date, ok := normaliseDateValue(element); ok && date != element {
						normalised[i] = date
						changed = true
					}
				}
				if changed {
					props[key] = normalised
				}
			}
		}
	}
	return props
}

// NormaliseRelationshipDatePropertyValues is the relationship analogue of
// NormaliseDatePropertyValues: date-typed edge properties converge to
// YYYY-MM-DD on read. An endpoint type may be empty when the caller does not
// know it; relationshipSchemaFor then falls back to the less specific schema.
func (s *Service) NormaliseRelationshipDatePropertyValues(ctx context.Context, projectID uuid.UUID, relType, srcType, dstType string, props map[string]any) map[string]any {
	if s == nil || len(props) == 0 || s.schemaProvider == nil || relType == "" {
		return props
	}
	schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
	if err != nil || schemas == nil || schemas.RelationshipSchemas == nil {
		return props
	}
	relSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, relType, srcType, dstType)
	if !ok || len(relSchema.Properties) == 0 {
		return props
	}
	objSchema := agents.ObjectSchema{Properties: relSchema.Properties}
	if !schemaHasDateProperties(objSchema) {
		return props
	}
	return normaliseDateProperties(copyProperties(props), objSchema)
}

// NormaliseDatePropertyValues returns props with date-typed values normalised
// to YYYY-MM-DD using the project schema for objType. It is the read-side
// counterpart to write-time coercion: rows written before the write path was
// fixed still hold RFC 3339 dates, and converge the first time they are read.
//
// A nil schema provider, an unknown type, or a schema load failure leaves props
// untouched — read normalisation is best-effort and must never make a read fail.
func (s *Service) NormaliseDatePropertyValues(ctx context.Context, projectID uuid.UUID, objType string, props map[string]any) map[string]any {
	if s == nil || len(props) == 0 || s.schemaProvider == nil || objType == "" {
		return props
	}
	schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
	if err != nil || schemas == nil || schemas.ObjectSchemas == nil {
		return props
	}
	schema, ok := schemas.ObjectSchemas[objType]
	if !ok || !schemaHasDateProperties(schema) {
		return props
	}
	return normaliseDateProperties(copyProperties(props), schema)
}

// normaliseRelationshipResponseDates normalises the properties of a page of
// relationship responses in a single schema load. endpointTypes optionally maps
// an endpoint's canonical id to its object type; when an endpoint is known the
// type-specific relationship schema is used. Without it only the
// endpoint-agnostic schema for the relationship type can match, which is a
// narrower guarantee — callers that already hold an endpoint (GetEdges,
// get-entity-edges) should pass its type.
func (s *Service) normaliseRelationshipResponseDates(ctx context.Context, projectID uuid.UUID, rels []*GraphRelationshipResponse, endpointTypes map[uuid.UUID]string) {
	if s == nil || len(rels) == 0 || s.schemaProvider == nil {
		return
	}
	schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
	if err != nil || schemas == nil || schemas.RelationshipSchemas == nil {
		return
	}
	for _, rel := range rels {
		if rel == nil || len(rel.Properties) == 0 || rel.Type == "" {
			continue
		}
		srcType, dstType := endpointTypes[rel.SrcID], endpointTypes[rel.DstID]
		relSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, rel.Type, srcType, dstType)
		if !ok || len(relSchema.Properties) == 0 {
			continue
		}
		objSchema := agents.ObjectSchema{Properties: relSchema.Properties}
		if !schemaHasDateProperties(objSchema) {
			continue
		}
		rel.Properties = normaliseDateProperties(copyProperties(rel.Properties), objSchema)
	}
}

// schemaHasDateProperties is a cheap fast path so date-free types do not pay
// for a copy on every read.
func schemaHasDateProperties(schema agents.ObjectSchema) bool {
	for _, def := range schema.Properties {
		if isDateProperty(def) || (strings.EqualFold(strings.TrimSpace(def.Type), "array") && def.Items != nil && isDateProperty(*def.Items)) {
			return true
		}
	}
	return false
}

// copyProperties shallow-copies a property map so read normalisation never
// mutates a model that a response aliases.
func copyProperties(props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
	}
	return out
}
