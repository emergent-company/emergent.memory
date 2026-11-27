// guard: the gateway's agent-runs `?ids=` batch cap matches the server's
// AgentRunIDsFilterMax declaration, so neither side can silently truncate the
// other's reads.
package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestAgentRunIDsFilterMaxMatchesServerRatchet pins the two ends of the same
// request cap. The web-ui gateway is a separate Go module and cannot import the
// server's agents package, so the value is mirrored in memory_runs.go; without
// this assertion a change on either side silently changes the meaning of a
// provenance read — a lower server cap truncates the gateway's batch, a higher
// one leaves the gateway batching for no reason.
//
// The server declaration is read from source rather than restated as a literal:
// a rename or a removed constant fails here instead of comparing two numbers
// that only happen to agree today.
func TestAgentRunIDsFilterMaxMatchesServerRatchet(t *testing.T) {
	const serverHandler = "../../server/domain/agents/handler.go"
	src, err := os.ReadFile(serverHandler)
	if err != nil {
		t.Fatalf("read %s: %v", serverHandler, err)
	}
	decl := regexp.MustCompile(`(?m)^const AgentRunIDsFilterMax = (\d+)$`).FindSubmatch(src)
	if decl == nil {
		t.Fatalf("%s: `const AgentRunIDsFilterMax = <n>` not found; the gateway's agentRunIDsFilterMax has nothing to pin", serverHandler)
	}
	serverCap, err := strconv.Atoi(string(decl[1]))
	if err != nil {
		t.Fatalf("%s: parse AgentRunIDsFilterMax: %v", serverHandler, err)
	}
	if agentRunIDsFilterMax != serverCap {
		t.Fatalf("gateway agentRunIDsFilterMax = %d but server AgentRunIDsFilterMax = %d; change both together so a batch read is never silently truncated",
			agentRunIDsFilterMax, serverCap)
	}
}
