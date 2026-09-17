package components

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// renderHTML renders a component into a string for assertion.
func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(t.Context(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// assertContains fails unless every want string appears in got.
func assertContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in rendered output:\n%s", want, got)
		}
	}
}

// assertNotContains fails if any unwanted string appears in got.
func assertNotContains(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, want := range unwanted {
		if strings.Contains(got, want) {
			t.Errorf("unexpected %q in rendered output:\n%s", want, got)
		}
	}
}
