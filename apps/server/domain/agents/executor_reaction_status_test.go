package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReactionWorkStatusSubject(t *testing.T) {
	subject := "canonical-1"
	empty := ""
	plain := &AgentDefinition{DispatchMode: DispatchModeSync}
	queued := &AgentDefinition{DispatchMode: DispatchModeQueued}
	work := &AgentDefinition{
		DispatchMode: DispatchModeSync,
		WorkConfig:   AgentWorkConfig{RequiresReview: true},
	}
	reaction := &Agent{TriggerType: TriggerTypeReaction}
	scheduled := &Agent{TriggerType: TriggerTypeSchedule}

	tests := []struct {
		name    string
		agent   *Agent
		def     *AgentDefinition
		subject *string
		want    string
	}{
		{"plain reaction with subject", reaction, plain, &subject, subject},
		{"plain reaction empty subject", reaction, plain, &empty, ""},
		{"plain reaction nil subject", reaction, plain, nil, ""},
		{"queued reaction", reaction, queued, &subject, ""},
		{"reaction with work config", reaction, work, &subject, ""},
		{"non-reaction", scheduled, plain, &subject, ""},
		{"nil agent", nil, plain, &subject, ""},
		{"nil definition", reaction, nil, &subject, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, reactionWorkStatusSubject(tt.agent, tt.def, tt.subject))
		})
	}
}
