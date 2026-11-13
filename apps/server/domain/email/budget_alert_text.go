package email

import (
	"fmt"
	"strings"
)

// BudgetAlertData is the plain-text rendering input for a budget-alert email.
// It is a struct rather than a long parameter list because the same figures
// appear in the subject, the HTML template and this body, and a positional call
// with eight formatted strings is exactly the shape that gets two of them
// swapped (a "$70 left" that is really the cap, say).
type BudgetAlertData struct {
	// ProjectName is the project the spend belongs to.
	ProjectName string
	// WindowLabel is "weekly" or "monthly".
	WindowLabel string
	// Exhausted is true at the hard cap (agent runs rejected), false at the
	// earlier threshold warning.
	Exhausted bool
	// SpendUSD and CapUSD are the window's spend and cap, in USD.
	SpendUSD float64
	CapUSD   float64
	// RemainingUSD is the unspent part of the cap, floored at zero.
	RemainingUSD float64
	// PercentUsed and ThresholdPercent are percentages (0-100+).
	PercentUsed      float64
	ThresholdPercent float64
	// ResetsOn is a human date for the end of the current window.
	ResetsOn string
	// UsageURL is the absolute link to the project usage page.
	UsageURL string
	// BudgetURL is the absolute link to the project budget settings.
	BudgetURL string
}

// BudgetAlertPlainText builds the plain-text alternative body for a
// budget-alert email. It mirrors the HTML template: the figures, the level, the
// reset date and what happens next, so a client that cannot render HTML still
// learns whether agent runs are blocked and when the window reopens.
func BudgetAlertPlainText(d BudgetAlertData) string {
	var b strings.Builder

	level := "has reached its alert threshold"
	if d.Exhausted {
		level = "is over its limit"
	}

	fmt.Fprintf(&b, "%s budget: %s %s.\n\n", strings.ToUpper(d.WindowLabel[:1])+d.WindowLabel[1:], d.ProjectName, level)
	fmt.Fprintf(&b, "Spent:  $%.2f of $%.2f (%.0f%%)\n", d.SpendUSD, d.CapUSD, d.PercentUsed)
	fmt.Fprintf(&b, "Left:   $%.2f\n", d.RemainingUSD)
	fmt.Fprintf(&b, "Resets: %s\n\n", d.ResetsOn)

	if d.Exhausted {
		b.WriteString("Agent runs in this project are rejected until the window resets.\n\n")
		b.WriteString("To unblock it now, raise the budget or reset the window in budget settings.\n\n")
	} else {
		fmt.Fprintf(&b, "The alert fires at %.0f%% of the budget.\n\n", d.ThresholdPercent)
	}

	fmt.Fprintf(&b, "View usage: %s\n", d.UsageURL)
	fmt.Fprintf(&b, "Manage budget: %s\n\n", d.BudgetURL)
	b.WriteString("You are receiving this because you are a project admin.\n\n")
	b.WriteString("— The emergent.memory team\n")

	return b.String()
}
