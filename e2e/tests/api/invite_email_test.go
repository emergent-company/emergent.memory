// Package api_test — invite_email_test.go
//
// End-to-end test for invite email delivery.
//
// The server sends the project-invitation email through an SMTP transport
// (EMAIL_TRANSPORT=smtp) into a Mailpit capture box. This test creates an
// invite, waits for the real email to arrive in Mailpit, and asserts the
// subject / from / to and — critically — that the accept URL embedded in the
// body matches the invitation token returned by the API.
//
// The test skips (not fails) when Mailpit is not reachable, so it stays green
// against servers that do not run the SMTP transport.
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

// mailpitBaseURL returns the Mailpit HTTP API base URL. Defaults to
// http://localhost:8025 (host runner with published port); set MAILPIT_BASE_URL
// to override (e.g. http://mailpit:8025 inside the docker stack).
func mailpitBaseURL() string {
	if v := os.Getenv("MAILPIT_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8025"
}

// mailpitAddress is a single To/From/Reply-To entry in Mailpit's message JSON.
type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

// mailpitSummary is a message entry in the /api/v1/messages list response.
type mailpitSummary struct {
	ID      string           `json:"ID"`
	Subject string           `json:"Subject"`
	To      []mailpitAddress `json:"To"`
	From    []mailpitAddress `json:"From"`
}

// mailpitMessage is the full message returned by /api/v1/message/{ID}.
type mailpitMessage struct {
	ID      string           `json:"ID"`
	Subject string           `json:"Subject"`
	To      []mailpitAddress `json:"To"`
	From    []mailpitAddress `json:"From"`
	Text    string           `json:"Text"`
	HTML    string           `json:"HTML"`
}

// mailpitListResponse is the /api/v1/messages list envelope.
type mailpitListResponse struct {
	Messages []mailpitSummary `json:"messages"`
}

// mailpitUp reports whether the Mailpit HTTP API is reachable.
func mailpitUp(t *testing.T, base string) bool {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/api/v1/info")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// waitForMessageTo polls Mailpit until a message addressed to email appears.
func waitForMessageTo(t *testing.T, base, email string, timeout time.Duration) mailpitSummary {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/v1/messages?start=0&limit=50")
		if err == nil {
			var list mailpitListResponse
			if err := json.NewDecoder(resp.Body).Decode(&list); err == nil {
				for _, m := range list.Messages {
					if hasAddress(m.To, email) {
						resp.Body.Close()
						return m
					}
				}
			}
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("no email to %s arrived in Mailpit within %s", email, timeout)
	return mailpitSummary{}
}

// fetchMessage returns the full Mailpit message body for a message ID.
func fetchMessage(t *testing.T, base, id string) mailpitMessage {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/api/v1/message/" + id)
	if err != nil {
		t.Fatalf("fetch mailpit message %s: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch mailpit message %s: status %d", id, resp.StatusCode)
	}
	var msg mailpitMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatalf("decode mailpit message %s: %v", id, err)
	}
	return msg
}

// hasAddress reports whether addrs contains address (case-insensitive).
func hasAddress(addrs []mailpitAddress, address string) bool {
	for _, a := range addrs {
		if strings.EqualFold(a.Address, address) {
			return true
		}
	}
	return false
}

func TestInvite_EmailDeliveredWithAcceptLink(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	base := mailpitBaseURL()
	if !mailpitUp(t, base) {
		t.Skipf("Mailpit not reachable at %s; skipping invite-email delivery test (set MAILPIT_BASE_URL)", base)
	}

	_, orgID := setupProjectLogged(t, rl)
	email := fmt.Sprintf("invitee-%d@example.com", time.Now().UnixNano())

	invite := createInvite(t, email, orgID, "", "org_admin")
	token, _ := invite["token"].(string)
	if token == "" {
		t.Fatalf("invite creation response missing token: %v", invite)
	}

	// Wait for the real email to be delivered to Mailpit.
	msg := waitForMessageTo(t, base, email, 60*time.Second)

	if !strings.Contains(strings.ToLower(msg.Subject), "invited") {
		t.Errorf("email subject %q does not mention invitation", msg.Subject)
	}
	if len(msg.From) == 0 || msg.From[0].Address == "" {
		t.Errorf("email has no from address: %+v", msg.From)
	}

	full := fetchMessage(t, base, msg.ID)

	want := "/invites/accept?token=" + token
	if !strings.Contains(full.HTML, want) {
		t.Errorf("accept URL %q not found in email HTML", want)
	}
	if !strings.Contains(full.Text, want) {
		t.Errorf("accept URL %q not found in email text", want)
	}
	if !strings.Contains(full.Text, "install.sh") {
		t.Errorf("email text is missing the CLI install instructions")
	}

	rl.Printf("invite email delivered to %s with accept link for token %s", email, token)
}
