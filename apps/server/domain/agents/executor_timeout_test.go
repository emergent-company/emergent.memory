package agents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// resolveRunTimeout pins the timeout precedence for a run: explicit per-request
// value > agent definition default_timeout (seconds) > hard-coded default.
// The hard-coded default (10 minutes) must be preserved when neither is set —
// wiring default_timeout in must not silently change the timeout of any run
// whose agent definition leaves it unset.
func TestResolveRunTimeout(t *testing.T) {
	t.Run("default when nothing is set", func(t *testing.T) {
		assert.Equal(t, defaultRunTimeout, resolveRunTimeout(ExecuteRequest{}))
	})

	t.Run("nil agent definition is safe", func(t *testing.T) {
		assert.Equal(t, defaultRunTimeout, resolveRunTimeout(ExecuteRequest{AgentDefinition: nil}))
	})

	t.Run("explicit request timeout wins", func(t *testing.T) {
		d := 90 * time.Second
		assert.Equal(t, d, resolveRunTimeout(ExecuteRequest{Timeout: &d}))
	})

	t.Run("agent default_timeout applies when request unset", func(t *testing.T) {
		secs := 1800
		assert.Equal(t, 30*time.Minute, resolveRunTimeout(ExecuteRequest{
			AgentDefinition: &AgentDefinition{DefaultTimeout: &secs},
		}))
	})

	t.Run("request timeout wins over agent default_timeout", func(t *testing.T) {
		secs := 1800
		d := 30 * time.Second
		assert.Equal(t, d, resolveRunTimeout(ExecuteRequest{
			Timeout:         &d,
			AgentDefinition: &AgentDefinition{DefaultTimeout: &secs},
		}))
	})

	t.Run("zero or negative default_timeout is ignored", func(t *testing.T) {
		zero := 0
		assert.Equal(t, defaultRunTimeout, resolveRunTimeout(ExecuteRequest{
			AgentDefinition: &AgentDefinition{DefaultTimeout: &zero},
		}))

		neg := -5
		assert.Equal(t, defaultRunTimeout, resolveRunTimeout(ExecuteRequest{
			AgentDefinition: &AgentDefinition{DefaultTimeout: &neg},
		}))
	})
}
