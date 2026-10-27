package sources

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/gmail"
	"github.com/emergent-company/emergent.memory/internal/webcrawl"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"google.golang.org/api/option"
)

// gmailReadonlyScope is the OAuth2 scope a Gmail source is authorized for. The
// internal/gmail client owns the scope it requests; this alias keeps the
// authorize request and the token refresh in step.
const gmailReadonlyScope = gmail.ReadonlyScope

// Gmail provider limits. A mailbox is polled rather than pushed, so the cadence
// matches the other network-backed providers: at most every 15 minutes, once an
// hour by default.
const (
	gmailDefaultQuery       = "in:inbox"
	gmailMinIntervalMin     = 15
	gmailDefaultIntervalMin = 60
	// gmailNameLimit bounds the thread-derived document title/filename.
	gmailNameLimit = 80
)

// GmailTransport is the Gmail transport surface the connector needs: the subset
// of *gmail.Client used for enumeration, thread fetch, connection testing, and
// label listing; a fake satisfies it in tests.
type GmailTransport interface {
	ListThreads(ctx context.Context, opts gmail.ListOptions) ([]gmail.ThreadRef, bool, error)
	GetThread(ctx context.Context, id string) (*gmail.Thread, error)
	ListLabels(ctx context.Context) ([]gmail.Label, error)
	TestConnection(ctx context.Context) error
}

// gmailProvider implements SourceProvider for the gmail provider.
type gmailProvider struct {
	newClient func(ctx context.Context, creds gmail.Credentials) (GmailTransport, error)
}

// newGmailProvider builds the Gmail provider. apiBaseURL and tokenBaseURL are
// deployment-level test seams (GMAIL_API_BASE_URL / GMAIL_TOKEN_URL): they point
// the API and the OAuth2 refresh grant at a hermetic stand-in for Google, and a
// source config can never choose either.
func newGmailProvider(apiBaseURL, tokenBaseURL string) *gmailProvider {
	return &gmailProvider{
		newClient: func(ctx context.Context, creds gmail.Credentials) (GmailTransport, error) {
			var opts []option.ClientOption
			if apiBaseURL != "" {
				opts = append(opts, option.WithEndpoint(apiBaseURL))
			}
			cfg := gmail.DefaultConfig()
			cfg.TokenEndpoint = tokenBaseURL
			return gmail.NewWithConfig(ctx, creds, cfg, opts...)
		},
	}
}

// NewGmailProvider builds the Gmail provider with an injectable transport
// constructor. It exists so tests can substitute a fake transport and exercise
// the real connector logic. Production wiring uses the default (gmail.New) via
// the registry.
func NewGmailProvider(newClient func(ctx context.Context, creds gmail.Credentials) (GmailTransport, error)) SourceProvider {
	return &gmailProvider{newClient: newClient}
}

func (p *gmailProvider) Catalog() Provider {
	return Provider{
		Type:        ProviderTypeGmail,
		Name:        "Gmail",
		Description: "Sync email threads from a Gmail mailbox into your knowledge base.",
		SourceType:  SourceTypeEmail,
		Vendor:      "google",
		VendorName:  "Google",
		Available:   true,
	}
}

// ConfigSchema is the non-secret Gmail scope contract. Credentials are
// OAuth-only: client id/secret are configured per project/org OAuth app, never
// per integration.
func (p *gmailProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{
		Type: ProviderTypeGmail,
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Gmail search expression selecting which threads to sync (default \"in:inbox\").",
			},
			"label_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional label ids; a thread must carry every listed label.",
			},
			"since": map[string]any{
				"type":        "string",
				"description": "Optional start date (YYYY-MM-DD). Threads dated before it are never synced. It is fixed at configuration time, so the window only grows and previously synced threads never fall out of scope.",
			},
			"max_threads": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum threads one pass enumerates: the newest threads listed on every sync, and the budget for the backfill windows that sync walks. A mailbox holding more than this is backfilled window by window on successive syncs until the whole scope is covered. Default %d; ceiling %d.", gmail.DefaultMaxThreads, gmail.MaxThreadsLimit),
			},
		},
	}
}

// CredentialFields is empty: Gmail connect is OAuth-only.
func (p *gmailProvider) CredentialFields() []ProviderField { return nil }

func (p *gmailProvider) AuthMethods() []AuthMethod { return []AuthMethod{AuthMethodOAuth} }

func (p *gmailProvider) OAuth() (OAuthStrategy, bool) {
	return newGmailOAuthStrategy(), true
}

// CredentialKeys returns the write-only credential keys the generic OAuth
// callback copies into the stored credential map.
func (p *gmailProvider) CredentialKeys() []string { return credentialKeyNames }

// gmailReconnectRequired is the remediation for a source whose stored refresh
// token Google no longer accepts (revoked or expired). No retry recovers the
// grant, so it is worded as a validation error and the UI shows the action.
const gmailReconnectRequired = "the Gmail connection for this source has expired or was revoked; reconnect the source to continue"

// gmailGrantOrRaw swaps Google's rejected-grant error for the Gmail reconnect
// message and leaves transient failures (5xx, timeouts) untouched so they stay
// retryable. isDriveGrantRejected inspects the Google token endpoint itself, so
// it applies unchanged to every Google-backed provider.
func gmailGrantOrRaw(err error) error {
	if isDriveGrantRejected(err) {
		return apperror.NewValidation(gmailReconnectRequired)
	}
	return err
}

// ListResources lists the mailbox's labels so the connect form can offer a
// label picker. query (a name filter) is applied client-side by the form, so it
// is ignored here — the label set is small and bounded. deps is unused: Google
// does not rotate refresh tokens, so a listing has no credential to write back.
func (p *gmailProvider) ListResources(ctx context.Context, creds Credentials, cfg map[string]any, query string, _ ConnectorDeps) ([]Resource, bool, error) {
	client, err := p.newClient(ctx, gmailCredentials(creds))
	if err != nil {
		return nil, false, err
	}
	labels, err := client.ListLabels(ctx)
	if err != nil {
		return nil, false, gmailGrantOrRaw(err)
	}
	out := make([]Resource, len(labels))
	for i, l := range labels {
		out[i] = Resource{ID: l.ID, Name: l.Name, Type: "label"}
	}
	return out, false, nil
}

func (p *gmailProvider) ValidateConfig(cfg map[string]any) error {
	_, err := parseGmailConfig(cfg)
	return err
}

func (p *gmailProvider) ExtractCredentials(cfg map[string]any) (Credentials, error) {
	creds := Credentials{}
	for _, k := range credentialKeyNames {
		if v, ok := cfg[k]; ok {
			creds[k] = v
		}
	}
	if token, _ := creds["refresh_token"].(string); strings.TrimSpace(token) == "" {
		return nil, apperror.NewValidation("config.refresh_token is required")
	}
	return creds, nil
}

func (p *gmailProvider) NewConnector(ctx context.Context, creds Credentials, deps ConnectorDeps) (Connector, error) {
	client, err := p.newClient(ctx, gmailCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &gmailConnector{client: client, deps: deps}, nil
}

// SyncIntervalBounds implements SyncIntervalBoundsProvider: a Gmail source
// syncs every 60 minutes by default and may not be scheduled faster than every
// 15 (Gmail's per-user quota punishes tighter polling).
func (p *gmailProvider) SyncIntervalBounds() (int, int) {
	return gmailMinIntervalMin, gmailDefaultIntervalMin
}

// gmailCredentials projects the opaque credential map onto the client's typed
// credential struct. The scope is not carried through: Gmail connect always
// requests gmail.readonly, and Google's refresh grant is bound to the scope the
// user approved, so a per-source scope would be cosmetic.
func gmailCredentials(creds Credentials) gmail.Credentials {
	c, _ := creds["client_id"].(string)
	s, _ := creds["client_secret"].(string)
	r, _ := creds["refresh_token"].(string)
	return gmail.Credentials{ClientID: c, ClientSecret: s, RefreshToken: r}
}

// gmailConnector implements Connector over a Gmail transport.
type gmailConnector struct {
	client GmailTransport
	deps   ConnectorDeps
}

// Keys the connector persists in the source cursor row. They share the map with
// the worker's total_items/synced_at, and the gmail_ prefix keeps a
// provider-neutral reader from mistaking them for worker state.
const (
	gmailCursorScopeKey    = "gmail_scope"
	gmailCursorBoundaryKey = "gmail_boundary"
	gmailCursorWindowKey   = "gmail_window_days"
	gmailCursorCompleteKey = "gmail_backfill_complete"
)

const (
	// gmailBackfillWindowDays is the starting backfill window. Gmail's after:/
	// before: operators are day-granular, so a calendar window is the finest
	// range the listing can express.
	gmailBackfillWindowDays = 30
	// gmailMinBackfillWindowDays is the narrowest window the walk uses when a
	// window is larger than one sync may ingest.
	gmailMinBackfillWindowDays = 1
	// gmailInitialItemCap keeps the first allocation bounded; a source may raise
	// max_threads to 50,000.
	gmailInitialItemCap = 1024
	// gmailMaxWindowsPerSync bounds how far one sync walks a mail-free stretch.
	// Without it a source with no mail for years would issue hundreds of
	// listings in one job and could be cancelled before the cursor advances,
	// which would restart the same stretch forever. 60 windows is about five
	// years at the default width, for roughly 600 quota units.
	gmailMaxWindowsPerSync = 60
)

// gmailBackfill is the persisted coverage watermark: the newest day not yet
// covered by the walk (Boundary), the window width to try next, and whether the
// walk reached the floor. Scope is the fingerprint of the config that produced
// it — a changed query, label set, or since date invalidates the watermark,
// because a different scope has different coverage.
type gmailBackfill struct {
	Scope      string
	Boundary   time.Time
	WindowDays int
	Complete   bool
}

// Enumerate lists the newest threads in scope. It is the cursor-free entry
// point (a connection test or a one-off enumeration); the sync worker drives
// EnumerateWithCursor so the backfill state survives across runs.
func (c *gmailConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	items, _, err := c.EnumerateWithCursor(ctx, cfg, nil)
	return items, err
}

// EnumerateWithCursor walks the source's threads in two passes and returns the
// cursor state to persist.
//
// The head pass lists the newest threads in scope — exactly what v1 did — so new
// mail and recent thread activity land on every sync. When that listing is
// capped, older windows are walked in the same sync from the persisted
// watermark, and the watermark advances window by window across syncs until it
// reaches the floor. That makes a mailbox larger than max_threads eventually
// fully covered instead of only ever contributing its newest slice.
//
// A window costs one listing whatever it contains, so the walk continues through
// empty stretches (a gap in the mailbox) up to gmailMaxWindowsPerSync per sync.
//
// The returned cursor is nil-free and complete: the worker merges it into the
// source row after the batch is applied, so an interrupted sync re-walks the
// same window and loses nothing (unchanged threads are skipped by revision).
func (c *gmailConnector) EnumerateWithCursor(ctx context.Context, cfg map[string]any, cursor map[string]any) ([]SourceItem, map[string]any, error) {
	parsed, err := parseGmailConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	state := gmailBackfillFrom(cursor, gmailScopeFingerprint(parsed), time.Now().UTC())
	floor := gmailBackfillFloor(parsed.Since)

	seen := make(map[string]bool, gmailInitialItemCap)
	out := make([]SourceItem, 0, gmailInitialItemCap)

	refs, truncated, err := c.client.ListThreads(ctx, gmail.ListOptions{
		Query:      parsed.Query,
		LabelIDs:   parsed.LabelIDs,
		Since:      parsed.Since,
		MaxThreads: parsed.MaxThreads,
	})
	if err != nil {
		return nil, nil, gmailGrantOrRaw(err)
	}
	out = appendGmailItems(out, seen, refs)

	switch {
	case !truncated:
		// The head pass saw every thread in scope: there is nothing older left
		// to walk, so the source is fully covered from the first sync.
		state.Complete = true
		state.Boundary = floor
	case state.Complete:
		// The mailbox outgrew the ceiling since its last full pass, so the
		// stored completion no longer proves coverage: a sticky flag would mean
		// a source that was once small never backfills again. Restart the walk.
		state = newGmailBackfill(state.Scope, time.Now().UTC())
	}

	budget := parsed.MaxThreads
	windows := 0
	for !state.Complete && budget > 0 && windows < gmailMaxWindowsPerSync {
		windows++
		start := state.Boundary.AddDate(0, 0, -state.WindowDays)
		if start.Before(floor) {
			start = floor
		}
		if !start.Before(state.Boundary) {
			state.Complete = true
			state.Boundary = floor
			break
		}

		refs, truncated, err := c.client.ListThreads(ctx, gmail.ListOptions{
			Query:      parsed.Query,
			LabelIDs:   parsed.LabelIDs,
			After:      start,
			Before:     state.Boundary,
			MaxThreads: budget,
		})
		if err != nil {
			return nil, nil, gmailGrantOrRaw(err)
		}
		out = appendGmailItems(out, seen, refs)
		budget -= len(refs)

		if truncated {
			// The window holds more threads than one sync may ingest. Keep what
			// was listed but do not advance past it; narrow the window so
			// successive syncs converge instead of re-listing the same block.
			if state.WindowDays > gmailMinBackfillWindowDays {
				state.WindowDays /= 2
				if state.WindowDays < gmailMinBackfillWindowDays {
					state.WindowDays = gmailMinBackfillWindowDays
				}
			} else {
				// A single day above the ceiling: accept the cap for that day —
				// the same truncation v1 applied to the whole listing — and step
				// past it so the walk always makes progress.
				state.Boundary = start
				if !state.Boundary.After(floor) {
					state.Complete = true
					state.Boundary = floor
				}
			}
			break
		}

		// Overlap the next window by one day. after:/before: are day-granular, so
		// a thread dated exactly on the boundary may be listed by either side;
		// re-listing it is free (the worker skips an unchanged revision), while
		// dropping it would be a permanent gap.
		next := start.AddDate(0, 0, 1)
		if !next.Before(state.Boundary) {
			next = state.Boundary.AddDate(0, 0, -1)
		}
		state.Boundary = next
		if !state.Boundary.After(floor) {
			state.Complete = true
			state.Boundary = floor
		}
	}

	return out, state.cursor(), nil
}

// appendGmailItems converts a listing batch into source items, keeping the first
// occurrence of a thread id. The head pass and the first backfill window
// overlap, so duplicates are expected.
func appendGmailItems(out []SourceItem, seen map[string]bool, refs []gmail.ThreadRef) []SourceItem {
	for _, ref := range refs {
		if ref.ID == "" || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		out = append(out, SourceItem{
			ID:         ref.ID,
			Name:       gmailThreadName(ref),
			MimeType:   "text/markdown",
			RevisionID: gmailRevision(ref),
		})
	}
	return out
}

// newGmailBackfill starts a walk at today: the head pass already covers the
// newest threads, and the first window re-lists them (free, deduped) before
// reaching older ones.
func newGmailBackfill(scope string, now time.Time) gmailBackfill {
	return gmailBackfill{
		Scope:      scope,
		Boundary:   gmailDay(now),
		WindowDays: gmailBackfillWindowDays,
	}
}

// gmailBackfillFrom reads the persisted watermark, falling back to a fresh walk
// when the cursor is empty, names another scope, or cannot be parsed. A corrupt
// or stale cursor must never fail a sync: the worst case is re-walking coverage
// the source already has, which the revision check makes cheap.
func gmailBackfillFrom(cursor map[string]any, scope string, now time.Time) gmailBackfill {
	fresh := newGmailBackfill(scope, now)
	if len(cursor) == 0 {
		return fresh
	}
	if got, _ := cursor[gmailCursorScopeKey].(string); got != scope {
		return fresh
	}
	raw, _ := cursor[gmailCursorBoundaryKey].(string)
	boundary, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return fresh
	}
	fresh.Boundary = boundary
	if days, ok := toInt(cursor[gmailCursorWindowKey]); ok && days >= gmailMinBackfillWindowDays && days <= gmailBackfillWindowDays {
		fresh.WindowDays = days
	}
	if complete, _ := cursor[gmailCursorCompleteKey].(bool); complete {
		fresh.Complete = true
	}
	return fresh
}

// cursor projects the watermark into the source cursor map.
func (s gmailBackfill) cursor() map[string]any {
	return map[string]any{
		gmailCursorScopeKey:    s.Scope,
		gmailCursorBoundaryKey: s.Boundary.Format("2006-01-02"),
		gmailCursorWindowKey:   s.WindowDays,
		gmailCursorCompleteKey: s.Complete,
	}
}

// gmailScopeFingerprint identifies the coverage scope a watermark belongs to.
// Two syncs with the same fingerprint cover the same mailbox region.
func gmailScopeFingerprint(c gmailConfig) string {
	since := ""
	if !c.Since.IsZero() {
		since = gmailDay(c.Since).Format("2006-01-02")
	}
	return strings.Join([]string{c.Query, strings.Join(c.LabelIDs, ","), since}, "\x1f")
}

// gmailBackfillFloor is the oldest day the walk visits: the source's own since
// date when set, else Gmail's launch day (2004-04-01) — no mailbox can hold mail
// older than that, so the walk terminates deterministically in either case.
func gmailBackfillFloor(since time.Time) time.Time {
	if !since.IsZero() {
		return gmailDay(since)
	}
	return time.Date(2004, 4, 1, 0, 0, 0, 0, time.UTC)
}

// gmailDay truncates a timestamp to its UTC calendar day, the unit Gmail's
// after:/before: operators work in.
func gmailDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (c *gmailConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	thread, err := c.client.GetThread(ctx, item.ID)
	if err != nil {
		return "", "", gmailGrantOrRaw(err)
	}
	content, err = gmailThreadMarkdown(thread)
	if err != nil {
		return "", "", err
	}
	return content, "text/markdown", nil
}

func (c *gmailConnector) TestConnection(ctx context.Context, _ map[string]any) error {
	return gmailGrantOrRaw(c.client.TestConnection(ctx))
}

// EnumerationComplete always reports false: a Gmail listing is a filter over a
// live mailbox, so a thread missing from it may simply have been archived,
// relabelled, or moved out of the configured query (the same scope-loss problem
// the Trale connector has), and a capped listing is partial by construction.
// Treating absence as deletion would bury documents the user still wants, so
// v1 never deletes Gmail-sourced documents; history-based deletion detection is
// a documented follow-up.
func (c *gmailConnector) EnumerationComplete() bool { return false }

// gmailConfig is the parsed, validated shape of a Gmail source config.
type gmailConfig struct {
	Query      string
	LabelIDs   []string
	Since      time.Time
	MaxThreads int
}

// parseGmailConfig enforces the Gmail config contract: query and label_ids
// optional, since (when present) a calendar date or RFC 3339 timestamp,
// max_threads an integer within the advertised range. Unknown keys are ignored,
// matching the other providers.
//
// Scope precedence: an explicit query is used as given; a label filter with no
// explicit query means "everything carrying those labels" (the default inbox
// query would otherwise AND with the labels and silently narrow a picked label);
// with neither, the default inbox query applies.
func parseGmailConfig(cfg map[string]any) (gmailConfig, error) {
	out := gmailConfig{MaxThreads: gmail.DefaultMaxThreads}
	querySet := false

	if v, ok := cfg["query"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return out, apperror.NewValidation("config.query must be a string")
		}
		if strings.TrimSpace(s) != "" {
			out.Query = strings.TrimSpace(s)
			querySet = true
		}
	}

	labelIDs, err := stringList(cfg["label_ids"])
	if err != nil {
		return out, apperror.NewValidation("config.label_ids must be an array of non-empty strings")
	}
	out.LabelIDs = labelIDs

	if !querySet && len(out.LabelIDs) == 0 {
		out.Query = gmailDefaultQuery
	}

	if v, ok := cfg["since"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return out, apperror.NewValidation("config.since must be a date string (YYYY-MM-DD)")
		}
		s = strings.TrimSpace(s)
		if s != "" {
			since, perr := parseSinceDate(s)
			if perr != nil {
				return out, apperror.NewValidation("config.since must be a date string (YYYY-MM-DD)")
			}
			out.Since = since
		}
	}

	if v, ok := cfg["max_threads"]; ok && v != nil {
		n, ok := toInt(v)
		if !ok || n <= 0 {
			return out, apperror.NewValidation("config.max_threads must be a positive integer")
		}
		if n > gmail.MaxThreadsLimit {
			return out, apperror.NewValidation(fmt.Sprintf("config.max_threads must be at most %d", gmail.MaxThreadsLimit))
		}
		out.MaxThreads = n
	}

	return out, nil
}

// parseSinceDate accepts a calendar date (the documented form) or an RFC 3339
// timestamp (SDK callers that pass a time value through JSON).
func parseSinceDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// stringList coerces a JSON/Go value into a slice of non-empty strings. A nil or
// absent value yields an empty slice.
func stringList(v any) ([]string, error) {
	switch val := v.(type) {
	case nil:
		return nil, nil
	case []string:
		out := make([]string, 0, len(val))
		for _, s := range val {
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("empty entry")
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("non-string entry")
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("wrong type")
	}
}

// toInt coerces a JSON number (float64) or a Go integer into an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// gmailRevision is the connector's revision id for a thread: Gmail's per-thread
// history id, which advances when a message is added, removed, or relabelled.
// An unchanged history id lets the worker skip the thread without a fetch.
func gmailRevision(ref gmail.ThreadRef) string {
	if ref.HistoryID == 0 {
		return ""
	}
	return strconv.FormatUint(ref.HistoryID, 10)
}

// gmailThreadName derives a document title from a thread listing. The listing
// carries only a snippet (no subject), so the title is the collapsed snippet,
// bounded to a readable length; a thread without one falls back to its id.
func gmailThreadName(ref gmail.ThreadRef) string {
	name := strings.Join(strings.Fields(ref.Snippet), " ")
	if name == "" {
		return "Gmail thread " + ref.ID
	}
	if runes := []rune(name); len(runes) > gmailNameLimit {
		name = strings.TrimSpace(string(runes[:gmailNameLimit])) + "…"
	}
	return name
}

// gmailSystemLabels maps Gmail's system label ids to readable names. Custom
// labels carry opaque ids (Label_123) in the message payload, so they are
// omitted rather than rendered as noise; UNREAD and CHAT are state, not
// provenance, and are always omitted.
var gmailSystemLabels = map[string]string{
	"INBOX":               "Inbox",
	"SENT":                "Sent",
	"DRAFT":               "Draft",
	"SPAM":                "Spam",
	"TRASH":               "Trash",
	"STARRED":             "Starred",
	"IMPORTANT":           "Important",
	"CATEGORY_PERSONAL":   "Personal",
	"CATEGORY_SOCIAL":     "Social",
	"CATEGORY_PROMOTIONS": "Promotions",
	"CATEGORY_UPDATES":    "Updates",
	"CATEGORY_FORUMS":     "Forums",
}

// gmailThreadMarkdown renders a thread as the document content: a header
// summarising the conversation followed by one section per message, oldest
// first. Bodies prefer the text/plain part and fall back to converting the HTML
// part to Markdown; attachments are counted, never downloaded.
func gmailThreadMarkdown(t *gmail.Thread) (string, error) {
	if t == nil {
		return "", apperror.NewInternal("gmail thread is nil", nil)
	}
	if len(t.Messages) == 0 {
		return "", fmt.Errorf("gmail: thread %s has no messages", t.ID)
	}

	messages := make([]gmail.Message, len(t.Messages))
	copy(messages, t.Messages)
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].Date.Before(messages[j].Date)
	})

	subject := ""
	for _, m := range messages {
		if strings.TrimSpace(m.Subject) != "" {
			subject = strings.TrimSpace(m.Subject)
			break
		}
	}
	if subject == "" {
		subject = "(no subject)"
	}

	var b strings.Builder
	b.WriteString("# " + subject + "\n\n")
	fmt.Fprintf(&b, "- **Thread id**: %s\n", t.ID)
	fmt.Fprintf(&b, "- **Messages**: %d\n", len(messages))
	if participants := gmailParticipants(messages); len(participants) > 0 {
		b.WriteString("- **Participants**: " + strings.Join(participants, ", ") + "\n")
	}
	if latest := messages[len(messages)-1].Date; !latest.IsZero() {
		b.WriteString("- **Latest message**: " + latest.UTC().Format(time.RFC3339) + "\n")
	}
	if labels := gmailThreadLabels(messages); len(labels) > 0 {
		b.WriteString("- **Labels**: " + strings.Join(labels, ", ") + "\n")
	}
	b.WriteString("\n---\n")

	for i, m := range messages {
		fmt.Fprintf(&b, "\n## %d. %s\n\n", i+1, gmailMessageHeading(m))
		if m.From != "" {
			b.WriteString("**From:** " + m.From + "\n\n")
		}
		if m.To != "" {
			b.WriteString("**To:** " + m.To + "\n\n")
		}
		if m.Cc != "" {
			b.WriteString("**Cc:** " + m.Cc + "\n\n")
		}
		if m.Subject != "" {
			b.WriteString("**Subject:** " + m.Subject + "\n\n")
		}
		b.WriteString(gmailMessageBody(m))
		b.WriteString("\n")
		if m.Attachments > 0 {
			fmt.Fprintf(&b, "\n_%d attachment(s) omitted._\n", m.Attachments)
		}
	}

	return strings.TrimSpace(b.String()) + "\n", nil
}

// gmailMessageHeading is a message's section heading: its sender and date when
// known, else its id.
func gmailMessageHeading(m gmail.Message) string {
	when := "unknown date"
	if !m.Date.IsZero() {
		when = m.Date.UTC().Format("2006-01-02 15:04 MST")
	}
	from := strings.TrimSpace(m.From)
	if from == "" {
		if m.ID != "" {
			return when + " — message " + m.ID
		}
		return when
	}
	return from + " — " + when
}

// gmailMessageBody renders one message body: the plain-text part when present,
// otherwise the HTML part converted to Markdown, otherwise an explicit
// placeholder so the document still records the message's existence (invites and
// notifications are frequently body-less).
func gmailMessageBody(m gmail.Message) string {
	if text := strings.TrimSpace(m.Text); text != "" {
		return text
	}
	if html := strings.TrimSpace(m.HTML); html != "" {
		if md, _, err := webcrawl.ToMarkdown([]byte(html), "text/html", ""); err == nil && strings.TrimSpace(md) != "" {
			return strings.TrimSpace(md)
		}
		// A conversion failure must not drop the message: fall back to the raw
		// markup, which the ingestion pipeline can still parse.
		return html
	}
	return "_(no text content)_"
}

// gmailParticipants returns the distinct senders of a thread, in first-seen
// order.
func gmailParticipants(messages []gmail.Message) []string {
	seen := make(map[string]bool, len(messages))
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		from := strings.TrimSpace(m.From)
		if from == "" || seen[from] {
			continue
		}
		seen[from] = true
		out = append(out, from)
	}
	return out
}

// gmailThreadLabels returns the thread's recognisable system labels, deduped and
// ordered.
func gmailThreadLabels(messages []gmail.Message) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range messages {
		for _, id := range m.Labels {
			name, ok := gmailSystemLabels[id]
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
