package agents

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestOrderedByUser pins the column value rule for the person who ordered a run.
// The column is a uuid, so anything that is not a real user id must persist NULL:
// provenance then claims no orderer rather than attributing the write to a
// stranger. "system" is the background/trigger marker, an empty string is a run
// nobody drove, and a share-run end-user ref is project-scoped and resolvable to
// no member.
func TestOrderedByUser(t *testing.T) {
	userID := uuid.NewString()

	cases := []struct {
		name  string
		in    string
		want  *string
		nilly bool
	}{
		{name: "user uuid", in: userID, want: &userID},
		{name: "empty", in: "", nilly: true},
		{name: "system", in: "system", nilly: true},
		{name: "not a uuid", in: "share-end-user-42", nilly: true},
		{name: "uuid-shaped prefix", in: userID[:8], nilly: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OrderedByUser(tc.in)
			if tc.nilly {
				require.Nil(t, got, "non-identity %q must persist NULL", tc.in)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, *tc.want, *got)
			// The returned pointer must not alias the caller's string storage.
			*got = uuid.NewString()
			require.NotEqual(t, *tc.want, *got)
		})
	}
}

// TestParseRunIDs pins the ids filter parse: the /agent-runs endpoint takes a
// comma-separated set from a caller that already holds run ids (object
// provenance). Malformed values are dropped — a typo must not turn a read into a
// 500 against a uuid column — duplicates collapse, and the set is capped at the
// endpoint's page maximum.
func TestParseRunIDs(t *testing.T) {
	a, b := uuid.NewString(), uuid.NewString()

	require.Equal(t, []string{a, b}, parseRunIDs(" "+a+" , "+b+" "))
	require.Equal(t, []string{a}, parseRunIDs(a+","+a))
	require.Empty(t, parseRunIDs(""))
	require.Empty(t, parseRunIDs(" , "))
	require.Empty(t, parseRunIDs("not-a-uuid"))
	require.Equal(t, []string{a}, parseRunIDs("not-a-uuid,"+a))

	// Cap at 100 distinct ids.
	ids := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		ids = append(ids, uuid.NewString())
	}
	require.Len(t, parseRunIDs(joinComma(ids)), 100)
}

func joinComma(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// TestOrderedByUserOrPrior pins resume continuity: a user principal takes the
// order, and a principal that is not a user (share end user, empty) keeps the
// orderer recorded on the run being resumed.
func TestOrderedByUserOrPrior(t *testing.T) {
	userID := uuid.NewString()
	priorID := uuid.NewString()
	prior := &priorID

	require.Equal(t, userID, *OrderedByUserOrPrior(userID, prior))
	require.Equal(t, priorID, *OrderedByUserOrPrior("", prior))
	require.Equal(t, priorID, *OrderedByUserOrPrior("system", prior))
	require.Equal(t, priorID, *OrderedByUserOrPrior("share-end-user-42", prior))
	require.Nil(t, OrderedByUserOrPrior("", nil))
}

// TestCreateRunWithOptions_PersistsOrderedByUser verifies the ordering person is
// on the row, and that a run nobody ordered records NULL rather than a guess.
func TestCreateRunWithOptions_PersistsOrderedByUser(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	userID := uuid.NewString()

	ordered, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:         agentID,
		OrderedByUserID: OrderedByUser(userID),
	})
	require.NoError(t, err)
	require.NotNil(t, ordered.OrderedByUserID)
	require.Equal(t, userID, *ordered.OrderedByUserID)

	// The value survives a read, not just the in-memory struct.
	var stored *string
	require.NoError(t, db.NewRaw(`SELECT ordered_by_user_id FROM kb.agent_runs WHERE id = ?`, ordered.ID).Scan(ctx, &stored))
	require.NotNil(t, stored)
	require.Equal(t, userID, *stored)

	unowned, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:         agentID,
		OrderedByUserID: OrderedByUser("system"),
	})
	require.NoError(t, err)
	require.Nil(t, unowned.OrderedByUserID)
	stored = nil
	require.NoError(t, db.NewRaw(`SELECT ordered_by_user_id FROM kb.agent_runs WHERE id = ?`, unowned.ID).Scan(ctx, &stored))
	require.Nil(t, stored)
}

// TestFindRunsByProjectPaginated_IDSFilter verifies the batch read object
// provenance uses: one project-scoped query for a set of run ids, returning only
// the runs that belong to this project. A foreign id is silently absent.
func TestFindRunsByProjectPaginated_IDSFilter(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	userID := uuid.NewString()

	first, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID, OrderedByUserID: OrderedByUser(userID)})
	require.NoError(t, err)
	second, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID})
	require.NoError(t, err)
	other, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID})
	require.NoError(t, err)

	runs, total, err := repo.FindRunsByProjectPaginated(ctx, projectID,
		RunFilters{IDs: []string{first.ID, second.ID}, AgentID: &agentID}, 100, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, runs, 2)

	byID := map[string]*AgentRun{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	require.Contains(t, byID, first.ID)
	require.Contains(t, byID, second.ID)
	require.NotContains(t, byID, other.ID, "an id outside the filter set must not come back")
	require.NotNil(t, byID[first.ID].OrderedByUserID)
	require.Equal(t, userID, *byID[first.ID].OrderedByUserID)
	require.Nil(t, byID[second.ID].OrderedByUserID)

	// A foreign run id (another project) is absent, never leaked.
	_, foreignProject := insertOrgAndProject(t, db, ctx)
	foreignDefID := insertWorkDefinition(t, db, ctx, foreignProject, "researcher", `{}`)
	foreignAgentID := insertWorkAgent(t, db, ctx, foreignProject, "researcher", foreignDefID)
	foreignRun, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: foreignAgentID})
	require.NoError(t, err)

	runs, total, err = repo.FindRunsByProjectPaginated(ctx, projectID,
		RunFilters{IDs: []string{foreignRun.ID, first.ID}}, 100, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, runs, 1)
	require.Equal(t, first.ID, runs[0].ID)
}
