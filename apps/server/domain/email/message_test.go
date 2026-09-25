package email

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// TestBuildMessageParsesIntoTextAndHTMLParts builds a message and re-parses it
// with the standard library mail/multipart stack, asserting the exact MIME shape
// the e2e test (and Mailpit) rely on: a multipart/alternative body with exactly
// two parts — text/plain and text/html — and the accept URL present in both.
//
// This is the structural guard for the "wrong MIME shape" defect: if the builder
// ever drops the text/plain part (or misroutes the URL), this fails without the
// e2e stack needing to run.
func TestBuildMessageParsesIntoTextAndHTMLParts(t *testing.T) {
	acceptURL := "https://app.example.com/invites/accept?token=abc123"
	cfg := &Config{FromName: "Memory", FromEmail: "noreply@example.com"}
	opts := SendOptions{
		To:      "invitee@example.com",
		ToName:  "Invitee",
		Subject: "You've been invited",
		Text:    "Accept invitation: " + acceptURL + "\n\ncurl -fsSL https://get.emergent.memory/install.sh | sh",
		HTML:    "<a href=\"" + acceptURL + "\">Accept invitation</a>",
	}

	msg, _ := buildMessage(cfg, opts)

	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("mail.ReadMessage: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("top-level content-type = %q, want multipart/alternative", mediaType)
	}

	mr := multipart.NewReader(m.Body, params["boundary"])

	var plainBody, htmlBody string
	var plainSeen, htmlSeen bool
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read next part: %v", err)
		}

		partType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		// multipart.Reader transparently decodes a quoted-printable part body
		// (and hides the Content-Transfer-Encoding header), so read directly.
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("decode %s part: %v", partType, err)
		}

		switch partType {
		case "text/plain":
			plainBody, plainSeen = string(body), true
		case "text/html":
			htmlBody, htmlSeen = string(body), true
		default:
			t.Errorf("unexpected part content-type %q", partType)
		}
	}

	if !plainSeen {
		t.Fatal("message has no text/plain part")
	}
	if !htmlSeen {
		t.Fatal("message has no text/html part")
	}

	// The e2e test asserts the accept URL in both Mailpit's Text (text/plain)
	// and HTML (text/html) views.
	if !strings.Contains(plainBody, acceptURL) {
		t.Errorf("text/plain part missing accept URL %q: %q", acceptURL, plainBody)
	}
	if !strings.Contains(htmlBody, acceptURL) {
		t.Errorf("text/html part missing accept URL %q: %q", acceptURL, htmlBody)
	}
	if !strings.Contains(plainBody, "install.sh") {
		t.Errorf("text/plain part missing install.sh instructions: %q", plainBody)
	}
}

// TestProjectInvitationPlainText verifies the plain-text alternative body for a
// project invitation carries the accept link and the CLI install instructions.
func TestProjectInvitationPlainText(t *testing.T) {
	acceptURL := "https://app.example.com/invites/accept?token=abc123"
	got := ProjectInvitationPlainText("Alice", "the project", "Admin", acceptURL)

	for _, want := range []string{
		acceptURL,
		"install.sh",
		"Alice",
		"the project",
		"Admin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ProjectInvitationPlainText missing %q:\n%s", want, got)
		}
	}
}

// TestInviteEmailMessageContainsAcceptLinkInBothParts exercises the full email
// path — plain-text body generation, message build, and MIME parse — to prove
// the accept link and install instructions reach the text/plain part that the
// e2e test reads. Before the fix the invites service passed no plain text, so
// generatePlainText produced only the subject and this assertion would fail.
func TestInviteEmailMessageContainsAcceptLinkInBothParts(t *testing.T) {
	acceptURL := "https://app.example.com/invites/accept?token=abc123"
	plainText := ProjectInvitationPlainText("A team member", "the project", "Admin", acceptURL)
	html := fmt.Sprintf("<a href=\"%s\">Accept invitation</a><code>curl -fsSL https://get.emergent.memory/install.sh | sh</code>", acceptURL)

	cfg := &Config{FromName: "Memory", FromEmail: "noreply@example.com"}
	msg, _ := buildMessage(cfg, SendOptions{
		To:      "invitee@example.com",
		Subject: "You've been invited to join the project on emergent.memory",
		Text:    plainText,
		HTML:    html,
	})

	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("mail.ReadMessage: %v", err)
	}
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])

	var plainBody, htmlBody string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read next part: %v", err)
		}
		partType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		body, _ := io.ReadAll(p)
		if partType == "text/plain" {
			plainBody = string(body)
		}
		if partType == "text/html" {
			htmlBody = string(body)
		}
	}

	if !strings.Contains(plainBody, acceptURL) {
		t.Errorf("text/plain part missing accept URL %q:\n%s", acceptURL, plainBody)
	}
	if !strings.Contains(plainBody, "install.sh") {
		t.Errorf("text/plain part missing install.sh:\n%s", plainBody)
	}
	if !strings.Contains(htmlBody, acceptURL) {
		t.Errorf("text/html part missing accept URL %q:\n%s", acceptURL, htmlBody)
	}
}
