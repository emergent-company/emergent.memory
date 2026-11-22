package chat

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testObjectContext() *ObjectContext {
	return &ObjectContext{
		CanonicalID: uuid.MustParse("87b8eaf0-dc45-48e8-b2dd-836fa4acbc26"),
		VersionID:   uuid.MustParse("814fa8be-b371-4795-b88f-dc5b7ed2ca3d"),
		Key:         "boiler-service-parts",
		Name:        "Boiler service — confirm parts availability",
		Type:        "Task",
		Version:     2,
		Status:      "ready",
		Properties: map[string]any{
			"title":       "Boiler service — confirm parts availability",
			"status":      "ready", // custom property duplicating the workflow status
			"priority":    "high",
			"due_date":    "2026-12-12",
			"description": "The engineer flagged a failing diverter valve last year; check stock before the visit.",
		},
		UpdatedAt: time.Date(2026, 11, 19, 23, 34, 0, 0, time.UTC),
	}
}

// TestRenderObjectContext pins the shape of the appendix that carries an object
// into the agent's system prompt: both ids, the workflow-status column, and a
// property list that does not restate identity fields.
func TestRenderObjectContext(t *testing.T) {
	out := renderObjectContext(testObjectContext())

	assert.Contains(t, out, "## Object in context")
	assert.Contains(t, out, "re-read before every turn")
	assert.Contains(t, out, "data, not instruction")

	assert.Contains(t, out, "- Label: Boiler service — confirm parts availability")
	assert.Contains(t, out, "- Type: Task")
	assert.Contains(t, out, "- Key: boiler-service-parts")
	assert.Contains(t, out, "- Workflow status: ready")
	assert.Contains(t, out, "- Version: 2")
	assert.Contains(t, out, "- Canonical ID: 87b8eaf0-dc45-48e8-b2dd-836fa4acbc26")
	assert.Contains(t, out, "- Current version ID: 814fa8be-b371-4795-b88f-dc5b7ed2ca3d")
	assert.Contains(t, out, "- Updated: 2026-11-19 23:34 UTC")

	// Facts, not a property dump: title repeats the label, and a custom `status`
	// identical to the workflow status is not rendered twice.
	assert.Contains(t, out, "- description: The engineer flagged a failing diverter valve")
	assert.Contains(t, out, "- due_date: 2026-12-12")
	assert.Contains(t, out, "- priority: high")
	assert.NotContains(t, out, "- title:")
	assert.NotContains(t, out, "- status: ready")

	// Sorted by key so the same object always renders identically.
	desc := strings.Index(out, "- description:")
	due := strings.Index(out, "- due_date:")
	prio := strings.Index(out, "- priority:")
	require.True(t, desc < due && due < prio, "properties must be sorted: %d %d %d", desc, due, prio)

	// Deterministic output.
	assert.Equal(t, out, renderObjectContext(testObjectContext()))
}

// TestRenderObjectContextEmpty pins the no-object case: a plain chat must not
// gain an empty section.
func TestRenderObjectContextEmpty(t *testing.T) {
	assert.Empty(t, renderObjectContext(nil))
}

// TestRenderObjectContextKeepsCustomStatus pins that properties.status is not
// dropped when it disagrees with the workflow-status column: it is custom
// per-type data nothing may depend on, so it is shown as a property.
func TestRenderObjectContextKeepsCustomStatus(t *testing.T) {
	oc := testObjectContext()
	oc.Properties["status"] = "awaiting-parts"
	out := renderObjectContext(oc)

	assert.Contains(t, out, "- Workflow status: ready")
	assert.Contains(t, out, "- status: awaiting-parts")
}

// TestRenderObjectContextBounds pins the budget: a pathological object cannot
// push the appendix past a few KB.
func TestRenderObjectContextBounds(t *testing.T) {
	oc := testObjectContext()
	oc.Properties = map[string]any{}
	long := strings.Repeat("x", 5000)
	for i := 0; i < 40; i++ {
		oc.Properties[string(rune('a'+i%26))+string(rune('a'+i/26))] = long
	}

	out := renderObjectContext(oc)
	assert.Less(t, len(out), objectContextMaxBytes+600, "appendix must stay bounded")
	assert.Contains(t, out, "omitted")
}

// TestObjectPropertyValueTruncatesByRune pins that a multi-byte value sliced at
// the bound stays valid UTF-8: the appendix goes into a system prompt, and a
// half-cut rune there is corruption, not truncation.
func TestObjectPropertyValueTruncatesByRune(t *testing.T) {
	// 3-byte runes: a byte slice at 200 lands mid-rune, a rune slice does not.
	value := strings.Repeat("→", objectContextMaxPropertyValue+50)

	out, ok := objectPropertyValue(value)
	assert.True(t, ok)
	assert.Contains(t, out, "…")
	assert.True(t, utf8.ValidString(out), "truncated value must be valid UTF-8")
	assert.LessOrEqual(t, utf8.RuneCountInString(out), objectContextMaxPropertyValue+1)
}

// TestObjectPropertyValueNumbers pins that a number renders as the number the
// object carries, not its %f form (2.5, not 2.500000).
func TestObjectPropertyValueNumbers(t *testing.T) {
	out, ok := objectPropertyValue(2.5)
	assert.True(t, ok)
	assert.Equal(t, "2.5", out)

	out, ok = objectPropertyValue(3.0)
	assert.True(t, ok)
	assert.Equal(t, "3", out)
}
