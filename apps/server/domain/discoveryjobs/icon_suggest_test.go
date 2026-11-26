package discoveryjobs

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaicon"
)

// TestSuggestedIconsAreRenderable pins that the heuristic icon suggester only
// emits names in the gateway's closed catalog; an outside name renders as the
// generic box, so a suggestion that cannot render is worse than "circle".
func TestSuggestedIconsAreRenderable(t *testing.T) {
	s := &Service{}
	for _, name := range []string{
		"Decision", "Requirement", "Task", "Issue", "Risk", "Person", "User",
		"Team", "Group", "Component", "System", "Document", "Meeting", "Widget",
	} {
		if icon := s.suggestIconForType(name); !schemaicon.IsRenderable(icon) {
			t.Errorf("suggestIconForType(%q) = %q, which is not renderable", name, icon)
		}
	}
}
