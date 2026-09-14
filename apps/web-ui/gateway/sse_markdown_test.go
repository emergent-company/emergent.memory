package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// --- splitSSEEvent / extractSSEData / fmtEvent / marshalNoEscape ---

// TestSplitSSEEvent locks the bufio.SplitFunc framing: events end at a blank
// line and the token includes the trailing "\n\n" (so framing can be re-emitted
// verbatim), while an unterminated trailing event is yielded at EOF.
func TestSplitSSEEvent(t *testing.T) {
	event1 := "data: {\"a\":1}\n\n"
	event2 := "data: {\"b\":2}\n\n"
	trailing := "data: {\"b\":2}"

	cases := []struct {
		name        string
		data        string
		atEOF       bool
		wantAdvance int
		wantToken   string
		wantErr     bool
	}{
		{"first of two", event1 + event2, false, len(event1), event1, false},
		{"single terminated", event1, false, len(event1), event1, false},
		{"unterminated at EOF", trailing, true, len(trailing), trailing, false},
		{"boundary wins over EOF", event1 + trailing, true, len(event1), event1, false},
		{"empty at EOF", "", true, 0, "", false},
		{"no boundary yet", "data: partial", false, 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			advance, token, err := splitSSEEvent([]byte(tc.data), tc.atEOF)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if advance != tc.wantAdvance {
				t.Errorf("advance = %d, want %d", advance, tc.wantAdvance)
			}
			if string(token) != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
		})
	}
}

// TestExtractSSEData locks payload extraction: only the first data: line is
// used, surrounding whitespace is trimmed, and non-data lines are ignored.
func TestExtractSSEData(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"simple", "data: {\"type\":\"done\"}\n\n", `{"type":"done"}`},
		{"padded", "  data:   {\"x\":1}  \n\n", `{"x":1}`},
		{"first of many data lines", "data: {\"y\":2}\ndata: {\"z\":3}\n\n", `{"y":2}`},
		{"event line before data", "event: foo\ndata: {\"y\":2}\n\n", `{"y":2}`},
		{"no data line", "event: foo\n\n", ""},
		{"empty payload", "data:\n\n", ""},
		{"comment ignored", ": comment\ndata: hello\n\n", "hello"},
		{"empty event", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractSSEData([]byte(tc.raw)); got != tc.want {
				t.Errorf("extractSSEData(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestFmtEvent asserts the exact SSE frame an emitted payload takes.
func TestFmtEvent(t *testing.T) {
	var buf bytes.Buffer
	n, err := fmtEvent(&buf, []byte(`{"type":"html"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "data: {\"type\":\"html\"}\n\n"
	if buf.String() != want {
		t.Errorf("frame = %q, want %q", buf.String(), want)
	}
	if n != len(want) {
		t.Errorf("n = %d, want %d", n, len(want))
	}
}

// TestMarshalNoEscape asserts HTML-sensitive characters survive unescaped and
// that the encoder's trailing newline is trimmed (valid single-line JSON).
func TestMarshalNoEscape(t *testing.T) {
	got, err := marshalNoEscape(map[string]string{"html": "<b>&</b>"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"html":"<b>&</b>"}`
	if string(got) != want {
		t.Errorf("marshalNoEscape = %q, want %q", got, want)
	}
	if strings.Contains(string(got), `\u003c`) || strings.Contains(string(got), `\u0026`) {
		t.Errorf("HTML must not be escaped: %q", got)
	}
	if !json.Valid(got) {
		t.Errorf("output must remain valid JSON: %q", got)
	}
}

// --- rewriteChatStream ---

func rewrite(t *testing.T, stream string) string {
	t.Helper()
	var out bytes.Buffer
	if err := rewriteChatStream(&out, strings.NewReader(stream)); err != nil {
		t.Fatalf("rewriteChatStream: %v", err)
	}
	return out.String()
}

// TestRewriteChatStreamTokenEvents asserts token events accumulate into
// full-message html events, in order, as `data: ...\n\n` frames.
func TestRewriteChatStreamTokenEvents(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"meta","conversationId":"c1"}`,
		`data: {"type":"token","token":"Hello"}`,
		`data: {"type":"token","token":" world"}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"

	out := rewrite(t, stream)

	if !strings.Contains(out, `"type":"html"`) {
		t.Fatalf("no html event emitted: %s", out)
	}
	// second html frame carries the accumulated message, not just the delta.
	if !strings.Contains(out, "Hello world") {
		t.Errorf("accumulated token text missing: %s", out)
	}
	// every emitted frame is a proper `data: ...\n\n` SSE frame.
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("output must end on an event boundary: %q", out)
	}
	// order preserved: meta < html < done.
	iMeta := strings.Index(out, `"type":"meta"`)
	iHTML := strings.Index(out, `"type":"html"`)
	iDone := strings.Index(out, `"type":"done"`)
	if iMeta < 0 || iHTML < 0 || iDone < 0 || iMeta >= iHTML || iHTML >= iDone {
		t.Errorf("event order not preserved (meta=%d html=%d done=%d): %s", iMeta, iHTML, iDone, out)
	}
}

// TestRewriteChatStreamMalformedPassthrough asserts malformed / partial input
// neither panics nor is dropped: the raw event is forwarded byte-for-byte and
// the stream terminates.
func TestRewriteChatStreamMalformedPassthrough(t *testing.T) {
	cases := []string{
		`data: {not json}` + "\n\n",
		`data: {"type":"token","token":"unterminated`, // EOF mid-token
		`data: [` + "\n\n",
	}
	for _, in := range cases {
		out := rewrite(t, in)
		if out != in {
			t.Errorf("malformed event %q: out = %q, want verbatim passthrough", in, out)
		}
	}
}

// TestRewriteChatStreamPassthroughEvents asserts non-rewritten event types are
// forwarded verbatim (approval, unknown, meta, done, empty data).
func TestRewriteChatStreamPassthroughEvents(t *testing.T) {
	cases := []string{
		`data: {"type":"approval","questionId":"a1","tool":"shell"}` + "\n\n",
		`data: {"type":"something_unknown","x":1}` + "\n\n",
		`data: {"type":"done"}` + "\n\n",
	}
	for _, in := range cases {
		if out := rewrite(t, in); out != in {
			t.Errorf("event %q: out = %q, want verbatim", in, out)
		}
	}
	// an event carrying no data line is skipped entirely.
	if out := rewrite(t, "event: ping\n\n"); out != "" {
		t.Errorf("data-less event should be skipped, got %q", out)
	}
}

// TestRewriteChatStreamToolResultHTML asserts a non-ask_user mcp_tool event is
// re-framed with a highlighted resultHtml while every original field survives.
func TestRewriteChatStreamToolResultHTML(t *testing.T) {
	in := `data: {"type":"mcp_tool","tool":"web_search","status":"completed","result":{"answer":1}}` + "\n\n"
	out := rewrite(t, in)
	if out == in {
		t.Fatalf("JSON tool result should be re-framed, got verbatim: %s", out)
	}
	if !strings.HasSuffix(out, "\n\n") || !strings.HasPrefix(out, "data: ") {
		t.Errorf("not a proper SSE frame: %q", out)
	}
	for _, want := range []string{`"resultHtml"`, `"tool":"web_search"`, `"result":{"answer":1}`, `"status":"completed"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

// TestRewriteChatStreamToolResultNonJSONPassthrough asserts a tool result that
// isn't a JSON object/array falls back to the verbatim raw event.
func TestRewriteChatStreamToolResultNonJSONPassthrough(t *testing.T) {
	in := `data: {"type":"mcp_tool","tool":"shell","result":"plain text"}` + "\n\n"
	if out := rewrite(t, in); out != in {
		t.Errorf("non-JSON result should passthrough, out = %q", out)
	}
}

// TestRewriteChatStreamAskUserQuestion asserts the synthesized question event
// preserves all ask_user fields, defaults interactionType, and suppresses the
// raw tool events.
func TestRewriteChatStreamAskUserQuestion(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"mcp_tool","tool":"ask_user","status":"started","result":{"question":"Scope?","options":[{"label":"Full","value":"full","description":"everything"}],"interaction_type":"select","placeholder":"pick one","max_length":10}}`,
		`data: {"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q1","status":"pausing"}}`,
	}, "\n\n") + "\n\n"

	out := rewrite(t, stream)

	var q questionEvent
	found := false
	for _, frame := range strings.Split(out, "\n\n") {
		frame = strings.TrimSpace(frame)
		if !strings.HasPrefix(frame, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(frame, "data:"))
		if !strings.Contains(payload, `"type":"question"`) {
			continue
		}
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			t.Fatalf("question payload not JSON: %v (%s)", err, payload)
		}
		found = true
	}
	if !found {
		t.Fatalf("no question event emitted: %s", out)
	}
	if q.Type != "question" || q.QuestionID != "q1" || q.Question != "Scope?" ||
		q.InteractionType != "select" || q.Placeholder != "pick one" || q.MaxLength != 10 {
		t.Errorf("question fields lost: %+v", q)
	}
	if len(q.Options) != 1 || q.Options[0].Label != "Full" || q.Options[0].Value != "full" || q.Options[0].Description != "everything" {
		t.Errorf("options lost: %+v", q.Options)
	}
	if q.QuestionHTML == "" || !strings.Contains(q.QuestionHTML, "Scope?") {
		t.Errorf("questionHtml missing: %q", q.QuestionHTML)
	}
	if strings.Contains(out, `"tool":"ask_user"`) {
		t.Errorf("raw ask_user tool events must be suppressed: %s", out)
	}
	if strings.Count(out, "\n\n") != 1 {
		t.Errorf("want exactly one emitted frame, got: %q", out)
	}
}

// TestRewriteChatStreamAskUserDefaultInteractionType asserts an omitted
// interaction_type defaults to "buttons".
func TestRewriteChatStreamAskUserDefaultInteractionType(t *testing.T) {
	stream := `data: {"type":"mcp_tool","tool":"ask_user","status":"running","result":{"question":"Q?"}}` + "\n\n" +
		`data: {"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q2"}}` + "\n\n"
	out := rewrite(t, stream)
	if !strings.Contains(out, `"interactionType":"buttons"`) {
		t.Errorf("interactionType should default to buttons: %s", out)
	}
}

// TestRewriteChatStreamAskUserErrorPassthrough asserts an ask_user completion
// with no question_id surfaces the raw event (e.g. validation error).
func TestRewriteChatStreamAskUserErrorPassthrough(t *testing.T) {
	stream := `data: {"type":"mcp_tool","tool":"ask_user","status":"started","result":{"question":"Q?"}}` + "\n\n" +
		`data: {"type":"mcp_tool","tool":"ask_user","status":"error","result":{"error":"options required"}}` + "\n\n"
	out := rewrite(t, stream)
	if strings.Contains(out, `"type":"question"`) {
		t.Errorf("error completion must not synthesize a question: %s", out)
	}
	if !strings.Contains(out, `"error":"options required"`) {
		t.Errorf("errored ask_user event should pass through: %s", out)
	}
}

// TestRewriteChatStreamWriterError asserts write failures propagate rather than
// being swallowed.
func TestRewriteChatStreamWriterError(t *testing.T) {
	err := rewriteChatStream(errWriter{}, strings.NewReader(`data: {"type":"done"}`+"\n\n"))
	if err == nil {
		t.Fatal("expected write error to propagate")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errTest }

// TestSplitSSEEventScanner drives splitSSEEvent through bufio.Scanner to prove
// multi-event streams and a trailing unterminated event are all yielded in order.
func TestSplitSSEEventScanner(t *testing.T) {
	stream := `data: {"n":1}` + "\n\n" + `data: {"n":2}` + "\n\n" + `data: {"n":3}`
	sc := bufio.NewScanner(strings.NewReader(stream))
	sc.Split(splitSSEEvent)
	var got []string
	for sc.Scan() {
		got = append(got, string(sc.Bytes()))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{`data: {"n":1}` + "\n\n", `data: {"n":2}` + "\n\n", `data: {"n":3}`}
	if len(got) != len(want) {
		t.Fatalf("got %d events %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- renderHistoryHTML (same source file) ---

// TestRenderHistoryHTML covers assistant message HTML injection, reasoning
// splitting, ask_user question rendering, and non-message passthrough.
func TestRenderHistoryHTML(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"kind":"message","role":"assistant","content":{"text":"thinking\n\n**answer**"}}`),
		json.RawMessage(`{"kind":"message","role":"user","content":{"text":"hi"}}`),
		json.RawMessage(`{"kind":"tool_call","tool_name":"ask_user","tool_input":{"question":"Q?"}}`),
		json.RawMessage(`{"kind":"tool_call","tool_name":"web_search","tool_input":{"q":"x"},"tool_output":{"n":1}}`),
		json.RawMessage(`not json`),
	}
	out := renderHistoryHTML(items)
	if len(out) != len(items) {
		t.Fatalf("len = %d, want %d", len(out), len(items))
	}

	// 0: reasoning split + html
	var m0 map[string]any
	if err := json.Unmarshal(out[0], &m0); err != nil {
		t.Fatal(err)
	}
	c0, _ := m0["content"].(map[string]any)
	if c0["reasoning"] != "thinking" {
		t.Errorf("reasoning = %v, want thinking", c0["reasoning"])
	}
	if c0["text"] != "**answer**" {
		t.Errorf("text = %v, want the answer without reasoning", c0["text"])
	}
	if html, _ := c0["html"].(string); !strings.Contains(html, "<strong>answer</strong>") {
		t.Errorf("html = %q, want rendered markdown", html)
	}

	// 1: user message untouched
	if string(out[1]) != string(items[1]) {
		t.Errorf("user message must be untouched, got %s", out[1])
	}

	// 2: ask_user gains question_html
	var m2 map[string]any
	if err := json.Unmarshal(out[2], &m2); err != nil {
		t.Fatal(err)
	}
	in2, _ := m2["tool_input"].(map[string]any)
	if html, _ := in2["question_html"].(string); !strings.Contains(html, "Q?") {
		t.Errorf("ask_user question_html missing, got %v", in2["question_html"])
	}

	// 3: other tool calls gain highlighted input/output html
	var m3 map[string]any
	if err := json.Unmarshal(out[3], &m3); err != nil {
		t.Fatal(err)
	}
	if _, ok := m3["tool_input_html"]; !ok {
		t.Errorf("tool_input_html missing: %v", m3)
	}
	if _, ok := m3["tool_output_html"]; !ok {
		t.Errorf("tool_output_html missing: %v", m3)
	}

	// 4: unparsable item kept as-is
	if string(out[4]) != string(items[4]) {
		t.Errorf("unparsable item must be kept as-is, got %s", out[4])
	}
}
