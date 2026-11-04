package graph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// whereClause returns the rendered SQL from its first WHERE onward, so an
// assertion about predicates cannot accidentally match a selected column name.
func whereClause(sql string) string {
	if i := strings.Index(sql, " WHERE "); i >= 0 {
		return sql[i:]
	}
	return ""
}

// TestValidCredentialKind locks the accepted credential_kind values to the ones
// pkg/auth actually records on a row (#2468).
func TestValidCredentialKind(t *testing.T) {
	for _, k := range []string{
		auth.CredentialAPIKey,
		auth.CredentialMCPLink,
		auth.CredentialSession,
		auth.CredentialNone,
	} {
		require.Truef(t, validCredentialKind(k), "expected validCredentialKind(%q) == true", k)
	}
	for _, k := range []string{"", "API_KEY", "oauth", "bearer"} {
		require.Falsef(t, validCredentialKind(k), "expected validCredentialKind(%q) == false", k)
	}
}

// TestValidDataClass locks the accepted data_class values.
func TestValidDataClass(t *testing.T) {
	for _, c := range []string{DataClassSource, DataClassAdopted, DataClassSynthesized} {
		require.Truef(t, validDataClass(c), "expected validDataClass(%q) == true", c)
	}
	for _, c := range []string{"", "SOURCE", "derived", "external"} {
		require.Falsef(t, validDataClass(c), "expected validDataClass(%q) == false", c)
	}
}

// TestWriteStampFilterRendersSQL asserts the stamp predicates the query builders
// emit, without a live Postgres (the queries are rendered, never executed).
func TestWriteStampFilterRendersSQL(t *testing.T) {
	repo := newRenderRepo(t)
	pid := uuid.New()
	runID := uuid.New()
	credID := uuid.New()
	kind := auth.CredentialAPIKey
	class := DataClassSource

	t.Run("no filters means no stamp predicates", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid})
		where := whereClause(q.String())
		require.NotEmpty(t, where)
		require.NotContains(t, where, "run_id")
		require.NotContains(t, where, "credential_id")
		require.NotContains(t, where, "credential_kind")
		require.NotContains(t, where, "data_class")
	})

	t.Run("run filter matches either run column", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, Stamp: WriteStampFilters{RunID: &runID}})
		require.Contains(t, q.String(),
			"(run_id = '"+runID.String()+"' OR root_run_id = '"+runID.String()+"')")
	})

	t.Run("credential filters are independent equalities", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{
			ProjectID: pid,
			Stamp:     WriteStampFilters{CredentialID: &credID, CredentialKind: &kind},
		})
		sql := q.String()
		require.Contains(t, sql, "credential_id = '"+credID.String()+"'")
		require.Contains(t, sql, "credential_kind = '"+auth.CredentialAPIKey+"'")
	})

	t.Run("data class filter is an equality", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, Stamp: WriteStampFilters{DataClass: &class}})
		require.Contains(t, q.String(), "data_class = '"+DataClassSource+"'")
	})

	t.Run("relationships filter credential and run but never data class", func(t *testing.T) {
		q := repo.db.NewSelect().Model((*GraphRelationship)(nil))
		q = applyRelationshipWriteStampFilters(q, WriteStampFilters{
			RunID:        &runID,
			CredentialID: &credID,
			DataClass:    &class, // rejected at the HTTP boundary; ignored here
		}, ProvenanceUpdated, pid, nil)
		sql := q.String()
		require.Contains(t, sql, "(run_id = '"+runID.String()+"' OR root_run_id = '"+runID.String()+"')")
		require.Contains(t, sql, "credential_id = '"+credID.String()+"'")
		require.NotContains(t, whereClause(sql), "data_class")
	})

	t.Run("the default scope matches the current version only", func(t *testing.T) {
		for _, mode := range []string{"", ProvenanceUpdated} {
			q := repo.buildObjectBaseQueryWith(repo.db, ListParams{
				ProjectID:  pid,
				Stamp:      WriteStampFilters{RunID: &runID},
				Provenance: mode,
			})
			sql := q.String()
			require.Contains(t, sql, "(run_id = '"+runID.String()+"' OR root_run_id = '"+runID.String()+"')")
			require.NotContains(t, sql, "version = 1", "mode %q must not widen to the creator version", mode)
		}
	})

	t.Run("created scope requires the root version to carry the stamp", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{
			ProjectID:  pid,
			Stamp:      WriteStampFilters{RunID: &runID},
			Provenance: ProvenanceCreated,
		})
		sql := q.String()
		require.Contains(t, sql, "canonical_id IN (SELECT canonical_id FROM kb.graph_objects WHERE")
		require.Contains(t, sql, "(run_id = '"+runID.String()+"' OR root_run_id = '"+runID.String()+"')")
		require.Contains(t, sql, "version = 1")
		require.Contains(t, sql, "deleted_at IS NULL")
		// The inner scan is scoped to the caller's project and branch, or a run
		// filter would match another project's version rows.
		require.Contains(t, sql, "project_id = '"+pid.String()+"' AND branch_id IS NULL")
	})

	t.Run("any scope unions the current version and the creator version", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{
			ProjectID:  pid,
			Stamp:      WriteStampFilters{CredentialID: &credID},
			Provenance: ProvenanceAny,
		})
		sql := q.String()
		require.Contains(t, sql, "credential_id = '"+credID.String()+"') OR canonical_id IN (SELECT canonical_id FROM kb.graph_objects")
		require.Contains(t, sql, "version = 1")
	})

	t.Run("created scope is scoped to the caller's branch", func(t *testing.T) {
		branchID := uuid.New()
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{
			ProjectID:  pid,
			BranchID:   &branchID,
			Stamp:      WriteStampFilters{RunID: &runID},
			Provenance: ProvenanceCreated,
		})
		sql := q.String()
		require.Contains(t, sql, "project_id = '"+pid.String()+"' AND branch_id = '"+branchID.String()+"'")
		require.NotContains(t, sql, "branch_id IS NULL")
	})

	t.Run("relationship scopes select from the relationship table", func(t *testing.T) {
		for _, tc := range []struct {
			mode     string
			wantRoot bool
		}{
			{ProvenanceCreated, true},
			{ProvenanceAny, true},
			{ProvenanceUpdated, false},
			{"", false},
		} {
			q := repo.db.NewSelect().Model((*GraphRelationship)(nil))
			q = applyRelationshipWriteStampFilters(q, WriteStampFilters{RunID: &runID}, tc.mode, pid, nil)
			sql := q.String()
			require.Equal(t, tc.wantRoot, strings.Contains(sql, "FROM kb.graph_relationships WHERE"),
				"mode %q", tc.mode)
			if tc.wantRoot {
				require.Contains(t, sql, "version = 1")
				require.NotContains(t, sql, "FROM kb.graph_objects")
			}
		}
	})
}

// TestParseWriteStampFilters covers the handler-level parsing: valid values
// populate the filter struct, malformed uuids and unknown enums are client
// errors, and a data_class filter is refused for relationships, which record no
// data class.
func TestParseWriteStampFilters(t *testing.T) {
	newCtx := func(query string) echo.Context {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, query, nil)
		return e.NewContext(req, httptest.NewRecorder())
	}

	t.Run("all filters populate", func(t *testing.T) {
		runID, credID := uuid.New(), uuid.New()
		c := newCtx("/?run_id=" + runID.String() + "&credential_id=" + credID.String() + "&credential_kind=mcp_link&data_class=adopted")
		f, err := parseWriteStampFilters(c, true)
		require.NoError(t, err)
		require.NotNil(t, f.RunID)
		require.Equal(t, runID, *f.RunID)
		require.NotNil(t, f.CredentialID)
		require.Equal(t, credID, *f.CredentialID)
		require.NotNil(t, f.CredentialKind)
		require.Equal(t, auth.CredentialMCPLink, *f.CredentialKind)
		require.NotNil(t, f.DataClass)
		require.Equal(t, DataClassAdopted, *f.DataClass)
	})

	t.Run("no params is a no-op", func(t *testing.T) {
		f, err := parseWriteStampFilters(newCtx("/"), true)
		require.NoError(t, err)
		require.Equal(t, WriteStampFilters{}, f)
	})

	t.Run("credential_id without credential_kind is allowed", func(t *testing.T) {
		credID := uuid.New()
		f, err := parseWriteStampFilters(newCtx("/?credential_id="+credID.String()), true)
		require.NoError(t, err)
		require.NotNil(t, f.CredentialID)
		require.Nil(t, f.CredentialKind)
	})

	t.Run("invalid run id rejected", func(t *testing.T) {
		_, err := parseWriteStampFilters(newCtx("/?run_id=not-a-uuid"), true)
		require.Error(t, err)
		require.IsType(t, &apperror.Error{}, err)
		require.Contains(t, err.Error(), "invalid run_id")
	})

	t.Run("invalid credential id rejected", func(t *testing.T) {
		_, err := parseWriteStampFilters(newCtx("/?credential_id=nope"), true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid credential_id")
	})

	t.Run("unknown credential kind rejected", func(t *testing.T) {
		_, err := parseWriteStampFilters(newCtx("/?credential_kind=oauth"), true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid credential_kind")
		require.Contains(t, err.Error(), "api_key")
	})

	t.Run("unknown data class rejected", func(t *testing.T) {
		_, err := parseWriteStampFilters(newCtx("/?data_class=derived"), true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid data_class")
	})

	t.Run("data class refused for relationships", func(t *testing.T) {
		_, err := parseWriteStampFilters(newCtx("/?data_class=source"), false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "data_class is not recorded on relationships")
	})

	t.Run("relationships accept the other three filters", func(t *testing.T) {
		runID := uuid.New()
		c := newCtx("/?run_id=" + runID.String() + "&credential_kind=session")
		f, err := parseWriteStampFilters(c, false)
		require.NoError(t, err)
		require.NotNil(t, f.RunID)
		require.Equal(t, auth.CredentialSession, *f.CredentialKind)
		require.Nil(t, f.DataClass)
	})
}

// TestParseProvenanceScope locks the boundary rules for the version-scope
// selector. `provenance` now narrows either an actor filter or a write-stamp
// filter, so the "requires actor_type" rule became "requires something to
// narrow"; "any" stays accepted on its own because clients send it
// unconditionally and it removes nothing.
func TestParseProvenanceScope(t *testing.T) {
	newCtx := func(target string) echo.Context {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		return e.NewContext(req, httptest.NewRecorder())
	}
	runID := uuid.New()

	t.Run("objects accept a narrowing mode with a stamp filter", func(t *testing.T) {
		params := ListParams{Stamp: WriteStampFilters{RunID: &runID}}
		require.NoError(t, parseActorProvenance(newCtx("/?provenance=created"), &params))
		require.Equal(t, ProvenanceCreated, params.Provenance)
	})

	t.Run("objects reject a narrowing mode with nothing to narrow", func(t *testing.T) {
		params := ListParams{}
		err := parseActorProvenance(newCtx("/?provenance=created"), &params)
		require.Error(t, err)
		require.IsType(t, &apperror.Error{}, err)
		require.Contains(t, err.Error(), "provenance requires actor_type or a write-stamp filter")
	})

	t.Run("objects keep the actor path working", func(t *testing.T) {
		params := ListParams{}
		require.NoError(t, parseActorProvenance(newCtx("/?actor_type=agent&provenance=updated"), &params))
		require.Equal(t, ProvenanceUpdated, params.Provenance)
	})

	t.Run("any is accepted alone and survives into the params", func(t *testing.T) {
		params := ListParams{}
		require.NoError(t, parseActorProvenance(newCtx("/?provenance=any"), &params))
		require.Equal(t, ProvenanceAny, params.Provenance)
	})

	t.Run("unknown modes are rejected", func(t *testing.T) {
		params := ListParams{Stamp: WriteStampFilters{RunID: &runID}}
		err := parseActorProvenance(newCtx("/?provenance=whenever"), &params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid provenance")
	})

	t.Run("relationships need a stamp filter for a narrowing mode", func(t *testing.T) {
		params := RelationshipListParams{}
		err := parseRelationshipProvenance(newCtx("/?provenance=created"), &params)
		require.Error(t, err)
		require.Contains(t, err.Error(), "provenance requires a write-stamp filter")

		params.Stamp = WriteStampFilters{RunID: &runID}
		require.NoError(t, parseRelationshipProvenance(newCtx("/?provenance=created"), &params))
		require.Equal(t, ProvenanceCreated, params.Provenance)
	})

	t.Run("relationships accept any on its own", func(t *testing.T) {
		params := RelationshipListParams{}
		require.NoError(t, parseRelationshipProvenance(newCtx("/?provenance=any"), &params))
		require.Equal(t, ProvenanceAny, params.Provenance)
	})
}
