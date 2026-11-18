package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestTypeScopeGatesFailClosedOnLookupFault drives every write site whose
// type-scope check used to fail open (#930): entity-update, entity-delete,
// entity-restore and relationship-update. A cancelled context makes the type
// lookup fault — the object exists and every argument is well formed, only the
// store read fails — so the gate must refuse the call. Before the fix the check
// was skipped (`if typ, lerr := lookup(); lerr == nil`) and the write went ahead,
// which is exactly the bypass this test exists to prevent: reverting a site to
// the old shape makes its subtest fail (the error stops being the gate refusal,
// and on the pre-fix code path the write is attempted instead).
func TestTypeScopeGatesFailClosedOnLookupFault(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	personID := insertGraphObject(t, db, projectID, "Person", "person-fail-closed")
	src := insertGraphObject(t, db, projectID, "Law", "law-fail-closed-1")
	dst := insertGraphObject(t, db, projectID, "Law", "law-fail-closed-2")
	relID := insertRelationship(t, db, projectID, "KNOWS", src, dst)

	faulted, cancel := context.WithCancel(context.Background())
	cancel()

	sites := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{
			name: "entity-update",
			call: func(ctx context.Context) error {
				_, err := svc.executeUpdateEntity(ctx, projectID, map[string]any{
					"entity_id":  personID,
					"properties": map[string]any{"name": "x"},
				})
				return err
			},
		},
		{
			name: "entity-delete",
			call: func(ctx context.Context) error {
				_, err := svc.executeDeleteEntity(ctx, projectID, map[string]any{"entity_id": personID})
				return err
			},
		},
		{
			name: "entity-restore",
			call: func(ctx context.Context) error {
				_, err := svc.executeRestoreEntity(ctx, projectID, map[string]any{"entity_id": personID})
				return err
			},
		},
		{
			name: "relationship-update",
			call: func(ctx context.Context) error {
				_, err := svc.executeUpdateRelationship(ctx, projectID, map[string]any{
					"relationship_id": relID,
					"properties":      map[string]any{"note": "x"},
				})
				return err
			},
		},
	}

	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			err := site.call(faulted)

			require.Error(t, err, "a lookup fault must refuse the call, not skip the scope check")
			appErr, ok := errors.AsType[*apperror.Error](err)
			require.True(t, ok,
				"the gate refusal must be an internal app error; a plain error means the fault fell through to the write, got %T: %v", err, err)
			assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
			assert.Equal(t, "An internal error occurred", apperror.PublicMessage(err))
		})
	}
}

// TestTypeScopeGatesKeepAMissAsNotFound is the regression guard for the other
// half of the same fix: distinguishing "provably absent" from "could not look
// it up" must not turn a real 404 into a 500. Each write site still has to
// report a not-found app error for an id that does not exist.
func TestTypeScopeGatesKeepAMissAsNotFound(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	// A restricting scope, so a wrong classification would be visible.
	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})
	missing := uuid.NewString()

	sites := []struct {
		name string
		call func() error
	}{
		{
			name: "entity-update",
			call: func() error {
				_, err := svc.executeUpdateEntity(ctx, projectID, map[string]any{
					"entity_id":  missing,
					"properties": map[string]any{"name": "x"},
				})
				return err
			},
		},
		{
			name: "entity-delete",
			call: func() error {
				_, err := svc.executeDeleteEntity(ctx, projectID, map[string]any{"entity_id": missing})
				return err
			},
		},
		{
			name: "entity-restore",
			call: func() error {
				_, err := svc.executeRestoreEntity(ctx, projectID, map[string]any{"entity_id": missing})
				return err
			},
		},
		{
			name: "relationship-update",
			call: func() error {
				_, err := svc.executeUpdateRelationship(ctx, projectID, map[string]any{
					"relationship_id": missing,
					"properties":      map[string]any{"note": "x"},
				})
				return err
			},
		},
	}

	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			err := site.call()

			require.Error(t, err)
			assert.Equal(t, ErrCodeNotFound, jsonRPCErrorCodeFor(err),
				"a genuinely absent target must stay a 404, not become a 500: %v", err)
		})
	}
}
