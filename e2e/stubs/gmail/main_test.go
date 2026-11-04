package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestStub(t *testing.T) (*httptest.Server, *stub) {
	t.Helper()
	s := newStub()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, s
}

// do performs one request and returns the response with its body decoded as a
// generic JSON value (nil for an empty body).
func do(t *testing.T, method, target, token string, form url.Values, payload any) (*http.Response, map[string]any) {
	t.Helper()

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = strings.NewReader(string(encoded))
	} else {
		body = strings.NewReader("")
	}

	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	switch {
	case form != nil:
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	case payload != nil:
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, target, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	var decoded map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode %s %s: %v", method, target, err)
		}
	}
	return resp, decoded
}

// seed installs a mailbox through the control route.
func seed(t *testing.T, base string, threads []seedThread) {
	t.Helper()
	resp, _ := do(t, http.MethodPost, base+"/_control/reset", "", nil, map[string]any{"threads": threads})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed: status %d", resp.StatusCode)
	}
}

func TestTokenGrantIssuesTheBearerTokenTheAPIRequires(t *testing.T) {
	srv, _ := newTestStub(t)

	// No token: the API refuses to serve anything.
	resp, body := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/profile", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("profile without token: status %d, want 401", resp.StatusCode)
	}
	if got := errReason(body); got != "authError" {
		t.Errorf("error reason = %q, want authError", got)
	}

	// A wrong refresh token fails the grant (the connector maps it to its
	// reconnect message).
	resp, body = do(t, http.MethodPost, srv.URL+"/token", "", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"nope"},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("bad grant: status %d body %v", resp.StatusCode, body)
	}

	// The right one issues the token the API accepts.
	resp, body = do(t, http.MethodPost, srv.URL+"/token", "", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {defaultRefreshToken},
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant: status %d body %v", resp.StatusCode, body)
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatalf("grant returned no access_token: %v", body)
	}
	resp, profile := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/profile", token, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile with token: status %d body %v", resp.StatusCode, profile)
	}
	if profile["emailAddress"] != defaultEmail {
		t.Errorf("emailAddress = %v, want %s", profile["emailAddress"], defaultEmail)
	}
	if _, ok := profile["historyId"].(string); !ok {
		t.Errorf("historyId = %T, want a JSON string (Gmail's wire format)", profile["historyId"])
	}
}

func TestListingFiltersByWindowAndLabelsAndPages(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, []seedThread{
		{ID: "recent-1", Snippet: "latest", Labels: []string{"INBOX"}, Messages: []seedMessage{
			{Subject: "Recent one", Text: "hello", DaysAgo: 2},
		}},
		{ID: "recent-2", Snippet: "second", Labels: []string{"INBOX"}, Messages: []seedMessage{
			{Subject: "Recent two", Text: "hello", DaysAgo: 5},
		}},
		{ID: "older", Snippet: "old", Labels: []string{"INBOX"}, Messages: []seedMessage{
			{Subject: "Older", Text: "hello", DaysAgo: 45},
		}},
		{ID: "sent", Snippet: "outbound", Labels: []string{"SENT"}, Messages: []seedMessage{
			{Subject: "Sent", Text: "hello", DaysAgo: 3},
		}},
	})

	// The connector's head query: in:inbox, newest first.
	_, body := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?maxResults=100&q="+url.QueryEscape("in:inbox"), defaultAccessToken, nil, nil)
	ids := threadIDs(body)
	if want := []string{"recent-1", "recent-2", "older"}; !equalStrings(ids, want) {
		t.Fatalf("in:inbox ids = %v, want %v", ids, want)
	}

	// A backfill window: in:inbox after:<start> before:<boundary>.
	today := startOfToday()
	window := fmt.Sprintf("in:inbox after:%s before:%s",
		today.AddDate(0, 0, -60).Format("2006/01/02"),
		today.AddDate(0, 0, -30).Format("2006/01/02"))
	_, body = do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?q="+url.QueryEscape(window), defaultAccessToken, nil, nil)
	if ids := threadIDs(body); !equalStrings(ids, []string{"older"}) {
		t.Fatalf("window ids = %v, want [older]", ids)
	}

	// labelIds is an AND filter, distinct from the in: operator.
	_, body = do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?labelIds=INBOX&labelIds=Label_9", defaultAccessToken, nil, nil)
	if ids := threadIDs(body); len(ids) != 0 {
		t.Fatalf("labelIds intersection ids = %v, want none", ids)
	}

	// Paging: two per page, nextPageToken until the end.
	_, body = do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?maxResults=2&q="+url.QueryEscape("in:inbox"), defaultAccessToken, nil, nil)
	if ids := threadIDs(body); !equalStrings(ids, []string{"recent-1", "recent-2"}) {
		t.Fatalf("page 1 = %v", ids)
	}
	next, _ := body["nextPageToken"].(string)
	if next == "" {
		t.Fatal("page 1 returned no nextPageToken")
	}
	_, body = do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?maxResults=2&pageToken="+url.QueryEscape(next)+"&q="+url.QueryEscape("in:inbox"), defaultAccessToken, nil, nil)
	if ids := threadIDs(body); !equalStrings(ids, []string{"older"}) {
		t.Fatalf("page 2 = %v", ids)
	}
	if _, ok := body["nextPageToken"]; ok {
		t.Fatalf("last page still carries a nextPageToken: %v", body["nextPageToken"])
	}
}

func TestThreadFetchRendersGmailPartsTheConnectorDecodes(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, []seedThread{{
		ID: "t1", Snippet: "snippet", Labels: []string{"INBOX"},
		Messages: []seedMessage{{
			ID: "m1", From: "alice@example.com", To: "bob@example.com", Cc: "carol@example.com",
			Subject: "Quarterly plan", Date: "2026-10-01T09:30:00Z",
			Text: "the plain body", HTML: "<p>the html body</p>",
		}},
	}})

	resp, body := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads/t1?format=full", defaultAccessToken, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch: status %d body %v", resp.StatusCode, body)
	}
	if _, ok := body["historyId"].(string); !ok {
		t.Errorf("historyId = %T, want a JSON string", body["historyId"])
	}

	messages, _ := body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %v, want 1", body["messages"])
	}
	msg := messages[0].(map[string]any)
	payload := msg["payload"].(map[string]any)
	if got := header(payload, "Subject"); got != "Quarterly plan" {
		t.Errorf("Subject header = %q", got)
	}
	if got := header(payload, "Cc"); got != "carol@example.com" {
		t.Errorf("Cc header = %q", got)
	}
	if _, ok := msg["internalDate"].(string); !ok {
		t.Errorf("internalDate = %T, want a JSON string", msg["internalDate"])
	}

	parts, _ := payload["parts"].([]any)
	var plain, html string
	for _, raw := range parts {
		part := raw.(map[string]any)
		data := part["body"].(map[string]any)["data"].(string)
		decoded, err := base64.RawURLEncoding.DecodeString(data)
		if err != nil {
			t.Fatalf("part %v is not base64url: %v", part["mimeType"], err)
		}
		switch part["mimeType"] {
		case "text/plain":
			plain = string(decoded)
		case "text/html":
			html = string(decoded)
		}
	}
	if plain != "the plain body" || html != "<p>the html body</p>" {
		t.Fatalf("decoded parts = (%q, %q)", plain, html)
	}

	// A thread removed from the listing is still fetchable: hiding models an
	// archive, not a deletion, so ingestion of a hidden thread keeps working.
	do(t, http.MethodPost, srv.URL+"/_control/threads", "", nil, seedThread{ID: "t1", Hidden: true})
	if resp, _ := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads/t1?format=full", defaultAccessToken, nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("hidden thread fetch: status %d, want 200", resp.StatusCode)
	}
}

func TestFailureInjectionThrottlesExactlyOneCall(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, nil)

	resp, _ := do(t, http.MethodPost, srv.URL+"/_control/fail", "", nil, map[string]any{
		"kind": "threads.list", "status": 429, "retryAfter": "2", "count": 1,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("arm failure: status %d", resp.StatusCode)
	}

	resp, body := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?q=in:inbox", defaultAccessToken, nil, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("throttled call: status %d body %v", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
	if got := errReason(body); got != "rateLimitExceeded" {
		t.Errorf("error reason = %q, want rateLimitExceeded", got)
	}

	// The next attempt succeeds: the throttle was consumed.
	resp, _ = do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads?q=in:inbox", defaultAccessToken, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retry after throttle: status %d", resp.StatusCode)
	}

	// Both calls are visible to the suite, which is how an e2e asserts that a
	// retry happened rather than the item failing.
	_, log := do(t, http.MethodGet, srv.URL+"/_control/requests?contains=threads.list", "", nil, nil)
	entries, _ := log["requests"].([]any)
	if len(entries) != 2 {
		t.Fatalf("request log = %v, want 2 listing calls", log["requests"])
	}
	if status := int(entries[0].(map[string]any)["status"].(float64)); status != http.StatusTooManyRequests {
		t.Errorf("first logged status = %d, want 429", status)
	}
}

func TestUpsertBumpsHistoryAndResetClearsState(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, []seedThread{{
		ID: "t1", Snippet: "first", Labels: []string{"INBOX"},
		Messages: []seedMessage{{Subject: "One", Text: "body", DaysAgo: 1}},
	}})

	_, before := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads/t1?format=full", defaultAccessToken, nil, nil)

	// A relabel/append bumps the revision without dropping the stored messages.
	resp, after := do(t, http.MethodPost, srv.URL+"/_control/threads", "", nil, seedThread{
		ID: "t1", Labels: []string{"INBOX", "Label_1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert: status %d", resp.StatusCode)
	}
	if after["historyId"] == before["historyId"] {
		t.Fatalf("historyId did not advance: %v", after["historyId"])
	}

	_, fetched := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/threads/t1?format=full", defaultAccessToken, nil, nil)
	if msgs, _ := fetched["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("messages after label-only upsert = %v, want the stored one", fetched["messages"])
	}

	// Reset replaces the mailbox and clears the request log.
	seed(t, srv.URL, nil)
	_, state := do(t, http.MethodGet, srv.URL+"/_control/state", "", nil, nil)
	if got := state["threads"].(float64); got != 0 {
		t.Errorf("threads after reset = %v, want 0", got)
	}
	if got := state["requests"].(float64); got != 0 {
		t.Errorf("requests after reset = %v, want 0", got)
	}
}

func TestLabelsAndProfileReflectTheMailbox(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, []seedThread{
		{ID: "t1", Labels: []string{"INBOX"}, Messages: []seedMessage{{Subject: "One", Text: "b", DaysAgo: 1}}},
		{ID: "t2", Labels: []string{"SENT"}, Messages: []seedMessage{{Subject: "Two", Text: "b", DaysAgo: 2}}},
		{ID: "t3", Labels: []string{"INBOX"}, Hidden: true, Messages: []seedMessage{{Subject: "Three", Text: "b", DaysAgo: 3}}},
	})

	_, body := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/labels", defaultAccessToken, nil, nil)
	labels, _ := body["labels"].([]any)
	if len(labels) < 4 {
		t.Fatalf("labels = %v, want the system and user labels", body["labels"])
	}

	_, profile := do(t, http.MethodGet, srv.URL+"/gmail/v1/users/me/profile", defaultAccessToken, nil, nil)
	if got := profile["threadsTotal"].(float64); got != 2 {
		t.Errorf("threadsTotal = %v, want 2 (hidden thread excluded)", got)
	}
	if got := profile["messagesTotal"].(float64); got != 2 {
		t.Errorf("messagesTotal = %v, want 2", got)
	}
}

// ─── test helpers ────────────────────────────────────────────────────────────

func errReason(body map[string]any) string {
	errObj, _ := body["error"].(map[string]any)
	errors, _ := errObj["errors"].([]any)
	if len(errors) == 0 {
		return ""
	}
	first, _ := errors[0].(map[string]any)
	reason, _ := first["reason"].(string)
	return reason
}

func header(payload map[string]any, name string) string {
	headers, _ := payload["headers"].([]any)
	for _, raw := range headers {
		h := raw.(map[string]any)
		if strings.EqualFold(fmt.Sprint(h["name"]), name) {
			return fmt.Sprint(h["value"])
		}
	}
	return ""
}

func threadIDs(body map[string]any) []string {
	threads, _ := body["threads"].([]any)
	out := make([]string, 0, len(threads))
	for _, raw := range threads {
		out = append(out, raw.(map[string]any)["id"].(string))
	}
	return out
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func startOfToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}
