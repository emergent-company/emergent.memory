package cmd

import (
	"testing"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// TestSessionTitleUsesNameColumn pins the CLI session read path to the `name`
// column. properties.title is what pre-#824 servers wrote; reading it kept a
// second title source alive, so a title that only exists there must print
// nothing (migration 00311 moves legacy values into the column).
func TestSessionTitleUsesNameColumn(t *testing.T) {
	name := "Q1 release notes"
	if got := sessionTitle(&sdkgraph.GraphObject{Name: &name, Properties: map[string]any{"title": "stale property"}}); got != name {
		t.Errorf("sessionTitle = %q, want the name column %q", got, name)
	}
	if got := sessionTitle(&sdkgraph.GraphObject{Properties: map[string]any{"title": "stale property"}}); got != "" {
		t.Errorf("sessionTitle = %q, want empty: properties.title is not a title", got)
	}
	if got := sessionTitle(nil); got != "" {
		t.Errorf("sessionTitle(nil) = %q, want empty", got)
	}
}
