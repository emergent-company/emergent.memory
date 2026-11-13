package email

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// budgetAlertTemplateData mirrors the map UsageService.sendBudgetAlertEmail
// passes to the queue. Keeping it in the test (rather than sharing a helper with
// the provider) is deliberate: the test should notice when the template starts
// needing a key the provider does not send.
func budgetAlertTemplateData(exhausted bool) TemplateContext {
	return TemplateContext{
		"projectName":      "Huma KB",
		"windowLabel":      "weekly",
		"levelLabel":       "exhausted",
		"exhausted":        exhausted,
		"spend":            "9.20",
		"cap":              "10.00",
		"remaining":        "0.80",
		"percent":          "92",
		"thresholdPercent": "80",
		"resetsOn":         "Mon, 16 Nov",
		"usageUrl":         "https://app.example.com/o/o1/p/p1/usage",
		"budgetUrl":        "https://app.example.com/o/o1/p/p1/settings/budget",
	}
}

// TestBudgetAlertPlainText covers the text alternative: the figures, the reset
// date, and — the part that changes behaviour — whether agent runs are blocked
// and what to do about it.
func TestBudgetAlertPlainText(t *testing.T) {
	data := BudgetAlertData{
		ProjectName:      "Huma KB",
		WindowLabel:      "weekly",
		Exhausted:        true,
		SpendUSD:         10.5,
		CapUSD:           10,
		RemainingUSD:     0,
		PercentUsed:      105,
		ThresholdPercent: 80,
		ResetsOn:         "Mon, 16 Nov",
		UsageURL:         "https://app.example.com/o/o1/p/p1/usage",
		BudgetURL:        "https://app.example.com/o/o1/p/p1/settings/budget",
	}

	text := BudgetAlertPlainText(data)
	for _, want := range []string{
		"Weekly budget: Huma KB is over its limit.",
		"Spent:  $10.50 of $10.00 (105%)",
		"Left:   $0.00",
		"Resets: Mon, 16 Nov",
		"Agent runs in this project are rejected until the window resets.",
		data.UsageURL,
		data.BudgetURL,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plain text missing %q:\n%s", want, text)
		}
	}

	// The warning level must not claim runs are blocked.
	warn := BudgetAlertPlainText(BudgetAlertData{
		ProjectName: "Huma KB", WindowLabel: "monthly", SpendUSD: 8.2, CapUSD: 10,
		RemainingUSD: 1.8, PercentUsed: 82, ThresholdPercent: 80, ResetsOn: "Tue, 1 Dec",
		UsageURL: "u", BudgetURL: "b",
	})
	if strings.Contains(warn, "rejected") {
		t.Errorf("warning text must not say runs are rejected:\n%s", warn)
	}
	for _, want := range []string{"Monthly budget: Huma KB has reached its alert threshold.", "Left:   $1.80", "The alert fires at 80% of the budget."} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning text missing %q:\n%s", want, warn)
		}
	}
}

// TestBudgetAlertTemplate_RendersBothLevels renders the shipped template (not a
// fixture) for both levels, because the figures and the exhausted/warning
// branch are the whole point of the email and a broken `{{#if}}` would only show
// up at delivery time.
func TestBudgetAlertTemplate_RendersBothLevels(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	for _, exhausted := range []bool{true, false} {
		result, err := svc.Render("budget-alert", budgetAlertTemplateData(exhausted), "default")
		if err != nil {
			t.Fatalf("Render(exhausted=%v) error: %v", exhausted, err)
		}
		if result == nil {
			t.Fatalf("Render(exhausted=%v) returned nil", exhausted)
		}
		for _, want := range []string{
			"Huma KB", "$9.20", "$10.00", "Mon, 16 Nov",
			"https://app.example.com/o/o1/p/p1/usage",
			"https://app.example.com/o/o1/p/p1/settings/budget",
			"project admin",
		} {
			if !strings.Contains(result.HTML, want) {
				t.Errorf("exhausted=%v HTML missing %q", exhausted, want)
			}
		}
		if exhausted {
			if !strings.Contains(result.HTML, "rejected until the window resets") {
				t.Errorf("exhausted HTML missing the blocked-runs message")
			}
		} else if !strings.Contains(result.HTML, "crossed the 80% alert threshold") {
			t.Errorf("warning HTML missing the threshold message")
		}
	}
}
