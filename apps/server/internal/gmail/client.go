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
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gapi "google.golang.org/api/gmail/v1"
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
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	// Scope is the OAuth2 scope the refresh token was authorized for. Empty
	// falls back to ReadonlyScope (only reachable for a hand-seeded source).
	Scope string `json:"scope,omitempty"`
}

// requestedScope resolves the OAuth2 scope for a credential.
func requestedScope(scope string) string {
	if strings.TrimSpace(scope) != "" {
		return scope
	}
	return ReadonlyScope
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
	// RequestTimeout is the per-call timeout.
	RequestTimeout time.Duration
}

// DefaultConfig returns the defaults used when Config fields are unset.
func DefaultConfig() Config {
	return Config{
		MaxBodyBytes:   DefaultMaxBodyBytes,
		RequestTimeout: DefaultRequestTimeout,
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
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return nil, errors.New("gmail: refresh token is required")
	}

	oauthCfg := oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{requestedScope(creds.Scope)},
	}
	tokenSource := oauthCfg.TokenSource(ctx, &oauth2.Token{RefreshToken: creds.RefreshToken})
	hc := oauth2.NewClient(ctx, tokenSource)

	return newClient(ctx, hc, opts...)
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
	c.cfg = cfg
	return c
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
		pageCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		call := c.svc.Users.Threads.List("me").MaxResults(pageSize).Context(pageCtx)
		if query != "" {
			call = call.Q(query)
		}
		if len(opts.LabelIDs) > 0 {
			call = call.LabelIds(opts.LabelIDs...)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, callErr := call.Do()
		cancel()
		if callErr != nil {
			// A page failure after some results is an error, not a partial
			// success: the caller must see the enumeration as failed so it does
			// not advance change-detection state on an incomplete view.
			return nil, false, fmt.Errorf("gmail: list threads: %w", callErr)
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

// buildQuery folds the label filter date window into a Gmail search expression.
// The listing's Q supports `after:` with a YYYY/MM/DD date, which Gmail
// interprets in the account's timezone.
func buildQuery(opts ListOptions) string {
	parts := make([]string, 0, 2)
	if q := strings.TrimSpace(opts.Query); q != "" {
		parts = append(parts, q)
	}
	if !opts.Since.IsZero() {
		parts = append(parts, "after:"+opts.Since.UTC().Format("2006/01/02"))
	}
	return strings.Join(parts, " ")
}

// GetThread fetches one thread with its full message bodies.
func (c *Client) GetThread(ctx context.Context, id string) (*Thread, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("gmail: thread id is required")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	raw, err := c.svc.Users.Threads.Get("me", id).Format("full").Context(callCtx).Do()
	if err != nil {
		return nil, fmt.Errorf("gmail: get thread %s: %w", id, err)
	}
	return c.parseThread(raw), nil
}

// ListLabels returns the account's labels (system and user), for the connect
// form's resource picker.
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	resp, err := c.svc.Users.Labels.List("me").Context(callCtx).Do()
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
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	if _, err := c.svc.Users.GetProfile("me").Context(callCtx).Do(); err != nil {
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
