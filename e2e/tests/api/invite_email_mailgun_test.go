// Package api_test — invite_email_mailgun_test.go
//
// End-to-end test for invite email delivery through the Mailgun transport.
//
// The server sends the project-invitation email through EMAIL_TRANSPORT=mailgun
// against a local stub that impersonates the Mailgun send API. This test
// creates an invite, waits for the captured request to appear at the stub, and
// asserts the recipient, from, subject, the accept URL embedded in the body,
// the request path, and that an Authorization header was sent.
//
// The test skips (not fails) when MAILGUN_STUB_URL is unset or the stub is not
// reachable, so it stays green against the default SMTP/Mailpit stack.
package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// mailgunStubBaseURL returns the Mailgun stub base URL. It is intentionally
// empty by default so this test only runs in the Mailgun-transport CI job.
func mailgunStubBaseURL() string {
	return strings.TrimRight(os.Getenv("MAILGUN_STUB_URL"), "/")
}

// capturedMailgunMessage mirrors one entry served by the stub's GET /captured.
type capturedMailgunMessage struct {
	ID          string   `json:"id"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Domain      string   `json:"domain"`
	From        string   `json:"from"`
	To          []string `json:"to"`
	ToAddresses []string `json:"to_addresses"`
	Subject     string   `json:"subject"`
	Text        string   `json:"text"`
	HTML        string   `json:"html"`
	AuthPresent bool     `json:"auth_present"`
}

// mailgunStubUp reports whether the Mailgun stub is reachable.
func mailgunStubUp(t *testing.T, base string) bool {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// fetchCaptured reads every request the stub has captured so far.
func fetchCaptured(t *testing.T, base string) []capturedMailgunMessage {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/captured")
	if err != nil {
		t.Fatalf("fetch captured from %s: %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch captured from %s: status %d", base, resp.StatusCode)
	}
	var captured []capturedMailgunMessage
	if err := json.NewDecoder(resp.Body).Decode(&captured); err != nil {
		t.Fatalf("decode captured from %s: %v", base, err)
	}
	return captured
}

// waitForCapturedTo polls the stub until a message addressed to email appears.
func waitForCapturedTo(t *testing.T, base, email string, timeout time.Duration) capturedMailgunMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, m := range fetchCaptured(t, base) {
			if hasCapturedRecipient(m, email) {
				return m
			}
		}
		time.Sleep(2 * time.Second)
	}
	dumpCaptured(t, base)
	t.Fatalf("no Mailgun request for %s captured within %s", email, timeout)
	return capturedMailgunMessage{}
}

// dumpCaptured logs every captured request so a delivery failure is actionable:
// it shows whether nothing arrived, the recipient differed, or a different path
// was used. Called on the timeout path only.
func dumpCaptured(t *testing.T, base string) {
	t.Helper()
	captured := fetchCaptured(t, base)
	if len(captured) == 0 {
		t.Logf("Mailgun stub captured NO requests — check server email-worker/Mailgun logs for enqueue/send errors")
		return
	}
	t.Logf("Mailgun stub captured %d request(s):", len(captured))
	for _, m := range captured {
		t.Logf("  - path=%s to=%v from=%q subject=%q", m.Path, m.ToAddresses, m.From, m.Subject)
	}
}

// hasCapturedRecipient reports whether the message was addressed to email.
func hasCapturedRecipient(m capturedMailgunMessage, email string) bool {
	for _, a := range m.ToAddresses {
		if strings.EqualFold(a, email) {
			return true
		}
	}
	for _, a := range m.To {
		if strings.EqualFold(a, email) || strings.Contains(strings.ToLower(a), strings.ToLower(email)) {
			return true
		}
	}
	return false
}

func TestInvite_EmailDeliveredViaMailgunTransport(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	base := mailgunStubBaseURL()
	if base == "" {
		t.Skip("MAILGUN_STUB_URL not set; skipping Mailgun-transport invite-email test")
	}
	if !mailgunStubUp(t, base) {
		t.Skipf("Mailgun stub not reachable at %s; skipping Mailgun-transport invite-email test", base)
	}

	_, orgID := setupProjectLogged(t, rl)
	email := fmt.Sprintf("invitee-%d@example.com", time.Now().UnixNano())

	invite := createInvite(t, email, orgID, "", "org_admin")
	token, _ := invite["token"].(string)
	if token == "" {
		t.Fatalf("invite creation response missing token: %v", invite)
	}

	// Wait for the Mailgun send request to be captured by the stub.
	msg := waitForCapturedTo(t, base, email, 60*time.Second)

	if msg.From == "" {
		t.Errorf("captured request has no from address: %+v", msg)
	}
	if !strings.Contains(strings.ToLower(msg.Subject), "invited") {
		t.Errorf("captured subject %q does not mention invitation", msg.Subject)
	}

	// The request path must be the Mailgun messages endpoint for the configured
	// domain: POST /v3/{domain}/messages.
	if !strings.HasPrefix(msg.Path, "/v3/") || !strings.HasSuffix(msg.Path, "/messages") {
		t.Errorf("captured path %q does not look like /v3/{domain}/messages", msg.Path)
	}
	if msg.Domain == "" {
		t.Errorf("captured path %q yielded no domain", msg.Path)
	}
	wantPath := "/v3/" + msg.Domain + "/messages"
	if msg.Path != wantPath {
		t.Errorf("captured path = %q, want %q", msg.Path, wantPath)
	}
	if wantDomain := os.Getenv("E2E_MAILGUN_DOMAIN"); wantDomain != "" && msg.Domain != wantDomain {
		t.Errorf("captured domain = %q, want %q (E2E_MAILGUN_DOMAIN)", msg.Domain, wantDomain)
	}

	if !msg.AuthPresent {
		t.Error("captured request is missing an Authorization header")
	}

	// The accept URL must be present in one of the message parts, matching the
	// invitation token returned by the API.
	want := "/invites/accept?token=" + token
	if !strings.Contains(msg.HTML, want) && !strings.Contains(msg.Text, want) {
		t.Errorf("accept URL %q not found in captured html/text (html=%d bytes, text=%d bytes)", want, len(msg.HTML), len(msg.Text))
	}

	rl.Printf("invite email captured via Mailgun transport to %s with accept link for token %s", email, token)
}
