package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/budgetwindow"
)

// weekPeriod is a fixed window so the state assertions do not depend on today.
func weekPeriod() budgetwindow.Period {
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	return budgetwindow.Period{Start: start, End: start.AddDate(0, 0, 7)}
}

// TestNewBudgetWindowState_ClassifiesLevels covers the four shapes a window can
// be in, including the two that must NOT alert: an uncapped window and a window
// with a non-positive threshold (the #1347 shape, where `spend >= cap*0` would
// otherwise fire at $0).
func TestNewBudgetWindowState_ClassifiesLevels(t *testing.T) {
	period := weekPeriod()

	tests := []struct {
		name      string
		capUSD    *float64
		threshold float64
		spend     float64
		want      string
	}{
		{name: "uncapped is ok", capUSD: nil, threshold: 0.8, spend: 100, want: budgetStatusOK},
		{name: "zero cap is uncapped", capUSD: f64(0), threshold: 0.8, spend: 100, want: budgetStatusOK},
		{name: "under threshold", capUSD: f64(10), threshold: 0.8, spend: 7.9, want: budgetStatusOK},
		{name: "exactly at threshold", capUSD: f64(10), threshold: 0.8, spend: 8, want: budgetStatusWarning},
		{name: "above threshold", capUSD: f64(10), threshold: 0.8, spend: 9.99, want: budgetStatusWarning},
		{name: "exactly at cap", capUSD: f64(10), threshold: 0.8, spend: 10, want: budgetStatusExhausted},
		{name: "over cap", capUSD: f64(10), threshold: 0.8, spend: 25, want: budgetStatusExhausted},
		{name: "zero threshold never warns", capUSD: f64(10), threshold: 0, spend: 9.99, want: budgetStatusOK},
		{name: "threshold one means cap only", capUSD: f64(10), threshold: 1, spend: 9.99, want: budgetStatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newBudgetWindowState(budgetwindow.Week, tt.capUSD, tt.threshold, tt.spend, period)
			require.Equal(t, tt.want, st.Status)
			require.Equal(t, period.Start, st.PeriodStart)
			require.Equal(t, period.End, st.PeriodEnd)
			if tt.capUSD == nil || *tt.capUSD <= 0 {
				require.Nil(t, st.CapUSD, "an uncapped window reports no cap")
				require.Nil(t, st.RemainingUSD, "an uncapped window reports no remaining")
			} else {
				require.NotNil(t, st.RemainingUSD)
				require.GreaterOrEqual(t, *st.RemainingUSD, 0.0)
			}
		})
	}
}

func TestNewBudgetWindowState_PercentAndRemaining(t *testing.T) {
	st := newBudgetWindowState(budgetwindow.Month, f64(10), 0.8, 2.5, weekPeriod())
	require.InDelta(t, 25.0, st.PercentUsed, 1e-9)
	require.InDelta(t, 7.5, *st.RemainingUSD, 1e-9)

	over := newBudgetWindowState(budgetwindow.Month, f64(10), 0.8, 12.5, weekPeriod())
	require.InDelta(t, 125.0, over.PercentUsed, 1e-9)
	require.Zero(t, *over.RemainingUSD)
}

// TestBudgetStateExhausted proves a single exhausted window blocks, a warning
// does not, and nil is safe (fail-open callers pass the result of a failed read
// as nil in some paths).
func TestBudgetStateExhausted(t *testing.T) {
	period := weekPeriod()
	warn := newBudgetWindowState(budgetwindow.Week, f64(10), 0.8, 9, period)
	ok := newBudgetWindowState(budgetwindow.Week, f64(10), 0.8, 1, period)
	over := newBudgetWindowState(budgetwindow.Month, f64(10), 0.8, 11, period)
	uncapped := newBudgetWindowState(budgetwindow.Week, nil, 0.8, 999, period)

	require.False(t, budgetStateExhausted(nil))
	require.False(t, budgetStateExhausted(&BudgetState{Week: warn, Month: ok}))
	require.True(t, budgetStateExhausted(&BudgetState{Week: warn, Month: over}))
	require.True(t, budgetStateExhausted(&BudgetState{Week: uncapped, Month: over}))
	require.False(t, budgetStateExhausted(&BudgetState{Week: uncapped, Month: ok}))
}
