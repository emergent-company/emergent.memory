package sources

import (
	"reflect"
	"testing"
)

// TestPlanRemovals drives the worker's removal decision in isolation. It covers
// the four behaviours the worker wires to:
//
//   - (a) legacy connectors (no EnumerationCompleteness / SourceTombstones)
//     delete absent items on a complete enumeration;
//   - (b) an incomplete enumeration deletes only tombstones;
//   - (c) a complete enumeration deletes absent items;
//   - (d) a tombstoned id is deleted even when it is still a prior HEAD and
//     absent from the current enumeration, despite the crawl being partial.
func TestPlanRemovals(t *testing.T) {
	cases := []struct {
		name       string
		headIDs    []string
		seen       map[string]bool
		complete   bool
		tombstones []string
		wantDelete []string
		wantSkip   []string
	}{
		{
			name:       "legacy connector deletes absent items on complete enumeration",
			headIDs:    []string{"a", "b", "c"},
			seen:       map[string]bool{"a": true},
			complete:   true,
			tombstones: nil,
			wantDelete: []string{"b", "c"},
			wantSkip:   nil,
		},
		{
			name:       "incomplete enumeration deletes only tombstones",
			headIDs:    []string{"a", "b", "c"},
			seen:       map[string]bool{"a": true},
			complete:   false,
			tombstones: []string{"c"},
			wantDelete: []string{"c"},
			wantSkip:   []string{"b"},
		},
		{
			name:       "complete enumeration deletes absent items",
			headIDs:    []string{"a", "b"},
			seen:       map[string]bool{"a": true},
			complete:   true,
			tombstones: nil,
			wantDelete: []string{"b"},
			wantSkip:   nil,
		},
		{
			name:       "tombstoned id still in head and absent from seen is deleted on partial crawl",
			headIDs:    []string{"a", "b"},
			seen:       map[string]bool{},
			complete:   false,
			tombstones: []string{"b"},
			wantDelete: []string{"b"},
			wantSkip:   []string{"a"},
		},
		{
			name:       "complete enumeration with no tombstones skips nothing",
			headIDs:    []string{"a", "b"},
			seen:       map[string]bool{},
			complete:   true,
			tombstones: nil,
			wantDelete: []string{"a", "b"},
			wantSkip:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			del, skip := planRemovals(tc.headIDs, tc.seen, tc.complete, tc.tombstones)
			if !reflect.DeepEqual(del, tc.wantDelete) {
				t.Errorf("planRemovals() delete = %v, want %v", del, tc.wantDelete)
			}
			if !reflect.DeepEqual(skip, tc.wantSkip) {
				t.Errorf("planRemovals() skipped = %v, want %v", skip, tc.wantSkip)
			}
		})
	}
}
