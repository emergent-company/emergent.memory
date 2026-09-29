package graph

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestValidProvenance locks the accepted provenance enum values (issue #1193).
func TestValidProvenance(t *testing.T) {
	for _, m := range []string{ProvenanceAny, ProvenanceCreated, ProvenanceUpdated} {
		if !validProvenance(m) {
			t.Errorf("expected validProvenance(%q) == true", m)
		}
	}
	for _, m := range []string{"", "CREATED", "bogus", "creator"} {
		if validProvenance(m) {
			t.Errorf("expected validProvenance(%q) == false", m)
		}
	}
}

// TestProvenanceFilterRendersSQL asserts the actor filter composes the right
// WHERE fragments for each provenance mode and the (actor_type, actor_id) pair
// rule, without a live Postgres (the query is only rendered, never executed).
func TestProvenanceFilterRendersSQL(t *testing.T) {
	repo := newRenderRepo(t)
	pid := uuid.New()
	actorType := "agent"
	actorID := uuid.New()

	t.Run("no actor_type means no filter", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorID: &actorID})
		sql := q.String()
		require.NotContains(t, sql, "actor_type = ")
		require.NotContains(t, sql, "version = 1")
		require.NotContains(t, sql, "canonical_id IN (SELECT canonical_id FROM kb.graph_objects WHERE")
	})

	t.Run("created mode uses version=1 subquery", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorType: &actorType, ActorID: &actorID, Provenance: ProvenanceCreated})
		sql := q.String()
		require.Contains(t, sql, "canonical_id IN (SELECT canonical_id FROM kb.graph_objects WHERE actor_type = 'agent' AND actor_id = '"+actorID.String()+"' AND version = 1 AND deleted_at IS NULL)")
		require.NotContains(t, sql, "OR canonical_id IN")
	})

	t.Run("updated mode filters the head pair only", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorType: &actorType, ActorID: &actorID, Provenance: ProvenanceUpdated})
		sql := q.String()
		require.Contains(t, sql, "actor_type = 'agent' AND actor_id = '"+actorID.String()+"'")
		require.NotContains(t, sql, "version = 1")
		require.NotContains(t, sql, "canonical_id IN (SELECT")
	})

	t.Run("any mode is creator OR updater", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorType: &actorType, ActorID: &actorID, Provenance: ProvenanceAny})
		sql := q.String()
		require.Contains(t, sql, "(actor_type = 'agent' AND actor_id = '"+actorID.String()+"') OR canonical_id IN (SELECT canonical_id FROM kb.graph_objects WHERE actor_type = 'agent' AND actor_id = '"+actorID.String()+"' AND version = 1 AND deleted_at IS NULL)")
	})

	t.Run("default (empty) provenance behaves like any", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorType: &actorType, ActorID: &actorID})
		sql := q.String()
		require.Contains(t, sql, "OR canonical_id IN (SELECT canonical_id FROM kb.graph_objects WHERE actor_type = 'agent' AND actor_id = '"+actorID.String()+"' AND version = 1 AND deleted_at IS NULL)")
	})

	t.Run("nil actor_id (system) uses actor_id IS NULL", func(t *testing.T) {
		q := repo.buildObjectBaseQueryWith(repo.db, ListParams{ProjectID: pid, ActorType: &actorType, Provenance: ProvenanceCreated})
		sql := q.String()
		require.Contains(t, sql, "actor_id IS NULL")
		require.NotContains(t, sql, "actor_id = ")
	})
}

// TestParseActorProvenance covers the handler-level parsing and validation:
// valid values populate ListParams; invalid provenance / actor_id, and the
// actor_id-without-actor_type pair rule, are rejected with a 400.
func TestParseActorProvenance(t *testing.T) {
	newCtx := func(query string) (echo.Context, *ListParams) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, query, nil)
		rec := httptest.NewRecorder()
		return e.NewContext(req, rec), &ListParams{}
	}

	t.Run("valid actor_type+actor_id+provenance", func(t *testing.T) {
		id := uuid.New()
		c, p := newCtx("/?actor_type=agent&actor_id=" + id.String() + "&provenance=created")
		require.NoError(t, parseActorProvenance(c, p))
		require.NotNil(t, p.ActorType)
		require.Equal(t, "agent", *p.ActorType)
		require.NotNil(t, p.ActorID)
		require.Equal(t, id, *p.ActorID)
		require.Equal(t, ProvenanceCreated, p.Provenance)
	})

	t.Run("invalid actor_id rejected", func(t *testing.T) {
		c, p := newCtx("/?actor_type=agent&actor_id=not-a-uuid")
		err := parseActorProvenance(c, p)
		require.Error(t, err)
		require.IsType(t, &apperror.Error{}, err)
		require.Contains(t, err.Error(), "invalid actor_id")
	})

	t.Run("actor_id without actor_type rejected", func(t *testing.T) {
		c, p := newCtx("/?actor_id=" + uuid.New().String())
		err := parseActorProvenance(c, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "actor_id requires actor_type")
	})

	t.Run("invalid provenance rejected", func(t *testing.T) {
		c, p := newCtx("/?actor_type=agent&provenance=bogus")
		err := parseActorProvenance(c, p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid provenance")
	})

	t.Run("no params is a no-op", func(t *testing.T) {
		c, p := newCtx("/")
		require.NoError(t, parseActorProvenance(c, p))
		require.Nil(t, p.ActorType)
		require.Nil(t, p.ActorID)
		require.Empty(t, p.Provenance)
	})

	t.Run("actor_type only (system) allowed", func(t *testing.T) {
		c, p := newCtx("/?actor_type=system")
		require.NoError(t, parseActorProvenance(c, p))
		require.NotNil(t, p.ActorType)
		require.Equal(t, "system", *p.ActorType)
		require.Nil(t, p.ActorID)
	})
}

// TestToResponsePopulatesActor pins the DTO wiring (issue #1193): the actor
// fields must be populated in ToResponse and emitted in the JSON (the custom
// MarshalJSON embeds an alias, so a declared-but-unpopulated field would be
// silently omitted).
func TestToResponsePopulatesActor(t *testing.T) {
	actorType := "user"
	actorID := uuid.New()

	objResp := (&GraphObject{ActorType: &actorType, ActorID: &actorID}).ToResponse()
	require.NotNil(t, objResp.ActorType)
	require.Equal(t, "user", *objResp.ActorType)
	require.NotNil(t, objResp.ActorID)
	require.Equal(t, actorID, *objResp.ActorID)

	relResp := (&GraphRelationship{ActorType: &actorType, ActorID: &actorID}).ToResponse()
	require.NotNil(t, relResp.ActorType)
	require.Equal(t, "user", *relResp.ActorType)
	require.NotNil(t, relResp.ActorID)
	require.Equal(t, actorID, *relResp.ActorID)

	// The object's custom MarshalJSON must emit actor_type/actor_id.
	b, err := objResp.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(b), `"actor_type":"user"`)
	require.Contains(t, string(b), `"actor_id":"`+actorID.String()+`"`)
}
