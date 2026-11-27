package extraction

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

func TestSortedObjectSchemaNames(t *testing.T) {
	got := sortedObjectSchemaNames(map[string]agents.ObjectSchema{
		"Person":       {},
		"Organization": {},
		"Location":     {},
	})
	want := []string{"Location", "Organization", "Person"}
	if len(got) != len(want) {
		t.Fatalf("sortedObjectSchemaNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedObjectSchemaNames = %v, want %v", got, want)
		}
	}
	if got := sortedObjectSchemaNames(nil); len(got) != 0 {
		t.Fatalf("sortedObjectSchemaNames(nil) = %v, want empty", got)
	}
}
