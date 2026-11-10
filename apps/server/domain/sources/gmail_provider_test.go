package sources

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/emergent-company/emergent.memory/internal/gmail"
)

// fakeGmailTransport is a GmailTransport with canned responses and recorded
// inputs, so the connector tests exercise the real provider/connector logic.
type fakeGmailTransport struct {
	refs      []gmail.ThreadRef
	truncated bool
	threads   map[string]*gmail.Thread
	labels    []gmail.Label

	listErr   error
	threadErr error
	labelsErr error
	testErr   error

	gotOpts  gmail.ListOptions
	gotID    string
	tested   bool
	listCall int
}

func (f *fakeGmailTransport) ListThreads(_ context.Context, opts gmail.ListOptions) ([]gmail.ThreadRef, bool, error) {
	f.listCall++
	f.gotOpts = opts
	if f.listErr != nil {
		return nil, false, f.listErr
	}
	return f.refs, f.truncated, nil
}

func (f *fakeGmailTransport) GetThread(_ context.Context, id string) (*gmail.Thread, error) {
	f.gotID = id
	if f.threadErr != nil {
		return nil, f.threadErr
	}
	if th, ok := f.threads[id]; ok {
		return th, nil
	}
	return &gmail.Thread{}, nil
}

func (f *fakeGmailTransport) ListLabels(_ context.Context) ([]gmail.Label, error) {
	if f.labelsErr != nil {
		return nil, f.labelsErr
	}
	return f.labels, nil
}

func (f *fakeGmailTransport) TestConnection(_ context.Context) error {
	f.tested = true
	return f.testErr
}

// gmailProviderWith builds the Gmail provider over a fake transport.
func gmailProviderWith(f GmailTransport) SourceProvider {
	return NewGmailProvider(func(context.Context, gmail.Credentials) (GmailTransport, error) {
		return f, nil
	})
}

func gmailTestCredentials() Credentials {
	return Credentials{"refresh_token": "rt", "client_id": "cid", "client_secret": "secret", "scope": gmailReadonlyScope}
}

func TestGmailProviderCatalog(t *testing.T) {
	p := newGmailProvider("", "")
	cat := p.Catalog()

	if cat.Type != ProviderTypeGmail {
		t.Errorf("type = %q, want %q", cat.Type, ProviderTypeGmail)
	}
	if cat.SourceType != SourceTypeEmail {
		t.Errorf("sourceType = %q, want %q", cat.SourceType, SourceTypeEmail)
	}
	if cat.Vendor != "google" || cat.VendorName != "Google" {
		t.Errorf("vendor = %q/%q, want google/Google", cat.Vendor, cat.VendorName)
	}
	if !cat.Available {
		t.Error("available = false, want true")
	}

	methods := p.AuthMethods()
	if len(methods) != 1 || methods[0] != AuthMethodOAuth {
		t.Errorf("auth methods = %v, want [oauth]", methods)
	}
	if _, ok := p.OAuth(); !ok {
		t.Error("provider does not advertise OAuth")
	}
	if fields := p.CredentialFields(); len(fields) != 0 {
		t.Errorf("credential fields = %v, want none (OAuth-only)", fields)
	}
}

func TestGmailProviderSchema(t *testing.T) {
	schema := newGmailProvider("", "").ConfigSchema()
	for _, key := range []string{"filter", "object_types", "mcp_write_tools_enabled", "since", "max_threads"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("schema missing %q", key)
		}
	}
	if len(schema.Required) != 0 {
		t.Errorf("required = %v, want nothing (query-or-filter is validated at runtime)", schema.Required)
	}
}

func TestGmailValidateConfig(t *testing.T) {
	p := newGmailProvider("", "")
	tests := []struct {
		name    string
		cfg     map[string]any
		wantErr string
	}{
		{name: "empty is rejected", cfg: map[string]any{}, wantErr: "config.filter or config.query"},
		{name: "full config", cfg: map[string]any{
			"query":       "label:work",
			"label_ids":   []any{"INBOX", "Label_7"},
			"since":       "2025-01-31",
			"max_threads": float64(500),
		}},
		{name: "go string slice labels", cfg: map[string]any{"label_ids": []string{"INBOX"}}},
		{name: "query wrong type", cfg: map[string]any{"query": 5}, wantErr: "config.query"},
		{name: "labels wrong type", cfg: map[string]any{"label_ids": "INBOX"}, wantErr: "config.label_ids"},
		{name: "labels empty entry", cfg: map[string]any{"label_ids": []any{"INBOX", ""}}, wantErr: "config.label_ids"},
		{name: "since wrong type", cfg: map[string]any{"query": "in:inbox", "since": 20250101}, wantErr: "config.since"},
		{name: "since bad date", cfg: map[string]any{"query": "in:inbox", "since": "31/01/2025"}, wantErr: "config.since"},
		{name: "max_threads zero", cfg: map[string]any{"query": "in:inbox", "max_threads": 0}, wantErr: "config.max_threads"},
		{name: "max_threads negative", cfg: map[string]any{"query": "in:inbox", "max_threads": -1}, wantErr: "config.max_threads"},
		{name: "max_threads fractional", cfg: map[string]any{"query": "in:inbox", "max_threads": 1.5}, wantErr: "config.max_threads"},
		{name: "max_threads over ceiling", cfg: map[string]any{"query": "in:inbox", "max_threads": 50001}, wantErr: "config.max_threads"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateConfig(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateConfig() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestGmailParseConfigDefaults(t *testing.T) {
	if _, err := parseGmailConfig(map[string]any{}); err == nil {
		t.Fatal("parseGmailConfig accepted a source with no sync scope")
	}

	parsed, err := parseGmailConfig(map[string]any{"filter": map[string]any{"categories": []any{"Primary"}}})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if parsed.Query != "(category:primary)" {
		t.Errorf("query = %q, want the compiled category filter", parsed.Query)
	}
	if parsed.MaxThreads != gmail.DefaultMaxThreads {
		t.Errorf("max_threads = %d, want %d", parsed.MaxThreads, gmail.DefaultMaxThreads)
	}
	if !parsed.Since.IsZero() {
		t.Errorf("since = %v, want zero", parsed.Since)
	}

	parsed, err = parseGmailConfig(map[string]any{"since": "2024-02-29", "query": "in:inbox"})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if want := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC); !parsed.Since.Equal(want) {
		t.Errorf("since = %v, want %v", parsed.Since, want)
	}
	if parsed.Query != "in:inbox" {
		t.Errorf("query = %q, want the explicit value", parsed.Query)
	}

	if _, err := parseGmailConfig(map[string]any{"since": "2024-13-01", "query": "in:inbox"}); err == nil {
		t.Fatal("parseGmailConfig accepted an impossible date")
	}
}

// TestGmailLabelScopePrecedence pins the scope rule: a label filter means the
// label (not label ∩ inbox), while an explicit query always wins.
func TestGmailLabelScopePrecedence(t *testing.T) {
	// A label-scoped source: no default inbox query is ANDed in, because Gmail
	// ANDs q with labelIds and the pair would silently narrow the label.
	parsed, err := parseGmailConfig(map[string]any{"label_ids": []any{"Label_7"}})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if parsed.Query != "" {
		t.Errorf("query = %q, want empty when a label scopes the source", parsed.Query)
	}
	if len(parsed.LabelIDs) != 1 || parsed.LabelIDs[0] != "Label_7" {
		t.Errorf("label ids = %v, want [Label_7]", parsed.LabelIDs)
	}

	// An explicit query is never replaced by the default.
	parsed, err = parseGmailConfig(map[string]any{"query": "to:me", "label_ids": []any{"Label_7"}})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if parsed.Query != "to:me" {
		t.Errorf("query = %q, want the explicit value", parsed.Query)
	}
}

func TestGmailExtractCredentials(t *testing.T) {
	p := newGmailProvider("", "")

	creds, err := p.ExtractCredentials(map[string]any{
		"refresh_token": "rt",
		"client_id":     "cid",
		"query":         "in:inbox",
	})
	if err != nil {
		t.Fatalf("ExtractCredentials: %v", err)
	}
	if creds["refresh_token"] != "rt" || creds["client_id"] != "cid" {
		t.Errorf("creds = %v", creds)
	}
	if _, ok := creds["query"]; ok {
		t.Error("non-credential config key leaked into the credential payload")
	}

	if _, err := p.ExtractCredentials(map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "config.refresh_token is required") {
		t.Fatalf("ExtractCredentials(empty) = %v, want refresh_token required", err)
	}

	for _, key := range []string{"refresh_token", "client_secret", "client_id", "access_token"} {
		if !contains(p.CredentialKeys(), key) {
			t.Errorf("CredentialKeys() missing %q", key)
		}
	}
}

func TestGmailSyncIntervalBounds(t *testing.T) {
	var provider any = newGmailProvider("", "")
	p, ok := provider.(SyncIntervalBoundsProvider)
	if !ok {
		t.Fatal("gmail provider does not implement SyncIntervalBoundsProvider")
	}
	min, def := p.SyncIntervalBounds()
	if min != 15 || def != 60 {
		t.Fatalf("bounds = %d/%d, want 15/60", min, def)
	}
}

func TestGmailProviderRegistered(t *testing.T) {
	if _, ok := NewDefaultRegistry().Get(ProviderTypeGmail); !ok {
		t.Fatal("NewDefaultRegistry() has no gmail provider")
	}
	if _, ok := NewConfiguredRegistry(nil).Get(ProviderTypeGmail); !ok {
		t.Fatal("NewConfiguredRegistry(nil) has no gmail provider")
	}
}

func TestGmailEnumerate(t *testing.T) {
	f := &fakeGmailTransport{refs: []gmail.ThreadRef{
		{ID: "t1", HistoryID: 4242, Snippet: "Quarterly\n  planning notes"},
		{ID: "t2", HistoryID: 0, Snippet: ""},
	}}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	items, err := conn.Enumerate(context.Background(), map[string]any{
		"query":       "label:work",
		"label_ids":   []any{"Label_7"},
		"since":       "2025-03-04",
		"max_threads": float64(250),
	})
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	if f.gotOpts.Query != "label:work" || f.gotOpts.MaxThreads != 250 {
		t.Errorf("ListOptions = %+v", f.gotOpts)
	}
	if len(f.gotOpts.LabelIDs) != 1 || f.gotOpts.LabelIDs[0] != "Label_7" {
		t.Errorf("label ids = %v", f.gotOpts.LabelIDs)
	}
	if want := time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC); !f.gotOpts.Since.Equal(want) {
		t.Errorf("since = %v, want %v", f.gotOpts.Since, want)
	}

	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].ID != "t1" || items[0].RevisionID != "4242" {
		t.Errorf("item[0] = %+v, want t1/4242", items[0])
	}
	if items[0].Name != "Quarterly planning notes" {
		t.Errorf("name = %q, want the collapsed snippet", items[0].Name)
	}
	if items[0].MimeType != "text/markdown" {
		t.Errorf("mime = %q, want text/markdown", items[0].MimeType)
	}
	if items[1].RevisionID != "" {
		t.Errorf("history id 0 produced revision %q, want empty (always fetch)", items[1].RevisionID)
	}
	if items[1].Name != "Gmail thread t2" {
		t.Errorf("name = %q, want the id fallback", items[1].Name)
	}
}

func TestGmailEnumerateError(t *testing.T) {
	f := &fakeGmailTransport{listErr: errors.New("boom")}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if _, err := conn.Enumerate(context.Background(), map[string]any{"query": "in:inbox"}); err == nil {
		t.Fatal("Enumerate returned nil error for a failed listing")
	}
}

func TestGmailFetchMarkdown(t *testing.T) {
	f := &fakeGmailTransport{threads: map[string]*gmail.Thread{
		"t1": {
			ID: "t1",
			Messages: []gmail.Message{
				{
					ID:      "m2",
					From:    "Bob <bob@example.com>",
					To:      "Alice <alice@example.com>",
					Cc:      "carol@example.com",
					Subject: "Re: Launch plan",
					Date:    time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC),
					Labels:  []string{"INBOX", "CATEGORY_PERSONAL", "Label_9"},
					Text:    "Sounds good to me.",
				},
				{
					ID:          "m1",
					From:        "Alice <alice@example.com>",
					Subject:     "Launch plan",
					Date:        time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
					Labels:      []string{"INBOX"},
					HTML:        "<html><body><p>Here is the <b>plan</b>.</p></body></html>",
					Attachments: 2,
				},
			},
		},
	}}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	content, mime, err := conn.Fetch(context.Background(), SourceItem{ID: "t1"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if mime != "text/markdown" {
		t.Errorf("mime = %q, want text/markdown", mime)
	}
	if f.gotID != "t1" {
		t.Errorf("fetched thread = %q, want t1", f.gotID)
	}

	for _, want := range []string{
		"# Launch plan",
		"- **Thread id**: t1",
		"- **Messages**: 2",
		"- **Participants**: Alice <alice@example.com>, Bob <bob@example.com>",
		"- **Latest message**: 2025-01-02T09:00:00Z",
		"- **Labels**: Inbox, Personal",
		"## 1. Alice <alice@example.com> — 2025-01-01 09:00 UTC",
		"## 2. Bob <bob@example.com> — 2025-01-02 09:00 UTC",
		"**Cc:** carol@example.com",
		"Here is the **plan**.",
		"Sounds good to me.",
		"_2 attachment(s) omitted._",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q\n---\n%s", want, content)
		}
	}

	// Oldest first: the first section is the original message.
	if strings.Index(content, "## 1. Alice") > strings.Index(content, "## 2. Bob") {
		t.Error("messages are not ordered oldest first")
	}
	// Custom label ids never appear; only recognised system labels do.
	if strings.Contains(content, "Label_9") {
		t.Error("content leaked an opaque custom label id")
	}
}

func TestGmailFetchPlaceholderBody(t *testing.T) {
	f := &fakeGmailTransport{threads: map[string]*gmail.Thread{
		"t1": {ID: "t1", Messages: []gmail.Message{{
			ID:      "m1",
			From:    "calendar@example.com",
			Subject: "Invitation",
			Date:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}}},
	}}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	content, _, err := conn.Fetch(context.Background(), SourceItem{ID: "t1"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(content, "_(no text content)_") {
		t.Errorf("content = %q, want the no-text placeholder", content)
	}
	if !strings.Contains(content, "**From:** calendar@example.com") {
		t.Errorf("content = %q, want the sender preserved", content)
	}
}

func TestGmailFetchEmptyThreadFails(t *testing.T) {
	f := &fakeGmailTransport{threads: map[string]*gmail.Thread{"t1": {ID: "t1"}}}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if _, _, err := conn.Fetch(context.Background(), SourceItem{ID: "t1"}); err == nil {
		t.Fatal("Fetch returned nil error for a thread with no messages")
	}
}

// TestGmailNeverDeletes pins the v1 deletion decision: a Gmail enumeration is a
// filter over a live mailbox, so absence can mean "archived", not "deleted".
func TestGmailNeverDeletes(t *testing.T) {
	f := &fakeGmailTransport{}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	complete, ok := conn.(EnumerationCompleteness)
	if !ok {
		t.Fatal("gmail connector does not implement EnumerationCompleteness")
	}
	if complete.EnumerationComplete() {
		t.Fatal("EnumerationComplete() = true; a Gmail listing must never authorise deletions")
	}

	f.truncated = true
	if _, err := conn.Enumerate(context.Background(), map[string]any{"query": "in:inbox"}); err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	if complete.EnumerationComplete() {
		t.Fatal("EnumerationComplete() = true after a truncated listing")
	}
}

func TestGmailListResources(t *testing.T) {
	f := &fakeGmailTransport{labels: []gmail.Label{
		{ID: "INBOX", Name: "INBOX", Type: "system"},
		{ID: "Label_7", Name: "Work", Type: "user"},
	}}
	p := gmailProviderWith(f)

	resources, truncated, err := p.ListResources(context.Background(), gmailTestCredentials(), nil, "", ConnectorDeps{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
	if len(resources) != 2 || resources[1].ID != "Label_7" || resources[1].Name != "Work" || resources[1].Type != "label" {
		t.Fatalf("resources = %+v", resources)
	}
}

// TestGmailGrantRejectedIsActionable pins the reconnect wording: a revoked grant
// must not surface Google's raw invalid_grant text to the user.
func TestGmailGrantRejectedIsActionable(t *testing.T) {
	rejected := &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	f := &fakeGmailTransport{testErr: rejected}
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	err = conn.TestConnection(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "reconnect the source") {
		t.Fatalf("TestConnection() = %v, want the reconnect remediation", err)
	}

	// A transient failure keeps its raw error so the caller retries.
	f2 := &fakeGmailTransport{testErr: errors.New("503 backend error")}
	conn2, err := gmailProviderWith(f2).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := conn2.TestConnection(context.Background(), nil); err == nil ||
		strings.Contains(err.Error(), "reconnect the source") {
		t.Fatalf("TestConnection() = %v, want the raw transient error", err)
	}
}

func TestGmailThreadNameTruncates(t *testing.T) {
	long := strings.Repeat("a", gmailNameLimit+20)
	got := gmailThreadName(gmail.ThreadRef{ID: "t1", Snippet: long})
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("name = %q, want an ellipsis suffix", got)
	}
	if runes := []rune(got); len(runes) != gmailNameLimit+1 {
		t.Fatalf("name length = %d runes, want %d", len(runes), gmailNameLimit+1)
	}
}

func TestGmailOAuthStrategyScopes(t *testing.T) {
	strategy, ok := newGmailProvider("", "").OAuth()
	if !ok {
		t.Fatal("gmail provider has no OAuth strategy")
	}
	scopes := strategy.DefaultScopes()
	if len(scopes) != 2 || scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" || scopes[1] != "https://www.googleapis.com/auth/gmail.compose" {
		t.Fatalf("scopes = %v, want readonly + compose", scopes)
	}

	// The Drive strategy stays on the Drive scope: the shared Google protocol
	// must not leak Gmail's scope into a Drive connect.
	drive, _ := newGoogleDriveProvider().OAuth()
	if got := drive.DefaultScopes(); len(got) != 1 || got[0] != googleDriveReadonly {
		t.Fatalf("drive scopes = %v, want %v", got, googleDriveReadonly)
	}
}

// contains reports whether the slice holds v.
func contains(haystack []string, v string) bool {
	for _, s := range haystack {
		if s == v {
			return true
		}
	}
	return false
}

// windowGmailTransport answers each ListThreads call from a scripted batch, in
// call order, and records the options so a test can assert the window maths.
type windowGmailTransport struct {
	batches [][]gmail.ThreadRef
	truncs  []bool
	err     error
	calls   []gmail.ListOptions
}

func (w *windowGmailTransport) ListThreads(_ context.Context, opts gmail.ListOptions) ([]gmail.ThreadRef, bool, error) {
	w.calls = append(w.calls, opts)
	if w.err != nil {
		return nil, false, w.err
	}
	i := len(w.calls) - 1
	var refs []gmail.ThreadRef
	if i < len(w.batches) {
		refs = w.batches[i]
	}
	var truncated bool
	if i < len(w.truncs) {
		truncated = w.truncs[i]
	}
	return refs, truncated, nil
}

func (w *windowGmailTransport) GetThread(_ context.Context, _ string) (*gmail.Thread, error) {
	return &gmail.Thread{}, nil
}

func (w *windowGmailTransport) ListLabels(_ context.Context) ([]gmail.Label, error) { return nil, nil }

func (w *windowGmailTransport) TestConnection(_ context.Context) error { return nil }

func newWindowConnector(t *testing.T, f GmailTransport) CursorConnector {
	t.Helper()
	conn, err := gmailProviderWith(f).NewConnector(context.Background(), gmailTestCredentials(), ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	cc, ok := conn.(CursorConnector)
	if !ok {
		t.Fatal("gmail connector does not implement CursorConnector")
	}
	return cc
}

// TestGmailBackfillWalksOlderWindows is the coverage-ceiling fix: a capped head
// listing starts an older window in the same sync, and the persisted watermark
// describes the calendar region the walk has covered.
func TestGmailBackfillWalksOlderWindows(t *testing.T) {
	today := gmailDay(time.Now().UTC())
	// A since date bounds the floor a few windows out, so the walk terminates
	// inside this sync and the assertions can name the whole sequence.
	since := today.AddDate(0, 0, -75)
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{
			{{ID: "new", HistoryID: 3}},                            // head
			{{ID: "new", HistoryID: 3}, {ID: "old", HistoryID: 2}}, // window 1
		},
		truncs: []bool{true, false},
	}
	conn := newWindowConnector(t, f)

	items, cursor, err := conn.EnumerateWithCursor(context.Background(), map[string]any{
		"query":       "in:inbox",
		"max_threads": float64(500),
		"since":       since.Format("2006-01-02"),
	}, nil)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(items) != 2 || items[0].ID != "new" || items[1].ID != "old" {
		t.Fatalf("items = %+v, want the head plus the backfill thread, deduped", items)
	}
	if len(f.calls) < 2 {
		t.Fatalf("list calls = %d, want a head pass plus a backfill window", len(f.calls))
	}
	window := f.calls[1]
	if !window.Before.Equal(today) {
		t.Errorf("first window before = %v, want today (%v)", window.Before, today)
	}
	if want := today.AddDate(0, 0, -gmailBackfillWindowDays); !window.After.Equal(want) {
		t.Errorf("first window after = %v, want %v", window.After, want)
	}
	if window.MaxThreads != 500 {
		t.Errorf("window max = %d, want the sync ceiling", window.MaxThreads)
	}
	if !window.Since.IsZero() {
		t.Errorf("window carried Since %v; the window owns the date range", window.Since)
	}
	if second := f.calls[2]; !second.After.Before(window.After) {
		t.Errorf("second window after = %v; windows must move backwards", second.After)
	}

	// Walking a region with no mail is cheap and bounded: the sync reaches the
	// since floor and marks the source complete.
	if complete, _ := cursor[gmailCursorCompleteKey].(bool); !complete {
		t.Error("cursor does not report completion after reaching the since floor")
	}
	if got, want := cursor[gmailCursorBoundaryKey], since.Format("2006-01-02"); got != want {
		t.Errorf("cursor boundary = %v, want the floor %q", got, want)
	}
	if days, _ := toInt(cursor[gmailCursorWindowKey]); days != gmailBackfillWindowDays {
		t.Errorf("cursor window days = %d, want the default", days)
	}
	if cursor[gmailCursorScopeKey] == "" {
		t.Error("cursor carries no scope fingerprint")
	}
}

// TestGmailBackfillResumesFromWatermark proves the cursor changes what the next
// sync lists: a persisted boundary is the window's upper bound, so coverage
// continues instead of restarting at the newest thread.
func TestGmailBackfillResumesFromWatermark(t *testing.T) {
	boundary := time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC)
	cursor := map[string]any{
		gmailCursorScopeKey:    gmailScopeFingerprint(gmailConfig{Query: gmailDefaultQuery}),
		gmailCursorBoundaryKey: boundary.Format("2006-01-02"),
		gmailCursorWindowKey:   float64(30),
	}
	// Both passes truncate, which stops the walk at the persisted window and
	// keeps the test's expectation independent of the floor.
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{{{ID: "new"}}, {{ID: "older"}}},
		truncs:  []bool{true, true},
	}
	conn := newWindowConnector(t, f)

	_, next, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox"}, cursor)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("list calls = %d, want head + one window", len(f.calls))
	}
	if got := f.calls[1].Before; !got.Equal(boundary) {
		t.Fatalf("window before = %v, want the persisted boundary %v", got, boundary)
	}
	if got, want := f.calls[1].After, boundary.AddDate(0, 0, -30); !got.Equal(want) {
		t.Fatalf("window after = %v, want %v", got, want)
	}
	// A truncated window holds the boundary and narrows the next attempt.
	if got, _ := next[gmailCursorBoundaryKey].(string); got != boundary.Format("2006-01-02") {
		t.Errorf("next boundary = %q, want it held at %q", got, boundary.Format("2006-01-02"))
	}
	if days, _ := toInt(next[gmailCursorWindowKey]); days != 15 {
		t.Errorf("next window days = %d, want 15 after a truncated window", days)
	}
}

// TestGmailBackfillCompletesOnWholeScope pins the common case: a mailbox smaller
// than the ceiling is fully covered by the head listing, so no window is walked
// and the watermark is marked complete.
func TestGmailBackfillCompletesOnWholeScope(t *testing.T) {
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{{{ID: "a"}, {ID: "b"}}},
		truncs:  []bool{false},
	}
	conn := newWindowConnector(t, f)

	items, cursor, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox"}, nil)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if len(f.calls) != 1 {
		t.Fatalf("list calls = %d, want the head only", len(f.calls))
	}
	if complete, _ := cursor[gmailCursorCompleteKey].(bool); !complete {
		t.Error("cursor does not report completion for an untruncated listing")
	}
	if got, want := cursor[gmailCursorBoundaryKey], gmailBackfillFloor(time.Time{}).Format("2006-01-02"); got != want {
		t.Errorf("boundary = %v, want the floor %q", got, want)
	}
}

// TestGmailBackfillResetsOnScopeChange keeps a narrowed or widened query from
// inheriting a watermark that described a different mailbox region.
func TestGmailBackfillResetsOnScopeChange(t *testing.T) {
	today := gmailDay(time.Now().UTC())
	stale := map[string]any{
		gmailCursorScopeKey:    "a-different-query\x1f\x1f",
		gmailCursorBoundaryKey: today.AddDate(0, 0, -400).Format("2006-01-02"),
		gmailCursorWindowKey:   float64(7),
	}
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{{{ID: "a"}}, {{ID: "b"}}},
		truncs:  []bool{true, false},
	}
	conn := newWindowConnector(t, f)

	_, cursor, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "to:me"}, stale)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(f.calls) < 2 {
		t.Fatalf("list calls = %d", len(f.calls))
	}
	if got := f.calls[1].Before.Format("2006-01-02"); got != today.Format("2006-01-02") {
		t.Errorf("window before = %q, want a fresh walk at today", got)
	}
	if days, _ := toInt(cursor[gmailCursorWindowKey]); days != gmailBackfillWindowDays {
		t.Errorf("window days = %d, want the default after a scope reset", days)
	}
	if cursor[gmailCursorScopeKey] == stale[gmailCursorScopeKey] {
		t.Error("cursor kept the stale scope fingerprint")
	}
}

// TestGmailBackfillShrinksTruncatedWindow keeps a window that exceeds the sync
// ceiling from stalling the walk: the boundary holds and the next attempt uses a
// narrower window.
func TestGmailBackfillShrinksTruncatedWindow(t *testing.T) {
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{
			{{ID: "head"}},
			{{ID: "w1"}, {ID: "w2"}},
		},
		truncs: []bool{true, true},
	}
	conn := newWindowConnector(t, f)

	_, cursor, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox", "max_threads": float64(2)}, nil)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if days, _ := toInt(cursor[gmailCursorWindowKey]); days != gmailBackfillWindowDays/2 {
		t.Errorf("window days = %d, want %d after a truncated window", days, gmailBackfillWindowDays/2)
	}
	if got, want := cursor[gmailCursorBoundaryKey], gmailDay(time.Now().UTC()).Format("2006-01-02"); got != want {
		t.Errorf("boundary = %v, want %q (a truncated window must not advance)", got, want)
	}
	if complete, _ := cursor[gmailCursorCompleteKey].(bool); complete {
		t.Error("cursor reports completion after a truncated window")
	}
}

// TestGmailBackfillStopsAtSinceFloor bounds the walk: with since set to the
// newest day there is nothing older in scope, so the watermark completes without
// walking a window past the floor.
func TestGmailBackfillStopsAtSinceFloor(t *testing.T) {
	today := gmailDay(time.Now().UTC())
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{{{ID: "head"}}},
		truncs:  []bool{true},
	}
	conn := newWindowConnector(t, f)

	items, cursor, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox", "since": today.Format("2006-01-02")}, nil)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(items) != 1 || items[0].ID != "head" {
		t.Fatalf("items = %+v", items)
	}
	if len(f.calls) != 1 {
		t.Fatalf("list calls = %d, want no window below the since floor", len(f.calls))
	}
	if complete, _ := cursor[gmailCursorCompleteKey].(bool); !complete {
		t.Error("cursor does not report completion at the since floor")
	}
	window, ok := toInt(cursor[gmailCursorWindowKey])
	if !ok || window != gmailBackfillWindowDays {
		t.Errorf("window days = %v, want the default", cursor[gmailCursorWindowKey])
	}
}

// TestGmailBackfillErrorIsActionable keeps the reconnect remediation on a
// window listing too, not just the head pass, and leaves a transient failure
// retryable.
func TestGmailBackfillErrorIsActionable(t *testing.T) {
	revoked := &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	conn := newWindowConnector(t, &fakeGmailTransport{listErr: revoked})
	_, _, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox"}, nil)
	if err == nil {
		t.Fatal("EnumerateWithCursor returned nil error for a failed listing")
	}
	if !strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("error = %v, want the reconnect remediation for a revoked grant", err)
	}

	transient := errors.New("gmail: list threads: 503 backend unavailable")
	conn = newWindowConnector(t, &fakeGmailTransport{listErr: transient})
	_, _, err = conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox"}, nil)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %v, want the transient failure passed through unchanged", err)
	}
}

// TestGmailScopeFingerprintDistinguishesScope pins what invalidates a watermark.
func TestGmailScopeFingerprintDistinguishesScope(t *testing.T) {
	base := gmailConfig{Query: "in:inbox"}
	since := gmailConfig{Query: "in:inbox", Since: time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC)}
	labels := gmailConfig{Query: "in:inbox", LabelIDs: []string{"INBOX"}}
	other := gmailConfig{Query: "label:work"}
	seen := map[string]string{
		"base":   gmailScopeFingerprint(base),
		"since":  gmailScopeFingerprint(since),
		"labels": gmailScopeFingerprint(labels),
		"other":  gmailScopeFingerprint(other),
	}
	for a, fa := range seen {
		for b, fb := range seen {
			if a != b && fa == fb {
				t.Errorf("%s and %s share fingerprint %q", a, b, fa)
			}
		}
	}
}

// TestGmailBackfillFloor covers the two floors: the source's since date, or
// Gmail's launch day when the source has no since.
func TestGmailBackfillFloor(t *testing.T) {
	since := time.Date(2025, 2, 3, 15, 4, 5, 0, time.UTC)
	if got, want := gmailBackfillFloor(since), time.Date(2025, 2, 3, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("floor(since) = %v, want %v", got, want)
	}
	if got, want := gmailBackfillFloor(time.Time{}), time.Date(2004, 4, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("floor(zero) = %v, want Gmail's launch day %v", got, want)
	}
}

// TestGmailBackfillRestartsAfterOutgrowingCeiling is the sticky-flag regression:
// a source that was fully covered and later grows past the ceiling must resume
// the walk, not trust a completion that no longer holds.
func TestGmailBackfillRestartsAfterOutgrowingCeiling(t *testing.T) {
	today := gmailDay(time.Now().UTC())
	complete := map[string]any{
		gmailCursorScopeKey:    gmailScopeFingerprint(gmailConfig{Query: gmailDefaultQuery}),
		gmailCursorBoundaryKey: gmailBackfillFloor(time.Time{}).Format("2006-01-02"),
		gmailCursorWindowKey:   float64(30),
		gmailCursorCompleteKey: true,
	}
	f := &windowGmailTransport{
		batches: [][]gmail.ThreadRef{{{ID: "new"}}, {{ID: "older"}}},
		truncs:  []bool{true, true},
	}
	conn := newWindowConnector(t, f)

	_, next, err := conn.EnumerateWithCursor(context.Background(), map[string]any{"query": "in:inbox"}, complete)
	if err != nil {
		t.Fatalf("EnumerateWithCursor: %v", err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("list calls = %d, want the head plus a restarted window", len(f.calls))
	}
	if got := f.calls[1].Before; !got.Equal(today) {
		t.Errorf("restarted window before = %v, want today (%v)", got, today)
	}
	if done, _ := next[gmailCursorCompleteKey].(bool); done {
		t.Error("cursor still reports completion after the head pass was capped again")
	}
}

// TestGmailCursorSurvivesJSONRoundTrip pins the waternmak's storage shape: the
// cursor row is jsonb, so a persisted window width comes back as float64.
func TestGmailCursorSurvivesJSONRoundTrip(t *testing.T) {
	original := gmailBackfill{
		Scope:      "in:inbox\x1f\x1f",
		Boundary:   time.Date(2024, 7, 8, 0, 0, 0, 0, time.UTC),
		WindowDays: 15,
		Complete:   false,
	}
	raw, err := json.Marshal(original.cursor())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	stored := map[string]any{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := gmailBackfillFrom(stored, original.Scope, time.Now().UTC())
	if got.Scope != original.Scope || !got.Boundary.Equal(original.Boundary) ||
		got.WindowDays != original.WindowDays || got.Complete != original.Complete {
		t.Fatalf("round trip = %+v, want %+v", got, original)
	}
}

// TestGmailCorruptCursorRestartsWalk never fails a sync on a bad cursor row: an
// unreadable boundary restarts the walk, and an unreadable window width falls
// back to the default without discarding a readable boundary.
func TestGmailCorruptCursorRestartsWalk(t *testing.T) {
	now := time.Date(2025, 3, 4, 9, 0, 0, 0, time.UTC)
	kept := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		cursor   map[string]any
		boundary time.Time
	}{
		{"nil", nil, gmailDay(now)},
		{"missing keys", map[string]any{gmailCursorScopeKey: "s"}, gmailDay(now)},
		{"bad boundary", map[string]any{gmailCursorScopeKey: "s", gmailCursorBoundaryKey: "yesterday"}, gmailDay(now)},
		{"bad window", map[string]any{gmailCursorScopeKey: "s", gmailCursorBoundaryKey: "2024-01-01", gmailCursorWindowKey: "wide"}, kept},
		{"zero window", map[string]any{gmailCursorScopeKey: "s", gmailCursorBoundaryKey: "2024-01-01", gmailCursorWindowKey: float64(0)}, kept},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gmailBackfillFrom(tc.cursor, "s", now)
			if !got.Boundary.Equal(tc.boundary) {
				t.Errorf("boundary = %v, want %v", got.Boundary, tc.boundary)
			}
			if got.WindowDays != gmailBackfillWindowDays {
				t.Errorf("window days = %d, want the default", got.WindowDays)
			}
		})
	}
}
