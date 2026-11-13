package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// budgetStateTTL bounds how stale the sidebar badge's budget reading may be.
// A minute is deliberate: the badge is an attention cue, and the authoritative
// signal (the alert email) is produced server-side on the usage write, not by a
// page render. Anything fresher would put an extra round trip on every page.
const budgetStateTTL = 60 * time.Second

// budgetStateEntry is one cached budget reading for a project.
type budgetStateEntry struct {
	state *BudgetState
	at    time.Time
}

// cachedBudgetState returns the project's budget state, reusing a recent result
// for the sidebar badge. A nil state with a nil error means "no active
// project"; callers treat an error as "no badge" rather than failing the page.
func (s *Server) cachedBudgetState(ctx context.Context) (*BudgetState, error) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return nil, nil
	}

	s.budgetMu.Lock()
	entry, ok := s.budgetCache[projectID]
	s.budgetMu.Unlock()
	if ok && time.Since(entry.at) < budgetStateTTL {
		return entry.state, nil
	}

	state, err := s.memory.GetProjectBudgetState(ctx)
	if err != nil {
		return nil, err
	}

	s.budgetMu.Lock()
	if s.budgetCache == nil {
		s.budgetCache = make(map[string]budgetStateEntry)
	}
	s.budgetCache[projectID] = budgetStateEntry{state: state, at: time.Now()}
	s.budgetMu.Unlock()
	return state, nil
}

// invalidateBudgetState drops the cached reading after a reset, so the next
// render (the page the user was sent back to) shows the fresh window.
func (s *Server) invalidateBudgetState(ctx context.Context) {
	projectID := s.currentProjectID(ctx)
	if projectID == "" {
		return
	}
	s.budgetMu.Lock()
	delete(s.budgetCache, projectID)
	s.budgetMu.Unlock()
}

// budgetBadgeLevel returns "exhausted", "warning" or "" for the nav badge. It
// ignores windows with no cap, so an uncapped project never shows a budget
// badge.
func budgetBadgeLevel(state *BudgetState) string {
	return state.AlertLevel()
}

// budgetWeekState / budgetMonthState are nil-safe accessors: a nil *BudgetState
// (budget unavailable) must read as "no window" rather than panic in a template
// that then loses the whole page.
func budgetWeekState(b *BudgetState) *BudgetWindowState {
	if b == nil {
		return nil
	}
	return b.Week
}

func budgetMonthState(b *BudgetState) *BudgetWindowState {
	if b == nil {
		return nil
	}
	return b.Month
}

// budgetWindowNoun is the adjective form used in prose ("weekly limit").
func budgetWindowNoun(window string) string {
	if window == "week" {
		return "weekly"
	}
	return "monthly"
}

// budgetRemaining is the unspent part of the cap, floored at zero so an
// over-spend reads as "$0.00 left" rather than a negative figure.
func budgetRemaining(w *BudgetWindowState) float64 {
	if w == nil || w.CapUSD == nil {
		return 0
	}
	r := *w.CapUSD - w.SpendUSD
	if r < 0 {
		return 0
	}
	return r
}

// --- formatting ---

// budgetWindowLabel renders the window's name as it reads in a heading.
func budgetWindowLabel(w *BudgetWindowState) string {
	if w != nil && w.Window == "week" {
		return "This week"
	}
	return "This month"
}

// budgetResetsOn renders the end of the current window as a statement of when
// the budget refills, in UTC (the window boundary the server enforces).
func budgetResetsOn(w *BudgetWindowState) string {
	if w == nil || w.PeriodEnd.IsZero() {
		return ""
	}
	return w.PeriodEnd.UTC().Format("Mon, 2 Jan 2006 15:04 UTC")
}

// budgetMoney renders a USD amount at cent precision.
func budgetMoney(v float64) string {
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

// budgetPct renders a percentage, capped at 100 for the bar's width but not for
// the number (over-spend is worth seeing).
func budgetPct(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64) + "%"
}

// budgetBarWidth is the clamped inline width for the usage bar.
func budgetBarWidth(percent float64) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return strconv.FormatFloat(percent, 'f', 0, 64) + "%"
}

// budgetStatusLabel is the human wording for a window status.
func budgetStatusLabel(status string) string {
	switch status {
	case "exhausted":
		return "Limit reached"
	case "warning":
		return "Near limit"
	default:
		return "Within budget"
	}
}

// --- reset handler ---

// budgetResetParams are the PRG params the usage and budget pages map to flash
// messages after a reset.
var budgetResetParams = []flashParam{
	{key: "budget_reset", msg: "Budget window reset. Spend from before the reset no longer counts toward the current window."},
}

// uiBudgetReset restarts one spending window (week or month) from now.
//
// The two windows reset independently: resetting the week must not clear the
// month's tracked spend. The backend is the authority on both the window
// arithmetic and the authority check (project admin); this handler only routes
// the form and turns a successful write into a PRG redirect back to the page
// that submitted it, so a refresh cannot repeat the reset.
func (s *Server) uiBudgetReset(c echo.Context) error {
	ctx := c.Request().Context()
	window := strings.ToLower(strings.TrimSpace(c.FormValue("window")))
	switch window {
	case "week", "month":
	default:
		return toastTrigger(c, "error", "Choose which window to reset (week or month).")
	}

	// Only ever redirect back into the app, never to a caller-supplied URL.
	back := "/usage"
	if c.FormValue("back") == "settings" {
		back = "/settings/budget"
	}

	if _, err := s.memory.ResetProjectBudgetWindow(ctx, window); err != nil {
		// Classify here, not on the receiving page: the PRG error travels as a
		// query param, so the HTTP status that carries the meaning is gone by
		// the time a page renders it.
		return redirectWithError(c, back, budgetResetError(err))
	}
	s.invalidateBudgetState(ctx)
	redirectAfterMutation(c, back+"?budget_reset="+window)
	return nil
}

// isBudgetAuthError reports whether a reset failed on authority rather than on
// transport. The UI renders a distinct message, because "ask an admin" is the
// only useful next step.
func isBudgetAuthError(err error) bool {
	return isMemoryStatus(err, http.StatusForbidden) || isMemoryStatus(err, http.StatusUnauthorized)
}

// budgetResetError renders the error shown above the budget controls after a
// failed reset.
func budgetResetError(err error) error {
	if isBudgetAuthError(err) {
		return fmt.Errorf("you need to be a project admin to reset the budget window")
	}
	return err
}
