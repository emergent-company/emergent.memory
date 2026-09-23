// Package acp_test — end-to-end tests for `memory acp`, the ACP v1 stdio agent
// that bridges ACP clients to a Memory agent over A2A.
//
// Two tests cover the surface:
//   - TestACP_JSONRPC_Protocol exercises the framing level (initialize,
//     session/new, malformed/invalid/unknown-method error responses, clean
//     shutdown) with dummy credentials — no network, so it always runs.
//   - TestACP_EndToEnd exercises the live path (prompt → streamed output →
//     HITL resume → clean shutdown) against a real Memory server, gated on
//     credentials so it SKIPS rather than silently passing when absent.
package acp_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	probeAgentEnv     = "MEMORY_AGENT"
	hitlAgentEnv      = "MEMORY_ACP_HITL_AGENT"
	defaultProbeAgent = "acp-probe-external"
	defaultHitlAgent  = "acp-hitl-probe"
)

// TestACP_JSONRPC_Protocol drives the ACP wire protocol with dummy credentials.
// The agent never reaches the network for these methods, so this test always
// runs and pins the framing contract independent of any live server.
func TestACP_JSONRPC_Protocol(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify the ACP JSON-RPC framing contract of `memory acp`",
		"Spawn the stdio agent with dummy credentials (no network)",
		"Assert initialize handshake and session/new",
		"Assert -32700 for malformed JSON, -32600 for invalid request, -32601 for unknown method",
		"Assert clean exit on stdin close",
	)

	bin, err := resolveMemoryBinary()
	if err != nil {
		rl.Skipf("%v", err)
	}

	rl.Section("Spawn agent (dummy credentials)")
	env := map[string]string{
		"MEMORY_SERVER_URL":    "http://127.0.0.1:1",
		"MEMORY_PROJECT_TOKEN": "emt-dummy-protocol-token",
		"MEMORY_PROJECT_ID":    "dummy-project",
		"MEMORY_AGENT":         "dummy-agent",
	}
	p := startACP(t, bin, "dummy-agent", env)
	rl.Printf("spawned %s acp --agent dummy-agent", bin)

	rl.Section("Initialize handshake")
	init := p.request(t, 1, "initialize", nil, 15*time.Second)
	resp, ok := responseFor(init, 1)
	if !ok {
		rl.Failf("no initialize response; got %d messages", len(init))
	}
	var ir struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
		AgentCapabilities struct {
			LoadSession         bool `json:"loadSession"`
			SessionCapabilities struct {
				Delete json.RawMessage `json:"delete"`
				Close  json.RawMessage `json:"close"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(resp.Result, &ir); err != nil {
		rl.Failf("initialize result malformed: %v — %s", err, string(resp.Result))
	}
	if ir.ProtocolVersion != 1 {
		rl.Failf("protocolVersion = %d, want 1", ir.ProtocolVersion)
	}
	if ir.AgentInfo.Name != "memory" {
		rl.Failf("agentInfo.name = %q, want \"memory\"", ir.AgentInfo.Name)
	}
	if ir.AgentCapabilities.LoadSession {
		rl.Failf("loadSession = true, want false")
	}
	if len(ir.AgentCapabilities.SessionCapabilities.Delete) == 0 {
		rl.Failf("sessionCapabilities.delete not advertised")
	}
	if len(ir.AgentCapabilities.SessionCapabilities.Close) == 0 {
		rl.Failf("sessionCapabilities.close not advertised")
	}
	rl.Printf("protocolVersion=1, name=memory, loadSession=false, delete+close advertised")

	rl.Section("Session new")
	snew := p.request(t, 2, "session/new", nil, 15*time.Second)
	sresp, ok := responseFor(snew, 2)
	if !ok {
		rl.Failf("no session/new response")
	}
	var snr struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sresp.Result, &snr); err != nil {
		rl.Failf("session/new result malformed: %v — %s", err, string(sresp.Result))
	}
	if !strings.HasPrefix(snr.SessionID, "sess-") {
		rl.Failf("sessionId = %q, want sess- prefix", snr.SessionID)
	}
	rl.Printf("allocated session %s", snr.SessionID)

	rl.Section("Malformed line → -32700")
	p.sendRaw(t, `this is not json`)
	mal := p.readUntil(t, 15*time.Second, func(m rpcMsg) bool {
		return m.Error != nil && m.Error.Code == -32700
	})
	merr := lastErr(mal)
	if merr == nil || merr.Code != -32700 {
		rl.Failf("expected -32700 parse error, got %+v", merr)
	}
	if id := mal[len(mal)-1].ID; len(id) > 0 && string(id) != "null" {
		rl.Failf("parse error id should be null, got %s", string(id))
	}
	rl.Printf("parse error -32700 returned with id null")

	rl.Section("Invalid request → -32600")
	p.sendRaw(t, `[]`)
	inv := p.readUntil(t, 15*time.Second, func(m rpcMsg) bool {
		return m.Error != nil && m.Error.Code == -32600
	})
	ierr := lastErr(inv)
	if ierr == nil || ierr.Code != -32600 {
		rl.Failf("expected -32600 invalid request, got %+v", ierr)
	}
	rl.Printf("invalid request -32600 returned")

	rl.Section("Unknown method → -32601")
	unk := p.request(t, 5, "session/list", nil, 15*time.Second)
	uresp, ok := responseFor(unk, 5)
	if !ok || uresp.Error == nil || uresp.Error.Code != -32601 {
		rl.Failf("expected -32601 for session/list, got %+v", uresp)
	}
	rl.Printf("unknown method -32601 returned")

	rl.Section("Clean shutdown")
	p.shutdown(t)
	rl.Printf("agent exited cleanly on stdin close")
}

// TestACP_EndToEnd drives the live protocol surface against a real Memory
// server: initialize → session/new → prompt (streamed) → HITL pause+resume →
// clean shutdown. It SKIPS when credentials or an external agent slug are
// absent, and verifies the agent is discoverable before prompting.
func TestACP_EndToEnd(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Drive `memory acp` end-to-end against a live Memory server",
		"Resolve credentials and the external agent slug",
		"Verify the slug is discoverable via memory a2a discover",
		"Prompt and assert streamed output ends with end_turn",
		"Exercise the HITL pause (TASK_STATE_INPUT_REQUIRED) and resume it",
		"Assert clean shutdown",
	)

	if !acpCredsReady() {
		rl.Skipf("credentials absent — set MEMORY_ACP_ENV_FILE or MEMORY_SERVER_URL/MEMORY_PROJECT_TOKEN/MEMORY_AGENT")
	}

	bin, err := resolveMemoryBinary()
	if err != nil {
		rl.Skipf("%v", err)
	}

	env := resolveACPEnv()
	probeAgent := env[probeAgentEnv]
	if probeAgent == "" {
		probeAgent = defaultProbeAgent
	}
	hitlAgent := env[hitlAgentEnv]
	if hitlAgent == "" {
		hitlAgent = defaultHitlAgent
	}

	rl.Section("Resolve credentials and agent")
	rl.Printf("server=%s agent=%s hitlAgent=%s", env["MEMORY_SERVER_URL"], probeAgent, hitlAgent)

	rl.Section("Verify agent discoverable")
	discOut, err := runMemoryDiscover(bin, env)
	if err != nil {
		rl.Failf("memory a2a discover failed: %v", err)
	}
	rl.CLI("memory a2a discover --json", discOut)
	slugs := discoverSlugs(discOut)
	if !contains(slugs, probeAgent) {
		rl.Failf("agent %q not in discovered skills: %v", probeAgent, slugs)
	}
	if !contains(slugs, hitlAgent) {
		rl.Failf("HITL agent %q not in discovered skills: %v", hitlAgent, slugs)
	}
	rl.Printf("discovered %d skills including %s and %s", len(slugs), probeAgent, hitlAgent)

	rl.Section("Initialize + session/new (probe agent)")
	probe := startACP(t, bin, probeAgent, env)
	init := probe.request(t, 1, "initialize", nil, 15*time.Second)
	if r, ok := responseFor(init, 1); !ok || r.Error != nil {
		rl.Failf("initialize failed: %+v", r)
	}
	snew := probe.request(t, 2, "session/new", nil, 15*time.Second)
	sresp, ok := responseFor(snew, 2)
	if !ok || sresp.Error != nil {
		rl.Failf("session/new failed: %+v", sresp)
	}
	var snr struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(sresp.Result, &snr)
	rl.Printf("probe session %s", snr.SessionID)

	rl.Section("Prompt streams output")
	pr := probe.request(t, 3, "session/prompt",
		map[string]any{
			"sessionId": snr.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "Reply with exactly: PONG"}},
		}, 120*time.Second)
	presp, ok := responseFor(pr, 3)
	if !ok {
		rl.Failf("no prompt response")
	}
	if presp.Error != nil {
		rl.Failf("prompt returned error: %+v", presp.Error)
	}
	if sr := parseStopReason(presp); sr != "end_turn" {
		rl.Failf("stopReason = %q, want end_turn", sr)
	}
	text := textChunks(pr)
	if !strings.Contains(text, "PONG") {
		rl.Failf("streamed text %q does not contain PONG", text)
	}
	rl.Printf("streamed %d chunk(s): %q", countChunks(pr), truncateText(text, 80))

	rl.Section("Clean shutdown (probe agent)")
	probe.shutdown(t)

	rl.Section("HITL pause (hitl agent)")
	hitl := startACP(t, bin, hitlAgent, env)
	_ = hitl.request(t, 1, "initialize", nil, 15*time.Second)
	hnew := hitl.request(t, 2, "session/new", nil, 15*time.Second)
	hresp, ok := responseFor(hnew, 2)
	if !ok || hresp.Error != nil {
		rl.Failf("hitl session/new failed: %+v", hresp)
	}
	var hsn struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(hresp.Result, &hsn)

	p1 := hitl.request(t, 3, "session/prompt",
		map[string]any{
			"sessionId": hsn.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "hi"}},
		}, 120*time.Second)
	p1resp, ok := responseFor(p1, 3)
	if !ok {
		rl.Failf("no HITL prompt response")
	}
	if p1resp.Error != nil {
		rl.Failf("HITL prompt error: %+v", p1resp.Error)
	}
	if sr := parseStopReason(p1resp); sr != "end_turn" {
		rl.Failf("HITL prompt stopReason = %q, want end_turn", sr)
	}
	qtext := textChunks(p1)
	if !strings.Contains(strings.ToLower(qtext), "favorite color") {
		rl.Failf("HITL question not streamed; text=%q", qtext)
	}
	rl.Printf("agent paused, streamed question: %q", truncateText(qtext, 80))

	rl.Section("HITL resume")
	p2 := hitl.request(t, 4, "session/prompt",
		map[string]any{
			"sessionId": hsn.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "blue"}},
		}, 120*time.Second)
	p2resp, ok := responseFor(p2, 4)
	if !ok {
		rl.Failf("no HITL resume response")
	}
	if p2resp.Error != nil {
		rl.Failf("HITL resume error: %+v", p2resp.Error)
	}
	if sr := parseStopReason(p2resp); sr != "end_turn" {
		rl.Failf("HITL resume stopReason = %q, want end_turn", sr)
	}
	rtext := textChunks(p2)
	if !strings.Contains(strings.ToLower(rtext), "color=blue") {
		rl.Failf("resume output %q does not echo the answer COLOR=blue", rtext)
	}
	rl.Printf("resumed; streamed: %q", truncateText(rtext, 80))

	rl.Section("Clean shutdown (hitl agent)")
	hitl.shutdown(t)
	rl.Printf("both sessions shut down cleanly")
}

// runMemoryDiscover runs `memory a2a discover --json` with the given env.
func runMemoryDiscover(bin string, env map[string]string) (string, error) {
	if env["HOME"] == "" {
		home, err := os.MkdirTemp("", "acp-discover-home")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(home)
		env["HOME"] = home
	}
	cmd := exec.Command(bin, "a2a", "discover", "--json")
	cmd.Env = buildEnv(env)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// discoverSlugs extracts the skill ids from an a2a discover --json payload.
func discoverSlugs(out string) []string {
	var card struct {
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(out), &card); err != nil {
		return nil
	}
	ids := make([]string, 0, len(card.Skills))
	for _, s := range card.Skills {
		ids = append(ids, s.ID)
	}
	return ids
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func lastErr(msgs []rpcMsg) *rpcError {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Error != nil {
			return msgs[i].Error
		}
	}
	return nil
}

func countChunks(msgs []rpcMsg) int {
	n := 0
	for _, m := range msgs {
		if m.Method == "session/update" {
			n++
		}
	}
	return n
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
