package graph

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphObjectDetailColumns is a hand-maintained list, so a column can be added
// to GraphObject (and to the table) and silently never be read: every scan into
// a GraphObject then leaves that field at its zero value. That is how `name`
// went missing — GetHeadByCanonicalID returned a nil Name, so Patch could not
// tell a no-op name write from a rename and created a version for it.
//
// This test fails when the list and the model disagree, so the next added
// column cannot repeat it.
func TestGraphObjectDetailColumnsCoverTheModel(t *testing.T) {
	model := modelColumns(t)
	assertNoDuplicates(t, "graphObjectDetailColumns", graphObjectDetailColumns)

	for _, c := range graphObjectDetailColumns {
		_, ok := model[c]
		assert.True(t, ok, "graphObjectDetailColumns lists %q, which is not a GraphObject column", c)
	}
	for c := range model {
		if _, ok := findColumn(graphObjectDetailColumns, c); !ok {
			assert.Fail(t, "GraphObject column is missing from graphObjectDetailColumns, so it is never read",
				"column: %s", c)
		}
	}
}

// The List projection is a deliberate subset (it omits merged_to_canonical_id,
// assignee, namespace, last_accessed_at, fts and delete_reason), so it cannot be
// diffed against the whole model. Every column the list DTO reports must still
// be there: name is reported, and a List row without it hands the caller an
// object with no title.
func TestGraphObjectListColumnsCarryName(t *testing.T) {
	assertNoDuplicates(t, "graphObjectListColumns", graphObjectListColumns)
	_, ok := findColumn(graphObjectListColumns, "name")
	assert.True(t, ok, "graphObjectListColumns must select name: List rows are returned as GraphObjects")
}

// graphObjectColumns is a raw-SQL list scanned positionally by scanGraphObject,
// so it is not diffable against the model either — but a missing column reads as
// nil there just the same, which is what made a search hit report no name.
func TestGraphObjectSearchColumnsCarryName(t *testing.T) {
	assertNoDuplicates(t, "graphObjectColumns", strings.Split(graphObjectColumns, ","))
	_, ok := findColumn(strings.Split(graphObjectColumns, ","), "name")
	assert.True(t, ok, "graphObjectColumns feeds scanGraphObject; a search hit must report the object's name")
}

func assertNoDuplicates(t *testing.T, name string, columns []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(columns))
	for _, c := range columns {
		c = strings.TrimSpace(c)
		_, dup := seen[c]
		require.False(t, dup, "%s lists %q twice", name, c)
		seen[c] = struct{}{}
	}
}

func findColumn(columns []string, want string) (struct{}, bool) {
	for _, c := range columns {
		if strings.TrimSpace(c) == want {
			return struct{}{}, true
		}
	}
	return struct{}{}, false
}

// modelColumns returns the persisted columns of GraphObject: every bun-tagged
// field except the table declaration, the `bun:"-"` fields (no column) and the
// scan-only computed `embedding_status`.
func modelColumns(t *testing.T) map[string]struct{} {
	t.Helper()
	computed := map[string]struct{}{
		// Selected as a ColumnExpr beside the detail columns, never stored.
		"embedding_status": {},
	}

	typ := reflect.TypeOf(GraphObject{})
	cols := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("bun")
		if tag == "" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" || strings.HasPrefix(name, "table:") {
			continue
		}
		if _, skip := computed[name]; skip {
			continue
		}
		cols[name] = struct{}{}
	}
	require.NotEmpty(t, cols)
	return cols
}
