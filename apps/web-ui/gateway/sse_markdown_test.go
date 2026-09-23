package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"slices"
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

// streamEvent is the subset of stream event fields the rewrite tests assert on.
type streamEvent struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	HTML  string `json:"html"`
}

// parseStream decodes the SSE frames in out into a slice of streamEvents,
// dropping any frame that is not a `data:` line.
func parseStream(t *testing.T, out string) []streamEvent {
	t.Helper()
	var events []streamEvent
	for _, frame := range strings.Split(out, "\n\n") {
		frame = strings.TrimSpace(frame)
		if !strings.HasPrefix(frame, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(frame, "data:"))
		var ev streamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("event payload not JSON: %v (%s)", err, payload)
		}
		events = append(events, ev)
	}
	return events
}

// TestRewriteChatStreamTokenEvents asserts token deltas are forwarded as raw
// `token` events and markdown is rendered exactly once per turn, as the single
// authoritative `html` snapshot emitted before `done`.
func TestRewriteChatStreamTokenEvents(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"meta","conversationId":"c1"}`,
		`data: {"type":"token","token":"Hel"}`,
		`data: {"type":"token","token":"lo "}`,
		`data: {"type":"token","token":"world"}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"

	events := parseStream(t, rewrite(t, stream))

	var types []string
	var tokenDeltas []string
	htmlCount := 0
	for _, ev := range events {
		types = append(types, ev.Type)
		switch ev.Type {
		case "token":
			tokenDeltas = append(tokenDeltas, ev.Token)
		case "html":
			htmlCount++
		}
	}
	if want := []string{"meta", "token", "token", "token", "html", "done"}; !slices.Equal(types, want) {
		t.Fatalf("event sequence = %v, want %v", types, want)
	}
	if want := []string{"Hel", "lo ", "world"}; !slices.Equal(tokenDeltas, want) {
		t.Errorf("token deltas = %v, want %v", tokenDeltas, want)
	}
	if htmlCount != 1 {
		t.Errorf("html events = %d, want exactly 1", htmlCount)
	}
	if !strings.Contains(events[4].HTML, "world") {
		t.Errorf("snapshot missing accumulated text: %q", events[4].HTML)
	}
}

// TestRewriteChatStreamRendersOncePerTurn feeds a 20-token turn and asserts a
// single `html` event — one markdown render — not one per token. The `html`
// event count equals the renderMarkdown invocation count because only
// emitMarkdownSnapshot renders in the token/snapshot path.
func TestRewriteChatStreamRendersOncePerTurn(t *testing.T) {
	var parts []string
	for range 20 {
		parts = append(parts, `data: {"type":"token","token":"x"}`)
	}
	parts = append(parts, `data: {"type":"done"}`)
	stream := strings.Join(parts, "\n\n") + "\n\n"

	events := parseStream(t, rewrite(t, stream))

	tokenCount, htmlCount := 0, 0
	for _, ev := range events {
		switch ev.Type {
		case "token":
			tokenCount++
		case "html":
			htmlCount++
		}
	}
	if tokenCount != 20 {
		t.Errorf("token events = %d, want 20", tokenCount)
	}
	if htmlCount != 1 {
		t.Errorf("html events (renders) = %d, want 1", htmlCount)
	}
}

// TestRewriteChatStreamDoneNoTokens asserts a `done` with no preceding token
// emits no `html` snapshot.
func TestRewriteChatStreamDoneNoTokens(t *testing.T) {
	out := rewrite(t, `data: {"type":"done"}`+"\n\n")
	if strings.Contains(out, `"type":"html"`) {
		t.Errorf("done with no tokens must not emit html: %s", out)
	}
	if !strings.Contains(out, `"type":"done"`) {
		t.Errorf("done must pass through: %s", out)
	}
}

// TestRewriteChatStreamErrorFallbackSnapshot asserts a stream terminated by an
// `error` event (no `done`) still emits the authoritative snapshot, followed by
// a synthesized terminal `error`.
func TestRewriteChatStreamErrorFallbackSnapshot(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"token","token":"par"}`,
		`data: {"type":"token","token":"tial"}`,
		`data: {"type":"error","message":"boom"}`,
	}, "\n\n") + "\n\n"

	events := parseStream(t, rewrite(t, stream))

	var types []string
	htmlCount := 0
	for _, ev := range events {
		types = append(types, ev.Type)
		if ev.Type == "html" {
			htmlCount++
		}
	}
	if want := []string{"token", "token", "error", "html", "error"}; !slices.Equal(types, want) {
		t.Fatalf("event sequence = %v, want %v", types, want)
	}
	if htmlCount != 1 {
		t.Errorf("html events = %d, want 1", htmlCount)
	}
	if !strings.Contains(events[3].HTML, "partial") {
		t.Errorf("snapshot missing accumulated text: %q", events[3].HTML)
	}
}

// TestRewriteChatStreamEOFFallbackSnapshot asserts EOF without `done` emits the
// authoritative snapshot followed by a synthesized terminal `error`.
func TestRewriteChatStreamEOFFallbackSnapshot(t *testing.T) {
	out := rewrite(t, `data: {"type":"token","token":"open"}`+"\n\n")
	events := parseStream(t, out)
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (%v)", len(events), events)
	}
	if events[0].Type != "token" || events[0].Token != "open" {
		t.Errorf("first event = %+v, want token \"open\"", events[0])
	}
	if events[1].Type != "html" || !strings.Contains(events[1].HTML, "open") {
		t.Errorf("second event = %+v, want html snapshot", events[1])
	}
	if events[2].Type != "error" {
		t.Errorf("third event = %+v, want error", events[2])
	}
}

// TestRewriteChatStreamInterruptedEmitsError asserts a stream that ends (clean
// EOF) without ever emitting `done` — e.g. the memory service restarted
// mid-run — emits a terminal `error` event after the authoritative snapshot, so
// the client surfaces the interruption instead of hanging on its indicator.
func TestRewriteChatStreamInterruptedEmitsError(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"token","token":"par"}`,
		`data: {"type":"token","token":"tial"}`,
	}, "\n\n") + "\n\n"

	events := parseStream(t, rewrite(t, stream))

	var types []string
	htmlCount := 0
	for _, ev := range events {
		types = append(types, ev.Type)
		if ev.Type == "html" {
			htmlCount++
		}
	}
	if want := []string{"token", "token", "html", "error"}; !slices.Equal(types, want) {
		t.Fatalf("event sequence = %v, want %v", types, want)
	}
	if htmlCount != 1 {
		t.Errorf("html events = %d, want 1", htmlCount)
	}
	if !strings.Contains(events[2].HTML, "partial") {
		t.Errorf("snapshot missing accumulated text: %q", events[2].HTML)
	}
}

// TestRewriteChatStreamInterruptedEmptyEmitsError asserts even a token-less
// stream that ends without `done` emits a terminal `error` (no snapshot, since
// there is no text to render).
func TestRewriteChatStreamInterruptedEmptyEmitsError(t *testing.T) {
	out := rewrite(t, `data: {"type":"meta","conversationId":"c1"}`+"\n\n")
	events := parseStream(t, out)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (%v)", len(events), events)
	}
	if events[0].Type != "meta" || events[1].Type != "error" {
		t.Errorf("events = %+v, want [meta error]", events)
	}
	if strings.Contains(out, `"type":"html"`) {
		t.Errorf("empty turn must not emit html: %s", out)
	}
}

// TestRewriteChatStreamDoneNoError asserts a stream that ends with `done` emits
// no synthesized `error` event.
func TestRewriteChatStreamDoneNoError(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"token","token":"hi"}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"

	for _, ev := range parseStream(t, rewrite(t, stream)) {
		if ev.Type == "error" {
			t.Fatalf("done-terminated stream must not emit error: %+v", ev)
		}
	}
}

// TestRewriteChatStreamNoDuplicateSnapshot asserts the post-loop fallback does
// not emit a second snapshot when `done` already emitted one.
func TestRewriteChatStreamNoDuplicateSnapshot(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"token","token":"hi"}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"

	htmlCount := 0
	for _, ev := range parseStream(t, rewrite(t, stream)) {
		if ev.Type == "html" {
			htmlCount++
		}
	}
	if htmlCount != 1 {
		t.Errorf("html events = %d, want exactly 1 (no duplicate)", htmlCount)
	}
}

// TestRewriteChatStreamSnapshotMatchesHistory asserts the live snapshot and the
// conversation-history render of the same text are byte-identical, because both
// call renderMarkdown.
func TestRewriteChatStreamSnapshotMatchesHistory(t *testing.T) {
	const text = "**bold** and `code`"
	stream := "data: {\"type\":\"token\",\"token\":\"**bold**\"}\n\n" +
		"data: {\"type\":\"token\",\"token\":\" and `code`\"}\n\n" +
		"data: {\"type\":\"done\"}\n\n"

	var snapshot string
	for _, ev := range parseStream(t, rewrite(t, stream)) {
		if ev.Type == "html" {
			snapshot = ev.HTML
		}
	}
	if snapshot == "" {
		t.Fatal("no html snapshot emitted")
	}
	if want := renderMarkdown(text); snapshot != want {
		t.Errorf("snapshot = %q, history render = %q", snapshot, want)
	}
}

// TestRewriteChatStreamMalformedPassthrough asserts malformed / partial input
// neither panics nor is dropped: the raw event is forwarded byte-for-byte,
// followed by a synthesized terminal `error` (no `done` was seen), and the
// stream terminates.
func TestRewriteChatStreamMalformedPassthrough(t *testing.T) {
	cases := []string{
		`data: {not json}` + "\n\n",
		`data: {"type":"token","token":"unterminated`, // EOF mid-token
		`data: [` + "\n\n",
	}
	for _, in := range cases {
		out := rewrite(t, in)
		if !strings.HasPrefix(out, in) {
			t.Errorf("malformed event %q: out = %q, want verbatim prefix", in, out)
		}
		if !strings.Contains(out, `"type":"error"`) {
			t.Errorf("malformed event %q: out = %q, want terminal error", in, out)
		}
	}
}

// TestRewriteChatStreamPassthroughEvents asserts non-rewritten event types are
// forwarded verbatim (approval, unknown, done) and that a stream ending without
// `done` gains a synthesized terminal `error`.
func TestRewriteChatStreamPassthroughEvents(t *testing.T) {
	// `done` terminates the stream normally: verbatim, no synthesized error.
	if in, out := `data: {"type":"done"}`+"\n\n", rewrite(t, `data: {"type":"done"}`+"\n\n"); out != in {
		t.Errorf("done event %q: out = %q, want verbatim", in, out)
	}
	// Non-done passthrough events are forwarded verbatim, then terminated with
	// a synthesized error (no `done` was seen).
	cases := []string{
		`data: {"type":"approval","questionId":"a1","tool":"shell"}` + "\n\n",
		`data: {"type":"something_unknown","x":1}` + "\n\n",
	}
	for _, in := range cases {
		out := rewrite(t, in)
		if !strings.HasPrefix(out, in) {
			t.Errorf("event %q: out = %q, want verbatim prefix", in, out)
		}
		if !strings.Contains(out, `"type":"error"`) {
			t.Errorf("event %q: out = %q, want terminal error", in, out)
		}
	}
	// a non-comment event carrying no data line is skipped, but still yields a
	// terminal error since no `done` terminated the stream.
	if out := rewrite(t, "event: ping\n\n"); !strings.Contains(out, `"type":"error"`) || strings.Contains(out, "ping") {
		t.Errorf("data-less event should be skipped but error-synthesized, got %q", out)
	}
}

// TestRewriteChatStreamCommentForwarding asserts SSE comment/keepalive frames
// (lines starting with ':') are forwarded verbatim rather than dropped, so a
// server-side heartbeat reaches the client end-to-end.
func TestRewriteChatStreamCommentForwarding(t *testing.T) {
	stream := ": ping\n\n" +
		`data: {"type":"done"}` + "\n\n" +
		":\n\n"
	out := rewrite(t, stream)
	if !strings.HasPrefix(out, ": ping\n\n") {
		t.Errorf("keepalive must be forwarded verbatim at its original position: %q", out)
	}
	if !strings.Contains(out, `"type":"done"`) {
		t.Errorf("data event after keepalive lost: %q", out)
	}
	if !strings.HasSuffix(out, ":\n\n") {
		t.Errorf("trailing bare comment must be forwarded: %q", out)
	}
	if strings.Count(out, ": ping") != 1 {
		t.Errorf("keepalive duplicated or altered: %q", out)
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
// isn't a JSON object/array falls back to the verbatim raw event, followed by a
// synthesized terminal `error` (no `done`).
func TestRewriteChatStreamToolResultNonJSONPassthrough(t *testing.T) {
	in := `data: {"type":"mcp_tool","tool":"shell","result":"plain text"}` + "\n\n"
	out := rewrite(t, in)
	if !strings.HasPrefix(out, in) {
		t.Errorf("non-JSON result should passthrough verbatim, out = %q", out)
	}
	if !strings.Contains(out, `"type":"error"`) {
		t.Errorf("non-JSON result should gain a terminal error, out = %q", out)
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
	// one question frame plus one synthesized terminal `error` (no `done`).
	if strings.Count(out, "\n\n") != 2 {
		t.Errorf("want exactly two emitted frames (question + error), got: %q", out)
	}
	if !strings.Contains(out, `"type":"error"`) {
		t.Errorf("ask_user stream should gain a terminal error, got: %q", out)
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
