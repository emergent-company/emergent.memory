package discoveryjobs

import (
	"strings"
	"testing"
)

// TestTypeDiscoveryPromptTreatsAttributesAsProperties pins that the type-discovery
// prompt tells the model to model attributes (occupation, gender, age, …) as
// properties of the owning entity rather than inventing separate types like
// "Occupation".
func TestTypeDiscoveryPromptTreatsAttributesAsProperties(t *testing.T) {
	svc := &Service{log: quietLogger()}
	prompt := svc.buildTypeDiscoveryPrompt(nil, "test purpose", 10, nil)
	for _, want := range []string{
		"PROPERTY on that entity",
		"occupation/job title",
		`NOT separate "Occupation"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("type-discovery prompt missing %q", want)
		}
	}
}
