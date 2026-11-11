package githubapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientPullRequestAndReview(t *testing.T) {
	var seenAuth, seenReview string
	var reviewBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open", "draft": false, "merged": false,
				"user": map[string]any{"login": "mkucharz", "type": "User"},
				"head": map[string]any{"sha": "head-sha", "ref": "fix/x"},
				"base": map[string]any{"ref": "dev"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/pulls/7/reviews":
			seenReview = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&reviewBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/check-runs":
			_ = json.NewDecoder(r.Body).Decode(&reviewBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/comments":
			_ = json.NewDecoder(r.Body).Decode(&reviewBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := NewClient(server.URL, "ghs_test", server.Client())

	pr, err := c.GetPullRequest(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.HeadSHA != "head-sha" || pr.BaseRef != "dev" || pr.UserLogin != "mkucharz" || pr.Draft {
		t.Fatalf("pull request = %#v", pr)
	}
	if seenAuth != "Bearer ghs_test" {
		t.Fatalf("authorization = %q", seenAuth)
	}

	if err := c.CreateReview(context.Background(), "owner/repo", 7, "head-sha", "REQUEST_CHANGES", "no"); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if seenReview != "/repos/owner/repo/pulls/7/reviews" {
		t.Fatalf("review path = %q", seenReview)
	}
	if reviewBody["commit_id"] != "head-sha" || reviewBody["event"] != "REQUEST_CHANGES" {
		t.Fatalf("review body = %#v", reviewBody)
	}

	if err := c.CreateReviewedCheck(context.Background(), "owner/repo", "head-sha", "failure", "blocked", "details"); err != nil {
		t.Fatalf("CreateReviewedCheck: %v", err)
	}
	if reviewBody["head_sha"] != "head-sha" || reviewBody["conclusion"] != "failure" || reviewBody["name"] != "reviewed" {
		t.Fatalf("check body = %#v", reviewBody)
	}

	if err := c.CreateIssueComment(context.Background(), "owner/repo", 7, "Reviewed-SHA: head-sha"); err != nil {
		t.Fatalf("CreateIssueComment: %v", err)
	}
	if reviewBody["body"] != "Reviewed-SHA: head-sha" {
		t.Fatalf("comment body = %#v", reviewBody)
	}
}

func TestClientSurfacesAPIErrorWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"commit_id is not part of the pull request"}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "ghs_secret", server.Client())
	err := c.CreateReview(context.Background(), "owner/repo", 7, "old-sha", "APPROVE", "ok")
	if err == nil {
		t.Fatal("expected an API error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("error = %#v", err)
	}
	if apiErr.Message == "" {
		t.Fatal("expected a message")
	}
}
