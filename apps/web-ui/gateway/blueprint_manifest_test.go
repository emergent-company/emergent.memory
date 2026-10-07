package main

import (
	"encoding/json"
	"testing"
)

// TestBundledAgentsFromManifestPreservesUI round-trips an agent's declared
// appearance from manifest JSON back into the detail view's BundledAgent shape.
func TestBundledAgentsFromManifestPreservesUI(t *testing.T) {
	manifest := json.RawMessage(`{"agents":[{"name":"operator","ui":{"icon":"database","color":"#2563EB"}},{"name":"plain"}]}`)
	var m blueprintManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	out := bundledAgentsFromManifest(m.Agents)
	if len(out) != 2 {
		t.Fatalf("agents = %d, want 2", len(out))
	}
	if out[0].UI == nil || out[0].UI.Icon != "database" || out[0].UI.Color != "#2563EB" {
		t.Errorf("ui not preserved: %#v", out[0].UI)
	}
	if out[1].UI != nil {
		t.Errorf("agent without ui must have a nil UI, got %#v", out[1].UI)
	}
}
