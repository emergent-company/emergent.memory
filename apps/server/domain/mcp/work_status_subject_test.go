package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestReactionSubjectMatches(t *testing.T) {
	subject := uuid.New()
	other := uuid.New()

	ctx := auth.WithAgentWorkStatusSubject(context.Background(), subject.String())
	require.True(t, reactionSubjectMatches(ctx, subject), "matching subject must be allowed")
	require.False(t, reactionSubjectMatches(ctx, other), "a different object must not be allowed")

	require.False(t, reactionSubjectMatches(context.Background(), subject), "no subject must not match")
	require.False(t, reactionSubjectMatches(auth.WithAgentWorkStatusSubject(context.Background(), ""), subject), "empty subject must not match")
}
