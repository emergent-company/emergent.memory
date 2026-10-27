package gmail

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	gapi "google.golang.org/api/gmail/v1"
)

// newTestClient builds a Client pointed at a fake Gmail API endpoint. Backoff is
// compressed so a retry-path test does not sleep seconds.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewWithHTTPClient(srv.Client(),
		option.WithEndpoint(srv.URL),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("NewWithHTTPClient: %v", err)
	}
	return c.WithConfig(Config{RetryBaseDelay: time.Millisecond})
}

// listPage is one page of a threads.list response.
func listPage(ids []string, historyIDs []uint64, nextPageToken string) string {
	var b strings.Builder
	b.WriteString(`{"threads":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"` + id + `","historyId":"` + uintToString(historyIDs[i]) + `","snippet":"preview ` + id + `"}`)
	}
	b.WriteString(`]`)
	if nextPageToken != "" {
		b.WriteString(`,"nextPageToken":"` + nextPageToken + `"`)
	}
	b.WriteString(`}`)
	return b.String()
}

func uintToString(v uint64) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func TestListThreadsPaginates(t *testing.T) {
	var pages int
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/threads" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		pages++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("pageToken") {
		case "":
			_, _ = w.Write([]byte(listPage([]string{"t1", "t2"}, []uint64{10, 11}, "p2")))
		case "p2":
			_, _ = w.Write([]byte(listPage([]string{"t3"}, []uint64{12}, "")))
		default:
			t.Errorf("unexpected pageToken %q", r.URL.Query().Get("pageToken"))
		}
	}))

	refs, truncated, err := c.ListThreads(t.Context(), ListOptions{Query: "in:inbox", MaxThreads: 100})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2", pages)
	}
	if len(refs) != 3 || refs[0].ID != "t1" || refs[2].ID != "t3" {
		t.Fatalf("refs = %+v, want t1,t2,t3", refs)
	}
	if refs[1].HistoryID != 11 {
		t.Errorf("history id = %d, want 11", refs[1].HistoryID)
	}
	if refs[0].Snippet != "preview t1" {
		t.Errorf("snippet = %q, want %q", refs[0].Snippet, "preview t1")
	}
}

func TestListThreadsCapReportsTruncated(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listPage([]string{"t1", "t2"}, []uint64{1, 2}, "p2")))
	}))

	refs, truncated, err := c.ListThreads(t.Context(), ListOptions{MaxThreads: 2})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if !truncated {
		t.Fatal("truncated = false, want true when the ceiling is hit with more pages")
	}
}

// TestListThreadsCapMidPageReportsTruncated pins the truncation flag when the
// ceiling falls inside a page: the remaining entries of that page are dropped,
// so the listing is not a complete view even with no nextPageToken.
func TestListThreadsCapMidPageReportsTruncated(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listPage([]string{"t1", "t2", "t3"}, []uint64{1, 2, 3}, "")))
	}))

	refs, truncated, err := c.ListThreads(t.Context(), ListOptions{MaxThreads: 2})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if !truncated {
		t.Fatal("truncated = false, want true when the ceiling cuts a page")
	}
}

// TestListThreadsCapAtPageEndNotTruncated is the counterpart: a ceiling that
// coincides with the end of the listing has dropped nothing.
func TestListThreadsCapAtPageEndNotTruncated(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listPage([]string{"t1", "t2"}, []uint64{1, 2}, "")))
	}))

	if _, truncated, err := c.ListThreads(t.Context(), ListOptions{MaxThreads: 2}); err != nil {
		t.Fatalf("ListThreads: %v", err)
	} else if truncated {
		t.Fatal("truncated = true, want false when the ceiling lands on the last entry")
	}
}

func TestListThreadsSendsQueryAndLabels(t *testing.T) {
	var gotQuery, gotLabels string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotLabels = strings.Join(r.URL.Query()["labelIds"], ",")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"threads":[]}`))
	}))

	since := time.Date(2025, 3, 4, 10, 0, 0, 0, time.UTC)
	if _, _, err := c.ListThreads(t.Context(), ListOptions{
		Query:    "in:inbox",
		LabelIDs: []string{"Label_1", "Label_2"},
		Since:    since,
	}); err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if gotQuery != "in:inbox after:2025/03/04" {
		t.Errorf("q = %q, want %q", gotQuery, "in:inbox after:2025/03/04")
	}
	if gotLabels != "Label_1,Label_2" {
		t.Errorf("labelIds = %q, want Label_1,Label_2", gotLabels)
	}
}

func TestListThreadsPageErrorFails(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":500,"message":"backend"}}`))
	}))
	if _, _, err := c.ListThreads(t.Context(), ListOptions{}); err == nil {
		t.Fatal("ListThreads returned nil error for a 500 page")
	}
}

// threadResponse builds a threads.get response with a multipart/alternative
// message plus one attachment.
func threadResponse() string {
	textBody := "aGVsbG8gd29ybGQ"                    // "hello world"
	htmlBody := "PHA-aGVsbG8gPGI-d29ybGQ8L2I-PC9wPg" // "<p>hello <b>world</b></p>"
	return `{
	  "id":"t1","historyId":"99","snippet":"hello",
	  "messages":[
	    {"id":"m2","threadId":"t1","labelIds":["INBOX","CATEGORY_PERSONAL"],"internalDate":"1735689600000",
	     "payload":{"mimeType":"multipart/mixed","headers":[
	       {"name":"Subject","value":"Re: Hello"},
	       {"name":"From","value":"Bob <bob@example.com>"},
	       {"name":"To","value":"Alice <alice@example.com>"},
	       {"name":"Date","value":"Wed, 1 Jan 2025 12:00:00 +0000"}],
	       "parts":[
	         {"mimeType":"multipart/alternative","parts":[
	            {"mimeType":"text/plain","body":{"data":"` + textBody + `"}},
	            {"mimeType":"text/html","body":{"data":"` + htmlBody + `"}}]},
	         {"mimeType":"application/pdf","filename":"invoice.pdf","body":{"attachmentId":"att-1","size":1234}}]}},
	    {"id":"m1","threadId":"t1","labelIds":["INBOX"],"internalDate":"1735603200000",
	     "payload":{"mimeType":"text/html","headers":[
	       {"name":"Subject","value":"Hello"},
	       {"name":"From","value":"Alice <alice@example.com>"},
	       {"name":"Date","value":"Tue, 31 Dec 2024 12:00:00 +0000"}],
	       "body":{"data":"` + htmlBody + `"}}}
	  ]
	}`
}

func TestGetThreadParsesMIME(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/threads/t1" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "full" {
			t.Errorf("format = %q, want full", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(threadResponse()))
	}))

	th, err := c.GetThread(t.Context(), "t1")
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if th.ID != "t1" || th.HistoryID != 99 || len(th.Messages) != 2 {
		t.Fatalf("thread = %+v", th)
	}

	newest, oldest := th.Messages[0], th.Messages[1]
	if newest.Subject != "Re: Hello" || newest.From != "Bob <bob@example.com>" {
		t.Errorf("headers = %+v", newest)
	}
	if newest.Text != "hello world" {
		t.Errorf("text body = %q, want %q", newest.Text, "hello world")
	}
	if !strings.Contains(newest.HTML, "<b>world</b>") {
		t.Errorf("html body = %q, want the inline markup", newest.HTML)
	}
	if newest.Attachments != 1 {
		t.Errorf("attachments = %d, want 1", newest.Attachments)
	}
	if want := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC); !newest.Date.Equal(want) {
		t.Errorf("date = %v, want %v", newest.Date, want)
	}
	if oldest.Text != "" {
		t.Errorf("html-only message text = %q, want empty", oldest.Text)
	}
	if !strings.Contains(oldest.HTML, "<b>world</b>") {
		t.Errorf("html-only message html = %q", oldest.HTML)
	}
}

func TestGetThreadBodyCap(t *testing.T) {
	// "AAAA" decodes to three bytes; a 2-byte cap truncates it.
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t1","messages":[{"id":"m1","payload":{"mimeType":"text/plain","body":{"data":"QUFBQQ"}}}]}`))
	}))
	th, err := c.WithConfig(Config{MaxBodyBytes: 2}).GetThread(t.Context(), "t1")
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if got := th.Messages[0].Text; len(got) > 2 {
		t.Fatalf("body = %q, want at most 2 bytes", got)
	}
}

// TestGetThreadBodyCapKeepsValidUTF8 pins that a byte cap cutting a multi-byte
// rune cannot emit invalid UTF-8 (which would make the ingest fail on every
// sync for that thread).
func TestGetThreadBodyCapKeepsValidUTF8(t *testing.T) {
	// "aaé" is 4 bytes ("aa" + 0xC3 0xA9); a 3-byte cap splits the rune.
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t1","messages":[{"id":"m1","payload":{"mimeType":"text/plain","body":{"data":"YWHDqQ"}}}]}`))
	}))
	th, err := c.WithConfig(Config{MaxBodyBytes: 3}).GetThread(t.Context(), "t1")
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	got := th.Messages[0].Text
	if !utf8.ValidString(got) {
		t.Fatalf("body %q is not valid UTF-8", got)
	}
	if got != "aa" {
		t.Fatalf("body = %q, want %q", got, "aa")
	}
}

func TestListLabels(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/labels" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"labels":[{"id":"INBOX","name":"INBOX","type":"system"},{"id":"Label_7","name":"Work","type":"user"}]}`))
	}))
	labels, err := c.ListLabels(t.Context())
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 2 || labels[1].Name != "Work" {
		t.Fatalf("labels = %+v", labels)
	}
}

func TestTestConnection(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/profile" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"emailAddress":"alice@example.com","messagesTotal":10}`))
	}))
	if err := c.TestConnection(t.Context()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestDecodeBase64URLToleratesPaddingAndWhitespace(t *testing.T) {
	for _, in := range []string{"aGVsbG8", "aGVsbG8=", "aGVs\nbG8"} {
		got, err := decodeBase64URL(in)
		if err != nil {
			t.Fatalf("decodeBase64URL(%q): %v", in, err)
		}
		if string(got) != "hello" {
			t.Errorf("decodeBase64URL(%q) = %q, want hello", in, got)
		}
	}
}

func TestMessageDateFallsBackToInternalDate(t *testing.T) {
	got := messageDate(&gapi.Message{InternalDate: 1735689600000})
	want := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("date = %v, want %v", got, want)
	}
}

// TestListThreadsRetriesThrottledResponse pins the backoff loop: a 429 is
// retried in place (honouring a zero-second Retry-After) and the page that
// follows succeeds, so a quota-bound backfill converges instead of failing the
// item until the next scheduled sync.
func TestListThreadsRetriesThrottledResponse(t *testing.T) {
	var calls int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Rate Limit Exceeded","errors":[{"reason":"rateLimitExceeded"}]}}`))
			return
		}
		_, _ = w.Write([]byte(listPage([]string{"t1"}, []uint64{7}, "")))
	}))

	refs, _, err := c.ListThreads(t.Context(), ListOptions{})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "t1" {
		t.Fatalf("refs = %+v, want the page that followed the throttles", refs)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want 3 (two throttles then success)", got)
	}
}

// TestListThreadsDoesNotRetryClientError keeps a terminal 400 from being retried:
// the request will never succeed, and retrying only delays the error.
func TestListThreadsDoesNotRetryClientError(t *testing.T) {
	var calls int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid q value"}}`))
	}))

	if _, _, err := c.ListThreads(t.Context(), ListOptions{}); err == nil {
		t.Fatal("ListThreads returned nil error for a 400")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (a 400 is terminal)", got)
	}
}

// TestListThreadsGivesUpAfterMaxAttempts bounds the retry loop.
func TestListThreadsGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":503,"message":"backend"}}`))
	}))

	if _, _, err := c.ListThreads(t.Context(), ListOptions{}); err == nil {
		t.Fatal("ListThreads returned nil error after exhausting attempts")
	}
	if got, want := atomic.LoadInt32(&calls), int32(DefaultMaxAttempts); got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

func TestRetryable(t *testing.T) {
	apiErr := func(code int, reasons ...string) error {
		e := &googleapi.Error{Code: code}
		for _, r := range reasons {
			e.Errors = append(e.Errors, googleapi.ErrorItem{Reason: r})
		}
		return e
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"429", apiErr(http.StatusTooManyRequests), true},
		{"500", apiErr(http.StatusInternalServerError), true},
		{"503", apiErr(http.StatusServiceUnavailable), true},
		{"403 rateLimitExceeded", apiErr(http.StatusForbidden, "rateLimitExceeded"), true},
		{"403 userRateLimitExceeded", apiErr(http.StatusForbidden, "userRateLimitExceeded"), true},
		{"403 insufficientPermissions", apiErr(http.StatusForbidden, "insufficientPermissions"), false},
		{"400", apiErr(http.StatusBadRequest), false},
		{"401", apiErr(http.StatusUnauthorized), false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range tests {
		if got := retryable(tc.err); got != tc.want {
			t.Errorf("%s: retryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRetryAfterIsCappedAndParsed(t *testing.T) {
	withHeader := func(v string) error {
		h := http.Header{}
		h.Set("Retry-After", v)
		return &googleapi.Error{Code: http.StatusTooManyRequests, Header: h}
	}
	if wait, ok := retryAfter(withHeader("7")); !ok || wait != 7*time.Second {
		t.Fatalf("retryAfter(7) = %v/%v, want 7s", wait, ok)
	}
	if wait, ok := retryAfter(withHeader("3600")); !ok || wait != MaxRetryDelay {
		t.Fatalf("retryAfter(3600) = %v/%v, want the %v cap", wait, ok, MaxRetryDelay)
	}
	if _, ok := retryAfter(withHeader("soon")); ok {
		t.Fatal("retryAfter accepted a non-numeric hint")
	}
	if _, ok := retryAfter(errors.New("boom")); ok {
		t.Fatal("retryAfter read a hint from a non-API error")
	}
}

// TestListThreadsWindowBounds pins the backfill window encoding: After replaces
// Since (the walk owns the window) and Before adds the upper bound.
func TestListThreadsWindowBounds(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(listPage([]string{"t1"}, []uint64{1}, "")))
	}))

	_, _, err := c.ListThreads(t.Context(), ListOptions{
		Query:  "in:inbox",
		Since:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		After:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Before: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if want := "in:inbox after:2024/06/01 before:2024/07/01"; gotQuery != want {
		t.Fatalf("q = %q, want %q (window overrides the source since)", gotQuery, want)
	}
}

// TestNewWithConfigServesRefreshGrantLocally covers the hermetic seam: with a
// TokenEndpoint override the client mints its access token from that server and
// sends it to the API, so a stub deployment never reaches Google.
func TestNewWithConfigServesRefreshGrantLocally(t *testing.T) {
	var tokenCalls int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("token form: %v", err)
		}
		if got := r.PostFormValue("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.PostFormValue("refresh_token"); got != "rt" {
			t.Errorf("refresh_token = %q, want rt", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"seam-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	var authHeader string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(listPage([]string{"t1"}, []uint64{1}, "")))
	}))
	defer apiSrv.Close()

	cfg := DefaultConfig()
	cfg.TokenEndpoint = tokenSrv.URL
	c, err := NewWithConfig(t.Context(),
		Credentials{ClientID: "cid", ClientSecret: "cs", RefreshToken: "rt"},
		cfg,
		option.WithEndpoint(apiSrv.URL),
	)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	if _, _, err := c.ListThreads(t.Context(), ListOptions{}); err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if authHeader != "Bearer seam-token" {
		t.Fatalf("Authorization = %q, want the locally minted token", authHeader)
	}
	if atomic.LoadInt32(&tokenCalls) == 0 {
		t.Fatal("token endpoint was never called; the seam is not wired")
	}
}
