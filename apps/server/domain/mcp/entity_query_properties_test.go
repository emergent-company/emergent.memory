package mcp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteQueryEntities_IncludeProperties is the #400 regression: entity-query
// projected `'{}'::jsonb` for every row unless the caller spelled
// field_strategy="full", and it did not recognize include_properties at all —
// the parameter was reported as "unrecognized ... ignored". A caller that asks
// for properties (include_properties=true or an explicit fields list) must get
// them; the default stays the compact projection that keeps a high-limit
// entity-query bounded (issue #1069).
func TestExecuteQueryEntities_IncludeProperties(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	insertQueryEntity(t, db, projectID, "booking/exim-764025750", map[string]any{
		"name":        "Booking EXIM",
		"status":      "confirmed",
		"guest_email": "guest@example.com",
	})

	t.Run("include_properties=true returns the properties blob", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name":          "LegalParagraph",
			"include_properties": true,
		})
		require.Len(t, out.Entities, 1)
		props := out.Entities[0].Properties
		require.NotEmpty(t, props, "include_properties=true must return properties")
		assert.Equal(t, "confirmed", props["status"])
		assert.Equal(t, "Booking EXIM", props["name"])
		assert.Equal(t, "guest@example.com", props["guest_email"])
		assert.NotContains(t, out.Warning, "include_properties",
			"include_properties must be a recognized parameter, not an ignored one")
	})

	t.Run("fields alone returns the projected properties", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name": "LegalParagraph",
			"fields":    []any{"status"},
		})
		require.Len(t, out.Entities, 1)
		props := out.Entities[0].Properties
		require.NotEmpty(t, props, "an explicit fields list must return properties")
		assert.Equal(t, "confirmed", props["status"])
		assert.Equal(t, "Booking EXIM", props["name"], "name is always kept for display")
		assert.NotContains(t, props, "guest_email", "unrequested fields must not be projected")
	})

	t.Run("include_properties=false keeps the compact default", func(t *testing.T) {
		out := runEntityQuery(t, svc, projectID, map[string]any{
			"type_name":          "LegalParagraph",
			"include_properties": false,
		})
		require.Len(t, out.Entities, 1)
		assert.Empty(t, out.Entities[0].Properties,
			"an explicit include_properties=false must keep the bounded compact projection")
	})
}

// TestExecuteQueryEntities_IncludePropertiesPaginationBound pins the pagination
// half of #400: turning properties on must not lift the payload bound. The full
// projection keeps the existing per-call limit cap (the same one
// field_strategy="full" has had since #1148), the cap is surfaced as a warning,
// and pagination metadata still reports the full match count.
func TestExecuteQueryEntities_IncludePropertiesPaginationBound(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db, entityQueryFullMaxLimit: 2}

	for i := 0; i < 5; i++ {
		insertQueryEntity(t, db, projectID, fmt.Sprintf("lov/props#p-%d", i),
			map[string]any{"content": "text", "chapter_id": fmt.Sprintf("kapittel-%d", i)})
	}

	out := runEntityQuery(t, svc, projectID, map[string]any{
		"type_name":          "LegalParagraph",
		"include_properties": true,
		"limit":              float64(5),
	})
	require.NotNil(t, out.Pagination)
	assert.Equal(t, 2, out.Pagination.Limit, "include_properties must honour the full-projection limit cap")
	assert.Equal(t, 5, out.Pagination.Total, "pagination must still report every matching row")
	assert.True(t, out.Pagination.HasMore)
	require.Len(t, out.Entities, 2)
	assert.NotEmpty(t, out.Entities[0].Properties)
	assert.Contains(t, out.Warning, "capped to 2")
	assert.NotContains(t, out.Warning, "field_strategy",
		"the cap warning must describe the properties payload, not name a parameter the caller did not pass")
}
