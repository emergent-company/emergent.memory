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
	Type      string     `json:"type"`
	Token     string     `json:"token"`
	HTML      string     `json:"html"`
	Citations []citation `json:"citations"`
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

// TestRewriteChatStreamPassesUIPassthrough asserts an A2UI `ui` event is
// re-emitted verbatim: the gateway does not interpret the declarative-card
// payload (memory has already validated the envelope), so the client receives
// the exact surface frame — surfaceId + the ordered messages array — untouched.
func TestRewriteChatStreamPassesUIPassthrough(t *testing.T) {
	uiFrame := `data: {"type":"ui","surfaceId":"s1","messages":[` +
		`{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}},` +
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"c1","component":"proposal","kind":"deploy","summary":"hi"}]}}]}`
	stream := uiFrame + "\n\n" + `data: {"type":"done"}` + "\n\n"

	out := rewrite(t, stream)

	// The ui frame is re-emitted byte-for-byte: no interpret, no re-marshal.
	if !strings.Contains(out, uiFrame) {
		t.Fatalf("ui frame not passed through verbatim:\n got %q\nwant it to contain %q", out, uiFrame)
	}
	types := make([]string, 0, 3)
	for _, ev := range parseStream(t, out) {
		types = append(types, ev.Type)
	}
	if !slices.Contains(types, "ui") {
		t.Fatalf("event types = %v, want a ui event", types)
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

// TestRewriteChatStreamToolResultPreservesIDStatus locks the additive mcp_tool
// contract: the new `id` and the existing `status` field survive the
// emitToolResultHTML re-marshal (map[string]json.RawMessage) verbatim.
func TestRewriteChatStreamToolResultPreservesIDStatus(t *testing.T) {
	in := `data: {"type":"mcp_tool","id":"call-42","tool":"web_search","status":"awaiting_confirmation","result":{"answer":1}}` + "\n\n"
	out := rewrite(t, in)
	if out == in {
		t.Fatalf("JSON tool result should be re-framed, got verbatim: %s", out)
	}
	for _, want := range []string{`"id":"call-42"`, `"status":"awaiting_confirmation"`, `"resultHtml"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

// TestRewriteChatStreamToolResultNonJSONIsEnriched asserts a tool result that is
// plain text is still categorised and still carries its original fields, followed
// by a synthesized terminal `error` (no `done`). Text results are common (a
// shell tool's output), so the chip would keep its wire name if only JSON results
// were enriched.
func TestRewriteChatStreamToolResultNonJSONIsEnriched(t *testing.T) {
	in := `data: {"type":"mcp_tool","tool":"shell","result":"plain text"}` + "\n\n"
	out := rewrite(t, in)
	for _, want := range []string{
		`"type":"mcp_tool"`,
		`"tool":"shell"`,
		`"result":"plain text"`,
		`"toolCategory":`,
		`"family":"tool"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
	if !strings.Contains(out, `"type":"error"`) {
		t.Errorf("non-JSON result should gain a terminal error, out = %q", out)
	}
}

// TestRewriteChatStreamToolFrameSubjectAndView pins the live frame's two shapes:
// a running frame carries the call's arguments (so it gets a subject and no view),
// and a terminal frame carries the result (so it gets a view and no subject).
func TestRewriteChatStreamToolFrameSubjectAndView(t *testing.T) {
	running := `data: {"type":"mcp_tool","id":"c1","tool":"search-hybrid","status":"running","result":{"query":"swiftui","limit":5}}` + "\n\n"
	out := rewrite(t, running)
	frame := decodeFirstFrame(t, out)
	if got := frame["toolSubject"]; got != "swiftui" {
		t.Errorf("running frame toolSubject = %v, want swiftui", got)
	}
	if _, ok := frame["toolView"]; ok {
		t.Errorf("running frame must not carry a view: %v", frame["toolView"])
	}
	category, _ := frame["toolCategory"].(map[string]any)
	if got := category["verb"]; got != "Searched memory" {
		t.Errorf("toolCategory.verb = %v, want Searched memory", got)
	}

	settled := `data: {"type":"mcp_tool","id":"c1","tool":"search-hybrid","status":"completed","result":{"ok":true,"data":{"data":[{"object":{"id":"e1","name":"Alice"}}],"total":1}}}` + "\n\n"
	out = rewrite(t, settled)
	frame = decodeFirstFrame(t, out)
	view, ok := frame["toolView"].(map[string]any)
	if !ok {
		t.Fatalf("terminal frame has no toolView: %v", frame)
	}
	if got := view["kind"]; got != "records" {
		t.Errorf("toolView.kind = %v, want records", got)
	}
	if got := view["outcome"]; got != "1 record" {
		t.Errorf("toolView.outcome = %v, want 1 record", got)
	}
	if _, ok := frame["toolSubject"]; ok {
		t.Errorf("terminal frame must not re-derive a subject from its result: %v", frame["toolSubject"])
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
// TestRewriteChatStreamCitations asserts the terminal `citations` event is
// forwarded verbatim before `done`, and the snapshot applies the citation link
// rule: cited object links stay, uncited ones are demoted to their label text.
func TestRewriteChatStreamCitations(t *testing.T) {
	cited := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"
	stream := strings.Join([]string{
		`data: {"type":"token","token":"[Acme](/objects/` + cited + `) and [Ghost](/objects/` + other + `)"}`,
		`data: {"type":"citations","citations":[{"kind":"object","id":"` + cited + `","type":"Person","label":"Acme","url":"/objects/` + cited + `"}]}`,
		`data: {"type":"done"}`,
	}, "\n\n") + "\n\n"

	events := parseStream(t, rewrite(t, stream))

	var types []string
	var snapshot string
	citationsForwarded := false
	for _, ev := range events {
		types = append(types, ev.Type)
		switch ev.Type {
		case "html":
			snapshot = ev.HTML
		case "citations":
			if len(ev.Citations) == 1 && ev.Citations[0].ID == cited && ev.Citations[0].Type == "Person" {
				citationsForwarded = true
			}
		}
	}
	if want := []string{"token", "citations", "html", "done"}; !slices.Equal(types, want) {
		t.Fatalf("event sequence = %v, want %v", types, want)
	}
	if !citationsForwarded {
		t.Error("citations event must be forwarded with its payload intact")
	}
	if !strings.Contains(snapshot, `href="/objects/`+cited+`"`) {
		t.Errorf("cited link must stay an anchor: %s", snapshot)
	}
	if strings.Contains(snapshot, "/objects/"+other) {
		t.Errorf("uncited link must be demoted (no href): %s", snapshot)
	}
	if !strings.Contains(snapshot, "Ghost") {
		t.Errorf("demoted link must keep its label text: %s", snapshot)
	}
}

// TestRewriteChatStreamNoCitationsUnchanged locks the additive contract: a
// stream with no `citations` event carries no citations frame and renders
// exactly as before.
func TestRewriteChatStreamNoCitationsUnchanged(t *testing.T) {
	stream := "data: {\"type\":\"token\",\"token\":\"plain answer\"}\n\n" +
		"data: {\"type\":\"done\"}\n\n"
	out := rewrite(t, stream)
	if strings.Contains(out, `"type":"citations"`) {
		t.Errorf("no citations event should be emitted: %s", out)
	}
}

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

// TestRenderHistoryHTMLCitations asserts a history item's `citations` are
// preserved on the re-encoded item for the client's Sources block, and the
// rendered answer applies the same citation link rule as the live snapshot.
func TestRenderHistoryHTMLCitations(t *testing.T) {
	cited := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"
	item := `{"kind":"message","role":"assistant",` +
		`"citations":[{"kind":"object","id":"` + cited + `","type":"Person","label":"Acme","url":"/objects/` + cited + `"}],` +
		`"content":{"text":"see [Acme](/objects/` + cited + `) and [Ghost](/objects/` + other + `)"}}`
	out := renderHistoryHTML([]json.RawMessage{json.RawMessage(item)})
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	var m map[string]any
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["citations"]; !ok {
		t.Fatalf("citations must be preserved on the item: %v", m)
	}
	c, _ := m["content"].(map[string]any)
	html, _ := c["html"].(string)
	if !strings.Contains(html, `href="/objects/`+cited+`"`) {
		t.Errorf("cited link missing anchor: %s", html)
	}
	if strings.Contains(html, "/objects/"+other) {
		t.Errorf("uncited link must be demoted: %s", html)
	}
	if !strings.Contains(html, "Ghost") {
		t.Errorf("demoted label text must remain: %s", html)
	}
}

func TestRenderHistoryHTMLLeavesSystemUntouched(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"kind":"message","role":"system","content":{"text":"# Instructions\nBe careful."}}`),
	}
	out := renderHistoryHTML(items)
	var m map[string]any
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	c, _ := m["content"].(map[string]any)
	if c["text"] != "# Instructions\nBe careful." {
		t.Errorf("system text altered: %v", c["text"])
	}
	if _, has := c["html"]; has {
		t.Errorf("system record must not gain markdown html: %v", c)
	}
}

// decodeFirstFrame returns the first `data:` frame of a rewritten stream as a map.
func decodeFirstFrame(t *testing.T, out string) map[string]any {
	t.Helper()
	for _, frame := range strings.Split(out, "\n\n") {
		frame = strings.TrimSpace(frame)
		if !strings.HasPrefix(frame, "data: ") {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(frame, "data: ")), &m); err != nil {
			t.Fatalf("frame is not JSON: %v (%s)", err, frame)
		}
		return m
	}
	t.Fatalf("no data frame in %q", out)
	return nil
}

// TestEnrichToolItemAddsTheStepFields pins the replay seam: a recorded tool_call
// gains its category, subject and readable view, so a run read back describes a
// call the way the live chip did.
func TestEnrichToolItemAddsTheStepFields(t *testing.T) {
	content := strings.TrimSuffix(strings.Repeat("A line of the retrieved document.\n", 12), "\n")
	item := `{"kind":"tool_call",` +
		`"tool_name":"document-get","tool_status":"completed",` +
		`"tool_input":{"id":"d1","path":"notes.md"},` +
		`"tool_output":{"id":"d1","filename":"notes.md","content":"` +
		strings.ReplaceAll(content, "\n", `\n`) + `","ok":true}}`

	out := enrichToolItem([]byte(item))
	if out == nil {
		t.Fatal("enrichToolItem returned nil")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}

	category, _ := m["tool_category"].(map[string]any)
	if got := category["family"]; got != "documents" {
		t.Errorf("tool_category.family = %v, want documents", got)
	}
	if got := category["verb"]; got != "Worked with documents" {
		t.Errorf("tool_category.verb = %v, want Worked with documents", got)
	}
	if got := category["icon"]; got != "lucide--file-text" {
		t.Errorf("tool_category.icon = %v, want lucide--file-text", got)
	}

	if got := m["tool_subject"]; got != "d1" {
		t.Errorf("tool_subject = %v, want d1", got)
	}

	view, ok := m["tool_view"].(map[string]any)
	if !ok {
		t.Fatalf("tool_view missing: %v", m)
	}
	if got := view["kind"]; got != "text" {
		t.Errorf("tool_view.kind = %v, want text", got)
	}
	if got := view["outcome"]; got != "A line of the retrieved document." {
		t.Errorf("tool_view.outcome = %v", got)
	}
	if got := view["moreLines"]; got != float64(4) {
		t.Errorf("tool_view.moreLines = %v, want 4", got)
	}

	// The original fields survive.
	if got := m["tool_name"]; got != "document-get" {
		t.Errorf("tool_name = %v, want document-get", got)
	}
	if _, ok := m["tool_input_html"]; !ok {
		t.Errorf("tool_input_html missing")
	}
}

// TestEnrichToolItemWithoutReadableOutput: a payload with no readable shape still
// gets its category, subject and view — the view's kind is empty and its bounded
// raw text is what the step shows, so the client bounds and labels the payload the
// same way it does a readable one.
// TestEnrichToolItemLeavesAnUnsettledCallAlone: while a call is running, what it
// carries is its arguments, not a result. A call paused for approval is worse:
// afterToolCb persists it as "completed" with the executor's pause sentinel as its
// output, so the payload — not the status — says it never ran. Reading either as
// a result replayed a paused step as "Used memory · Note — 3 fields".
func TestEnrichToolItemLeavesAnUnsettledCallAlone(t *testing.T) {
	running := `{"kind":"tool_call","tool_name":"entity-create","tool_status":"running",` +
		`"tool_input":{"title":"Note","type":"note"},"tool_output":{"title":"Note","type":"note"}}`
	out := enrichToolItem([]byte(running))
	if out == nil {
		t.Fatal("enrichToolItem returned nil")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["tool_view"]; ok {
		t.Error("running: tool_view present, want none for an unsettled call")
	}

	// The shape a paused call is really persisted as.
	paused := `{"kind":"tool_call","tool_name":"entity-create","tool_status":"completed",` +
		`"tool_input":{"title":"Note","type":"note"},` +
		`"tool_output":{"status":"awaiting_confirmation","question_id":"q1",` +
		`"message":"Tool execution paused, awaiting user confirmation."}}`
	out = enrichToolItem([]byte(paused))
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["tool_view"]; ok {
		t.Errorf("paused: tool_view present, want none: %v", m["tool_view"])
	}
	// The subject still comes from the arguments, so the row can name the call.
	if got, _ := m["tool_subject"].(string); got != "Note" {
		t.Errorf("paused: tool_subject = %q, want Note (from the arguments)", got)
	}

	// No status at all means settled: history writes the output at completion.
	settled := `{"kind":"tool_call","tool_name":"entity-create",` +
		`"tool_input":{"title":"Note"},"tool_output":{"object":{"title":"Note"}}}`
	out = enrichToolItem([]byte(settled))
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["tool_view"]; !ok {
		t.Error("tool_view missing for an item with no status")
	}
}

// TestRewriteChatStreamPausedFrameNamesNothing: a paused frame's `result` is the
// executor's sentinel, so it is neither a result nor the call's arguments — the
// chip keeps the subject the running frame gave it.
func TestRewriteChatStreamPausedFrameNamesNothing(t *testing.T) {
	paused := `data: {"type":"mcp_tool","id":"c1","tool":"entity-create","status":"awaiting_confirmation",` +
		`"result":{"status":"awaiting_confirmation","question_id":"q1",` +
		`"message":"Tool execution paused, awaiting user confirmation."}}` + "\n\n"
	frame := decodeFirstFrame(t, rewrite(t, paused))
	if got, ok := frame["toolSubject"]; ok {
		t.Errorf("paused frame toolSubject = %v, want none (the sentinel names nothing)", got)
	}
	if _, ok := frame["toolView"]; ok {
		t.Errorf("paused frame must not carry a view: %v", frame["toolView"])
	}
	category, _ := frame["toolCategory"].(map[string]any)
	if got := category["verb"]; got != "Used memory" {
		t.Errorf("toolCategory.verb = %v, want Used memory", got)
	}
}

func TestEnrichToolItemWithoutReadableOutput(t *testing.T) {
	item := `{"kind":"tool_call","tool_name":"entity-get","tool_status":"completed",` +
		`"tool_input":{"id":"e1"},"tool_output":{"object":{"a":{"b":1}}}}`
	out := enrichToolItem([]byte(item))
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["tool_category"]; !ok {
		t.Error("tool_category missing")
	}
	view, ok := m["tool_view"].(map[string]any)
	if !ok {
		t.Fatalf("tool_view missing: %v", m)
	}
	if got := view["kind"]; got != "" {
		t.Errorf("tool_view.kind = %v, want empty (no readable body)", got)
	}
	if got, _ := view["raw"].(string); !strings.Contains(got, `"a"`) {
		t.Errorf("tool_view.raw = %q, want the bounded payload", got)
	}
}
