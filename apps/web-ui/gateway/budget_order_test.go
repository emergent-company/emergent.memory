package main

import (
	"strings"
	"testing"
)

// TestUsageBudgetWindowsMonthFirst pins the requested reading order on the
// usage page: the monthly window is the primary budget figure, so its card
// comes before the weekly one. The test asserts rendered positions rather than
// template source, because the order the user sees is what was reported.
func TestUsageBudgetWindowsMonthFirst(t *testing.T) {
	html := renderHTML(t, UsagePage(usagePageData{
		Days: 30,
		Budget: &BudgetState{
			Week:  budgetWindow("week", 5, 4.2, "warning"),
			Month: budgetWindow("month", 25.5, 4.2, "ok"),
		},
	}))

	// Both windows must still be findable by their stable anchors: this is a
	// reorder, not a rename.
	month := strings.Index(html, `data-testid="budget-month"`)
	week := strings.Index(html, `data-testid="budget-week"`)
	if month < 0 {
		t.Fatalf("monthly card missing its data-testid anchor:\n%s", html)
	}
	if week < 0 {
		t.Fatalf("weekly card missing its data-testid anchor:\n%s", html)
	}
	if month > week {
		t.Errorf("monthly budget card must render before the weekly card (month at %d, week at %d)", month, week)
	}

	// The labels make the same claim to the reader, in another way.
	if thisMonth, thisWeek := strings.Index(html, "This month"), strings.Index(html, "This week"); thisMonth < 0 || thisWeek < 0 || thisMonth > thisWeek {
		t.Errorf("monthly card heading must precede the weekly one (month at %d, week at %d)", thisMonth, thisWeek)
	}
}

// TestBudgetSettingsWindowsMonthFirst keeps the settings page on the same order
// as the usage page, so the two views cannot disagree about which window is
// "first".
func TestBudgetSettingsWindowsMonthFirst(t *testing.T) {
	html := renderHTML(t, BudgetSettingsPage(budgetSettingsPageData{Project: &Project{ID: "p1", Name: "Home"}}))

	month := strings.Index(html, `data-testid="budget-window-month"`)
	week := strings.Index(html, `data-testid="budget-window-week"`)
	if month < 0 || week < 0 {
		t.Fatalf("settings windows missing anchors (month at %d, week at %d)", month, week)
	}
	if month > week {
		t.Errorf("settings page must list the monthly window before the weekly one (month at %d, week at %d)", month, week)
	}
}
