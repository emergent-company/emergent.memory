package email

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// TestProjectInvitationTemplate_NoCLIInstructions renders the real
// project-invitation template (not a fixture: the template is data, so only a
// test over the shipped file catches a re-added block) and asserts that it
// carries the accept link and no command-line instructions. The email's only
// action is the browser link; the CLI block it used to carry told invitees to
// install a client and run `memory login`, which is not the path to accepting an
// invitation.
func TestProjectInvitationTemplate_NoCLIInstructions(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	const acceptURL = "https://app.example.com/invites/accept?token=tok123"
	result, err := svc.Render("project-invitation", TemplateContext{
		"inviterName": "Alice",
		"projectName": "Huma KB",
		"roleLabel":   "Project Admin",
		"acceptUrl":   acceptURL,
	}, "default")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if result == nil {
		t.Fatal("Render() returned nil result")
	}

	for _, want := range []string{acceptURL, "Huma KB", "Project Admin", "Alice"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("invitation HTML missing %q", want)
		}
	}

	unwanted := []string{
		"curl -fsSL",      // the install one-liner
		"memory login",    // step 2 of the removed block
		"memory projects", // step 3
		"Install the CLI", // heading of the removed block
		"install-section", // its wrapper (would render an empty box)
	}
	for _, never := range unwanted {
		if strings.Contains(result.HTML, never) {
			t.Errorf("invitation HTML must not carry CLI instructions: found %q", never)
		}
	}
}
