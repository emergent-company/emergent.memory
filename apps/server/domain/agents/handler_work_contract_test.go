package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestToWorkItemDTO_JSONContract pins the wire names of the fields the board
// card reads. The card's identity block (title, `#ref`, excerpt) is fed from
// these three keys, so a renamed or dropped JSON tag degrades the card silently:
// the Go structs still compile, every other test still passes, and the board
// quietly goes back to rendering the machine key. Assert the marshalled keys,
// not the struct fields.
func TestToWorkItemDTO_JSONContract(t *testing.T) {
	name := "Add rate limiting to the public API"
	it := &graph.WorkItem{
		ProjectID:   "p1",
		CanonicalID: "9cc039e3-8ea3-42f0-a43d-2dda1a9f7ae9",
		Type:        "GitHubIssue",
		Name:        &name,
		Ref:         "#42f0a43d2dda",
		Key:         "github:357590e8-6cbf-46bb-8c29-d8f839dd4ba8:emergent-company/emergent.memory#772",
		Properties:  map[string]any{"body": "The token bucket is exhausted under burst traffic."},
	}

	raw, err := json.Marshal(toWorkItemDTO(it, true))
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	require.Equal(t, name, obj["name"])
	require.Equal(t, "#42f0a43d2dda", obj["ref"])
	props, ok := obj["properties"].(map[string]any)
	require.True(t, ok, "properties must be an object, got %T", obj["properties"])
	require.Equal(t, "The token bucket is exhausted under burst traffic.", props["body"])

	// A nameless object must still emit `name` and `ref` (empty name, present ref)
	// rather than omitting them: the card falls back on "", it does not decode a
	// missing key into a different meaning.
	nameless := &graph.WorkItem{CanonicalID: it.CanonicalID, Type: it.Type, Ref: it.Ref}
	raw, err = json.Marshal(toWorkItemDTO(nameless, false))
	require.NoError(t, err)
	obj = map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &obj))
	require.Equal(t, "", obj["name"])
	require.Equal(t, "#42f0a43d2dda", obj["ref"])

	// includeProperties=false must keep the bag off the wire: the 1000-item
	// run→item joins discard it, and a body of text per item is real width.
	_, ok = obj["properties"]
	require.False(t, ok, "properties must be omitted unless explicitly requested")
}
