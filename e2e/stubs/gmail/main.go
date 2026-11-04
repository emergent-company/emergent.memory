// Command gmail-stub is a hermetic stand-in for the Gmail v1 API, used by the
// e2e harness. It lets the Gmail data-source connector be exercised end to end
// — OAuth2 refresh grant, label listing, connection test, two-pass thread
// enumeration with the backfill watermark, throttled-call retry, and thread
// ingestion as one document per thread — with no Google credentials, no real
// mailbox, and no network egress.
//
// Two deployment seams point the server here: GMAIL_API_BASE_URL (the API
// origin, reached as http://gmail-stub:8080) and GMAIL_TOKEN_URL (the OAuth2
// token endpoint, http://gmail-stub:8080/token). The stub rejects every API
// call that does not carry the bearer token it issued from /token, so a suite
// that passes also proves the refresh grant actually ran.
//
// Gmail-compatible routes:
//
//	POST /token                                        refresh_token grant
//	GET  /gmail/v1/users/{userId}/profile              connection test
//	GET  /gmail/v1/users/{userId}/labels               label listing
//	GET  /gmail/v1/users/{userId}/threads              listing (q, labelIds, paging)
//	GET  /gmail/v1/users/{userId}/threads/{id}         full thread (format=full)
//	GET  /healthz                                      200 "ok"
//
// The listing understands the subset of Gmail search syntax the connector
// emits: in:<label>, label:<id>, and the day-granular after:/before: operators.
// Every other token is matched case-insensitively as free text against the
// thread's snippet, subject, and participants.
//
// Test-control routes (no auth; not part of the Gmail contract):
//
//	POST   /_control/reset     replace the mailbox: {threads:[...], email, refreshToken}
//	POST   /_control/threads   upsert one thread (bumps its history id)
//	DELETE /_control/threads   delete one thread (?id=)
//	POST   /_control/fail      arm N injected failures: {kind, status, retryAfter, count}
//	DELETE /_control/fail      clear injected failures
//	GET    /_control/requests  the Gmail requests seen so far (?since=N&contains=...)
//	GET    /_control/state     mailbox/request counters
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort         = "8080"
	defaultAccessToken  = "e2e-gmail-access-token"
	defaultRefreshToken = "e2e-gmail-refresh-token"
	defaultEmail        = "e2e@example.com"
	// pageLimit is Gmail's maximum page size; the connector always asks for it.
	pageLimit = 100
)

// seedMessage is one message supplied by a control request. Date is an RFC
// 3339 timestamp; empty means "now". DaysAgo is a convenience for tests that
// reason in calendar windows: it wins over Date when set.
type seedMessage struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Cc      string `json:"cc"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	DaysAgo int    `json:"daysAgo"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// seedThread is one thread supplied by a control request. Messages may be
// omitted on an upsert, which keeps the stored ones and still bumps the
// thread's history id (a relabel, not a new message).
type seedThread struct {
	ID       string        `json:"id"`
	Snippet  string        `json:"snippet"`
	Labels   []string      `json:"labels"`
	Hidden   bool          `json:"hidden"`
	Messages []seedMessage `json:"messages,omitempty"`
}

type message struct {
	id      string
	from    string
	to      string
	cc      string
	subject string
	text    string
	html    string
	date    time.Time
	labels  []string
}

type thread struct {
	id        string
	snippet   string
	historyID uint64
	labels    []string
	hidden    bool
	messages  []message
}

// lastDate is the thread's ordering/timestamp anchor: its newest message.
func (t *thread) lastDate() time.Time {
	var newest time.Time
	for _, m := range t.messages {
		if m.date.After(newest) {
			newest = m.date
		}
	}
	return newest
}

// searchText is the free-text surface for non-operator query tokens.
func (t *thread) searchText() string {
	var b strings.Builder
	b.WriteString(t.snippet)
	for _, m := range t.messages {
		b.WriteString(" ")
		b.WriteString(m.subject)
		b.WriteString(" ")
		b.WriteString(m.from)
		b.WriteString(" ")
		b.WriteString(m.to)
		b.WriteString(" ")
		b.WriteString(m.text)
	}
	return strings.ToLower(b.String())
}

type label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// failure is one armed injected error. remaining counts how many matching
// requests still get it, so a test can throttle exactly one page.
type failure struct {
	kind       string
	status     int
	retryAfter string
	remaining  int
}

// requestEntry is one recorded API request, for assertions that a sync
// re-listed a window (the source cursor is not part of the public API).
type requestEntry struct {
	N      int    `json:"n"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query"`
	Kind   string `json:"kind"`
	Status int    `json:"status"`
}

// stub is the in-memory Gmail impersonation.
type stub struct {
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	email        string
	seq          uint64
	threads      map[string]*thread
	labels       []label
	failures     []failure
	requests     []requestEntry
	grants       int
}

func newStub() *stub {
	return &stub{
		accessToken:  defaultAccessToken,
		refreshToken: defaultRefreshToken,
		email:        defaultEmail,
		seq:          1000,
		threads:      map[string]*thread{},
		labels: []label{
			{ID: "INBOX", Name: "INBOX", Type: "system"},
			{ID: "SENT", Name: "SENT", Type: "system"},
			{ID: "STARRED", Name: "STARRED", Type: "system"},
			{ID: "Label_1", Name: "Project Alpha", Type: "user"},
		},
	}
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}
	s := newStub()
	if v := strings.TrimSpace(os.Getenv("GMAIL_STUB_ACCESS_TOKEN")); v != "" {
		s.accessToken = v
	}
	if v := strings.TrimSpace(os.Getenv("GMAIL_STUB_REFRESH_TOKEN")); v != "" {
		s.refreshToken = v
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("gmail-stub listening on :%s (refresh grant at /token; API calls need the issued bearer token)", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("gmail-stub: %v", err)
	}
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := pathSegments(r.URL.EscapedPath())

	switch {
	case len(segments) == 1 && segments[0] == "healthz":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
		return
	case len(segments) > 0 && segments[0] == "_control":
		s.serveControl(w, r, segments[1:])
		return
	case len(segments) == 1 && segments[0] == "token":
		s.serveToken(w, r)
		return
	}

	// /gmail/v1/users/{userId}/{resource...}
	if len(segments) >= 5 && segments[0] == "gmail" && segments[1] == "v1" && segments[2] == "users" {
		s.serveAPI(w, r, segments[4:])
		return
	}

	writeGmailError(w, http.StatusNotFound, "Not Found", "")
}

// serveAPI dispatches one Gmail v1 users resource call. Auth and failure
// injection happen here (after routing has decided it is an API call), so a
// 401 never consumes an armed throttle and a throttle is recorded.
func (s *stub) serveAPI(w http.ResponseWriter, r *http.Request, rest []string) {
	kind := apiKind(rest)
	if kind == "" {
		writeGmailError(w, http.StatusNotFound, "Not Found", "")
		return
	}

	if !s.authorized(r) {
		s.record(r, "_unauthorized", http.StatusUnauthorized)
		writeGmailError(w, http.StatusUnauthorized, "Invalid Credentials", "authError")
		return
	}

	if status, retryAfter, ok := s.consumeFailure(kind); ok {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		s.record(r, kind, status)
		writeGmailError(w, status, rateLimitMessage(status), "rateLimitExceeded")
		return
	}

	switch {
	case kind == "profile" && r.Method == http.MethodGet:
		s.serveProfile(w, r, kind)
	case kind == "labels.list" && r.Method == http.MethodGet:
		s.serveLabels(w, r, kind)
	case kind == "threads.list" && r.Method == http.MethodGet:
		s.serveThreads(w, r, kind)
	case kind == "threads.get" && r.Method == http.MethodGet:
		s.serveThread(w, r, rest[1], kind)
	default:
		writeGmailError(w, http.StatusNotFound, "Not Found", "")
	}
}

// apiKind classifies a users-resource path, or returns "" for an unknown one.
func apiKind(rest []string) string {
	if len(rest) == 0 {
		return ""
	}
	switch rest[0] {
	case "profile":
		return "profile"
	case "labels":
		return "labels.list"
	case "threads":
		if len(rest) == 1 {
			return "threads.list"
		}
		if len(rest) == 2 {
			return "threads.get"
		}
	}
	return ""
}

func (s *stub) authorized(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == s.accessToken
}

// serveToken answers the OAuth2 refresh grant. It is the only unauthenticated
// route: the connector exchanges the stored refresh token for an access token
// here instead of at Google.
func (s *stub) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "invalid_request"})
		return
	}
	// The oauth2 client sends the grant as form data; ParseForm reads both the
	// body and any query string.
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, throttled := s.consumeFailureLocked("token"); throttled {
		w.Header().Set("Retry-After", "1")
		s.recordLocked(r, "token", http.StatusTooManyRequests)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "rate_limit_exceeded", "error_description": "Too many refresh grants.",
		})
		return
	}

	grant := r.PostForm.Get("grant_type")
	refresh := r.PostForm.Get("refresh_token")
	if grant != "refresh_token" || refresh != s.refreshToken {
		s.recordLocked(r, "token", http.StatusBadRequest)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":             "invalid_grant",
			"error_description": "Token has been expired or revoked.",
		})
		return
	}

	s.grants++
	s.recordLocked(r, "token", http.StatusOK)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": s.accessToken,
		"expires_in":   3600,
		"token_type":   "Bearer",
		"scope":        "https://www.googleapis.com/auth/gmail.readonly",
	})
}

func (s *stub) serveProfile(w http.ResponseWriter, r *http.Request, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := 0
	for _, t := range s.threads {
		if !t.hidden {
			messages += len(t.messages)
		}
	}
	s.recordLocked(r, kind, http.StatusOK)
	writeJSON(w, http.StatusOK, map[string]any{
		"emailAddress":  s.email,
		"messagesTotal": messages,
		"threadsTotal":  s.visibleCountLocked(),
		// Gmail serialises history ids as strings.
		"historyId": strconv.FormatUint(s.seq, 10),
	})
}

func (s *stub) serveLabels(w http.ResponseWriter, r *http.Request, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordLocked(r, kind, http.StatusOK)
	writeJSON(w, http.StatusOK, map[string]any{"labels": s.labels})
}

func (s *stub) serveThreads(w http.ResponseWriter, r *http.Request, kind string) {
	q := r.URL.Query()
	max := pageLimit
	if v := strings.TrimSpace(q.Get("maxResults")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < max {
			max = n
		}
	}
	after, before, labels, text := parseGmailQuery(q.Get("q"))
	labels = append(labels, splitLabelIDs(q["labelIds"])...)
	offset := parsePageOffset(q.Get("pageToken"))

	s.mu.Lock()
	defer s.mu.Unlock()

	matched := make([]*thread, 0, len(s.threads))
	for _, t := range s.threads {
		if t.hidden {
			continue
		}
		last := t.lastDate()
		if !after.IsZero() && last.Before(after) {
			continue
		}
		if !before.IsZero() && !last.Before(before) {
			continue
		}
		if !hasAllLabels(t, labels) {
			continue
		}
		if text != "" && !strings.Contains(t.searchText(), text) {
			continue
		}
		matched = append(matched, t)
	}
	sort.Slice(matched, func(i, j int) bool {
		di, dj := matched[i].lastDate(), matched[j].lastDate()
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return matched[i].id < matched[j].id
	})

	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + max
	if end > len(matched) {
		end = len(matched)
	}
	page := matched[offset:end]

	out := make([]map[string]any, 0, len(page))
	for _, t := range page {
		out = append(out, map[string]any{
			"id":        t.id,
			"snippet":   t.snippet,
			"historyId": strconv.FormatUint(t.historyID, 10),
		})
	}
	resp := map[string]any{
		"threads":            out,
		"resultSizeEstimate": len(matched),
	}
	if end < len(matched) {
		resp["nextPageToken"] = "off:" + strconv.Itoa(end)
	}
	s.recordLocked(r, kind, http.StatusOK)
	writeJSON(w, http.StatusOK, resp)
}

func (s *stub) serveThread(w http.ResponseWriter, r *http.Request, id, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.threads[id]
	if !ok {
		s.recordLocked(r, kind, http.StatusNotFound)
		writeGmailError(w, http.StatusNotFound, "Requested entity was not found.", "")
		return
	}

	messages := make([]map[string]any, 0, len(t.messages))
	for _, m := range t.messages {
		messages = append(messages, gmailMessageJSON(t.id, m))
	}
	s.recordLocked(r, kind, http.StatusOK)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        t.id,
		"snippet":   t.snippet,
		"historyId": strconv.FormatUint(t.historyID, 10),
		"messages":  messages,
	})
}

// gmailMessageJSON renders one message in the Gmail v1 shape the connector
// parses: headers, an internalDate, and a multipart/alternative payload whose
// text/plain and text/html bodies are base64url encoded.
func gmailMessageJSON(threadID string, m message) map[string]any {
	parts := make([]map[string]any, 0, 2)
	if m.text != "" {
		parts = append(parts, map[string]any{
			"partId":   "0",
			"mimeType": "text/plain",
			"body":     bodyJSON(m.text),
		})
	}
	if m.html != "" {
		parts = append(parts, map[string]any{
			"partId":   "1",
			"mimeType": "text/html",
			"body":     bodyJSON(m.html),
		})
	}

	headers := []map[string]string{
		{"name": "From", "value": m.from},
		{"name": "To", "value": m.to},
	}
	if m.cc != "" {
		headers = append(headers, map[string]string{"name": "Cc", "value": m.cc})
	}
	headers = append(headers,
		map[string]string{"name": "Subject", "value": m.subject},
		map[string]string{"name": "Date", "value": m.date.UTC().Format(time.RFC1123Z)},
	)

	return map[string]any{
		"id":           m.id,
		"threadId":     threadID,
		"labelIds":     m.labels,
		"snippet":      snippetOf(m),
		"internalDate": strconv.FormatInt(m.date.UnixMilli(), 10),
		"payload": map[string]any{
			"partId":   "",
			"mimeType": "multipart/alternative",
			"headers":  headers,
			"parts":    parts,
		},
	}
}

func bodyJSON(s string) map[string]any {
	return map[string]any{
		"size": len(s),
		"data": base64.RawURLEncoding.EncodeToString([]byte(s)),
	}
}

func snippetOf(m message) string {
	text := strings.TrimSpace(m.text)
	if text == "" {
		text = strings.TrimSpace(m.subject)
	}
	if runes := []rune(text); len(runes) > 100 {
		return string(runes[:100])
	}
	return text
}

// ─────────────────────────────────────────────────────────────────────────────
// Query parsing
// ─────────────────────────────────────────────────────────────────────────────

// parseGmailQuery splits a Gmail search expression into the operators the
// connector emits: after:/before: (day-granular, YYYY/MM/DD or YYYY-MM-DD) and
// in:/label: label filters. Everything else is joined as free text.
func parseGmailQuery(q string) (after, before time.Time, labels []string, text string) {
	words := strings.Fields(q)
	free := make([]string, 0, len(words))
	for _, w := range words {
		lower := strings.ToLower(w)
		switch {
		case strings.HasPrefix(lower, "after:"):
			if t, ok := parseQueryDate(w[len("after:"):]); ok {
				after = t
			}
		case strings.HasPrefix(lower, "before:"):
			if t, ok := parseQueryDate(w[len("before:"):]); ok {
				before = t
			}
		case strings.HasPrefix(lower, "in:"), strings.HasPrefix(lower, "label:"):
			value := w[strings.Index(w, ":")+1:]
			if strings.EqualFold(value, "inbox") {
				value = "INBOX"
			}
			if value != "" {
				labels = append(labels, value)
			}
		default:
			free = append(free, w)
		}
	}
	return after, before, labels, strings.ToLower(strings.Join(free, " "))
}

func parseQueryDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006/01/02", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// splitLabelIDs flattens repeated and comma-separated labelIds values.
func splitLabelIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// hasAllLabels mirrors Gmail's labelIds contract: a thread must carry every
// requested label. A thread's labels are the union of its messages' labels plus
// its own.
func hasAllLabels(t *thread, want []string) bool {
	if len(want) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, l := range t.labels {
		have[l] = true
	}
	for _, m := range t.messages {
		for _, l := range m.labels {
			have[l] = true
		}
	}
	for _, l := range want {
		if !have[l] {
			return false
		}
	}
	return true
}

func parsePageOffset(token string) int {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "off:") {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(token, "off:"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Failure injection
// ─────────────────────────────────────────────────────────────────────────────

func (s *stub) consumeFailure(kind string) (int, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeFailureLocked(kind)
}

func (s *stub) consumeFailureLocked(kind string) (int, string, bool) {
	for i := range s.failures {
		f := &s.failures[i]
		if f.kind != kind || f.remaining <= 0 {
			continue
		}
		f.remaining--
		status := f.status
		if status == 0 {
			status = http.StatusTooManyRequests
		}
		return status, f.retryAfter, true
	}
	return 0, "", false
}

func rateLimitMessage(status int) string {
	if status >= 500 {
		return "Backend Error"
	}
	return "User-rate limit exceeded."
}

// ─────────────────────────────────────────────────────────────────────────────
// Request log
// ─────────────────────────────────────────────────────────────────────────────

func (s *stub) record(r *http.Request, kind string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordLocked(r, kind, status)
}

func (s *stub) recordLocked(r *http.Request, kind string, status int) {
	q := r.URL.RawQuery
	s.requests = append(s.requests, requestEntry{
		N:      len(s.requests) + 1,
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  q,
		Kind:   kind,
		Status: status,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Control routes
// ─────────────────────────────────────────────────────────────────────────────

func (s *stub) serveControl(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) != 1 {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown control route"})
		return
	}
	switch rest[0] {
	case "reset":
		s.controlReset(w, r)
	case "threads":
		s.controlThreads(w, r)
	case "fail":
		s.controlFail(w, r)
	case "requests":
		s.controlRequests(w, r)
	case "state":
		s.controlState(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown control route"})
	}
}

func (s *stub) controlReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST required"})
		return
	}
	var req struct {
		Email        string       `json:"email"`
		RefreshToken string       `json:"refreshToken"`
		Threads      []seedThread `json:"threads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if email := strings.TrimSpace(req.Email); email != "" {
		s.email = email
	}
	if token := strings.TrimSpace(req.RefreshToken); token != "" {
		s.refreshToken = token
	}
	s.threads = map[string]*thread{}
	s.seq = 1000
	s.failures = nil
	s.requests = nil
	s.grants = 0

	messages := 0
	for _, st := range req.Threads {
		t, err := s.upsertThreadLocked(st)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
			return
		}
		messages += len(t.messages)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"threads":  len(s.threads),
		"messages": messages,
	})
}

func (s *stub) controlThreads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req seedThread
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "id is required"})
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		t, err := s.upsertThreadLocked(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        t.id,
			"historyId": strconv.FormatUint(t.historyID, 10),
			"messages":  len(t.messages),
			"hidden":    t.hidden,
		})

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.threads[id]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown thread"})
			return
		}
		delete(s.threads, id)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST or DELETE required"})
	}
}

// upsertThreadLocked replaces (or creates) a thread and always bumps its history
// id, which is the connector's revision. A message with no explicit id is
// numbered within the thread.
func (s *stub) upsertThreadLocked(st seedThread) (*thread, error) {
	id := strings.TrimSpace(st.ID)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	t, ok := s.threads[id]
	if !ok {
		t = &thread{id: id}
		s.threads[id] = t
	}
	s.seq++
	t.historyID = s.seq
	if st.Snippet != "" {
		t.snippet = st.Snippet
	}
	if st.Labels != nil {
		t.labels = append([]string(nil), st.Labels...)
	}
	if t.labels == nil {
		t.labels = []string{"INBOX"}
	}
	t.hidden = st.Hidden

	if st.Messages != nil {
		t.messages = make([]message, 0, len(st.Messages))
		for i, sm := range st.Messages {
			m, err := toMessage(id, i, sm, t.labels)
			if err != nil {
				return nil, err
			}
			t.messages = append(t.messages, m)
		}
		if len(t.messages) > 0 && t.snippet == "" {
			t.snippet = snippetOf(t.messages[len(t.messages)-1])
		}
	}
	return t, nil
}

func toMessage(threadID string, index int, sm seedMessage, threadLabels []string) (message, error) {
	date, err := messageDate(sm)
	if err != nil {
		return message{}, err
	}
	id := strings.TrimSpace(sm.ID)
	if id == "" {
		id = fmt.Sprintf("%s-m%d", threadID, index+1)
	}
	return message{
		id:      id,
		from:    sm.From,
		to:      sm.To,
		cc:      sm.Cc,
		subject: sm.Subject,
		text:    sm.Text,
		html:    sm.HTML,
		date:    date,
		labels:  append([]string(nil), threadLabels...),
	}, nil
}

// messageDate resolves a seed message's timestamp: DaysAgo (midday UTC, so a
// calendar window boundary never lands on the message) wins over Date.
func messageDate(sm seedMessage) (time.Time, error) {
	now := time.Now().UTC()
	if sm.DaysAgo > 0 {
		day := now.AddDate(0, 0, -sm.DaysAgo)
		return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC), nil
	}
	if strings.TrimSpace(sm.Date) == "" {
		return now, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(sm.Date))
	if err != nil {
		return time.Time{}, fmt.Errorf("message %q: date must be RFC 3339", sm.ID)
	}
	return t.UTC(), nil
}

func (s *stub) controlFail(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Kind       string `json:"kind"`
			Status     int    `json:"status"`
			RetryAfter string `json:"retryAfter"`
			Count      int    `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "kind is required"})
			return
		}
		count := req.Count
		if count <= 0 {
			count = 1
		}
		s.mu.Lock()
		s.failures = append(s.failures, failure{
			kind:       kind,
			status:     req.Status,
			retryAfter: strings.TrimSpace(req.RetryAfter),
			remaining:  count,
		})
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "count": count})

	case http.MethodDelete:
		s.mu.Lock()
		s.failures = nil
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"cleared": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST or DELETE required"})
	}
}

func (s *stub) controlRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "GET required"})
		return
	}
	since := 0
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			since = n
		}
	}
	contains := r.URL.Query().Get("contains")

	s.mu.Lock()
	out := make([]requestEntry, 0, len(s.requests))
	for _, e := range s.requests {
		if e.N <= since {
			continue
		}
		if contains != "" && !strings.Contains(e.Kind, contains) && !strings.Contains(e.Path, contains) {
			continue
		}
		out = append(out, e)
	}
	next := since
	if len(s.requests) > 0 {
		next = s.requests[len(s.requests)-1].N
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"next": next, "requests": out})
}

func (s *stub) controlState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "GET required"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"threads":  s.visibleCountLocked(),
		"hidden":   len(s.threads) - s.visibleCountLocked(),
		"requests": len(s.requests),
		"grants":   s.grants,
		"seq":      s.seq,
	})
}

func (s *stub) visibleCountLocked() int {
	n := 0
	for _, t := range s.threads {
		if !t.hidden {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

// writeGmailError writes the JSON error envelope googleapi.Error parses. reason
// fills errors[].reason, which the connector inspects to tell a throttle (403
// rateLimitExceeded) from a terminal permission error.
func writeGmailError(w http.ResponseWriter, status int, message, reason string) {
	errObj := map[string]any{"code": status, "message": message}
	if reason != "" {
		errObj["errors"] = []map[string]string{{
			"message": message, "domain": "usageLimits", "reason": reason,
		}}
	}
	writeJSON(w, status, map[string]any{"error": errObj})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// pathSegments splits an escaped URL path into unescaped, non-empty segments.
func pathSegments(escaped string) []string {
	raw := strings.Split(strings.Trim(escaped, "/"), "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s == "" {
			continue
		}
		out = append(out, unescapeSegment(s))
	}
	return out
}

func unescapeSegment(s string) string {
	// Thread ids are opaque but the connector only emits plain ones; still,
	// decode %XX so a path token with an encoded character matches.
	if !strings.Contains(s, "%") {
		return s
	}
	if decoded, err := decodePathSegment(s); err == nil {
		return decoded
	}
	return s
}

func decodePathSegment(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("truncated escape")
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", err
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), nil
}
