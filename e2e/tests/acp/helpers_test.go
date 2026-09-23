// Package acp_test — helpers for driving `memory acp` over stdio.
package acp_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

type runLog = framework.RunLog

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	return framework.NewRunLog(t)
}

// rpcMsg is a generic JSON-RPC 2.0 message on either side of the wire. Not all
// fields are populated on every message; callers use the helpers below.
type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// isNotification reports whether m is a server->client notification (no id).
func (m *rpcMsg) isNotification() bool { return len(m.ID) == 0 }

// isResponse reports whether m is a response to the given request id.
func (m *rpcMsg) isResponse(id int) bool {
	return string(m.ID) == fmt.Sprintf("%d", id)
}

// acpProc is a running `memory acp` process driven over stdio.
type acpProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *safeBuffer
	ch     chan rpcMsg
	done   chan struct{} // closed once the process exits
	mu     sync.Mutex
	exitErr error
	once   sync.Once
}

// exitedErr returns the process exit error, valid after done is closed.
func (p *acpProc) exitedErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// safeBuffer is a goroutine-safe byte buffer for stderr capture.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// resolveMemoryBinary locates the memory CLI. It prefers PATH (the Docker
// install puts it there) and falls back to the operator default.
func resolveMemoryBinary() (string, error) {
	if p, err := exec.LookPath("memory"); err == nil {
		return p, nil
	}
	const fallback = "/root/.memory/bin/memory"
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return "", fmt.Errorf("memory binary not found on PATH or at %s", fallback)
}

// acpEnvVarNames are the credentials/settings `memory acp` reads from the
// environment (or, via MEMORY_ACP_ENV_FILE, a sourced KEY=VALUE file).
var acpEnvVarNames = []string{
	"MEMORY_SERVER_URL",
	"MEMORY_PROJECT_TOKEN",
	"MEMORY_PROJECT_ID",
	"MEMORY_AGENT",
	"MEMORY_ACP_HITL_AGENT",
}

// loadACPEnvFile parses a MEMORY_ACP_ENV_FILE KEY=VALUE file. Blank lines and
// lines starting with '#' are ignored; values are not shell-expanded (the
// files in practice hold bare URLs/tokens/ids).
func loadACPEnvFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m, nil
}

// resolveACPEnv returns the env vars `memory acp` needs, in priority order:
// explicit process env var > MEMORY_ACP_ENV_FILE value. The env file is the
// same mechanism the Paseo wrapper uses (MEMORY_ACP_ENV_FILE), so a test run
// can reuse an operator's dedicated credentials file without exporting tokens.
func resolveACPEnv() map[string]string {
	out := map[string]string{}
	if f := os.Getenv("MEMORY_ACP_ENV_FILE"); f != "" {
		if m, err := loadACPEnvFile(f); err == nil {
			for _, k := range acpEnvVarNames {
				if v, ok := m[k]; ok && v != "" {
					out[k] = v
				}
			}
		}
	}
	for _, k := range acpEnvVarNames {
		if v := os.Getenv(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// acpCredsReady reports whether live credentials and an agent slug are present,
// either directly in the environment or via MEMORY_ACP_ENV_FILE.
func acpCredsReady() bool {
	e := resolveACPEnv()
	return e["MEMORY_SERVER_URL"] != "" && e["MEMORY_PROJECT_TOKEN"] != "" && e["MEMORY_AGENT"] != ""
}

// startACP spawns `memory acp --agent <agent>` with the given env map merged
// over a sanitised base environment. HOME is pointed at a fresh temp dir so the
// process never reads the operator's ~/.memory/config.yaml.
func startACP(t *testing.T, bin, agent string, env map[string]string) *acpProc {
	t.Helper()

	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	cmd := exec.Command(bin, "acp", "--agent", agent)
	cmd.Env = buildEnv(env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr := &safeBuffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start memory acp: %v", err)
	}

	p := &acpProc{
		cmd:    cmd,
		stdin:  stdin,
		stderr: stderr,
		ch:     make(chan rpcMsg, 256),
		done:   make(chan struct{}),
	}

	go p.readLoop(stdout)

	t.Cleanup(func() { p.cleanup() })
	return p
}

// buildEnv builds the child process environment: a minimal base plus the ACP
// vars. It deliberately drops Paseo/agent session vars and any inherited
// MEMORY_* / HOME so the child is isolated from whatever harness spawned it.
func buildEnv(extra map[string]string) []string {
	strip := func(k string) bool {
		if strings.HasPrefix(k, "PASEO_") || k == "AGENT" || k == "HOME" {
			return true
		}
		if strings.HasPrefix(k, "MEMORY_") || strings.HasPrefix(k, "MEMORY_ACP_") {
			return true
		}
		return false
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strip(k) {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// readLoop forwards stdout lines to the message channel, then records the
// process exit status and closes done.
func (p *acpProc) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m rpcMsg
		if err := json.Unmarshal(line, &m); err != nil {
			// A non-JSON line on stdout would be a protocol violation; still
			// surface it so the caller can fail loudly rather than hang.
			m = rpcMsg{JSONRPC: "2.0", Result: json.RawMessage(line)}
		}
		p.ch <- m
	}
	err := p.cmd.Wait()
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
	close(p.done)
}

// request sends one JSON-RPC request and returns every message received until
// the response for `id` arrives (inclusive), or timeout elapses.
func (p *acpProc) request(t *testing.T, id int, method string, params any, timeout time.Duration) []rpcMsg {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := p.stdin.Write(append(raw, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var out []rpcMsg
	deadline := time.After(timeout)
	for {
		select {
		case m := <-p.ch:
			out = append(out, m)
			if m.isResponse(id) {
				return out
			}
		case <-p.done:
			t.Fatalf("memory acp exited before answering id %d (exit=%v); stderr: %s", id, p.exitedErr(), p.stderr.String())
		case <-deadline:
			t.Fatalf("timed out waiting for response to id %d; stderr: %s", id, p.stderr.String())
		}
	}
}

// sendRaw writes a raw line (for malformed/edge-case inputs).
func (p *acpProc) sendRaw(t *testing.T, line string) {
	t.Helper()
	if _, err := p.stdin.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("write raw line: %v", err)
	}
}

// readUntil reads messages until a message satisfying pred arrives, or timeout.
func (p *acpProc) readUntil(t *testing.T, timeout time.Duration, pred func(rpcMsg) bool) []rpcMsg {
	t.Helper()
	var out []rpcMsg
	deadline := time.After(timeout)
	for {
		select {
		case m := <-p.ch:
			out = append(out, m)
			if pred(m) {
				return out
			}
		case <-deadline:
			t.Fatalf("timed out reading; stderr: %s", p.stderr.String())
		}
	}
}

// shutdown closes stdin and waits for a clean (exit 0) termination.
func (p *acpProc) shutdown(t *testing.T) {
	t.Helper()
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(15 * time.Second):
		t.Fatalf("memory acp did not exit after stdin close; stderr: %s", p.stderr.String())
	}
	if err := p.exitedErr(); err != nil {
		t.Fatalf("expected clean exit, got: %v; stderr: %s", err, p.stderr.String())
	}
}

// cleanup tears down the process if still running (used by t.Cleanup).
func (p *acpProc) cleanup() {
	p.once.Do(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
}

// textChunks concatenates the text carried by session/update agent_message_chunk
// notifications in a batch of messages, in order.
func textChunks(msgs []rpcMsg) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Method != "session/update" {
			continue
		}
		var params struct {
			Update struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if err := json.Unmarshal(m.Params, &params); err != nil {
			continue
		}
		sb.WriteString(params.Update.Content.Text)
	}
	return sb.String()
}

// responseFor returns the response to id within a batch of messages.
func responseFor(msgs []rpcMsg, id int) (rpcMsg, bool) {
	for _, m := range msgs {
		if m.isResponse(id) {
			return m, true
		}
	}
	return rpcMsg{}, false
}

// parseStopReason extracts the stopReason from a prompt response result.
func parseStopReason(m rpcMsg) string {
	var r struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(m.Result, &r)
	return r.StopReason
}
