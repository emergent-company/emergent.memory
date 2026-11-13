package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	for _, want := range []string{`data-testid="budget-nav-badge"`, `data-level="warning"`, "Near limit"} {
		if !strings.Contains(html, want) {
			t.Errorf("warning badge missing %q: %s", want, html)
		}
	}

	_, html = usageItem(t, "exhausted")
	for _, want := range []string{`data-level="exhausted"`, "Over budget"} {
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
