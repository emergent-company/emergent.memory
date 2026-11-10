package sources

import (
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/gmail"
)

func TestGmailFilterRequiresScope(t *testing.T) {
	if _, err := parseGmailConfig(map[string]any{}); err == nil {
		t.Fatal("empty Gmail scope must be rejected")
	}
	if _, err := parseGmailConfig(map[string]any{"filter": map[string]any{}}); err == nil {
		t.Fatal("empty filter object must be rejected")
	}
}

func TestGmailFilterCompilesStructuredGroups(t *testing.T) {
	parsed, err := parseGmailConfig(map[string]any{
		"filter": map[string]any{
			"categories":      []any{"Primary", "Updates"},
			"labels":          []any{"Label_7"},
			"excludeLabels":   []any{"SPAM"},
			"senders":         []any{"alice@example.com"},
			"excludeDomains":  []any{"news.example.com"},
			"subjectContains": []any{"invoice"},
			"hasAttachment":   true,
			"unreadOnly":      true,
			"after":           "2025-01-01",
			"before":          "2025-02-01",
		},
	})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	for _, want := range []string{
		"category:primary", "category:updates",
		`label:"Label_7"`, `-label:"SPAM"`,
		`from:("alice@example.com")`, `-from:("news.example.com")`,
		`subject:("invoice")`, "has:attachment", "is:unread",
		"after:2025/01/01", "before:2025/02/01",
	} {
		if !strings.Contains(parsed.Query, want) {
			t.Errorf("compiled query %q missing %q", parsed.Query, want)
		}
	}
}

func TestGmailFilterRejectsUnknownCategory(t *testing.T) {
	_, err := parseGmailConfig(map[string]any{
		"filter": map[string]any{"categories": []any{"Important"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown category") {
		t.Fatalf("err = %v, want unknown-category rejection", err)
	}
}

func TestGmailConfigObjectTypes(t *testing.T) {
	parsed, err := parseGmailConfig(map[string]any{
		"query":        "in:inbox",
		"object_types": []any{"EmailMessage"},
	})
	if err != nil {
		t.Fatalf("parseGmailConfig: %v", err)
	}
	if !parsed.ObjectTypes["EmailMessage"] || parsed.ObjectTypes["EmailThread"] {
		t.Fatalf("object types = %v, want only EmailMessage", parsed.ObjectTypes)
	}
	if _, err := parseGmailConfig(map[string]any{
		"query":        "in:inbox",
		"object_types": []any{"EmailAddress"},
	}); err == nil {
		t.Fatal("unknown object type must be rejected")
	}
}

func TestGmailThreadRecordsRespectSelection(t *testing.T) {
	thread := &gmail.Thread{
		ID: "t1",
		Messages: []gmail.Message{
			{ID: "m1", From: "alice@example.com", To: "bob@example.com", Subject: "Hi", Date: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Labels: []string{"INBOX"}},
		},
	}
	records := gmailThreadRecords(thread, gmailConfig{ObjectTypes: map[string]bool{"EmailMessage": true}})
	if len(records) != 1 || records[0].Type != "EmailMessage" || records[0].Key != "m1" {
		t.Fatalf("records = %+v, want one EmailMessage", records)
	}

	records = gmailThreadRecords(thread, gmailConfig{ObjectTypes: map[string]bool{"EmailThread": true, "EmailMessage": true}})
	if len(records) != 2 {
		t.Fatalf("records = %d, want thread + message", len(records))
	}
	threadRec := records[0]
	if threadRec.Type != "EmailThread" || len(threadRec.Relationships) != 1 || threadRec.Relationships[0].Type != "HAS_MESSAGE" {
		t.Fatalf("thread record = %+v", threadRec)
	}
}

func TestGmailToolNamesIncludeAlias(t *testing.T) {
	defs := newGmailProvider("", "").ToolDefinitions("alice", false)
	if len(defs) != 3 {
		t.Fatalf("read defs = %d, want 3", len(defs))
	}
	for _, def := range defs {
		if !strings.HasPrefix(def.Name, "gmail-alice-") {
			t.Errorf("tool %q does not carry the account alias", def.Name)
		}
		if def.RequiredScope != "email:read" {
			t.Errorf("tool %q scope = %q", def.Name, def.RequiredScope)
		}
	}

	writeDefs := newGmailProvider("", "").ToolDefinitions("alice", true)
	if len(writeDefs) != len(defs)+6 {
		t.Fatalf("write defs = %d, want read + 6", len(writeDefs))
	}
	found := false
	for _, def := range writeDefs {
		if def.Name == "gmail-alice-message-send" {
			found = true
			if def.RequiredScope != "email:write" {
				t.Errorf("send scope = %q, want email:write", def.RequiredScope)
			}
		}
	}
	if !found {
		t.Fatal("message-send definition missing")
	}
}
