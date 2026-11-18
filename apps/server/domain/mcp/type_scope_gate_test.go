package mcp

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestTypeForScopeGateFailsClosedOnLookupFault is the #930 guard test. Every
// type-scope write gate used to read `if typ, lerr := lookup(); lerr == nil`,
// so a store fault skipped the authorization check entirely and the write
// proceeded. The guard must now refuse: an internal app error, no type, and a
// public message that carries no driver detail. Reverting the guard to treat a
// fault as "nothing to check" reds this test.
func TestTypeForScopeGateFailsClosedOnLookupFault(t *testing.T) {
	driverErr := errors.New(`pq: relation "kb.graph_objects" does not exist (SQLSTATE 42P01)`)

	typ, found, err := typeForScopeGate(func() (string, error) { return "", driverErr })

	require.Error(t, err, "a lookup fault must not be treated as an absent object")
	assert.False(t, found)
	assert.Empty(t, typ)

	appErr, ok := errors.AsType[*apperror.Error](err)
	require.True(t, ok, "the gate refusal must be an app error so every transport maps it, got %T", err)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Equal(t, "An internal error occurred", apperror.PublicMessage(err),
		"the caller gets the stable sentence, not the store detail")
	assert.NotContains(t, apperror.PublicMessage(err), "kb.graph_objects")
	assert.NotContains(t, apperror.PublicMessage(err), "SQLSTATE")
}

// TestTypeForScopeGateDistinguishesAMissFromAHit pins the other half of the
// contract: a genuinely absent target is not a fault. The gate reports
// found=false with no error so the caller's write path can report the 404 it
// already reports, and a successful lookup hands the type to the scope check.
func TestTypeForScopeGateDistinguishesAMissFromAHit(t *testing.T) {
	typ, found, err := typeForScopeGate(func() (string, error) {
		return "", errTypeScopeTargetNotFound
	})
	require.NoError(t, err, "a genuine miss must stay a 404, not become a 500")
	assert.False(t, found)
	assert.Empty(t, typ)

	typ, found, err = typeForScopeGate(func() (string, error) { return "Person", nil })
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "Person", typ)
}
