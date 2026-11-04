package main

import (
	"slices"
	"strings"
	"testing"
)

// The board's lanes are the union of every board-enabled type's statuses, while a
// status write is validated against the item's own type. A lane can therefore
// exist for one type and always refuse another (#2470). These tests pin the two
// places that now honour the per-type set: the card's drag eligibility and the
// drawer's "Set status" menu.

func TestBoardAllowancesFromCompiled(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			// Not board-enabled: the write path does not constrain these.
			{Name: "Note", BoardEnabled: false, AllowedStatuses: []string{"x"}},
			// Board-enabled with no declaration: unconstrained.
			{Name: "Loose", BoardEnabled: true},
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"todo", "", "doing", "todo"}},
		},
	}
	got := boardAllowancesFromCompiled(compiled)
	want := boardAllowances{"Task": {"todo", "doing"}}
	if len(got) != len(want) {
		t.Fatalf("boardAllowancesFromCompiled = %v, want %v", got, want)
	}
	for name, statuses := range want {
		if !slices.Equal(got[name], statuses) {
			t.Errorf("allowances[%q] = %v, want %v", name, got[name], statuses)
		}
	}
	if _, ok := got["Note"]; ok {
		t.Error("a non-board type must not constrain a card")
	}
	if _, ok := got["Loose"]; ok {
		t.Error("a board-enabled type with no declared set must stay unconstrained")
	}
	if boardAllowancesFromCompiled(nil) != nil {
		t.Error("nil compiled must yield nil allowances")
	}
}

// A shadowed duplicate loses, exactly like the lane order: the effective winner's
// set is the one a card carries.
func TestBoardAllowancesFromCompiledSkipsShadowed(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"backlog", "shipped"}, Shadowed: true},
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"todo", "doing"}},
		},
	}
	got := boardAllowancesFromCompiled(compiled)
	if want := []string{"todo", "doing"}; !slices.Equal(got["Task"], want) {
		t.Errorf("allowances[Task] = %v, want %v (shadowed set must not leak)", got["Task"], want)
	}
}

// The card tells the client which lanes its own type accepts, so app.js can leave
// out a lane that exists for another type. A card whose type declares nothing
// carries no attribute and keeps every lane as a target.
func TestBoardCardCarriesItsTypesAllowedStatuses(t *testing.T) {
	allowances := boardAllowances{"Task": {"ready", "review"}}

	constrained := renderHTML(t, boardCard(
		WorkItem{CanonicalID: "w1", Type: "Task", Key: "t", Status: "ready"}, allowances))
	if !strings.Contains(constrained, `data-board-allowed="[&#34;ready&#34;,&#34;review&#34;]"`) {
		t.Errorf("card of a constrained type must carry its allowed statuses as JSON:\n%s", constrained)
	}

	for name, item := range map[string]WorkItem{
		"unconstrained type": {CanonicalID: "w2", Type: "Suggestion", Key: "s", Status: "suggested"},
		"no type":            {CanonicalID: "w3", Key: "k", Status: "ready"},
	} {
		html := renderHTML(t, boardCard(item, allowances))
		if strings.Contains(html, "data-board-allowed") {
			t.Errorf("%s: card must not constrain its lanes:\n%s", name, html)
		}
	}

	unconstrained := renderHTML(t, boardCard(
		WorkItem{CanonicalID: "w1", Type: "Task", Key: "t", Status: "ready"}, nil))
	if strings.Contains(unconstrained, "data-board-allowed") {
		t.Error("no allowances at all must leave every card unconstrained")
	}
}

// boardAllowances.lanes backs the drawer menu: a status the item's type refuses
// is not offered there either.
func TestBoardAllowancesLanes(t *testing.T) {
	lanes := []string{"ready", "in_progress", "review", "suggested"}
	allowances := boardAllowances{"Task": {"ready", "review"}}

	if got, want := allowances.lanes(lanes, "Task"), []string{"ready", "review"}; !slices.Equal(got, want) {
		t.Errorf("lanes(Task) = %v, want %v", got, want)
	}
	if got := allowances.lanes(lanes, "Suggestion"); !slices.Equal(got, lanes) {
		t.Errorf("lanes(unconstrained type) = %v, want every lane %v", got, lanes)
	}
	if got := allowances.lanes(nil, "Task"); len(got) != 0 {
		t.Errorf("lanes(nil) = %v, want none", got)
	}
	// A rendering gap must not empty the control: no intersection, no filter.
	if got := allowances.lanes([]string{"suggested"}, "Task"); !slices.Equal(got, []string{"suggested"}) {
		t.Errorf("lanes without intersection = %v, want the list unchanged", got)
	}
}

// The drawer's status control is the second surface that used to offer a
// cross-type status: it now receives the item's filtered lane list.
func TestBoardDrawerStatusControlHonoursTypeAllowances(t *testing.T) {
	detail := &WorkItemDetail{Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "t", Status: "ready"}}
	lanes := boardAllowances{"Task": {"ready", "review"}}.
		lanes([]string{"ready", "in_progress", "review", "suggested"}, "Task")

	html := renderHTML(t, BoardDrawer(detail, defaultBoardStatusMap(), lanes, nil, "", nil))
	if !strings.Contains(html, `name="status" value="review"`) {
		t.Errorf("drawer must offer an allowed status:\n%s", html)
	}
	for _, refused := range []string{"in_progress", "suggested"} {
		if strings.Contains(html, `name="status" value="`+refused+`"`) {
			t.Errorf("drawer offers %q, which the item's type refuses:\n%s", refused, html)
		}
	}
}
