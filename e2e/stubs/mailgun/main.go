// Command mailgun-stub is a hermetic stand-in for the Mailgun send API
// (https://api.mailgun.net/v3) used by the e2e harness. It accepts the
// POST /v3/{domain}/messages multipart request the Mailgun SDK emits, records
// it in memory, and answers with the same JSON shape Mailgun returns, so the
// Mailgun email transport can be exercised end-to-end without real credentials
// or network egress.
//
// Routes:
//
//	POST /v3/{domain}/messages   capture a send request, return {"id","message"}
//	POST /{domain}/messages      same, for a base URL that omits the /v3 prefix
//	GET  /captured               JSON array of everything captured so far
//	GET  /healthz                200 "ok"
//
// Configuration (env):
//
//	PORT   listen port (default 8080)
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync"
	"time"
)

// CapturedMessage is a single recorded POST /v3/{domain}/messages request.
// Field names are part of the e2e contract consumed by
// e2e/tests/api/invite_email_mailgun_test.go.
type CapturedMessage struct {
	ID          string   `json:"id"`
	Timestamp   string   `json:"timestamp"`
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

// stubHandler records Mailgun send requests in memory and serves them back over
// /captured. The mutex guards the captured slice against concurrent sends.
type stubHandler struct {
	mu       sync.Mutex
	captured []CapturedMessage
}

func newHandler() *stubHandler {
	return &stubHandler{captured: make([]CapturedMessage, 0)}
}

func (h *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log every request so docker logs show exactly what was requested.
	log.Printf("%s %s", r.Method, r.URL.Path)

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	case r.Method == http.MethodGet && r.URL.Path == "/captured":
		h.handleCaptured(w)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		h.handleMessages(w, r)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}
}

// handleCaptured serves GET /captured with the full list of captured requests.
// Always a JSON array (never null) so callers can decode unconditionally.
func (h *stubHandler) handleCaptured(w http.ResponseWriter) {
	h.mu.Lock()
	out := make([]CapturedMessage, len(h.captured))
	copy(out, h.captured)
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// handleMessages parses the Mailgun multipart send request, records the fields
// the e2e test asserts on, and returns the Mailgun success payload.
func (h *stubHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// ParseMultipartForm still populates r.Form for a urlencoded body, so
		// only fail when nothing usable was parsed at all.
		if len(r.Form) == 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "cannot parse form: %v", err)
			return
		}
	}

	to := append([]string(nil), r.Form["to"]...)
	msg := CapturedMessage{
		ID:          newID(),
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		Domain:      domainFromPath(r.URL.Path),
		From:        r.FormValue("from"),
		To:          to,
		ToAddresses: normalizeAddresses(to),
		Subject:     r.FormValue("subject"),
		Text:        r.FormValue("text"),
		HTML:        r.FormValue("html"),
		AuthPresent: r.Header.Get("Authorization") != "",
	}

	h.mu.Lock()
	h.captured = append(h.captured, msg)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{
		"id":      msg.ID,
		"message": "Queued. Thank you.",
	})
}

// domainFromPath extracts the Mailgun domain from a messages endpoint path,
// tolerating both /v3/{domain}/messages and /{domain}/messages. It returns ""
// when the path does not have the expected three (or two) segments.
func domainFromPath(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if !strings.HasSuffix(trimmed, "/messages") {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, "/messages")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if parts[0] == "v3" || parts[0] == "v2" {
		if len(parts) < 2 {
			return ""
		}
		return parts[1]
	}
	return parts[0]
}

// normalizeAddresses parses each raw To value ("Name <addr>" or bare address)
// down to its addr-spec, falling back to the raw value when parsing fails.
func normalizeAddresses(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if parsed, err := mail.ParseAddress(v); err == nil {
			out = append(out, parsed.Address)
			continue
		}
		out = append(out, v)
	}
	return out
}

// newID returns a unique, Mailgun-shaped message id.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d@mailgun-stub", time.Now().UnixNano())
	}
	return "<" + hex.EncodeToString(b[:]) + "@mailgun-stub>"
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode error: %v", err)
	}
}

func main() {
	log.SetOutput(os.Stdout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	srv := &http.Server{
		Addr:    addr,
		Handler: newHandler(),
	}

	log.Printf("mailgun stub listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
