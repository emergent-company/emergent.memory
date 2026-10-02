package notifications

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
)

// TestResolveRequiresAction_TaxonomyIsAuthoritative is the #1364 regression:
// whether a notification appears under Action required is a property of its
// taxonomy event type, not of whatever the producer happened to pass. A
// producer that omits RequiresAction for an action-required type (agent.question)
// must still yield an action item, so the Action required tab always agrees with
// the taxonomy.
func TestResolveRequiresAction_TaxonomyIsAuthoritative(t *testing.T) {
	agentQuestion, ok := taxonomy.Lookup("agent.question")
	require.True(t, ok, "agent.question must be registered")
	require.True(t, agentQuestion.RequiresAction)

	// Producer forgot the flag: taxonomy still classifies it as action-required.
	require.True(t, resolveRequiresAction(false, agentQuestion))

	// FYI types stay non-action even if an over-eager producer sets the flag.
	commentReply, ok := taxonomy.Lookup("comment.reply")
	require.True(t, ok)
	require.False(t, resolveRequiresAction(false, commentReply))
	require.True(t, resolveRequiresAction(true, commentReply))

	// The producer's flag is never lost for a type the taxonomy marks actionable.
	invite, ok := taxonomy.Lookup("invite.received")
	require.True(t, ok)
	require.True(t, resolveRequiresAction(false, invite))
}
