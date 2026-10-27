package sources

import (
	"context"
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
func gmailProviderWith(f *fakeGmailTransport) SourceProvider {
	return NewGmailProvider(func(context.Context, gmail.Credentials) (GmailTransport, error) {
		return f, nil
	})
}

func gmailTestCredentials() Credentials {
	return Credentials{"refresh_token": "rt", "client_id": "cid", "client_secret": "secret", "scope": gmailReadonlyScope}
}

func TestGmailProviderCatalog(t *testing.T) {
	p := newGmailProvider("")
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
	schema := newGmailProvider("").ConfigSchema()
	for _, key := range []string{"query", "label_ids", "since", "max_threads"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("schema missing %q", key)
		}
	}
	if len(schema.Required) != 0 {
		t.Errorf("required = %v, want nothing (a default query applies)", schema.Required)
	}
}

func TestGmailValidateConfig(t *testing.T) {
	p := newGmailProvider("")
	tests := []struct {
		name    string
		cfg     map[string]any
		wantErr string
	}{
		{name: "empty applies defaults", cfg: map[string]any{}},
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
		{name: "since wrong type", cfg: map[string]any{"since": 20250101}, wantErr: "config.since"},
		{name: "since bad date", cfg: map[string]any{"since": "31/01/2025"}, wantErr: "config.since"},
		{name: "max_threads zero", cfg: map[string]any{"max_threads": 0}, wantErr: "config.max_threads"},
		{name: "max_threads negative", cfg: map[string]any{"max_threads": -1}, wantErr: "config.max_threads"},
		{name: "max_threads fractional", cfg: map[string]any{"max_threads": 1.5}, wantErr: "config.max_threads"},
		{name: "max_threads over ceiling", cfg: map[string]any{"max_threads": 50001}, wantErr: "config.max_threads"},
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
	parsed, err := parseGmailConfig(map[string]any{})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if parsed.Query != gmailDefaultQuery {
		t.Errorf("query = %q, want %q", parsed.Query, gmailDefaultQuery)
	}
	if parsed.MaxThreads != gmail.DefaultMaxThreads {
		t.Errorf("max_threads = %d, want %d", parsed.MaxThreads, gmail.DefaultMaxThreads)
	}
	if !parsed.Since.IsZero() {
		t.Errorf("since = %v, want zero", parsed.Since)
	}

	parsed, err = parseGmailConfig(map[string]any{"since": "2024-02-29", "query": "  "})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if want := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC); !parsed.Since.Equal(want) {
		t.Errorf("since = %v, want %v", parsed.Since, want)
	}
	if parsed.Query != gmailDefaultQuery {
		t.Errorf("blank query = %q, want the default", parsed.Query)
	}

	if _, err := parseGmailConfig(map[string]any{"since": "2024-13-01"}); err == nil {
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
	p := newGmailProvider("")

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
	var provider any = newGmailProvider("")
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
	if _, err := conn.Enumerate(context.Background(), map[string]any{}); err == nil {
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
	if _, err := conn.Enumerate(context.Background(), map[string]any{}); err != nil {
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
	strategy, ok := newGmailProvider("").OAuth()
	if !ok {
		t.Fatal("gmail provider has no OAuth strategy")
	}
	scopes := strategy.DefaultScopes()
	if len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Fatalf("scopes = %v, want the Gmail readonly scope", scopes)
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
