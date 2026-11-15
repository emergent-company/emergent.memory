package graph

import "testing"

func TestWorkObjectHeadCarriesProperties(t *testing.T) {
	head := workObjectHead(&GraphObject{Type: "GitHubReviewLane", Properties: map[string]any{"repo": "acme/repo"}})
	if head.Properties["repo"] != "acme/repo" {
		t.Fatalf("properties = %#v", head.Properties)
	}
}
