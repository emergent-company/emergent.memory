package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type detachTestKey struct{}

// TestDetachedRunContextOutlivesParentCancellation is the fail-first regression
// for issue #1149: a run started on a detached context must not be cancelled
// when the triggering request context is. If DetachedRunContext is reverted to
// return the parent unchanged (the old binding), this goes RED because the
// derived context would observe the cancellation.
func TestDetachedRunContextOutlivesParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	parent = context.WithValue(parent, detachTestKey{}, "kept-value")

	runCtx := DetachedRunContext(parent)

	// The control: the raw parent is bound to the request and must cancel.
	cancel()
	require.ErrorIs(t, parent.Err(), context.Canceled,
		"the request context itself must still be cancelled")
	require.NoError(t, runCtx.Err(),
		"a detached run context must survive cancellation of the request context")
	require.Nil(t, runCtx.Done(),
		"a detached run context carries no cancellation channel")
	require.Equal(t, "kept-value", runCtx.Value(detachTestKey{}),
		"detaching must preserve context values (auth, project, trace span)")
}

// TestDetachedRunContextNilParent returns a usable context for a nil parent.
func TestDetachedRunContextNilParent(t *testing.T) {
	var nilParent context.Context
	runCtx := DetachedRunContext(nilParent)
	require.NotNil(t, runCtx)
	require.NoError(t, runCtx.Err())
}

// TestRunCancelRegistryCancelStopsRun proves an explicit cancel routed by run
// id reaches the registered run's context — the mechanism that keeps the stop
// button working once a run is detached from the request context (issue #1149).
func TestRunCancelRegistryCancelStopsRun(t *testing.T) {
	var reg runCancelRegistry
	runCtx, cancel := context.WithCancel(context.Background())
	handle := reg.register("run-1", cancel)

	require.NoError(t, runCtx.Err(), "run must be live before the cancel")
	require.True(t, reg.cancel("run-1", userCancelReason), "cancel of a registered run must report true")
	require.ErrorIs(t, runCtx.Err(), context.Canceled, "explicit cancel must cancel the run context")
	require.Equal(t, userCancelReason, handle.Reason(), "the user-cancel reason must be recorded")

	// A second cancel is idempotent and does not overwrite the first reason.
	require.True(t, reg.cancel("run-1", "other"))
	require.Equal(t, userCancelReason, handle.Reason(), "the first recorded reason wins")

	reg.unregister("run-1", handle)
	require.False(t, reg.cancel("run-1", userCancelReason), "an unregistered run must not be cancellable")
}

// TestRunCancelRegistryUnregisterDoesNotEvictReplacement guards the key-reuse
// race: a finished run's late unregister must not remove a newer handle that
// reused the same id.
func TestRunCancelRegistryUnregisterDoesNotEvictReplacement(t *testing.T) {
	var reg runCancelRegistry

	_, cancelOld := context.WithCancel(context.Background())
	old := reg.register("run-1", cancelOld)

	runCtx, cancelNew := context.WithCancel(context.Background())
	_ = reg.register("run-1", cancelNew)

	reg.unregister("run-1", old)

	require.True(t, reg.cancel("run-1", userCancelReason),
		"the replacement handle must still be registered after the stale unregister")
	require.ErrorIs(t, runCtx.Err(), context.Canceled)
}
