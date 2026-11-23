package main

// guard: the object-history js-dom fixtures rendered from the shipping
// objectPreviewHistory templ — a template change must regenerate them in the
// same PR (#1083, selected by name since #1087).

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// updateE2EFixtures regenerates the HTML fixtures the hermetic browser specs in
// ../tests/e2e/specs/js load, from the templates that actually ship. The
// fixtures are not hand-copied: this test re-renders every fixture on each
// `go test` run and fails when the committed file no longer matches the
// template, so a templ change that alters the markup cannot pass without the
// fixture diff landing in the same PR (#1083).
var updateE2EFixtures = flag.Bool("update-fixtures", false,
	"regenerate the js-dom Playwright HTML fixtures from the real templates")

// e2eFixtureDir is where the browser specs read their fixtures from, relative
// to this package (go test runs with the package directory as its working dir).
const e2eFixtureDir = "../tests/e2e/specs/js/fixtures"

// e2eFixture is one browser fixture: the file the spec loads and the rendered
// component it must stay byte-identical to.
type e2eFixture struct {
	file   string
	render func(t *testing.T) string
}

// TestE2EObjectPreviewFixturesRatchet keeps the object-history browser
// fixtures in sync with objectPreviewHistory. The specs assert native
// <details> disclosure behaviour in a real Chromium engine; the markup they
// drive must be the one the gateway renders, because a hand-copied fixture
// silently drifts from the template (the v2 provenance fixture had already
// lost the actor and status badges and invented the avatar internals before
// #1083).
//
// Regenerate after an intentional templ change:
//
//	go test ./gateway -run TestE2EObjectPreviewFixturesRatchet -update-fixtures
//
// It ends in Ratchet so the required `ci` job's `go test -run 'Ratchet' ./...`
// runs it on every gateway PR; before #1087 it only ran on the promotion.
func TestE2EObjectPreviewFixturesRatchet(t *testing.T) {
	// The "when" stamp formats in the viewer's zone; pin it so the fixture is
	// byte-identical on every machine and CI timezone.
	prevLocation := objectVersionLocation
	objectVersionLocation = func() *time.Location { return time.UTC }
	t.Cleanup(func() { objectVersionLocation = prevLocation })

	fixtures := []e2eFixture{
		{
			file: "object-preview-history-changes.html",
			render: func(t *testing.T) string {
				return renderHTML(t, objectPreviewHistory(objectPreviewChangesFixtureVersions(), nil, nil))
			},
		},
		{
			file: "object-preview-history-provenance.html",
			render: func(t *testing.T) string {
				return renderHTML(t, objectPreviewHistory(
					objectPreviewProvenanceFixtureVersions(),
					ordererIdentities(),
					map[string]string{objectPreviewOrderedRun: "u1"},
				))
			},
		},
	}

	for _, f := range fixtures {
		checkE2EFixture(t, f.file, e2eDocument(f.render(t)))
	}
}

// objectPreviewOrderedRun is the run an ordered write names in the provenance
// fixture; its orderer resolves to u1.
const objectPreviewOrderedRun = "0193f2a1-0000-4000-8000-000000000001"

// objectPreviewChangesFixtureVersions is a newest-first history that exercises
// every change-line shape the disclosure reveals: a recorded change_summary
// (added, removed, updated), a version whose list is derived by diffing the
// preceding version (a status transition), and the oldest version with no
// preceding version (the "no recorded changes" note).
func objectPreviewChangesFixtureVersions() []GraphObject {
	return []GraphObject{
		{
			Version: 3,
			Status:  "in_progress",
			Key:     "note:pr-42",
			ChangeSummary: map[string]any{
				"added":   map[string]any{"/summary": "a short summary"},
				"removed": []any{"/old"},
				"updated": map[string]any{"/title": map[string]any{"from": "draft", "to": "final"}},
			},
			CreatedAt: "2024-01-03T09:30:00Z",
		},
		{
			Version:   2,
			Status:    "in_progress",
			Key:       "note:pr-42",
			CreatedAt: "2024-01-02T09:30:00Z",
		},
		{
			Version:   1,
			Status:    "ready",
			Key:       "note:pr-42",
			CreatedAt: "2024-01-01T09:30:00Z",
		},
	}
}

// objectPreviewProvenanceFixtureVersions is the provenance v2 pair: an agent's
// write a person ordered (orderer leads, agent is the mechanism, credential
// dropped) and one that predates ordering (writer + credential line). Both
// carry a status so the summary's status badge is part of the fixture; the
// timestamps are old enough that the tooltip stays the stable "Recorded <iso>"
// branch instead of a relative age that would drift with the clock.
func objectPreviewProvenanceFixtureVersions() []GraphObject {
	return []GraphObject{
		{
			Version:        4,
			Status:         "done",
			Key:            "note:pr-42",
			ActorType:      "agent",
			ActorID:        "a1",
			CredentialKind: "mcp_link",
			RunID:          objectPreviewOrderedRun,
			CreatedAt:      "2024-01-02T09:30:00Z",
		},
		{
			Version:        3,
			Status:         "in_progress",
			Key:            "note:pr-42",
			ActorType:      "agent",
			ActorID:        "a1",
			CredentialKind: "mcp_link",
			RunID:          "0193f2a1-0000-4000-8000-000000000002",
			CreatedAt:      "2024-01-01T09:30:00Z",
		},
	}
}

// e2eDocument wraps a rendered component in the minimal document the browser
// spec hands to page.setContent. The wrapper is fixed here, not in the spec, so
// the fixture is the whole document the browser sees.
func e2eDocument(body string) string {
	return "<!doctype html>\n<html>\n<body>\n" + body + "\n</body>\n</html>\n"
}

// checkE2EFixture writes the fixture with -update-fixtures, and otherwise
// requires the committed file to equal the freshly rendered markup.
func checkE2EFixture(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(e2eFixtureDir, name)
	if *updateE2EFixtures {
		if err := os.MkdirAll(e2eFixtureDir, 0o755); err != nil {
			t.Fatalf("create fixture dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("regenerated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — regenerate with `go test ./gateway -run TestE2EObjectPreviewFixturesRatchet -update-fixtures`", path, err)
	}
	if string(want) != got {
		t.Errorf("%s is stale: the rendered template no longer matches it. "+
			"Regenerate with `go test ./gateway -run TestE2EObjectPreviewFixturesRatchet -update-fixtures` "+
			"and commit the diff with the template change.", path)
	}
}
