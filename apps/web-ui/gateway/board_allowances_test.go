package main

import (
	"slices"
	"strings"
	"testing"
)

// A board matrix's lanes are its own type's statuses, but a status write is
// validated against the item's type. A card must therefore carry the set its type
// accepts, so the drag affordance can leave out a lane its type would refuse
// (#2470). These tests pin the derivation of that set and the attribute that
// carries it.

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

// allowedJSON is what data-board-allowed carries: the set as JSON for app.js, and
// the empty string for an unconstrained type (so the attribute stays present but
// parses to null, which leaves every lane a target).
func TestBoardAllowancesAllowedJSON(t *testing.T) {
	allowances := boardAllowances{"Task": {"ready", "review"}}
	if got, want := allowances.allowedJSON("Task"), `["ready","review"]`; got != want {
		t.Errorf("allowedJSON(Task) = %s, want %s", got, want)
	}
	if got := allowances.allowedJSON("Suggestion"); got != "" {
		t.Errorf("allowedJSON(unconstrained) = %q, want empty", got)
	}
	if got := (boardAllowances(nil)).allowedJSON("Task"); got != "" {
		t.Errorf("allowedJSON with no allowances = %q, want empty", got)
	}
}

// The card tells the client which statuses its own type accepts, so app.js can
// leave out a lane that exists for another type. A card whose type declares
// nothing carries an empty set and keeps every lane as a target.
func TestWorkCardCarriesItsTypesAllowedStatuses(t *testing.T) {
	allowances := boardAllowances{"Task": {"ready", "review"}}

	card := func(item WorkItem) string {
		return renderHTML(t, workCard(item, workData{Allowances: allowances}))
	}

	constrained := card(WorkItem{CanonicalID: "w1", Type: "Task", Key: "t", Status: "ready"})
	if !strings.Contains(constrained, `data-board-allowed="[&#34;ready&#34;,&#34;review&#34;]"`) {
		t.Errorf("card of a constrained type must carry its allowed statuses as JSON:\n%s", constrained)
	}

	for name, item := range map[string]WorkItem{
		"unconstrained type": {CanonicalID: "w2", Type: "Suggestion", Key: "s", Status: "suggested"},
		"no type":            {CanonicalID: "w3", Key: "k", Status: "ready"},
	} {
		if html := card(item); !strings.Contains(html, `data-board-allowed=""`) {
			t.Errorf("%s: card must carry an empty allowed set:\n%s", name, html)
		}
	}
}
