package projects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProjectToDTO_CarriesStatsAndBothWindows guards the DTO projection.
//
// `Stats` is populated by the repository only when the caller asks for it, and
// ToDTO is the only thing that copies it onto the DTO — the CLI's `--stats`, the
// REST `?include_stats=true` parameter and the SDK's IncludeStats all read that
// single line. When it was dropped, every one of them silently returned no stats
// and no test noticed, because the field is optional everywhere downstream.
//
// The weekly fields are asserted in the same test because they are the other
// half of the same projection: a new project reports a $10 monthly cap and a
// $2.50 weekly cap, both thresholds 0.8, and an uncapped weekly window must stay
// nil rather than being flattened to 0.
func TestProjectToDTO_CarriesStatsAndBothWindows(t *testing.T) {
	monthly := 10.0
	weekly := 2.5
	stats := &ProjectStats{DocumentCount: 7, ObjectCount: 11}

	p := &Project{
		ID:                         "project-1",
		OrganizationID:             "org-1",
		Name:                       "Budget Project",
		BudgetUSD:                  &monthly,
		BudgetAlertThreshold:       0.80,
		BudgetWeeklyUSD:            &weekly,
		BudgetWeeklyAlertThreshold: 0.80,
		Stats:                      stats,
	}

	dto := p.ToDTO()

	require.Same(t, stats, dto.Stats,
		"ToDTO must carry the loaded stats: --stats / ?include_stats=true read this field and nothing else sets it")
	require.NotNil(t, dto.BudgetWeeklyUSD)
	require.InDelta(t, 2.5, *dto.BudgetWeeklyUSD, 1e-9)
	require.NotNil(t, dto.BudgetWeeklyAlertThreshold)
	require.InDelta(t, 0.80, *dto.BudgetWeeklyAlertThreshold, 1e-9)

	// An existing project that was never backfilled stays weekly-uncapped.
	uncapped := &Project{ID: "project-2", OrganizationID: "org-1", Name: "Legacy"}
	legacy := uncapped.ToDTO()
	require.Nil(t, legacy.BudgetWeeklyUSD,
		"a nil weekly cap must stay nil, not become $0 — the two mean different things to the UI")
	require.NotNil(t, legacy.BudgetWeeklyAlertThreshold,
		"the weekly threshold is NOT NULL in the schema and is always reported")
}
