// Package budgetwindow computes the spending period of a project budget
// window.
//
// The two windows — week and month — either follow the calendar or a manual
// reset anchor:
//
//   - Anchor nil: the week is the ISO week (Monday 00:00 UTC to the next
//     Monday) and the month is the UTC calendar month. This is the pre-existing
//     behaviour for the monthly cap (`GetProjectCurrentMonthSpend`), kept so an
//     existing project's accounting does not move under it.
//   - Anchor set: the window repeats from that instant — [anchor, anchor+7d)
//     for weeks, [anchor, anchor+1 calendar month) for months — so an admin who
//     hits the cap can restart one cycle immediately through the reset
//     endpoint.
//
// Everything here is UTC because the usage ledger (`kb.llm_usage_events`) is
// written with UTC timestamps and the SQL that sums it buckets on UTC; a local
// boundary would put events in one window for the UI and another for the alert.
package budgetwindow

import "time"

// Window identifies one budget window.
type Window string

const (
	// Week is the weekly spending window.
	Week Window = "week"
	// Month is the monthly spending window.
	Month Window = "month"
)

// Period is a half-open spending window, [Start, End).
type Period struct {
	Start time.Time `json:"periodStart"`
	End   time.Time `json:"periodEnd"`
}

// PeriodFor returns the window containing now, for the given anchor (nil = the
// calendar window). Both the server (spend sums, alerts, enforcement) and the
// gateway (remaining/reset copy) call this, so a period boundary cannot differ
// between what the UI shows and what the alert fires on.
func PeriodFor(now time.Time, anchor *time.Time, w Window) Period {
	now = now.UTC()
	if anchor == nil || anchor.IsZero() {
		if w == Week {
			return calendarWeek(now)
		}
		return calendarMonth(now)
	}
	a := anchor.UTC()
	if w == Week {
		return anchoredWeek(now, a)
	}
	return anchoredMonth(now, a)
}

// calendarWeek is the ISO week: Monday 00:00 UTC through the next Monday.
func calendarWeek(now time.Time) Period {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// time.Weekday counts Sunday as 0; shift so Monday is 0.
	offset := (int(start.Weekday()) + 6) % 7
	start = start.AddDate(0, 0, -offset)
	return Period{Start: start, End: start.AddDate(0, 0, 7)}
}

// calendarMonth is the UTC calendar month.
func calendarMonth(now time.Time) Period {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return Period{Start: start, End: start.AddDate(0, 1, 0)}
}

// anchoredWeek advances the anchor in whole 7-day steps. Duration arithmetic is
// exact here because both ends are UTC (no DST transition can insert an hour).
func anchoredWeek(now, anchor time.Time) Period {
	if now.Before(anchor) {
		// A future anchor (clock skew, or a reset recorded by a host running
		// slightly ahead) must not produce an empty window: treat the anchor as
		// the current period start.
		return Period{Start: anchor, End: anchor.AddDate(0, 0, 7)}
	}
	elapsed := now.Sub(anchor)
	steps := elapsed / (7 * 24 * time.Hour)
	start := anchor.AddDate(0, 0, int(steps)*7)
	return Period{Start: start, End: start.AddDate(0, 0, 7)}
}

// anchoredMonth advances the anchor in whole calendar months, clamping the day
// of month so a Jan 31 anchor yields Feb 28 (and Mar 31 after that, because
// every boundary is computed from the anchor rather than from its predecessor).
func anchoredMonth(now, anchor time.Time) Period {
	if now.Before(anchor) {
		return Period{Start: anchor, End: addMonthsClamped(anchor, 1)}
	}
	// Month distance is a safe upper bound on the number of steps; walk back
	// only if rounding overshoots.
	months := (now.Year()-anchor.Year())*12 + int(now.Month()) - int(anchor.Month())
	for months > 0 && addMonthsClamped(anchor, months).After(now) {
		months--
	}
	for addMonthsClamped(anchor, months+1).Before(now) || addMonthsClamped(anchor, months+1).Equal(now) {
		months++
	}
	start := addMonthsClamped(anchor, months)
	return Period{Start: start, End: addMonthsClamped(anchor, months+1)}
}

// addMonthsClamped shifts t by n calendar months, keeping the time of day and
// clamping the day of month to the target month's length.
func addMonthsClamped(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	lastDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := t.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// Key renders a stable, human-readable identity for the period, used in the
// notification group keys that deduplicate budget alerts. The format is the
// period start's date, e.g. "2026-11-16".
func (p Period) Key() string {
	return p.Start.UTC().Format("2006-01-02")
}

// CalendarKeyMonth renders the legacy monthly key ("2006-01") for an
// unanchored month window. Existing budget-alert notifications were written
// with that key, so keeping it for calendar projects is what stops a rollout
// from re-alerting every project that is already over its threshold.
func CalendarKeyMonth(p Period) string {
	return p.Start.UTC().Format("2006-01")
}
