package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestParseMemoryErrorShapes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		raw        string
		wantStatus int
		wantText   string
	}{
		{
			name:       "code and message shape",
			status:     http.StatusConflict,
			raw:        `{"error":{"code":"conflict","message":"name taken"}}`,
			wantStatus: http.StatusConflict,
			wantText:   "memory 409 conflict: name taken",
		},
		{
			name:       "error string shape",
			status:     http.StatusNotFound,
			raw:        `{"error":"not_found"}`,
			wantStatus: http.StatusNotFound,
			wantText:   "memory 404: not_found",
		},
		{
			// A body that is neither recognised shape is never echoed: a proxy
			// page or an unenveloped JSON slab is not user-facing copy.
			name:       "unrecognised body gets status-derived copy",
			status:     http.StatusInternalServerError,
			raw:        "internal boom",
			wantStatus: http.StatusInternalServerError,
			wantText:   "memory 500: The service failed (HTTP 500).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseMemoryError(tt.status, []byte(tt.raw))
			if got := memoryStatus(err); got != tt.wantStatus {
				t.Fatalf("memoryStatus = %d, want %d", got, tt.wantStatus)
			}
			if got := err.Error(); got != tt.wantText {
				t.Fatalf("Error() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestParseMemoryErrorNeverEchoesUnknownBodies pins the flash-copy hardening: a
// body that is not one of memory's two error shapes (a proxy page, an Echo-style
// {"message":...}, a bare slab) becomes status-derived copy, never the raw body.
func TestParseMemoryErrorNeverEchoesUnknownBodies(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		raw      string
		wantText string
	}{
		{
			name:     "proxy HTML page",
			status:   http.StatusBadGateway,
			raw:      "<html>502 Bad Gateway</html>",
			wantText: "memory 502: The service failed (HTTP 502).",
		},
		{
			name:     "echo not-found JSON",
			status:   http.StatusNotFound,
			raw:      `{"message":"Not Found"}`,
			wantText: "memory 404: The request was rejected (HTTP 404).",
		},
		{
			name:     "bare slab",
			status:   http.StatusBadRequest,
			raw:      `not json at all`,
			wantText: "memory 400: The request was rejected (HTTP 400).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseMemoryError(tt.status, []byte(tt.raw))
			if got := memoryStatus(err); got != tt.status {
				t.Fatalf("memoryStatus = %d, want %d", got, tt.status)
			}
			if got := err.Error(); got != tt.wantText {
				t.Fatalf("Error() = %q, want %q", got, tt.wantText)
			}
			for _, leak := range []string{"<html>", "Bad Gateway", `{"message"`, "not json"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("Error() = %q leaks %q", err.Error(), leak)
				}
			}
		})
	}
}

// TestParseMemoryErrorKeepsCodeAndMessage pins that hardening did not stop the
// recognised shape from reaching the operator with its code intact.
func TestParseMemoryErrorKeepsCodeAndMessage(t *testing.T) {
	err := parseMemoryError(http.StatusBadRequest, []byte(`{"error":{"code":"skill_scan_blocked","message":"blocked by scan"}}`))
	var he *memoryHTTPError
	if !errors.As(err, &he) {
		t.Fatalf("error type = %T, want *memoryHTTPError", err)
	}
	if he.Status != http.StatusBadRequest || he.Code != "skill_scan_blocked" || he.Message != "blocked by scan" {
		t.Errorf("error = %+v", he)
	}
}

// TestParseMemoryErrorTruncatesLongMessage pins the length cap: a multi-kilobyte
// backend message is a payload, not copy, and must not be rendered whole.
func TestParseMemoryErrorTruncatesLongMessage(t *testing.T) {
	long := strings.Repeat("x", 3000)
	err := parseMemoryError(http.StatusBadRequest, []byte(`{"error":{"code":"huge","message":"`+long+`"}}`))
	var he *memoryHTTPError
	if !errors.As(err, &he) {
		t.Fatalf("error type = %T, want *memoryHTTPError", err)
	}
	if got := len([]rune(he.Message)); got != maxMemoryErrorMessageRunes {
		t.Errorf("message is %d runes, want %d", got, maxMemoryErrorMessageRunes)
	}
	if !strings.HasSuffix(he.Message, "…") {
		t.Errorf("truncated message must end in an ellipsis: %q", he.Message[len(he.Message)-5:])
	}
	if he.Code != "huge" || he.Status != http.StatusBadRequest {
		t.Errorf("code/status lost: %+v", he)
	}
}

func TestIsMemoryStatus(t *testing.T) {
	conflict := &memoryHTTPError{Status: http.StatusConflict, Code: "conflict", Message: "dup"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "matching status", err: conflict, want: true},
		{name: "non-matching status", err: conflict, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil error", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := http.StatusConflict
			if tt.name == "non-matching status" {
				status = http.StatusNotFound
			}
			if got := isMemoryStatus(tt.err, status); got != tt.want {
				t.Fatalf("isMemoryStatus(%v, %d) = %v, want %v", tt.err, status, got, tt.want)
			}
		})
	}
}

func TestUserFacingMemoryError(t *testing.T) {
	urlErr := &url.Error{
		Op:  "Get",
		URL: "http://172.17.0.1:8080/api/orgs",
		Err: errors.New("dial tcp 172.17.0.1:8080: connect: connection refused"),
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{
			name: "503 service_unavailable",
			err:  &memoryHTTPError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "Token validation temporarily unavailable"},
			want: memoryUnavailableMessage,
		},
		{name: "502 bad gateway", err: &memoryHTTPError{Status: http.StatusBadGateway}, want: memoryUnavailableMessage},
		{name: "500 internal", err: &memoryHTTPError{Status: http.StatusInternalServerError, Message: "boom"}, want: memoryUnavailableMessage},
		{name: "url.Error transport", err: urlErr, want: memoryUnavailableMessage},
		{name: "context deadline", err: context.DeadlineExceeded, want: memoryUnavailableMessage},
		{
			name: "plain transport error",
			err:  errors.New("dial tcp 172.17.0.1:8080: connect: connection refused"),
			want: genericErrorMessage,
		},
	}

	// Internal detail that must never leak into user-facing copy.
	leakTokens := []string{"172.17.0.1", "dial tcp", ":8080", "memory 5", "service_unavailable", "Token validation"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userFacingMemoryError(tt.err)
			if got != tt.want {
				t.Fatalf("userFacingMemoryError() = %q, want %q", got, tt.want)
			}
			for _, token := range leakTokens {
				if strings.Contains(got, token) {
					t.Errorf("userFacingMemoryError() = %q leaks %q", got, token)
				}
			}
		})
	}
}

func TestIsMemoryNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typed 404",
			err:  &memoryHTTPError{Status: http.StatusNotFound, Message: "missing"},
			want: true,
		},
		{
			name: "plain not found text",
			err:  errors.New("memory 404: not found"),
			want: true,
		},
		{
			name: "typed 500",
			err:  &memoryHTTPError{Status: http.StatusInternalServerError, Message: "boom"},
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMemoryNotFound(tt.err); got != tt.want {
				t.Fatalf("isMemoryNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestSkillProposalApproveFlashSanitisesProxyBody pins the flash-copy fix end to
// end: memory's proxy answers with an HTML 502 page and the resulting toast must
// carry status-derived copy, never the upstream body.
func TestSkillProposalApproveFlashSanitisesProxyBody(t *testing.T) {
	// The approve POST fails upstream with a proxy page; the PRG re-read
	// succeeds, which is the realistic shape of this failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html><body>502 Bad Gateway</body></html>")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: NewMemoryClient(srv.URL, "proj-1")}
	e := echo.New()
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)
	e.GET("/skills", s.uiSkills)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location %q: %v", rec.Header().Get("Location"), err)
	}
	flash := loc.Query().Get("err")
	for _, leak := range []string{"<html>", "</html>", "<body>", "Bad Gateway"} {
		if strings.Contains(flash, leak) {
			t.Errorf("flash leaks the upstream body (%q): %q", leak, flash)
		}
	}
	if !strings.Contains(flash, "The service failed (HTTP 502).") {
		t.Errorf("flash = %q, want the generic 502 copy", flash)
	}

	// The flash affordance is the rendered toast, not just the redirect: follow
	// the PRG target and assert the operator actually sees the sanitised copy.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, loc.String(), nil))
	page := rec.Body.String()
	if !strings.Contains(page, "The service failed (HTTP 502).") {
		t.Error("the redirected page does not render the sanitised flash copy")
	}
	for _, leak := range []string{"<html>", "Bad Gateway"} {
		if strings.Contains(page, leak) {
			t.Errorf("the redirected page still shows %q", leak)
		}
	}
}

// TestSkillProposalApproveFlashKeepsMemoryValidationCopy pins the other half of
// the contract: a recognised {error:{code,message}} body is still shown to the
// operator — the hardening must not swallow actionable backend copy.
func TestSkillProposalApproveFlashKeepsMemoryValidationCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"skill_scan_blocked","message":"blocked by scan"}}`)
	}))
	defer srv.Close()

	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: NewMemoryClient(srv.URL, "proj-1")}
	e := echo.New()
	e.POST("/skills/proposals/:id/approve", s.uiApproveSkillCaptureProposal)
	e.GET("/skills", s.uiSkills)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/skills/proposals/p1/approve", nil))
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location %q: %v", rec.Header().Get("Location"), err)
	}
	flash := loc.Query().Get("err")
	if !strings.Contains(flash, "blocked by scan") {
		t.Errorf("flash = %q, want memory's validation copy", flash)
	}
	if strings.Contains(flash, `{"error"`) {
		t.Errorf("flash leaks raw JSON: %q", flash)
	}

	// Follow the PRG target: the operator must actually see that copy.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, loc.String(), nil))
	if !strings.Contains(rec.Body.String(), "blocked by scan") {
		t.Error("the redirected page does not render memory's validation copy")
	}
}
