package budgetwindow

import (
	"testing"
	"time"
)

func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func mustEqual(t *testing.T, label string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", label, got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestPeriodFor_CalendarWeek(t *testing.T) {
	// 2026-11-13 is a Friday; the ISO week starts on Monday 2026-11-09.
	p := PeriodFor(utc(2026, 11, 13, 9, 30), nil, Week)
	mustEqual(t, "start", p.Start, utc(2026, 11, 9, 0, 0))
	mustEqual(t, "end", p.End, utc(2026, 11, 16, 0, 0))
}

func TestPeriodFor_CalendarWeek_SundayIsLastDay(t *testing.T) {
	// Sunday 2026-11-15 belongs to the week that began Monday 2026-11-09.
	p := PeriodFor(utc(2026, 11, 15, 23, 59), nil, Week)
	mustEqual(t, "start", p.Start, utc(2026, 11, 9, 0, 0))
	mustEqual(t, "end", p.End, utc(2026, 11, 16, 0, 0))
	// Monday 00:00 is the first instant of the next week.
	next := PeriodFor(utc(2026, 11, 16, 0, 0), nil, Week)
	mustEqual(t, "next start", next.Start, utc(2026, 11, 16, 0, 0))
}

func TestPeriodFor_CalendarMonth(t *testing.T) {
	p := PeriodFor(utc(2026, 11, 13, 9, 30), nil, Month)
	mustEqual(t, "start", p.Start, utc(2026, 11, 1, 0, 0))
	mustEqual(t, "end", p.End, utc(2026, 12, 1, 0, 0))

	// December rolls into January of the next year.
	p = PeriodFor(utc(2026, 12, 31, 23, 59), nil, Month)
	mustEqual(t, "dec start", p.Start, utc(2026, 12, 1, 0, 0))
	mustEqual(t, "dec end", p.End, utc(2027, 1, 1, 0, 0))
}

func TestPeriodFor_AnchoredWeek(t *testing.T) {
	anchor := utc(2026, 11, 20, 14, 30)

	p := PeriodFor(utc(2026, 11, 22, 8, 0), &anchor, Week)
	mustEqual(t, "start", p.Start, anchor)
	mustEqual(t, "end", p.End, utc(2026, 11, 27, 14, 30))

	// The end instant already belongs to the next cycle.
	p = PeriodFor(utc(2026, 11, 27, 14, 30), &anchor, Week)
	mustEqual(t, "next start", p.Start, utc(2026, 11, 27, 14, 30))
	mustEqual(t, "next end", p.End, utc(2026, 12, 4, 14, 30))

	// Two cycles later.
	p = PeriodFor(utc(2026, 12, 6, 0, 0), &anchor, Week)
	mustEqual(t, "third start", p.Start, utc(2026, 12, 4, 14, 30))
	mustEqual(t, "third end", p.End, utc(2026, 12, 11, 14, 30))
}

func TestPeriodFor_AnchoredMonthClampsDayOfMonth(t *testing.T) {
	// A Jan 31 anchor yields Feb 28, and March is computed from the anchor again
	// (Mar 31), not from the clamped February boundary.
	anchor := utc(2026, 1, 31, 12, 0)

	p := PeriodFor(utc(2026, 2, 10, 0, 0), &anchor, Month)
	mustEqual(t, "feb start", p.Start, anchor)
	mustEqual(t, "feb end", p.End, utc(2026, 2, 28, 12, 0))

	p = PeriodFor(utc(2026, 3, 5, 0, 0), &anchor, Month)
	mustEqual(t, "mar start", p.Start, utc(2026, 2, 28, 12, 0))
	mustEqual(t, "mar end", p.End, utc(2026, 3, 31, 12, 0))

	p = PeriodFor(utc(2026, 4, 1, 0, 0), &anchor, Month)
	mustEqual(t, "apr start", p.Start, utc(2026, 3, 31, 12, 0))
	mustEqual(t, "apr end", p.End, utc(2026, 4, 30, 12, 0))
}

func TestPeriodFor_AnchoredMonthCrossesYear(t *testing.T) {
	anchor := utc(2026, 12, 20, 9, 0)
	p := PeriodFor(utc(2027, 1, 3, 0, 0), &anchor, Month)
	mustEqual(t, "start", p.Start, anchor)
	mustEqual(t, "end", p.End, utc(2027, 1, 20, 9, 0))

	p = PeriodFor(utc(2027, 1, 20, 9, 0), &anchor, Month)
	mustEqual(t, "next start", p.Start, utc(2027, 1, 20, 9, 0))
	mustEqual(t, "next end", p.End, utc(2027, 2, 20, 9, 0))
}

func TestPeriodFor_AnchorExactlyNowAndFutureAnchor(t *testing.T) {
	anchor := utc(2026, 11, 20, 14, 30)
	p := PeriodFor(anchor, &anchor, Week)
	mustEqual(t, "now start", p.Start, anchor)

	// A future anchor (clock skew) still yields a usable window rather than an
	// empty one.
	future := utc(2026, 12, 1, 0, 0)
	p = PeriodFor(utc(2026, 11, 20, 0, 0), &future, Week)
	mustEqual(t, "future start", p.Start, future)
	mustEqual(t, "future end", p.End, utc(2026, 12, 8, 0, 0))
}

func TestPeriodKey(t *testing.T) {
	p := PeriodFor(utc(2026, 11, 13, 0, 0), nil, Week)
	if got, want := p.Key(), "2026-11-09"; got != want {
		t.Errorf("week key = %q, want %q", got, want)
	}
	m := PeriodFor(utc(2026, 11, 13, 0, 0), nil, Month)
	if got, want := CalendarKeyMonth(m), "2026-11"; got != want {
		t.Errorf("month calendar key = %q, want %q", got, want)
	}
	if got, want := m.Key(), "2026-11-01"; got != want {
		t.Errorf("month period key = %q, want %q", got, want)
	}
}
