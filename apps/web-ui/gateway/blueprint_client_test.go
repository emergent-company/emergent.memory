package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDeleteBlueprintClientPostsRequest verifies the client issues DELETE
// /api/blueprints/:id with the id path-escaped.
func TestDeleteBlueprintClientPostsRequest(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	if err := m.DeleteBlueprint(t.Context(), "bp-1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/blueprints/bp-1" {
		t.Errorf("request = %s %s, want DELETE /api/blueprints/bp-1", gotMethod, gotPath)
	}
}
