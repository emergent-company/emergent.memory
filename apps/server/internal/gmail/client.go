// Package gmail implements a Gmail v1 transport client used by the Gmail
// data-source connector. It wraps google.golang.org/api/gmail/v1 and
// golang.org/x/oauth2 and is constructed so tests can inject an HTTP client
// (option.WithHTTPClient) and a test endpoint (option.WithEndpoint) — no live
// Google calls are made in tests.
//
// The client deliberately exposes only what the connector needs: a paginated
// thread listing (id + history id + snippet, the cheap change-detection
// surface), full thread fetches, label listing for the connect-form resource
// picker, and a connection test. MIME decoding lives here because it is
// protocol detail, not domain logic; the caller decides what to do with the
// plain-text and HTML bodies.
package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const (
	// ReadonlyScope is the OAuth2 scope the Gmail connector requires. It is a
	// Google restricted scope: a public (external) OAuth app needs verification
	// and a CASA assessment for it, while a Workspace-internal app does not.
	ReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

	// DefaultMaxThreads is the default enumeration ceiling. A listing that hits
	// it is reported as truncated so the sync worker never treats an abbreviated
	// enumeration as authoritative.
	DefaultMaxThreads = 10000
	// MaxThreadsLimit is the largest enumeration ceiling a source may configure.
	MaxThreadsLimit = 50000

	// DefaultMaxBodyBytes caps a single decoded message body (256 KiB). Email
	// bodies are conversational text; anything larger is a newsletter or an
	// inline digest, and the cap keeps one giant message from dominating memory.
	DefaultMaxBodyBytes int64 = 256 << 10

	// DefaultRequestTimeout bounds one Gmail API call.
	DefaultRequestTimeout = 60 * time.Second

	// pageSize is the per-page thread-listing size (Gmail's maximum).
	pageSize = 100
)

// Credentials holds the OAuth2 client credentials and refresh token for a Gmail
// source. It is never stored in plaintext (the sources domain encrypts the
// credential map at rest).
//
// There is no scope field: the connector requests ReadonlyScope at connect time
// and Google's refresh grant is bound to the scope the user already approved, so
// a per-source scope would be cosmetic. The stored credential map may still
// carry a `scope` key written by an OAuth callback; this client ignores it.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

// ThreadRef is one entry from a thread listing. HistoryID is Gmail's
// per-thread change marker: it advances when a message is added, removed, or
// relabelled, which makes it the connector's revision id.
type ThreadRef struct {
	ID        string
	HistoryID uint64
	Snippet   string
}

// Message is one decoded message inside a thread.
type Message struct {
	ID      string
	From    string
	To      string
	Cc      string
	Subject string
	Date    time.Time
	Labels  []string
	// Text is the message's text/plain body (empty when the message has none).
	Text string
	// HTML is the message's text/html body (empty when the message has none).
	HTML string
	// Attachments counts parts that carry attachment content. Their payloads are
	// never downloaded.
	Attachments int
}

// Thread is one fully fetched Gmail thread.
type Thread struct {
	ID        string
	HistoryID uint64
	Snippet   string
	Messages  []Message
}

// Label is a Gmail label/ mailbox, used for the connect-form resource picker.
type Label struct {
	ID   string
	Name string
	Type string
}

// Config controls client behaviour. Zero values fall back to the defaults.
type Config struct {
	// MaxBodyBytes is the per-message decoded-body cap.
	MaxBodyBytes int64
	// RequestTimeout is the per-call timeout (one HTTP attempt).
	RequestTimeout time.Duration
	// TokenEndpoint overrides Google's OAuth2 token endpoint. It exists so a
	// hermetic test can serve the refresh grant locally
	// (GMAIL_TOKEN_URL); empty means Google's real endpoint.
	TokenEndpoint string
	// MaxAttempts bounds one API call including retries (default 4). Retries
	// apply only to responses that can succeed on a later attempt: 429, 5xx, and
	// Google's 403 rate-limit reasons. A 4xx client error fails on attempt one.
	MaxAttempts int
	// RetryBaseDelay is the first backoff delay, doubling per attempt up to
	// MaxRetryDelay (default 1s). Tests set it to a few milliseconds.
	RetryBaseDelay time.Duration
}

// DefaultRetryBaseDelay is the first backoff delay for a retryable API error.
const DefaultRetryBaseDelay = time.Second

// DefaultMaxAttempts is the attempt ceiling for one API call, retries included.
const DefaultMaxAttempts = 4

// MaxRetryDelay caps a single backoff wait, including a server-supplied
// Retry-After, so one throttled call cannot stall a sync for minutes.
const MaxRetryDelay = 30 * time.Second

// DefaultConfig returns the defaults used when Config fields are unset.
func DefaultConfig() Config {
	return Config{
		MaxBodyBytes:   DefaultMaxBodyBytes,
		RequestTimeout: DefaultRequestTimeout,
		MaxAttempts:    DefaultMaxAttempts,
		RetryBaseDelay: DefaultRetryBaseDelay,
	}
}

// Client is a Gmail v1 transport client.
type Client struct {
	svc *gapi.Service
	cfg Config
}

// New builds a Client from OAuth2 refresh-token credentials. Additional opts
// (e.g. option.WithEndpoint) are appended after the credential-backed HTTP
// client.
func New(ctx context.Context, creds Credentials, opts ...option.ClientOption) (*Client, error) {
	return NewWithConfig(ctx, creds, DefaultConfig(), opts...)
}

// NewWithConfig builds a Client like New and applies cfg. A non-empty
// cfg.TokenEndpoint replaces Google's token endpoint so a hermetic test can
// serve the refresh grant itself; the API origin is still redirected with
// option.WithEndpoint.
func NewWithConfig(ctx context.Context, creds Credentials, cfg Config, opts ...option.ClientOption) (*Client, error) {
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return nil, errors.New("gmail: refresh token is required")
	}

	endpoint := google.Endpoint
	if strings.TrimSpace(cfg.TokenEndpoint) != "" {
		endpoint.TokenURL = strings.TrimSpace(cfg.TokenEndpoint)
	}
	oauthCfg := oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     endpoint,
		Scopes:       []string{ReadonlyScope},
	}
	tokenSource := oauthCfg.TokenSource(ctx, &oauth2.Token{RefreshToken: creds.RefreshToken})
	hc := oauth2.NewClient(ctx, tokenSource)

	client, err := newClient(ctx, hc, opts...)
	if err != nil {
		return nil, err
	}
	return client.WithConfig(cfg), nil
}

// NewWithHTTPClient builds a Client from an already-configured *http.Client (no
// credentials). Intended for tests: pass option.WithEndpoint(server.URL) to
// point the client at a mock Gmail endpoint.
func NewWithHTTPClient(hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	if hc == nil {
		return nil, errors.New("gmail: http client is required")
	}
	return newClient(context.Background(), hc, opts...)
}

func newClient(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	opts = append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := gapi.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gmail: create service: %w", err)
	}
	return &Client{svc: svc, cfg: DefaultConfig()}, nil
}

// WithConfig sets the client configuration, applying defaults for zero-valued
// fields. It returns the client for chaining.
func (c *Client) WithConfig(cfg Config) *Client {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = DefaultRetryBaseDelay
	}
	c.cfg = cfg
	return c
}

// call runs one API operation with a per-attempt timeout and bounded
// exponential backoff. Gmail is quota-bound (threads.get costs 40 units, 120
// with format=full, against 6,000 units/minute/user), so a large backfill
// inevitably meets 429/403 rate-limit responses; retrying in place makes it
// converge instead of failing each item until the next scheduled sync.
//
// Retries are limited to errors that can succeed later: 429, 5xx, and Google's
// rate-limit 403 reasons. A 4xx client error (bad request, auth failure) fails
// immediately — retrying it only delays the report. A server-supplied
// Retry-After is honoured up to MaxRetryDelay.
func (c *Client) call(ctx context.Context, fn func(context.Context) error) error {
	delay := c.cfg.RetryBaseDelay
	var lastErr error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		lastErr = fn(attemptCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == c.cfg.MaxAttempts || !retryable(lastErr) {
			return lastErr
		}
		wait := delay
		if after, ok := retryAfter(lastErr); ok {
			wait = after
		}
		select {
		case <-ctx.Done():
			// The caller's deadline or cancellation outranks the retry: return
			// the response that made us consider retrying, not just ctx.Err().
			return lastErr
		case <-time.After(wait):
		}
		if delay *= 2; delay > MaxRetryDelay {
			delay = MaxRetryDelay
		}
	}
	return lastErr
}

// retryable reports whether err is a throttle or transient server failure.
func retryable(err error) bool {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	switch {
	case apiErr.Code == http.StatusTooManyRequests:
		return true
	case apiErr.Code >= 500 && apiErr.Code <= 599:
		return true
	case apiErr.Code == http.StatusForbidden:
		// Google reports daily/per-user quota exhaustion as 403 with a
		// rate-limit reason. Any other 403 (scope or permission) is terminal.
		for _, e := range apiErr.Errors {
			if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
				return true
			}
		}
		return false
	}
	return false
}

// retryAfter returns the server's Retry-After hint, capped at MaxRetryDelay.
func retryAfter(err error) (time.Duration, bool) {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) || apiErr.Header == nil {
		return 0, false
	}
	raw := strings.TrimSpace(apiErr.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	secs, convErr := strconv.Atoi(raw)
	if convErr != nil || secs < 0 {
		return 0, false
	}
	wait := time.Duration(secs) * time.Second
	if wait > MaxRetryDelay {
		wait = MaxRetryDelay
	}
	return wait, true
}

// ListOptions is the enumeration scope for ListThreads.
type ListOptions struct {
	// Query is a Gmail search expression (e.g. "in:inbox -label:spam"). Empty
	// lists everything the token can see.
	Query string
	// LabelIDs restricts the listing to messages carrying every given label.
	LabelIDs []string
	// Since, when non-zero, is folded into the query as an `after:` clause. It
	// comes from a source's absolute `since` date, so the window only grows and
	// never drops previously ingested threads out of scope.
	Since time.Time
	// After, when non-zero, replaces Since as the `after:` bound. The backfill
	// walk uses it to list one bounded window at a time; wherever it is set the
	// caller owns the window, so Since must not narrow or widen it.
	After time.Time
	// Before, when non-zero, bounds the window from above as a `before:`
	// clause. Only the backfill walk sets it.
	Before time.Time
	// MaxThreads caps the enumeration. Zero means DefaultMaxThreads.
	MaxThreads int
}

// ListThreads enumerates threads matching opts, newest first, paginating until
// the ceiling or the end of the listing. truncated reports that the ceiling was
// hit with more results available, so the caller never treats the listing as a
// complete view of the mailbox.
func (c *Client) ListThreads(ctx context.Context, opts ListOptions) (refs []ThreadRef, truncated bool, err error) {
	max := opts.MaxThreads
	if max <= 0 {
		max = DefaultMaxThreads
	}
	query := buildQuery(opts)

	pageToken := ""
	for {
		var resp *gapi.ListThreadsResponse
		err := c.call(ctx, func(callCtx context.Context) error {
			call := c.svc.Users.Threads.List("me").MaxResults(pageSize).Context(callCtx)
			if query != "" {
				call = call.Q(query)
			}
			if len(opts.LabelIDs) > 0 {
				call = call.LabelIds(opts.LabelIDs...)
			}
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			var callErr error
			resp, callErr = call.Do()
			return callErr
		})
		if err != nil {
			// A page failure after some results is an error, not a partial
			// success: the caller must see the enumeration as failed so it does
			// not advance change-detection state on an incomplete view.
			return nil, false, fmt.Errorf("gmail: list threads: %w", err)
		}

		for i, t := range resp.Threads {
			if t == nil || t.Id == "" {
				continue
			}
			refs = append(refs, ThreadRef{ID: t.Id, HistoryID: t.HistoryId, Snippet: t.Snippet})
			if len(refs) >= max {
				// Truncated when an unread entry follows in this page or another
				// page exists: stopping mid-page still drops threads.
				return refs, i < len(resp.Threads)-1 || resp.NextPageToken != "", nil
			}
		}

		pageToken = resp.NextPageToken
		if pageToken == "" {
			return refs, false, nil
		}
	}
}

// buildQuery folds the date window and label filter into a Gmail search
// expression. The listing's Q supports `after:`/`before:` with a YYYY/MM/DD
// date, which Gmail interprets in the account's timezone; both operators are
// day-granular, so a caller walking windows must overlap by a day to avoid
// dropping threads that share the boundary date.
func buildQuery(opts ListOptions) string {
	parts := make([]string, 0, 3)
	if q := strings.TrimSpace(opts.Query); q != "" {
		parts = append(parts, q)
	}
	switch {
	case !opts.After.IsZero():
		parts = append(parts, "after:"+opts.After.UTC().Format("2006/01/02"))
	case !opts.Since.IsZero():
		parts = append(parts, "after:"+opts.Since.UTC().Format("2006/01/02"))
	}
	if !opts.Before.IsZero() {
		parts = append(parts, "before:"+opts.Before.UTC().Format("2006/01/02"))
	}
	return strings.Join(parts, " ")
}

// GetThread fetches one thread with its full message bodies.
func (c *Client) GetThread(ctx context.Context, id string) (*Thread, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("gmail: thread id is required")
	}
	var raw *gapi.Thread
	err := c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		raw, callErr = c.svc.Users.Threads.Get("me", id).Format("full").Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: get thread %s: %w", id, err)
	}
	return c.parseThread(raw), nil
}

// ListLabels returns the account's labels (system and user), for the connect
// form's resource picker.
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	var resp *gapi.ListLabelsResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = c.svc.Users.Labels.List("me").Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: list labels: %w", err)
	}
	out := make([]Label, 0, len(resp.Labels))
	for _, l := range resp.Labels {
		if l == nil || l.Id == "" {
			continue
		}
		out = append(out, Label{ID: l.Id, Name: l.Name, Type: l.Type})
	}
	return out, nil
}

// TestConnection performs a bounded identity call to validate the credentials
// and that the token carries a scope the Gmail API accepts.
func (c *Client) TestConnection(ctx context.Context) error {
	err := c.call(ctx, func(callCtx context.Context) error {
		_, callErr := c.svc.Users.GetProfile("me").Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return fmt.Errorf("gmail: connection test: %w", err)
	}
	return nil
}

// parseThread decodes a Gmail thread resource into the connector's shape.
func (c *Client) parseThread(raw *gapi.Thread) *Thread {
	t := &Thread{ID: raw.Id, HistoryID: raw.HistoryId, Snippet: raw.Snippet}
	t.Messages = make([]Message, 0, len(raw.Messages))
	for _, m := range raw.Messages {
		if m == nil {
			continue
		}
		msg := Message{
			ID:      m.Id,
			Labels:  append([]string(nil), m.LabelIds...),
			Subject: headerValue(m.Payload, "Subject"),
			From:    headerValue(m.Payload, "From"),
			To:      headerValue(m.Payload, "To"),
			Cc:      headerValue(m.Payload, "Cc"),
		}
		msg.Date = messageDate(m)
		if m.Payload != nil {
			c.walkParts(m.Payload, &msg)
		}
		t.Messages = append(t.Messages, msg)
	}
	return t
}

// walkParts collects the first text/plain and text/html bodies anywhere in the
// part tree and counts attachment parts. Body parts that are themselves
// multipart are descended; attachment payloads are never decoded.
func (c *Client) walkParts(part *gapi.MessagePart, msg *Message) {
	if part == nil {
		return
	}
	mimeType := strings.ToLower(strings.TrimSpace(part.MimeType))

	// An attachment part carries its content out-of-band behind an attachment
	// id; it is counted and skipped whichever MIME type it declares.
	if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
		msg.Attachments++
		return
	}

	switch {
	case mimeType == "text/plain" || strings.HasPrefix(mimeType, "text/plain;"):
		if msg.Text == "" {
			msg.Text = c.decodeBody(part.Body)
		}
		return
	case mimeType == "text/html" || strings.HasPrefix(mimeType, "text/html;"):
		if msg.HTML == "" {
			msg.HTML = c.decodeBody(part.Body)
		}
		return
	}

	for _, child := range part.Parts {
		c.walkParts(child, msg)
	}
}

// decodeBody base64url-decodes a part body, capping the result at
// MaxBodyBytes.
func (c *Client) decodeBody(body *gapi.MessagePartBody) string {
	if body == nil || body.Data == "" {
		return ""
	}
	raw, err := decodeBase64URL(body.Data)
	if err != nil {
		return ""
	}
	if int64(len(raw)) > c.cfg.MaxBodyBytes {
		// Slicing bytes can cut a multi-byte rune, and invalid UTF-8 would make
		// every ingest of this thread fail at the database. Drop the partial rune
		// instead of poisoning the document.
		raw = []byte(strings.ToValidUTF8(string(raw[:c.cfg.MaxBodyBytes]), ""))
	}
	return strings.TrimSpace(string(raw))
}

// decodeBase64URL decodes Gmail's URL-safe base64, tolerating missing padding.
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// headerValue returns the first header value with the given (case-insensitive)
// name.
func headerValue(payload *gapi.MessagePart, name string) string {
	if payload == nil {
		return ""
	}
	for _, h := range payload.Headers {
		if h == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(h.Name), name) {
			return strings.TrimSpace(h.Value)
		}
	}
	return ""
}

// dateLayouts are the Date header formats mail clients actually emit, tried in
// order before falling back to the message's internal date.
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	time.RFC3339,
}

// messageDate resolves a message's timestamp: the Date header when parseable,
// else Gmail's internal date (epoch milliseconds).
func messageDate(m *gapi.Message) time.Time {
	if v := headerValue(m.Payload, "Date"); v != "" {
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	if m.InternalDate > 0 {
		return time.UnixMilli(m.InternalDate).UTC()
	}
	return time.Time{}
}
