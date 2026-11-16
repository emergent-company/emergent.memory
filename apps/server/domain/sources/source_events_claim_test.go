package sources

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

func claimTestRSAKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// TestClaimIssueFailsClosedOnPartialLabelWrite is the regression test for the
// review blocker: a successful HTTP response that omits a requested label must
// roll back the claim instead of leaving a status label with no manager label.
func TestClaimIssueFailsClosedOnPartialLabelWrite(t *testing.T) {
	keyHex, err := cryptoutil.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatal(err)
	}
	sealedKey, err := SealSourceBlob(keyBytes, claimTestRSAKeyPEM(t))
	if err != nil {
		t.Fatal(err)
	}

	var labelCreates, removedLabels, removedAssignees int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/7":
			_, _ = w.Write([]byte(`{"number":7,"title":"fix","state":"open","labels":[],"assignees":[]}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/labels/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/labels":
			labelCreates++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/assignees":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/labels":
			// Deliberately omit manager: memory-coding to simulate GitHub
			// silently dropping an unknown label.
			_, _ = w.Write([]byte(`[{"name":"status: in-progress"}]`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/7/labels/"):
			removedLabels++
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/issues/7/assignees":
			removedAssignees++
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	d := NewSourceEventDispatcher(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.key = keyBytes
	d.apiBaseURL = fake.URL
	d.minter = githubapp.NewTokenMinter(fake.URL, fake.Client())
	d.newClient = func(_, token string) *githubapp.Client {
		return githubapp.NewClient(fake.URL, token, fake.Client())
	}

	src := &Source{Config: map[string]any{"assignee_login": "mkucharz"}}
	app := &SourceApp{AppID: 99, PrivateKeyEncrypted: sealedKey}
	p := &githubapp.WebhookPayload{}
	p.Installation.ID = 42
	p.Issue.Number = 7

	if _, err := d.claimIssue(context.Background(), &VerifiedSourceEvent{App: app, Payload: p}, src, "owner/repo", "coding"); err == nil {
		t.Fatal("expected a partial label write to fail the claim")
	}
	if labelCreates != 2 {
		t.Fatalf("label creates = %d, want 2", labelCreates)
	}
	if removedLabels != 2 {
		t.Fatalf("removed labels = %d, want 2", removedLabels)
	}
	if removedAssignees != 1 {
		t.Fatalf("removed assignees = %d, want 1", removedAssignees)
	}
}
