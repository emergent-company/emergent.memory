package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/mcpauth"
)

// traleMarkdownMimeType is the MIME type stamped on meeting documents.
const traleMarkdownMimeType = "text/markdown"

// traleDefaultBackfillDays is the sync window when a source sets no backfill.
const traleDefaultBackfillDays = 90

// traleMaxBackfillDays bounds the backfill window (10 years).
const traleMaxBackfillDays = 3650

// tralePageSize is the list_transcriptions page size (the tool caps at 25).
const tralePageSize = 25

// traleMaxPages bounds pagination so a misbehaving server that always reports
// has_next cannot spin the sync forever (400 pages = 10k meetings).
const traleMaxPages = 400

// traleCompleted is the meeting status the connector syncs. Meetings still
// recording/processing have no transcript yet, so they are skipped.
const traleCompleted = "completed"

// traleCredentials is the parsed, typed credential set. Trale registers a
// public client, so there is no client secret — refresh uses the client_id and
// the stored token_endpoint/resource.
type traleCredentials struct {
	accessToken   string
	refreshToken  string
	expiresAt     time.Time
	clientID      string
	tokenEndpoint string
	resource      string
	scope         string
}

func parseTraleCredentials(creds Credentials) (traleCredentials, error) {
	tc := traleCredentials{
		accessToken:   asString(creds["access_token"]),
		refreshToken:  asString(creds["refresh_token"]),
		clientID:      asString(creds["client_id"]),
		tokenEndpoint: asString(creds["token_endpoint"]),
		resource:      asString(creds["resource"]),
		scope:         asString(creds["scope"]),
	}
	if strings.TrimSpace(tc.resource) == "" {
		tc.resource = traleResource
	}
	if strings.TrimSpace(tc.tokenEndpoint) == "" {
		tc.tokenEndpoint = traleTokenURL
	}
	if strings.TrimSpace(tc.accessToken) == "" {
		return tc, apperror.NewValidation("trale: access_token is required")
	}
	if e := asString(creds["expires_at"]); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			tc.expiresAt = t
		}
	}
	return tc, nil
}

// traleSyncConfig is the parsed enumeration scope of a Trale source config.
type traleSyncConfig struct {
	tags              []string
	nameRegex         *regexp.Regexp
	includeTranscript bool
	backfillDays      int
}

func validateTraleConfig(cfg map[string]any) error {
	tags := stringSlice(cfg["tags"])
	if len(tags) == 0 {
		return apperror.NewValidation("config.tags is required: choose at least one Trale tag to sync")
	}
	for _, t := range tags {
		if strings.TrimSpace(t) == "" {
			return apperror.NewValidation("config.tags must be non-empty slugs")
		}
	}
	if p, ok := cfg["name_pattern"].(string); ok && strings.TrimSpace(p) != "" {
		if _, err := regexp.Compile(p); err != nil {
			return apperror.NewValidation("config.name_pattern is not a valid regular expression: " + err.Error())
		}
	}
	if raw, ok := cfg["backfill_days"]; ok && raw != nil {
		v, ok := intValue(raw)
		if !ok {
			// A blank value means "use the default"; anything else must be an
			// integer, so a typo is reported instead of silently replaced.
			if s, isStr := raw.(string); !isStr || strings.TrimSpace(s) != "" {
				return apperror.NewValidation("config.backfill_days must be an integer")
			}
		} else if v < 0 || v > traleMaxBackfillDays {
			return apperror.NewValidation("config.backfill_days must be between 0 and 3650")
		}
	}
	return nil
}

func parseTraleConfig(cfg map[string]any) (*traleSyncConfig, error) {
	if err := validateTraleConfig(cfg); err != nil {
		return nil, err
	}
	tags := normalizeTags(stringSlice(cfg["tags"]))

	var re *regexp.Regexp
	if p := asString(cfg["name_pattern"]); strings.TrimSpace(p) != "" {
		// Compiled already by validateTraleConfig; a second compile is cheap and
		// never fails here.
		re, _ = regexp.Compile(p)
	}

	backfill := traleDefaultBackfillDays
	if v, ok := intValue(cfg["backfill_days"]); ok {
		backfill = v
	}

	include, _ := cfg["include_transcript"].(bool)

	return &traleSyncConfig{
		tags:              tags,
		nameRegex:         re,
		includeTranscript: include,
		backfillDays:      backfill,
	}, nil
}

// normalizeTags trims, lowercases, drops empties, and dedupes tag slugs.
func normalizeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// intValue extracts an integer from a config value, tolerating the shapes JSONB
// round-trips produce (float64 from the API, int/int64/json.Number in-process)
// and the string a form submits for an integer field.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// traleTokenState holds the current access token and, when the stored token is
// expired, refreshes it in-memory. Rotated tokens are intentionally NOT
// persisted: the sync worker has no credential-write path (known limitation —
// see docs).
type traleTokenState struct {
	mu            sync.Mutex
	accessToken   string
	refreshToken  string
	expiresAt     time.Time
	clientID      string
	tokenEndpoint string
	resource      string
	http          *http.Client
}

func newTraleTokenState(creds traleCredentials, httpc *http.Client) *traleTokenState {
	return &traleTokenState{
		accessToken:   creds.accessToken,
		refreshToken:  creds.refreshToken,
		expiresAt:     creds.expiresAt,
		clientID:      creds.clientID,
		tokenEndpoint: creds.tokenEndpoint,
		resource:      creds.resource,
		http:          httpc,
	}
}

// validAccessToken returns a non-expired access token, refreshing in-memory
// when the stored one has expired. A refresh failure is an actionable error
// telling the user to reconnect (the worker cannot persist rotated tokens).
func (t *traleTokenState) validAccessToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.expiresAt.IsZero() || time.Now().UTC().Before(t.expiresAt) {
		return t.accessToken, nil
	}
	if err := t.refreshLocked(ctx); err != nil {
		return "", err
	}
	return t.accessToken, nil
}

func (t *traleTokenState) refreshLocked(ctx context.Context) error {
	if strings.TrimSpace(t.refreshToken) == "" {
		return traleReconnectError("the stored token has expired and there is no refresh token")
	}
	mc := mcpauth.NewClient(t.clientID, t.tokenEndpoint, t.resource, t.http)
	tok, err := mc.Refresh(ctx, t.refreshToken)
	if err != nil {
		return traleReconnectError(err.Error())
	}
	t.accessToken = tok.AccessToken
	t.expiresAt = tok.ExpiresAt(time.Now().UTC())
	if tok.RefreshToken != "" {
		t.refreshToken = tok.RefreshToken
	}
	return nil
}

// traleReconnectError is the actionable error surfaced when a stored token is
// expired and cannot be refreshed: the source must be reconnected.
func traleReconnectError(reason string) error {
	return fmt.Errorf("trale: access token expired and refresh failed (%s); reconnect the source", reason)
}

// traleMCP owns a lazily-initialised MCP session over Trale's streamable-HTTP
// transport. The session echoes the Mcp-Session-Id header and accepts both
// application/json and text/event-stream responses (mcp-go handles both).
type traleMCP struct {
	resource string
	http     *http.Client
	toks     *traleTokenState

	mu     sync.Mutex
	client *mcpclient.Client
}

func newTraleMCP(resource string, httpc *http.Client, toks *traleTokenState) *traleMCP {
	return &traleMCP{resource: resource, http: httpc, toks: toks}
}

func (m *traleMCP) session(ctx context.Context) (*mcpclient.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		return m.client, nil
	}
	token, err := m.toks.validAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	opts := []transport.StreamableHTTPCOption{
		transport.WithHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + token,
			"Accept":        "application/json, text/event-stream",
		}),
		transport.WithHTTPBasicClient(m.http),
	}
	t, err := transport.NewStreamableHTTP(m.resource, opts...)
	if err != nil {
		return nil, fmt.Errorf("trale: create MCP transport: %w", err)
	}
	cl := mcpclient.NewClient(t)
	if err := cl.Start(ctx); err != nil {
		cl.Close()
		return nil, fmt.Errorf("trale: start MCP session: %w", err)
	}
	if _, err := cl.Initialize(ctx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcpgo.Implementation{
				Name:    "emergent",
				Version: "1.0.0",
			},
		},
	}); err != nil {
		cl.Close()
		return nil, fmt.Errorf("trale: initialize MCP session: %w", err)
	}
	m.client = cl
	return cl, nil
}

// Close releases the MCP session created lazily by the first tool call. The
// connector satisfies io.Closer so the sync worker tears the session down when
// the job finishes instead of leaving it to the server's session TTL.
func (c *traleConnector) Close() error {
	if c.mcp != nil {
		c.mcp.close()
	}
	return nil
}

func (m *traleMCP) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
}

func (m *traleMCP) callTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	cl, err := m.session(ctx)
	if err != nil {
		return nil, err
	}
	return cl.CallTool(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: name, Arguments: args},
	})
}

func (m *traleMCP) listTools(ctx context.Context) (map[string]bool, error) {
	cl, err := m.session(ctx)
	if err != nil {
		return nil, err
	}
	res, err := cl.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(res.Tools))
	for _, t := range res.Tools {
		names[t.Name] = true
	}
	return names, nil
}

// traleConnector implements Connector over Trale's remote MCP server.
type traleConnector struct {
	creds  traleCredentials
	http   *http.Client
	config traleSyncConfig
	toks   *traleTokenState
	mcp    *traleMCP
}

func newTraleConnector(creds traleCredentials, httpc *http.Client) *traleConnector {
	if httpc == nil {
		httpc = mcpauth.OutboundHTTPClient(creds.resource)
	}
	toks := newTraleTokenState(creds, httpc)
	return &traleConnector{
		creds: creds,
		http:  httpc,
		toks:  toks,
		mcp:   newTraleMCP(creds.resource, httpc, toks),
	}
}

// Enumerate pages list_transcriptions over the backfill window, skipping
// non-completed meetings, then applies the name_pattern filter client-side.
func (c *traleConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	parsed, err := parseTraleConfig(cfg)
	if err != nil {
		return nil, err
	}
	c.config = *parsed

	now := time.Now().UTC()
	dateFrom := now.Add(-time.Duration(parsed.backfillDays) * 24 * time.Hour)

	var items []SourceItem
	for page := 1; ; page++ {
		if page > traleMaxPages {
			return nil, fmt.Errorf("trale: list_transcriptions exceeded %d pages without reaching the last one", traleMaxPages)
		}
		result, err := c.listTranscriptions(ctx, map[string]any{
			"limit":    tralePageSize,
			"page":     page,
			"dateFrom": dateFrom.Format(time.RFC3339),
			"dateTo":   now.Format(time.RFC3339),
			"tags":     parsed.tags,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range result.Meetings {
			if m.Status != traleCompleted {
				continue
			}
			if parsed.nameRegex != nil && !parsed.nameRegex.MatchString(m.Title) {
				continue
			}
			items = append(items, SourceItem{
				ID:           m.ID,
				Name:         m.Title,
				MimeType:     traleMarkdownMimeType,
				RevisionID:   traleRevisionID(m),
				ModifiedTime: traleParseDate(m.Date),
			})
		}
		if !result.Pagination.HasNext {
			break
		}
	}
	return items, nil
}

func (c *traleConnector) listTranscriptions(ctx context.Context, args map[string]any) (*traleListResult, error) {
	res, err := c.mcp.callTool(ctx, "list_transcriptions", args)
	if err != nil {
		return nil, fmt.Errorf("trale: list_transcriptions: %w", err)
	}
	var out traleListResult
	if err := decodeToolResult(res, &out); err != nil {
		return nil, fmt.Errorf("trale: list_transcriptions: %w", err)
	}
	return &out, nil
}

func (c *traleConnector) listTags(ctx context.Context) ([]traleTag, error) {
	res, err := c.mcp.callTool(ctx, "list_tags", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("trale: list_tags: %w", err)
	}
	var wrapper struct {
		Tags []traleTag `json:"tags"`
	}
	if err := decodeToolResult(res, &wrapper); err != nil {
		return nil, fmt.Errorf("trale: list_tags: %w", err)
	}
	return wrapper.Tags, nil
}

// Fetch renders one meeting as a Markdown document.
func (c *traleConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	res, err := c.mcp.callTool(ctx, "fetch", map[string]any{"id": item.ID})
	if err != nil {
		return "", "", fmt.Errorf("trale: fetch: %w", err)
	}
	var detail traleMeetingDetail
	if err := decodeToolResult(res, &detail); err != nil {
		return "", "", fmt.Errorf("trale: fetch: %w", err)
	}
	return renderTraleMarkdown(detail, c.config.includeTranscript), traleMarkdownMimeType, nil
}

// TestConnection initializes the MCP session, lists tools, and requires
// list_transcriptions and fetch, naming what is missing.
func (c *traleConnector) TestConnection(ctx context.Context, cfg map[string]any) error {
	if _, err := parseTraleConfig(cfg); err != nil {
		return err
	}
	names, err := c.mcp.listTools(ctx)
	if err != nil {
		return err
	}
	var missing []string
	for _, required := range []string{"list_transcriptions", "fetch"} {
		if !names[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("trale: MCP server is missing required tools: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ---- MCP result shapes ----

// traleListResult is the structuredContent of list_transcriptions.
type traleListResult struct {
	Meetings   []traleMeeting  `json:"meetings"`
	Pagination tralePagination `json:"pagination"`
}

type traleMeeting struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	Date             string `json:"date"`
	Status           string `json:"status"`
	Participants     any    `json:"participants"`
	ParticipantCount int    `json:"participant_count"`
	Tags             []any  `json:"tags"`
	Keyframes        any    `json:"keyframes"`
}

type tralePagination struct {
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasNext bool `json:"has_next"`
	HasPrev bool `json:"has_prev"`
}

// traleMeetingDetail is the structuredContent of fetch(id).
type traleMeetingDetail struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Date              string `json:"date"`
	Language          string `json:"language"`
	MeetingType       string `json:"meeting_type"`
	Status            string `json:"status"`
	Summary           string `json:"summary"`
	StructuredSummary string `json:"structured_summary"` // JSON-encoded string
	Content           string `json:"content"`            // speaker-tagged transcript
	Participants      []any  `json:"participants"`
	ParticipantCount  int    `json:"participant_count"`
	Platform          string `json:"platform"`
	Type              string `json:"type"`
	HasVideo          bool   `json:"has_video"`
	StartTime         string `json:"start_time"`
	EndTime           string `json:"end_time"`
	Tags              []any  `json:"tags"`
	Keyframes         any    `json:"keyframes"`
	Notes             any    `json:"notes"`
}

type traleTag struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	SortOrder   int    `json:"sort_order"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
}

// decodeToolResult extracts the structuredContent of a tool result (falling back
// to the text block that duplicates it) and decodes it into out.
func decodeToolResult(res *mcpgo.CallToolResult, out any) error {
	v, err := traleStructuredContent(res)
	if err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func traleStructuredContent(res *mcpgo.CallToolResult) (any, error) {
	if res == nil {
		return nil, fmt.Errorf("tool returned no result")
	}
	if res.IsError {
		msg := "unknown error"
		for _, c := range res.Content {
			if tc, ok := mcpgo.AsTextContent(c); ok {
				msg = tc.Text
				break
			}
		}
		return nil, fmt.Errorf("tool error: %s", msg)
	}
	if res.StructuredContent != nil {
		return res.StructuredContent, nil
	}
	for _, c := range res.Content {
		if tc, ok := mcpgo.AsTextContent(c); ok && strings.TrimSpace(tc.Text) != "" {
			var v any
			if err := json.Unmarshal([]byte(tc.Text), &v); err == nil {
				return v, nil
			}
			break
		}
	}
	return nil, fmt.Errorf("tool returned no structured content")
}

// traleRevisionID is the stable revision fingerprint: any change to status,
// participant count, tag slugs, or summary changes it, so the sync worker
// re-fetches the meeting.
func traleRevisionID(m traleMeeting) string {
	slugs := traleTagSlugs(m.Tags)
	sort.Strings(slugs)
	material := fmt.Sprintf("%s|%d|%s|%s", m.Status, m.ParticipantCount, strings.Join(slugs, ","), m.Summary)
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

// traleTagSlugs extracts the slug (or name) of each tag element, tolerating
// both string elements and object elements.
func traleTagSlugs(tags []any) []string {
	var out []string
	for _, t := range tags {
		switch v := t.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		case map[string]any:
			if slug, ok := v["slug"].(string); ok && strings.TrimSpace(slug) != "" {
				out = append(out, slug)
			} else if name, ok := v["name"].(string); ok && strings.TrimSpace(name) != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// traleParseDate parses a meeting date string with the common layouts; it
// returns the zero time when unrecognised (ModifiedTime is informational only).
func traleParseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// traleFormatTime renders a timestamp as "2006-01-02 15:04 UTC" (date-only
// values keep their date). An unparseable non-empty value is returned trimmed
// rather than dropped, so the document never loses information silently.
func traleFormatTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			if layout == "2006-01-02" {
				return t.Format("2006-01-02")
			}
			return t.UTC().Format("2006-01-02 15:04 UTC")
		}
	}
	return s
}

// traleTimeSpan renders a scheduled start–end span. When both ends fall on the
// same UTC day the end collapses to a clock time ("2026-10-21 08:15–09:15 UTC");
// a missing end returns the start alone.
func traleTimeSpan(start, end string) string {
	s := traleFormatTime(start)
	e := traleFormatTime(end)
	switch {
	case s == "":
		return e
	case e == "":
		return s
	}
	// "2006-01-02 15:04 UTC" is 20 chars: date(10) + space + clock(5) + " UTC".
	if len(s) == 20 && len(e) == 20 && s[:10] == e[:10] {
		return s[:16] + "\u2013" + e[11:16] + " UTC"
	}
	return s + "\u2013" + e
}

// ---- Markdown rendering ----

func renderTraleMarkdown(d traleMeetingDetail, includeTranscript bool) string {
	var b strings.Builder

	b.WriteString("# ")
	b.WriteString(strings.TrimSpace(d.Title))
	b.WriteString("\n\n")

	var meta []string
	// Prefer the scheduled start–end span and fall back to the recorded date:
	// emitting both duplicated the day and read as noise. Raw RFC3339 strings are
	// normalised, since this line is read by people, not machines.
	if span := traleTimeSpan(d.StartTime, d.EndTime); span != "" {
		meta = append(meta, span)
	} else if ts := traleFormatTime(d.Date); ts != "" {
		meta = append(meta, ts)
	}
	if strings.TrimSpace(d.Platform) != "" {
		meta = append(meta, d.Platform)
	}
	if strings.TrimSpace(d.Status) != "" {
		meta = append(meta, d.Status)
	}
	if slugs := traleTagSlugs(d.Tags); len(slugs) > 0 {
		meta = append(meta, "tags: "+strings.Join(slugs, ", "))
	}
	if len(meta) > 0 {
		b.WriteString(strings.Join(meta, " \u00b7 "))
		b.WriteString("\n\n")
	}

	if parts := traleParticipants(d.Participants); len(parts) > 0 {
		b.WriteString("## Participants\n\n")
		for _, p := range parts {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(d.Summary) != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(d.Summary)
		b.WriteString("\n\n")
	}

	if s := strings.TrimSpace(d.StructuredSummary); s != "" {
		body := prettyJSON(s)
		// The fence is one backtick longer than the longest run in the content, so
		// a summary containing ``` cannot terminate the block early and inject
		// arbitrary markdown into the document.
		fence := traleMarkdownFence(body)
		b.WriteString("## Structured summary\n\n" + fence + "json\n")
		b.WriteString(body)
		b.WriteString("\n" + fence + "\n\n")
	}

	if includeTranscript && strings.TrimSpace(d.Content) != "" {
		b.WriteString("## Transcript\n\n")
		b.WriteString(d.Content)
		b.WriteString("\n")
	}

	return b.String()
}

// traleParticipants renders participants as "display / email / org" lines,
// tolerating string elements and object elements with varying key names.
func traleParticipants(parts []any) []string {
	var out []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		case map[string]any:
			var seg []string
			display := ""
			for _, k := range []string{"display", "display_name", "displayName", "name", "full_name"} {
				if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
					display = strings.TrimSpace(s)
					break
				}
			}
			email := ""
			if s, ok := v["email"].(string); ok && strings.TrimSpace(s) != "" {
				email = strings.TrimSpace(s)
			}
			// "Unknown" is Trale's placeholder for an unmatched contact; when the
			// email is known it identifies the participant on its own.
			if display != "" && (email == "" || !strings.EqualFold(display, "unknown")) {
				seg = append(seg, display)
			}
			if email != "" {
				seg = append(seg, email)
			}
			for _, k := range []string{"org", "organization", "company"} {
				if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
					seg = append(seg, s)
					break
				}
			}
			if len(seg) > 0 {
				out = append(out, strings.Join(seg, " / "))
			}
		}
	}
	return out
}

// prettyJSON re-indents a JSON-encoded string for the fenced block; a value that
// is not valid JSON is included verbatim.
// traleMarkdownFence returns a backtick fence longer than any run of backticks
// in content, so an embedded code block cannot close the fence early.
func traleMarkdownFence(content string) string {
	longest, run := 0, 0
	for _, r := range content {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest >= n {
		n = longest + 1
	}
	return strings.Repeat("`", n)
}

func prettyJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}
