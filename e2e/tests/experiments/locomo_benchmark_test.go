//go:build locomo_benchmark

// Package experiments_test — locomo_benchmark_test.go
//
// Go-native LoCoMo smoke benchmark. Ingests LoCoMo conv-0, sessions 1–3 via the
// MCP `remember` tool, then queries 5 single-hop questions (category 1) and
// measures Token F1 against gold answers.
//
// Build tag: locomo_benchmark (not run in standard CI; run manually or in
// scheduled quality jobs). The full 10-conversation Python runner is at
// tools/benchmarks/locomo/run.sh.
//
// Usage:
//
//	cd tests/experiments
//	go test -v -tags locomo_benchmark -run TestLoCoMo ./...
//
// Results written to /tmp/locomo_results/YYYY-MM-DD_smoke.json.
//
// Assumptions verified: L1 (single-hop recall), L5 (retrieval not ingest failure).
//
// Model note: ensure the server is configured with a model that is not
// Google AI to avoid quota errors. Set MEMORY_LLM_MODEL=deepseek-v4-flash
// in the server environment before running.
package experiments_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Benchmark gate
// ─────────────────────────────────────────────────────────────────────────────

// minLoCoMoCat1F1 is the minimum Token F1 for single-hop (category 1) questions.
// Restores smoke-v1 baseline (0.352 overall, 0.383 cat-1).
const minLoCoMoCat1F1 = 0.30

// ─────────────────────────────────────────────────────────────────────────────
// LoCoMo conv-0 sessions 1–3, category-1 questions (single-hop)
// Source: locomo10.json[0].qa filtered by category_num==1 and evidence in D1–D3
// ─────────────────────────────────────────────────────────────────────────────

type locomoQA struct {
	Question string
	Gold     string
	Evidence string
}

// locomoSmokeSessions is the dialogue text for conv-0 sessions 1–3.
// Reused from extraction_eval_test.go — defined here for self-containment.
var locomoSmokeSessions = map[int]string{
	1: `Session date: 8 May, 2023
[D1:1] Caroline: Hey Mel! Good to see you! How have you been?
[D1:2] Melanie: Hey Caroline! Good to see you! I'm swamped with the kids & work. What's up with you? Anything new?
[D1:3] Caroline: I went to a LGBTQ support group yesterday and it was so powerful.
[D1:4] Melanie: Wow, that's cool, Caroline! What happened that was so awesome? Did you hear any inspiring stories?
[D1:5] Caroline: The transgender stories were so inspiring! I was so happy and thankful for all the support.
[D1:6] Melanie: Wow, love that painting! So cool you found such a helpful group. What's it done for you?
[D1:7] Caroline: The support group has made me feel accepted and given me courage to embrace myself.
[D1:8] Melanie: That's really cool. You've got guts. What now?
[D1:9] Caroline: Gonna continue my edu and check out career options, which is pretty exciting!
[D1:10] Melanie: Wow, Caroline! What kinda jobs are you thinkin' of? Anything that stands out?
[D1:11] Caroline: I'm keen on counseling or working in mental health - I'd love to support those with similar issues.
[D1:12] Melanie: You'd be a great counselor! Your empathy and understanding will really help the people you work with.
[D1:13] Caroline: Thanks, Melanie! That's really sweet. Is this your own painting?
[D1:14] Melanie: Yeah, I painted that lake sunrise last year! It's special to me.
[D1:15] Caroline: Wow, Melanie! The colors really blend nicely. Painting looks like a great outlet for expressing yourself.
[D1:16] Melanie: Thanks, Caroline! Painting's a fun way to express my feelings and get creative. It's a great way to relax after a long day.
[D1:17] Caroline: Totally agree, Mel. Relaxing and expressing ourselves is key. Well, I'm off to go do some research.
[D1:18] Melanie: Yep, Caroline. Taking care of ourselves is vital. I'm off to go swimming with the kids. Talk to you soon!`,

	2: `Session date: May 2023
[D2:1] Caroline: Hey Mel! How are the kids?
[D2:2] Melanie: They're great! Growing so fast. What have you been up to?
[D2:3] Caroline: I've been doing a lot of research lately.
[D2:4] Melanie: Oh yeah? What kind of research?
[D2:5] Melanie: By the way, I started playing clarinet again. I also play violin.
[D2:6] Caroline: That's amazing! I didn't know you played instruments.
[D2:7] Melanie: Yeah, music is a great outlet. So what were you researching?
[D2:8] Caroline: I've been looking into adoption agencies. It's been on my mind a lot.
[D2:9] Melanie: Wow, that's a big step! How are you feeling about it?
[D2:10] Caroline: Excited but nervous. There's a lot to consider.
[D2:11] Melanie: That makes sense. Are you considering domestic or international adoption?
[D2:12] Caroline: I'm looking at both options right now.
[D2:13] Melanie: Well I'm happy for you! It's a wonderful thing to do.
[D2:14] Caroline: Thanks! It's just me, I'm single, so I want to make sure I'm ready.
[D2:15] Melanie: You'll be a wonderful parent. You have so much love to give.`,

	3: `Session date: May 2023
[D3:1] Caroline: I went to another LGBTQ event last weekend!
[D3:2] Melanie: Oh nice! What kind of event was it?
[D3:3] Caroline: I started a mentoring program to help kids in need. It's very rewarding.
[D3:4] Melanie: That's wonderful! You're making such a positive difference.
[D3:5] Caroline: I really feel like I'm giving back to the community.
[D3:6] Melanie: The kids are lucky to have someone like you.
[D3:7] Caroline: It means a lot to hear that, Mel.
[D3:8] Melanie: So how are things going otherwise?
[D3:9] Caroline: Pretty good! I'm still figuring out my path.
[D3:10] Melanie: You're doing amazing. Don't forget that.
[D3:11] Caroline: Thank you. My family and friends have been so supportive.
[D3:12] Melanie: That's so important to have that support system.
[D3:13] Caroline: Yeah, I moved here from Sweden about 4 years ago and it took time to find my people.
[D3:14] Melanie: We've been doing a lot as a family too - pottery, painting, camping, museum trips, swimming, hiking.
[D3:15] Caroline: That sounds amazing! Your family is so active.`,
}

// locomoSmokeQuestions are 5 category-1 (single-hop) questions from conv-0
// with evidence in sessions 1–3 only.
var locomoSmokeQuestions = []locomoQA{
	{
		Question: "What is Caroline's identity?",
		Gold:     "Transgender woman",
		Evidence: "D1:5",
	},
	{
		Question: "What career path has Caroline decided to pursue?",
		Gold:     "counseling or mental health for Transgender people",
		Evidence: "D1:11",
	},
	{
		Question: "What did Caroline research?",
		Gold:     "Adoption agencies",
		Evidence: "D2:8",
	},
	{
		Question: "What is Caroline's relationship status?",
		Gold:     "Single",
		Evidence: "D2:14",
	},
	{
		Question: "Where did Caroline move from 4 years ago?",
		Gold:     "Sweden",
		Evidence: "D3:13",
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers (self-contained — no dependency on other test files)
// ─────────────────────────────────────────────────────────────────────────────

func locomoDoJSON(t *testing.T, method, url, token, projectID string, body []byte) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("locomoDoJSON: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", token)
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("locomoDoJSON: %s %s: %v", method, url, err)
	}
	return resp
}

func locomoReadBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}

func locomoMustStatus(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	body := locomoReadBody(resp)
	if resp.StatusCode != want {
		t.Fatalf("expected %d, got %d\nbody: %s", want, resp.StatusCode, body)
	}
	return body
}

// ─────────────────────────────────────────────────────────────────────────────
// Project setup
// ─────────────────────────────────────────────────────────────────────────────

func locomoCreateProject(t *testing.T, srv, token string) (string, string) {
	t.Helper()
	ts := time.Now().UnixMilli()

	ob, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("locomo-org-%d", ts)})
	or_ := locomoMustStatus(t, locomoDoJSON(t, "POST", srv+"/api/orgs", token, "", ob), http.StatusCreated)
	var om map[string]any
	json.Unmarshal([]byte(or_), &om)
	orgID, _ := om["id"].(string)
	t.Cleanup(func() { locomoDoJSON(t, "DELETE", srv+"/api/orgs/"+orgID, token, "", nil) })

	pb, _ := json.Marshal(map[string]any{
		"name":         fmt.Sprintf("locomo-proj-%d", ts),
		"orgId":        orgID,
		"project_info": "Track personal memories, events, relationships, and activities from multi-session conversations between two friends",
	})
	req, _ := http.NewRequest("POST", srv+"/api/projects", bytes.NewReader(pb))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", token)
	req.Header.Set("X-Org-ID", orgID)
	resp, _ := http.DefaultClient.Do(req)
	pr := locomoMustStatus(t, resp, http.StatusCreated)
	var pm map[string]any
	json.Unmarshal([]byte(pr), &pm)
	projectID, _ := pm["id"].(string)
	t.Cleanup(func() { locomoDoJSON(t, "DELETE", srv+"/api/projects/"+projectID, token, "", nil) })
	t.Logf("locomo project: %s", projectID)
	return orgID, projectID
}

// ─────────────────────────────────────────────────────────────────────────────
// Ingest via MCP remember tool
// ─────────────────────────────────────────────────────────────────────────────

// locomoIngestSession sends one session's text to the MCP `remember` tool.
// The MCP endpoint accepts a tool_name + input map and executes the server-side
// remember pipeline (extraction + graph write).
func locomoIngestSession(t *testing.T, srv, token, projectID string, session int, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"tool_name": "remember",
		"input": map[string]any{
			"content": fmt.Sprintf("Conversation session %d:\n\n%s", session, text),
		},
	})
	resp := locomoDoJSON(t, "POST", srv+"/api/mcp/tools/call", token, projectID, body)
	raw := locomoReadBody(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest session %d: status %d\nbody: %s", session, resp.StatusCode, raw)
	}
	t.Logf("session %d ingested (status %d)", session, resp.StatusCode)
}

// ─────────────────────────────────────────────────────────────────────────────
// Query via MCP recall_memories tool
// ─────────────────────────────────────────────────────────────────────────────

// locomoQuery sends a question to the MCP `recall_memories` tool and returns
// the predicted answer text.
func locomoQuery(t *testing.T, srv, token, projectID, question string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"tool_name": "recall_memories",
		"input": map[string]any{
			"query": question,
		},
	})
	resp := locomoDoJSON(t, "POST", srv+"/api/mcp/tools/call", token, projectID, body)
	raw := locomoReadBody(resp)
	if resp.StatusCode != http.StatusOK {
		t.Logf("query error (status %d): %s", resp.StatusCode, raw)
		return ""
	}
	// Extract the content/text from the MCP tool response.
	var m map[string]any
	json.Unmarshal([]byte(raw), &m)
	// MCP response: {content: [{type: "text", text: "..."}]} or {result: "..."}
	if content, ok := m["content"].([]any); ok && len(content) > 0 {
		if first, ok := content[0].(map[string]any); ok {
			if text, ok := first["text"].(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	if result, ok := m["result"].(string); ok {
		return strings.TrimSpace(result)
	}
	// Fallback: return raw body trimmed.
	return strings.TrimSpace(raw)
}

// ─────────────────────────────────────────────────────────────────────────────
// Token F1 (duplicated here for self-containment — same implementation as
// extraction_eval_test.go but under a different name to avoid redeclaration
// when both build tags are active simultaneously)
// ─────────────────────────────────────────────────────────────────────────────

func locomoNormalizeText(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func locomoTokenF1(predicted, gold string) float64 {
	p := strings.Fields(locomoNormalizeText(predicted))
	g := strings.Fields(locomoNormalizeText(gold))
	if len(p) == 0 && len(g) == 0 {
		return 1.0
	}
	if len(p) == 0 || len(g) == 0 {
		return 0.0
	}
	goldSet := make(map[string]int, len(g))
	for _, tok := range g {
		goldSet[tok]++
	}
	var common int
	for _, tok := range p {
		if goldSet[tok] > 0 {
			common++
			goldSet[tok]--
		}
	}
	if common == 0 {
		return 0.0
	}
	precision := float64(common) / float64(len(p))
	recall := float64(common) / float64(len(g))
	return 2 * precision * recall / (precision + recall)
}

// ─────────────────────────────────────────────────────────────────────────────
// Result persistence
// ─────────────────────────────────────────────────────────────────────────────

type locomoSmokeResult struct {
	Date        string           `json:"date"`
	Sessions    []int            `json:"sessions"`
	Categories  []int            `json:"categories"`
	Questions   int              `json:"questions"`
	TokenF1Mean float64          `json:"token_f1_mean"`
	ExactMatch  float64          `json:"exact_match"`
	PassGate    bool             `json:"pass_gate"`
	Gate        float64          `json:"gate"`
	PerQuestion []questionResult `json:"per_question"`
}

type questionResult struct {
	Question  string  `json:"question"`
	Gold      string  `json:"gold"`
	Predicted string  `json:"predicted"`
	TokenF1   float64 `json:"token_f1"`
	Elapsed   int64   `json:"elapsed_ms"`
}

func saveLocomoResult(t *testing.T, r locomoSmokeResult) {
	t.Helper()
	dir := "/tmp/locomo_results"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("warn: could not create result dir: %v", err)
		return
	}
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+"_smoke.json")
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Logf("warn: could not write result: %v", err)
		return
	}
	t.Logf("locomo result saved: %s", path)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestLoCoMo_Smoke ingests conv-0 sessions 1–3, queries 5 single-hop questions,
// and asserts Token F1 ≥ minLoCoMoCat1F1.
//
// Assumptions verified:
//   - L1: single-hop recall recoverable to ≥ 0.30 after model/quota fix
//   - L5: empty predictions indicate retrieval failure (logged per question)
func TestLoCoMo_Smoke(t *testing.T) {
	srv := serverURL()
	token := e2eTestToken()

	_, projectID := locomoCreateProject(t, srv, token)

	// Ingest sessions 1–3.
	t.Log("ingesting sessions 1–3...")
	for session := 1; session <= 3; session++ {
		text, ok := locomoSmokeSessions[session]
		if !ok {
			t.Fatalf("missing session %d", session)
		}
		locomoIngestSession(t, srv, token, projectID, session, text)
	}
	t.Log("ingest complete — querying...")

	// Query.
	results := make([]questionResult, 0, len(locomoSmokeQuestions))
	var totalF1, totalEM float64

	for _, qa := range locomoSmokeQuestions {
		start := time.Now()
		predicted := locomoQuery(t, srv, token, projectID, qa.Question)
		elapsed := time.Since(start).Milliseconds()

		f1 := locomoTokenF1(predicted, qa.Gold)
		em := 0.0
		if locomoNormalizeText(predicted) == locomoNormalizeText(qa.Gold) {
			em = 1.0
		}

		totalF1 += f1
		totalEM += em

		if predicted == "" {
			t.Logf("EMPTY prediction for: %q (evidence: %s)", qa.Question, qa.Evidence)
		}
		t.Logf("Q: %q\n  gold:      %q\n  predicted: %q\n  F1: %.3f  EM: %.0f  (%dms)",
			qa.Question, qa.Gold, predicted, f1, em, elapsed)

		results = append(results, questionResult{
			Question:  qa.Question,
			Gold:      qa.Gold,
			Predicted: predicted,
			TokenF1:   math.Round(f1*1000) / 1000,
			Elapsed:   elapsed,
		})
	}

	n := float64(len(locomoSmokeQuestions))
	meanF1 := totalF1 / n
	meanEM := totalEM / n

	t.Logf("─── Results ───────────────────────────────")
	t.Logf("Questions:    %d", len(locomoSmokeQuestions))
	t.Logf("Token F1:     %.4f  (gate ≥ %.2f)", meanF1, minLoCoMoCat1F1)
	t.Logf("Exact Match:  %.4f", meanEM)

	saveLocomoResult(t, locomoSmokeResult{
		Date:        time.Now().Format("2006-01-02"),
		Sessions:    []int{1, 2, 3},
		Categories:  []int{1},
		Questions:   len(locomoSmokeQuestions),
		TokenF1Mean: math.Round(meanF1*10000) / 10000,
		ExactMatch:  math.Round(meanEM*10000) / 10000,
		PassGate:    meanF1 >= minLoCoMoCat1F1,
		Gate:        minLoCoMoCat1F1,
		PerQuestion: results,
	})

	if meanF1 < minLoCoMoCat1F1 {
		t.Errorf("L1 FAIL: Token F1 %.4f < %.2f on category-1 questions\n"+
			"Investigate: check empty predictions above (L5) and server model config (quota issue?)",
			meanF1, minLoCoMoCat1F1)
	}
}
