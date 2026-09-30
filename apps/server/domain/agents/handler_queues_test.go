package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeQueueName(t *testing.T) {
	assert.Equal(t, DefaultQueueName, normalizeQueueName(""))
	assert.Equal(t, DefaultQueueName, normalizeQueueName("   "))
	assert.Equal(t, "security-review", normalizeQueueName("  security-review "))
}

func TestToAgentQueueDTO(t *testing.T) {
	q := &AgentQueue{
		ProjectID:   "p1",
		Name:        "security-review",
		DisplayName: "Security Review",
		Description: "d",
		Concurrency: 2,
		Priority:    50,
		Enabled:     true,
		Pending:     3,
		Processing:  1,
	}
	dto := toAgentQueueDTO(q)
	assert.Equal(t, "security-review", dto.Name)
	assert.Equal(t, 2, dto.Concurrency)
	assert.Equal(t, 3, dto.Pending)
	assert.Equal(t, 1, dto.Processing)
	assert.True(t, dto.Enabled)
}
