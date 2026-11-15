package sources

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// SourceTriggerInvocationResolver resolves an auto-start board item's invocation
// from the server-owned source trigger row. Graph properties only carry the
// trigger id; trusting an invocation blob from them would let a project writer
// attach a write credential to an arbitrary issue item.
type SourceTriggerInvocationResolver struct {
	repo *Repository
}

func NewSourceTriggerInvocationResolver(repo *Repository) *SourceTriggerInvocationResolver {
	return &SourceTriggerInvocationResolver{repo: repo}
}

func (r *SourceTriggerInvocationResolver) ResolveSourceTriggerInvocation(ctx context.Context, projectID, sourceID, triggerID string) (*agents.AgentInvocation, error) {
	projectUUID, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return nil, fmt.Errorf("sources: invalid project id")
	}
	triggerUUID, err := uuid.Parse(strings.TrimSpace(triggerID))
	if err != nil {
		return nil, fmt.Errorf("sources: invalid source trigger id")
	}
	trigger, err := r.repo.FindSourceTriggerByID(ctx, projectUUID, triggerUUID)
	if err != nil {
		return nil, err
	}
	if sourceID != "" && trigger.SourceID.String() != sourceID {
		return nil, fmt.Errorf("sources: trigger %s does not belong to source %s", triggerID, sourceID)
	}
	return agents.NormalizeInvocation(trigger.Invocation), nil
}
