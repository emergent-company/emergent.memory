package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkItemJSONContract decodes a server-shaped work-items response into the
// gateway's WorkItem and checks the three fields the card's identity block needs
// survived the wire. The server test pins the same keys from the producing side
// (agents.TestToWorkItemDTO_JSONContract); this one pins the consuming side, so
// renaming a tag in either module fails a test instead of silently putting the
// machine key back on the board.
func TestWorkItemJSONContract(t *testing.T) {
	const body = `{"success":true,"data":[{
		"projectId":"p1",
		"canonicalId":"9cc039e3-8ea3-42f0-a43d-2dda1a9f7ae9",
		"type":"GitHubIssue",
		"name":"Add rate limiting to the public API",
		"ref":"#42f0a43d2dda",
		"key":"github:357590e8-6cbf-46bb-8c29-d8f839dd4ba8:emergent-company/emergent.memory#772",
		"status":"in_progress",
		"properties":{"body":"The token bucket is exhausted under burst traffic."}
	}]}`

	var env successEnvelope[[]WorkItem]
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode work-items envelope: %v", err)
	}
	if len(env.Data) != 1 {
		t.Fatalf("items = %d, want 1", len(env.Data))
	}
	it := env.Data[0]
	if it.Name != "Add rate limiting to the public API" {
		t.Errorf("Name = %q", it.Name)
	}
	if it.Ref != "#42f0a43d2dda" {
		t.Errorf("Ref = %q", it.Ref)
	}
	if got := objectExcerpt(it.Properties); !strings.Contains(got, "token bucket") {
		t.Errorf("excerpt = %q, want the property body", got)
	}
	// The end-to-end point of the change: the card title comes from the decoded
	// name, never from the machine key the response also carries.
	if got := workItemLabel(it); got != it.Name {
		t.Errorf("label = %q, want the decoded name", got)
	}
}
