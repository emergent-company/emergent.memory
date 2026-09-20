// Package api_test — agent_graph_query_live_test.go
// Tests natural language graph queries against live test data.
// Requires TEST_SERVER_URL, TEST_API_KEY, TEST_PROJECT_ID, TEST_AGENT_DEF_ID.
package api_test

import (
	"testing"
)

func TestAgentGraphQueryLive_VectorSearchOnRelationships(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("Live test requires TEST_SERVER_URL, TEST_API_KEY, TEST_PROJECT_ID, TEST_AGENT_DEF_ID")
}

func TestAgentGraphQueryLive_MultiStepGraphTraversal(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("Live test requires TEST_SERVER_URL, TEST_API_KEY, TEST_PROJECT_ID, TEST_AGENT_DEF_ID")
}
