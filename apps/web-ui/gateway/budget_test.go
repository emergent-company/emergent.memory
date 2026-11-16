package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/devmode"
	"github.com/labstack/echo/v4"
)

// budgetWindow builds one capped window for the fixtures below.
func budgetWindow(window string, capUSD, spend float64, status string) *BudgetWindowState {
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	if window == "month" {
		start = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		end = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	}
	remaining := capUSD - spend
	if remaining < 0 {
		remaining = 0
	}
	return &BudgetWindowState{
		Window:       window,
		CapUSD:       &capUSD,
		Threshold:    0.8,
		SpendUSD:     spend,
		RemainingUSD: &remaining,
		PercentUsed:  spend / capUSD * 100,
		PeriodStart:  start,
		PeriodEnd:    end,
		Status:       status,
	}
}

// TestBudgetStateAlertLevel pins the collapse rule behind the nav badge:
// exhausted beats warning regardless of window order, an uncapped window is
// ignored (a project with no caps never lights the badge), and nil is safe.
func TestBudgetStateAlertLevel(t *testing.T) {
	capped := func(status string) *BudgetWindowState {
		c := 10.0
		return &BudgetWindowState{Window: "week", CapUSD: &c, Status: status}
	}
	uncapped := &BudgetWindowState{Window: "month", Status: "exhausted"}

	tests := []struct {
		name  string
		state *BudgetState
		want  string
	}{
		{"nil", nil, ""},
		{"both ok", &BudgetState{Week: capped("ok"), Month: capped("ok")}, ""},
		{"warning only", &BudgetState{Week: capped("ok"), Month: capped("warning")}, "warning"},
		{"exhausted wins", &BudgetState{Week: capped("warning"), Month: capped("exhausted")}, "exhausted"},
		{"exhausted week wins over ok month", &BudgetState{Week: capped("exhausted"), Month: capped("ok")}, "exhausted"},
		// An uncapped window cannot be "exhausted": status is never computed
		// against a missing cap, and the badge must stay dark.
		{"uncapped ignored", &BudgetState{Week: capped("ok"), Month: uncapped}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.AlertLevel(); got != tt.want {
				t.Errorf("AlertLevel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSidebarBudgetBadge proves the Usage nav entry carries the budget badge at
// warning and exhausted, with the level exposed for styling/tests, and carries
// none when there is nothing to say.
func TestSidebarBudgetBadge(t *testing.T) {
	usageItem := func(t *testing.T, level string) (found bool, warn string) {
		t.Helper()
		for _, g := range sidebarGroupsWithWarn("/o/o1/p/p1", false, sidebarWarns{Budget: level}) {
			for _, item := range g.Items {
				if item.Label != "Usage" {
					continue
				}
				found = true
				if item.Warn == nil {
					return found, ""
				}
				var buf bytes.Buffer
				if err := item.Warn.Render(context.Background(), &buf); err != nil {
					t.Fatalf("render badge: %v", err)
				}
				warn = buf.String()
			}
		}
		return found, warn
	}

	found, html := usageItem(t, "warning")
	if !found {
		t.Fatal("Usage item missing from sidebar")
	}
	for _, want := range []string{`data-testid="budget-nav-badge"`, `data-level="warning"`, "Near limit", "ml-1"} {
		if !strings.Contains(html, want) {
			t.Errorf("warning badge missing %q: %s", want, html)
		}
	}

	_, html = usageItem(t, "exhausted")
	for _, want := range []string{`data-level="exhausted"`, "Over budget", "ml-1"} {
		if !strings.Contains(html, want) {
			t.Errorf("exhausted badge missing %q: %s", want, html)
		}
	}

	if _, html := usageItem(t, ""); html != "" {
		t.Errorf("no budget level must render no badge, got %q", html)
	}
}

// TestUsageBudgetSection covers the usage page's budget block: the per-window
// remaining/reset figures and reset control, the all-uncapped state (which
// offers a link to set one), and the unavailable state (a budget outage must
// degrade, not fail the page).
func TestUsageBudgetSection(t *testing.T) {
	capped := renderHTML(t, UsagePage(usagePageData{
		Days: 30,
		Budget: &BudgetState{
			Week:  budgetWindow("week", 5, 4.2, "warning"),
			Month: budgetWindow("month", 25.5, 4.2, "ok"),
		},
	}))
	for _, want := range []string{
		"Budget",
		`data-testid="budget-week"`,
		`data-testid="budget-month"`,
		`data-testid="budget-remaining-week"`,
		`data-testid="budget-resets-week"`,
		"$0.80 left",
		"Resets Mon, 16 Nov 2026 00:00 UTC",
		`data-testid="budget-reset-week"`,
		`data-testid="budget-reset-month"`,
		`name="window" value="week"`,
		`data-testid="usage-manage-budget"`,
		"Near limit",
	} {
		if !strings.Contains(capped, want) {
			t.Errorf("capped usage page missing %q", want)
		}
	}

	// No caps at all: the page explains that runs are never blocked and offers
	// the settings link (no reset controls, which would be meaningless).
	open := renderHTML(t, UsagePage(usagePageData{
		Days: 30,
		Budget: &BudgetState{
			Week:  &BudgetWindowState{Window: "week", Status: "ok"},
			Month: &BudgetWindowState{Window: "month", Status: "ok"},
		},
	}))
	if !strings.Contains(open, "No spend cap is set for this project") || !strings.Contains(open, "Set a budget") {
		t.Errorf("uncapped usage page missing the no-cap explanation:\n%s", open)
	}
	if strings.Contains(open, "budget-reset-") {
		t.Error("an uncapped project must not offer a window reset")
	}

	// Unavailable budget reading: section degrades, page still renders.
	down := renderHTML(t, UsagePage(usagePageData{Days: 30, BudgetErr: errors.New("boom")}))
	if !strings.Contains(down, "Budget details are unavailable right now.") {
		t.Errorf("budget outage must degrade the section:\n%s", down)
	}

	// Half a payload (one window missing) is a server this build does not
	// understand, not a project with an uncapped window: rendering the card we
	// did get would present the missing window as "no limit" — possibly the one
	// blocking runs. Degrade instead of mislabeling.
	partial := renderHTML(t, UsagePage(usagePageData{
		Days:   30,
		Budget: &BudgetState{Month: budgetWindow("month", 25.5, 4.2, "ok")},
	}))
	if !strings.Contains(partial, "Budget details are unavailable right now.") {
		t.Errorf("a payload missing a window must degrade the section:\n%s", partial)
	}
	if strings.Contains(partial, `data-testid="budget-month"`) || strings.Contains(partial, "No spend cap is set") {
		t.Errorf("a partial payload must not render a single window as uncapped:\n%s", partial)
	}
}

// TestUIUsageShowsResetFlash proves the PRG param from a reset surfaces as a
// toast on the usage page (and that a failed budget read does not fail the
// page).
func TestUIUsageShowsResetFlash(t *testing.T) {
	f := &fakeMemory{
		usageSummary:   sampleUsageSummary(),
		usageSeries:    sampleUsageSeries(),
		budgetState:    &BudgetState{Week: budgetWindow("week", 5, 4.5, "warning"), Month: budgetWindow("month", 20, 4.5, "ok")},
		budgetStateErr: nil,
	}
	_, e := newUsageEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?budget_reset=week", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Budget window reset") {
		t.Errorf("reset flash missing from usage page:\n%s", rec.Body.String())
	}

	// A budget read failure is non-fatal: summary and charts still render.
	f2 := &fakeMemory{
		usageSummary:   sampleUsageSummary(),
		usageSeries:    sampleUsageSeries(),
		budgetStateErr: errors.New("budget down"),
	}
	_, e2 := newUsageEcho(f2)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("budget outage must not fail the page: status = %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "Budget details are unavailable right now.") {
		t.Errorf("budget outage note missing:\n%s", rec2.Body.String())
	}
}

// newBudgetResetEcho wires the reset handler on a bare server.
func newBudgetResetEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/budget/reset", s.uiBudgetReset)
	return s, e
}

// TestUIBudgetReset covers the form handler: each window is forwarded, the PRG
// target follows the submitting page, an unknown window is rejected before any
// write, and a backend failure redirects back with the error rather than a
// success.
func TestUIBudgetReset(t *testing.T) {
	t.Run("week from usage page", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newBudgetResetEcho(f)
		rec := voicePost(e, "/settings/budget/reset", "window=week&back=usage")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body %s)", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get(echo.HeaderLocation); loc != "/usage?budget_reset=week" {
			t.Errorf("Location = %q, want /usage?budget_reset=week", loc)
		}
		if len(f.budgetResets) != 1 || f.budgetResets[0] != "week" {
			t.Errorf("resets = %v, want [week]", f.budgetResets)
		}
	})

	t.Run("month from settings page", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newBudgetResetEcho(f)
		rec := voicePost(e, "/settings/budget/reset", "window=month&back=settings")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get(echo.HeaderLocation); loc != "/settings/budget?budget_reset=month" {
			t.Errorf("Location = %q, want /settings/budget?budget_reset=month", loc)
		}
		if len(f.budgetResets) != 1 || f.budgetResets[0] != "month" {
			t.Errorf("resets = %v, want [month]", f.budgetResets)
		}
	})

	// A caller-supplied `back` is never followed: only the two known values route.
	t.Run("unknown back falls back to usage", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newBudgetResetEcho(f)
		rec := voicePost(e, "/settings/budget/reset", "window=week&back=https://evil.example/")
		if loc := rec.Header().Get(echo.HeaderLocation); loc != "/usage?budget_reset=week" {
			t.Errorf("Location = %q, want the usage page", loc)
		}
	})

	t.Run("unknown window is rejected without a write", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newBudgetResetEcho(f)
		rec := voicePost(e, "/settings/budget/reset", "window=day")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (toast)", rec.Code)
		}
		if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"error"`) {
			t.Errorf("invalid window should trigger an error toast, got %q", hdr)
		}
		if len(f.budgetResets) != 0 {
			t.Errorf("an invalid window must not reach the backend, got %v", f.budgetResets)
		}
	})

	t.Run("backend failure redirects back with the error", func(t *testing.T) {
		f := &fakeMemory{budgetResetErr: fmt.Errorf("memory 500: boom")}
		_, e := newBudgetResetEcho(f)
		rec := voicePost(e, "/settings/budget/reset", "window=week&back=settings")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		loc := rec.Header().Get(echo.HeaderLocation)
		if !strings.HasPrefix(loc, "/settings/budget?err=") {
			t.Errorf("Location = %q, want an error redirect to the budget page", loc)
		}
	})
}

// TestUIBudgetResetAuthErrorWording pins the one message that matters after a
// rejected reset: "you need to be a project admin". The reset handler classifies
// the failure before redirecting — the PRG error arrives as a query string, so
// the HTTP status is already gone when a page renders it — and both entry points
// then show the wording verbatim.
func TestUIBudgetResetAuthErrorWording(t *testing.T) {
	forbidden := &memoryHTTPError{Status: http.StatusForbidden, Code: "forbidden", Message: "project admin authority required"}

	_, e := newBudgetResetEcho(&fakeMemory{budgetResetErr: forbidden})
	rec := voicePost(e, "/settings/budget/reset", "window=week&back=usage")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get(echo.HeaderLocation)
	if !strings.HasPrefix(loc, "/usage?err=") {
		t.Fatalf("Location = %q, want a redirect to the usage page", loc)
	}
	msg, err := url.QueryUnescape(strings.TrimPrefix(loc, "/usage?err="))
	if err != nil {
		t.Fatalf("unescape redirect error: %v", err)
	}
	if !strings.Contains(msg, "you need to be a project admin to reset the budget window") {
		t.Fatalf("redirected error = %q, want the admin wording", msg)
	}

	// The usage page renders that param into the toast, so the user actually
	// sees it (and never the "memory 403 forbidden:" prefix).
	f := &fakeMemory{
		usageSummary:   sampleUsageSummary(),
		usageSeries:    sampleUsageSeries(),
		budgetStateErr: forbidden,
	}
	_, usageEcho := newUsageEcho(f)
	rec2 := httptest.NewRecorder()
	usageEcho.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, loc, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, "you need to be a project admin to reset the budget window") {
		t.Errorf("usage page should surface the admin wording:\n%s", body)
	}
	if strings.Contains(body, "memory 403 forbidden") {
		t.Errorf("usage page must not leak the backend error prefix:\n%s", body)
	}
}

// renderBudgetFragment renders a templ fragment to HTML through the dev-mode
// context the annotation contract requires (ui-guard check 3).
func renderBudgetFragment(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(devmode.WithDevMode(context.Background()), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// TestBudgetResetControlSubmits pins the reset button's submit contract.
//
// It was broken in production: the call passed "type": "submit" through
// ButtonProps.Attrs while leaving Type empty, and ui.Button emits its own
// type attribute first (type="button"). A browser honours the first one, so the
// button rendered, the confirm dialog appeared, and the form was never
// submitted — no request, no error, no change. Asserting the rendered tag
// (rather than the source) is what catches that.
func TestBudgetResetControlSubmits(t *testing.T) {
	month := budgetWindow("month", 10, 9.78, "warning")
	tests := []struct {
		name string
		html string
	}{
		{"usage page card", renderBudgetFragment(t, budgetWindowCard(month, "month"))},
		{"settings page fields", renderBudgetFragment(t, budgetWindowFields("month", "Monthly", month.CapUSD, nil, month, nil))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.Count(tt.html, `type="submit"`); got != 1 {
				t.Errorf("reset button should render exactly one type=\"submit\", got %d:\n%s", got, buttonTags(tt.html))
			}
			if strings.Contains(tt.html, `type="button"`) {
				t.Errorf("reset button must not render type=\"button\" (it wins over submit):\n%s", buttonTags(tt.html))
			}
			if !strings.Contains(tt.html, `<form method="post" action="/settings/budget/reset"`) {
				t.Errorf("reset control must be inside a POST form:\n%s", tt.html)
			}
		})
	}
}

// buttonTags extracts the opening <button> tags from a rendered fragment, so a
// failure shows the actual attributes rather than a wall of HTML.
func buttonTags(html string) string {
	var out []string
	for _, part := range strings.Split(html, "<button")[1:] {
		end := strings.Index(part, ">")
		if end < 0 {
			continue
		}
		out = append(out, "<button"+part[:end+1])
	}
	return strings.Join(out, "\n")
}

// TestTemplNoAttributeTypeRatchet keeps the duplicate-attribute trap from
// coming back anywhere in the gateway: ui.Button/ui.SimpleButton own the
// `type` attribute, and "type" inside a templ.Attributes literal emits a second
// one. The Ratchet suffix puts this in the required ci job's
// `go test -run Ratchet ./...` selection.
func TestTemplNoAttributeTypeRatchet(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatalf("glob templ files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no .templ files found — the scan is not looking at the gateway")
	}
	// Only a form-submission type is the trap; a JSON literal such as
	// {"type": "service_account"} in a textarea placeholder is not.
	trap := regexp.MustCompile(`"type":\s*"(submit|button|reset)"`)
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !trap.MatchString(line) {
				continue
			}
			t.Errorf("%s:%d passes \"type\" through templ.Attributes; set ButtonProps.Type instead "+
				"(a duplicate type attribute leaves the first one — type=\"button\" — in effect)", name, i+1)
		}
	}
}
