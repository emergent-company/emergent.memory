package discoveryjobs

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIncludeRelationshipsDefaultsOn is the regression for the empty
// relationship half of #234. The web UI's "Discover from this document" and
// project actions post only scope + document ids (see
// apps/web-ui/gateway/memory_discovery.go), so a zero-valued bool here silently
// disabled relationship discovery for every UI run. Omitted now means yes;
// an explicit false still opts out.
func TestIncludeRelationshipsDefaultsOn(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "UI document discovery body omits the field",
			body: `{"scope":"documents","document_ids":["5f0d4c4e-6a1e-4e3e-9d1e-1a2b3c4d5e6f"]}`,
			want: true,
		},
		{
			name: "project discovery body omits the field",
			body: `{"scope":"project"}`,
			want: true,
		},
		{
			name: "explicit opt-out is honoured",
			body: `{"scope":"project","include_relationships":false}`,
			want: false,
		},
		{
			name: "explicit opt-in",
			body: `{"scope":"project","include_relationships":true}`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req StartDiscoveryRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if got := includeRelationshipsRequested(&req); got != tc.want {
				t.Errorf("includeRelationshipsRequested = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRelationshipDiscoveryGate pins the pipeline gate: the phase runs when the
// job asked for it and there are at least two types to relate.
func TestRelationshipDiscoveryGate(t *testing.T) {
	if !relationshipDiscoveryEnabled(true, 3) {
		t.Error("three types with relationships requested must run the phase")
	}
	if relationshipDiscoveryEnabled(true, 1) {
		t.Error("a single type has nothing to relate")
	}
	if relationshipDiscoveryEnabled(false, 3) {
		t.Error("an explicit opt-out must not run the phase")
	}
}

// TestBuildRelationshipDiscoveryPrompt_IncludesTypeProperties checks that the
// relationship prompt can see the evidence that links types: the table's Guest
// column is what makes "a guest appears in an episode" inferable.
func TestBuildRelationshipDiscoveryPrompt_IncludesTypeProperties(t *testing.T) {
	svc := testService()
	prompt := svc.buildRelationshipDiscoveryPrompt([]DiscoveredType{
		{
			TypeName:    "PodcastEpisode",
			Description: "One row of the episode table",
			Properties: map[string]any{
				"Title": map[string]any{"type": "string"},
				"Guest": map[string]any{"type": "string"},
			},
		},
		{TypeName: "GuestGroup", Description: "A group of recurring guests"},
	}, "Podcast knowledge base")

	for _, want := range []string{"PodcastEpisode", "GuestGroup", "Guest", "Title"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "properties: , ") {
		t.Error("prompt rendered an empty property list")
	}
}
