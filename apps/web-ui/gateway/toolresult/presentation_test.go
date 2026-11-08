package toolresult

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The fixtures are the shapes the server actually sends, not ones invented for
// the test: #2248 was caused by fixtures that encoded a payload no handler
// produces. Sizes the reader should never see are generated (see sizes_test.go)
// rather than committed as walls of text.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

func TestParseNoResult(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t "} {
		if got := Parse(raw); got != nil {
			t.Errorf("Parse(%q) = %+v, want nil", raw, got)
		}
	}
}

// TestParseFixtures pins what each real payload reads as. A change to these
// expectations is a change to what a person sees, so it has to be deliberate.
func TestParseFixtures(t *testing.T) {
	tests := []struct {
		fixture string
		kind    Kind
		outcome string
		titles  []string
		more    int
		err     string
		// rawContains is a string the bounded raw payload must still contain: the
		// disclosure reveals the payload, not a summary of it.
		rawContains string
	}{
		{
			fixture:     "envelope-search.json",
			kind:        KindRecords,
			outcome:     "2 records",
			titles:      []string{"Alice", "retrieval"},
			rawContains: `"score": 0.91`,
		},
		{
			fixture:     "entity-query.json",
			kind:        KindRecords,
			outcome:     "1 record",
			titles:      []string{"Alice"},
			rawContains: `"projectId": "p1"`,
		},
		{
			fixture:     "entity-create.json",
			kind:        KindRecords,
			outcome:     "1 record",
			titles:      []string{"e1"},
			rawContains: `"similar_count": 0`,
		},
		{
			fixture:     "entity-update.json",
			kind:        KindFields,
			outcome:     "2 fields",
			rawContains: `"type": "Note"`,
		},
		{
			fixture:     "document-get.json",
			kind:        KindText,
			outcome:     "A line of the retrieved document.",
			rawContains: `"filename": "retrieval-notes.md"`,
		},
		{
			fixture:     "list-documents.json",
			kind:        KindRecords,
			outcome:     "2 records",
			titles:      []string{"notes.md", "spec.pdf"},
			rawContains: `"next_cursor": null`,
		},
		{
			fixture:     "web-search.json",
			kind:        KindRecords,
			outcome:     "1 record",
			titles:      []string{"SwiftUI"},
			rawContains: `"resultCount": 1`,
		},
		{
			fixture:     "record-scalars.json",
			kind:        KindFields,
			outcome:     "5 fields",
			rawContains: `"status": "ok"`,
		},
		{
			fixture:     "failed-envelope.json",
			kind:        KindText,
			outcome:     "entity not found: e9",
			err:         "entity not found: e9",
			rawContains: `"ok": false`,
		},
		{
			fixture:     "string-list.json",
			kind:        KindRecords,
			outcome:     "2 records",
			titles:      []string{"alpha", "beta"},
			rawContains: `"keys"`,
		},
		{
			// The untitled element is counted, not shown, and the list survives.
			fixture:     "untitled-record.json",
			kind:        KindRecords,
			outcome:     "2 records",
			titles:      []string{"Only one"},
			more:        1,
			rawContains: `"score": 0.4`,
		},
		{
			// A record of nested structures has no readable shape: the payload is
			// the presentation.
			fixture:     "nested-record.json",
			rawContains: `"b": 1`,
		},
		{
			// Re-pointed by #2488: this used to pin the empty list as "opens as
			// the raw envelope", deferred to #2277. The empty list now reads as
			// "No results", with the envelope still behind the raw disclosure.
			fixture:     "empty-list.json",
			kind:        KindEmpty,
			outcome:     EmptyText,
			rawContains: `"documents": []`,
		},
		{
			// search-hybrid with no hits: the nested data list is the empty one.
			fixture:     "empty-search.json",
			kind:        KindEmpty,
			outcome:     EmptyText,
			rawContains: `"has_more": false`,
		},
		{
			// The payload is the first data/result value that is itself a payload;
			// no "ok" flag is required. This mirrors the shipped iOS parser, so the
			// two clients read the same result the same way. Pinned so a future
			// tightening is a decision, not a drift.
			fixture:     "envelope-without-ok.json",
			kind:        KindRecords,
			outcome:     "2 records",
			titles:      []string{"alpha", "beta"},
			rawContains: `"data"`,
		},
		{
			// Not JSON at all: nothing to read, so the raw text is the result.
			fixture:     "plain-text.txt",
			rawContains: "line two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got := Parse(fixture(t, tt.fixture))
			if got == nil {
				t.Fatalf("Parse returned nil for a non-empty result")
			}
			if got.Kind != tt.kind {
				t.Errorf("kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.Outcome != tt.outcome {
				t.Errorf("outcome = %q, want %q", got.Outcome, tt.outcome)
			}
			if got.Error != tt.err {
				t.Errorf("error = %q, want %q", got.Error, tt.err)
			}
			if tt.more != 0 && got.MoreRecords != tt.more {
				t.Errorf("moreRecords = %d, want %d", got.MoreRecords, tt.more)
			}
			if len(tt.titles) > 0 {
				if len(got.Items) != len(tt.titles) {
					t.Fatalf("items = %d, want %d", len(got.Items), len(tt.titles))
				}
				for i, title := range tt.titles {
					if got.Items[i].Title != title {
						t.Errorf("items[%d].Title = %q, want %q", i, got.Items[i].Title, title)
					}
				}
			}
			if !strings.Contains(got.Raw, tt.rawContains) {
				t.Errorf("raw does not contain %q:\n%s", tt.rawContains, got.Raw)
			}
		})
	}
}

// TestParseEmptyListIsNoResults pins the empty state the iOS client renders
// (#2277): the body is the copy, not the pretty-printed envelope, and the
// envelope is still the evidence behind the raw disclosure.
func TestParseEmptyListIsNoResults(t *testing.T) {
	for _, name := range []string{"empty-list.json", "empty-search.json"} {
		t.Run(name, func(t *testing.T) {
			got := Parse(fixture(t, name))
			if got.Kind != KindEmpty {
				t.Fatalf("kind = %q, want %q", got.Kind, KindEmpty)
			}
			if got.Text != EmptyText {
				t.Errorf("text = %q, want %q", got.Text, EmptyText)
			}
			if got.Outcome != EmptyText {
				t.Errorf("outcome = %q, want %q", got.Outcome, EmptyText)
			}
			if len(got.Items) != 0 {
				t.Errorf("items = %+v, want none", got.Items)
			}
		})
	}

	// The same shape without an envelope: a bare empty array.
	if got := Parse(`[]`); got.Kind != KindEmpty {
		t.Errorf("kind = %q, want %q for a bare empty list", got.Kind, KindEmpty)
	}
	if got := Parse(`{"ok": true, "data": []}`); got.Kind != KindEmpty {
		t.Errorf("kind = %q, want %q for an empty data list", got.Kind, KindEmpty)
	}
}

// TestParseEmptyListBesideTextPrefersTheText: an empty list beside a real body of
// text is not the answer, so text keeps its chance first (#2277).
func TestParseEmptyListBesideTextPrefersTheText(t *testing.T) {
	message := strings.TrimSpace(strings.Repeat("the search covered every project; ", 6))
	got := Parse(`{"ok": true, "data": {"documents": [], "message": "` + message + `"}}`)
	if got.Kind != KindText {
		t.Fatalf("kind = %q, want %q", got.Kind, KindText)
	}
	if got.Text == EmptyText || !strings.Contains(got.Text, "covered every project") {
		t.Errorf("text = %q, want the readable message", got.Text)
	}
}

// parseBatch parses a payload and returns its tally, failing when the payload was
// not read as a batch.
func parseBatch(t *testing.T, raw string) *BatchTally {
	t.Helper()
	got := Parse(raw)
	if got == nil || got.Batch == nil {
		t.Fatalf("Parse(%s).Batch = nil, want a tally", raw)
	}
	return got.Batch
}

// TestParseBatchCreateTally: entity-create answers with data.results plus a meta
// tally beside the envelope; the readable list keeps only created records, so the
// counts come from meta (#2277).
func TestParseBatchCreateTally(t *testing.T) {
	got := Parse(fixture(t, "entity-create.json"))
	if got.Batch == nil {
		t.Fatal("batch = nil, want the create's counts")
	}
	if want := (BatchTally{Created: 1, Failed: 0, Similar: 0}); *got.Batch != want {
		t.Errorf("batch = %+v, want %+v", *got.Batch, want)
	}
	if got.Batch.IsFailure() {
		t.Error("IsFailure() = true, want false for a clean batch")
	}
	if text := got.Batch.Text(); text != "1 created, 0 failed" {
		t.Errorf("text = %q, want %q", text, "1 created, 0 failed")
	}
	// The body is still the created records.
	if got.Kind != KindRecords || len(got.Items) != 1 {
		t.Errorf("body = %+v, want the one created record", got)
	}
}

// TestParseFailedBatchReadsAsAFailure: the real envelope for a half-failed batch
// is ok:false plus the error summary, so the body is that message; the tally is
// what keeps the created/failed counts visible and marks the batch serious
// (#2277).
func TestParseFailedBatchReadsAsAFailure(t *testing.T) {
	got := Parse(fixture(t, "entity-create-partial.json"))
	if got.Batch == nil {
		t.Fatal("batch = nil, want the create's counts")
	}
	if got.Batch.Created != 1 || got.Batch.Failed != 1 || got.Batch.Similar != 0 {
		t.Errorf("batch = %+v, want 1 created, 1 failed, 0 similar", *got.Batch)
	}
	if !got.Batch.IsFailure() {
		t.Error("IsFailure() = false, want true when an item failed")
	}
	if text := got.Batch.Text(); text != "1 created, 1 failed" {
		t.Errorf("text = %q, want %q", text, "1 created, 1 failed")
	}
	if got.Kind != KindText || got.Error != "1 of 2 entities failed to create" {
		t.Errorf("body = %+v, want the envelope's error text", got)
	}
}

// TestParseBatchNearDuplicateCount: similar_count is not derivable from the
// records, so this pins that the tally comes from meta, not from counting items;
// the copy inflects near-duplicate(s) (#2277).
func TestParseBatchNearDuplicateCount(t *testing.T) {
	got := Parse(fixture(t, "entity-create-similar.json"))
	if got.Batch == nil {
		t.Fatal("batch = nil, want the create's counts")
	}
	if want := (BatchTally{Created: 2, Failed: 0, Similar: 3}); *got.Batch != want {
		t.Errorf("batch = %+v, want %+v", *got.Batch, want)
	}
	if got.Batch.IsFailure() {
		t.Error("IsFailure() = true, want false for a clean batch")
	}
	if text := got.Batch.Text(); text != "2 created, 0 failed, 3 near-duplicates" {
		t.Errorf("text = %q, want %q", text, "2 created, 0 failed, 3 near-duplicates")
	}
	if text := (&BatchTally{Created: 1, Failed: 0, Similar: 1}).Text(); text != "1 created, 0 failed, 1 near-duplicate" {
		t.Errorf("singular text = %q, want %q", text, "1 created, 0 failed, 1 near-duplicate")
	}
}

// TestParseBatchTallyFromItemsWhenMetaIsMissing: per-item ok flags are the
// fallback when the payload carries no tally (#2277).
func TestParseBatchTallyFromItemsWhenMetaIsMissing(t *testing.T) {
	raw := `{"ok": true, "data": {"results": [{"ok": true, "index": 0, "entity": {"id": "e1", "type": "Note"}}, {"ok": false, "index": 1}]}}`
	tally := parseBatch(t, raw)
	if want := (BatchTally{Created: 1, Failed: 1, Similar: 0}); *tally != want {
		t.Errorf("batch = %+v, want %+v", *tally, want)
	}
	if !tally.IsFailure() {
		t.Error("IsFailure() = false, want true when an item failed")
	}
}

// TestParseBatchAcceptsResultWrapper: the MCP envelope uses data; the agent's
// wrapper uses result, and meta may sit either beside it or inside it (#2277).
func TestParseBatchAcceptsResultWrapper(t *testing.T) {
	beside := parseBatch(t, `{"result": {"results": [{"ok": true, "index": 0}, {"ok": false, "index": 1}]}, "meta": {"created": 1, "failed": 1, "total": 2, "similar_count": 0}}`)
	if want := (BatchTally{Created: 1, Failed: 1, Similar: 0}); *beside != want {
		t.Errorf("batch beside result = %+v, want %+v", *beside, want)
	}

	inside := parseBatch(t, `{"result": {"results": [{"ok": true, "index": 0}], "meta": {"created": 1, "failed": 0, "similar_count": 2}}}`)
	if want := (BatchTally{Created: 1, Failed: 0, Similar: 2}); *inside != want {
		t.Errorf("batch inside result = %+v, want %+v", *inside, want)
	}
}

// TestParseBatchCountsInlineRelationshipFailures: an entity-create batch can
// report ok=false solely because an inline relationship failed (#401) while
// meta.failed stays 0. The tally must still read as a failure, and an entity
// whose relationship failed is still one created entity.
func TestParseBatchCountsInlineRelationshipFailures(t *testing.T) {
	tally := parseBatch(t, `{"ok": false, "data": {"results": [{"ok": true, "index": 0, "entity": {"id": "e1", "type": "Note"}, "relationships_failed": 1}]}, "meta": {"created": 1, "failed": 0, "relationships_failed": 1, "similar_count": 0}}`)
	if want := (BatchTally{Created: 1, Failed: 1, Similar: 0}); *tally != want {
		t.Errorf("batch = %+v, want %+v", *tally, want)
	}
	if !tally.IsFailure() {
		t.Error("IsFailure() = false, want true when an inline relationship failed")
	}

	// Without a meta tally the per-item count is the fallback.
	fromItems := parseBatch(t, `{"ok": false, "data": {"results": [{"ok": true, "index": 0, "entity": {"id": "e1", "type": "Note"}, "relationships_failed": 2}]}}`)
	if want := (BatchTally{Created: 1, Failed: 2, Similar: 0}); *fromItems != want {
		t.Errorf("batch from items = %+v, want %+v", *fromItems, want)
	}
}

// TestParseSearchResultsAreNotABatch: a plain results array is a search, not a
// batch, and must never be reported as "0 created" (#2277).
func TestParseSearchResultsAreNotABatch(t *testing.T) {
	for _, name := range []string{"envelope-search.json", "web-search.json", "entity-query.json", "list-documents.json"} {
		t.Run(name, func(t *testing.T) {
			if got := Parse(fixture(t, name)); got.Batch != nil {
				t.Errorf("batch = %+v, want nil", got.Batch)
			}
		})
	}
}

// TestBatchTallyJSONCarriesTheWordedSummary: the live chat chip renders the
// server's wording, so the tally travels with its one-line summary rather than
// being re-derived in JavaScript (#2277).
func TestBatchTallyJSONCarriesTheWordedSummary(t *testing.T) {
	encoded, err := json.Marshal(&BatchTally{Created: 1, Failed: 1, Similar: 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"created":1,"failed":1,"similar":0,"text":"1 created, 1 failed"}`
	if string(encoded) != want {
		t.Errorf("json = %s, want %s", encoded, want)
	}

	var decoded BatchTally
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := (BatchTally{Created: 1, Failed: 1, Similar: 0}); decoded != want {
		t.Errorf("round trip = %+v, want %+v", decoded, want)
	}
}

func TestParseRecordDetailAndLink(t *testing.T) {
	got := Parse(fixture(t, "envelope-search.json"))
	if items := got.Items; len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	} else {
		if items[0].Detail != "Engineer" {
			t.Errorf("items[0].Detail = %q, want %q", items[0].Detail, "Engineer")
		}
		if items[1].Title != "retrieval" {
			t.Errorf("items[1].Title = %q, want %q", items[1].Title, "retrieval")
		}
	}

	web := Parse(fixture(t, "web-search.json"))
	if len(web.Items) != 1 {
		t.Fatalf("web items = %d, want 1", len(web.Items))
	}
	want := "https://developer.apple.com/documentation/swiftui"
	if web.Items[0].Link != want {
		t.Errorf("link = %q, want %q", web.Items[0].Link, want)
	}
	if web.Items[0].Detail != "Declarative UI" {
		t.Errorf("detail = %q, want %q", web.Items[0].Detail, "Declarative UI")
	}
}

// TestParseFields pins the compact key/value reading: sorted labels, a null that
// is a value rather than a reason to give up, and numbers and booleans rendered
// as written.
func TestParseFields(t *testing.T) {
	got := Parse(fixture(t, "record-scalars.json"))
	want := []Field{
		{Label: "count", Value: "3"},
		{Label: "error", Value: "—"},
		{Label: "ok", Value: "true"},
		{Label: "paused", Value: "false"},
		{Label: "status", Value: "ok"},
	}
	if len(got.Fields) != len(want) {
		t.Fatalf("fields = %d, want %d: %+v", len(got.Fields), len(want), got.Fields)
	}
	for i := range want {
		if got.Fields[i] != want[i] {
			t.Errorf("fields[%d] = %+v, want %+v", i, got.Fields[i], want[i])
		}
	}
}

// TestParseErrorPrecedence: the failure is the answer, even when the tool also
// returned a partial payload beside it.
func TestParseErrorPrecedence(t *testing.T) {
	raw := `{"ok": false, "error": "boom", "data": {"documents": [{"title": "A"}]}}`
	got := Parse(raw)
	if got.Kind != KindText {
		t.Errorf("kind = %q, want %q", got.Kind, KindText)
	}
	if got.Error != "boom" {
		t.Errorf("error = %q, want %q", got.Error, "boom")
	}
	if got.Outcome != "boom" {
		t.Errorf("outcome = %q, want %q", got.Outcome, "boom")
	}

	// A failed result whose only message is beside "ok".
	got = Parse(`{"ok": false, "message": "bad input"}`)
	if got.Error != "bad input" {
		t.Errorf("error = %q, want %q", got.Error, "bad input")
	}

	// A failure with nothing to say is not an error message. It is still a record
	// with one field, and calling it "1 field" is the truth.
	got = Parse(`{"ok": false}`)
	if got.Error != "" {
		t.Errorf("error = %q, want empty", got.Error)
	}
	if got.Outcome != "1 field" {
		t.Errorf("outcome = %q, want %q", got.Outcome, "1 field")
	}
}

// TestParseUnwrapsNestedEnvelopes: the payload can sit two envelope levels down
// (search-hybrid wraps its list in a second "data"), and the unwrapping is
// bounded.
func TestParseUnwrapsNestedEnvelopes(t *testing.T) {
	got := Parse(`{"ok": true, "data": {"ok": true, "result": {"items": [{"title": "A"}]}}}`)
	if got.Kind != KindRecords || len(got.Items) != 1 || got.Items[0].Title != "A" {
		t.Fatalf("got %+v, want one record titled A", got)
	}

	// A scalar under data is a body of text, not a field list.
	got = Parse(`{"ok": true, "data": "the answer"}`)
	if got.Kind != KindText || got.Text != "the answer" {
		t.Fatalf("got %+v, want text body %q", got, "the answer")
	}
}

// TestParseLiftsWrapperWithoutCopyingIt: {"object": {…}, "score": 0.5} reads as a
// record, and a wrapped single record reads as its own fields — the wrapper key
// never comes back as a nested field.
func TestParseLiftsWrapperWithoutCopyingIt(t *testing.T) {
	got := Parse(`{"results": [{"object": {"id": "e1", "name": "Alice"}, "score": 0.5}]}`)
	if got.Kind != KindRecords || len(got.Items) != 1 {
		t.Fatalf("got %+v, want one record", got)
	}
	if got.Items[0].Title != "Alice" {
		t.Errorf("title = %q, want %q", got.Items[0].Title, "Alice")
	}

	single := Parse(`{"object": {"id": "e1", "type": "Note"}}`)
	if single.Kind != KindFields {
		t.Fatalf("got %+v, want a field list", single)
	}
	for _, field := range single.Fields {
		if field.Label == "object" {
			t.Errorf("wrapper key came back as a field: %+v", single.Fields)
		}
	}
	if len(single.Fields) != 2 {
		t.Errorf("fields = %+v, want id and type only", single.Fields)
	}
}

// TestParseEmptyObject: an object with nothing in it has no body; the payload is
// the presentation.
func TestParseEmptyObject(t *testing.T) {
	got := Parse(`{}`)
	if got.Kind != "" {
		t.Errorf("kind = %q, want no body", got.Kind)
	}
}

func TestParseTruncatesFieldValues(t *testing.T) {
	long := strings.Repeat("v", fieldValueWidth+50)
	got := Parse(`{"a": "` + long + `"}`)
	if got.Kind != KindFields || len(got.Fields) != 1 {
		t.Fatalf("got %+v, want one field", got)
	}
	value := got.Fields[0].Value
	if !strings.HasSuffix(value, "…") {
		t.Errorf("value does not mark its cut: %q", value)
	}
	if n := len([]rune(value)); n != fieldValueWidth+1 {
		t.Errorf("value runes = %d, want %d", n, fieldValueWidth+1)
	}
}

// TestParseTrailingContent: two JSON values in one result is not a result the
// parser understands, so the text is the presentation.
func TestParseTrailingContent(t *testing.T) {
	got := Parse(`{"a": 1}{"b": 2}`)
	if got.Kind != "" {
		t.Errorf("kind = %q, want no body", got.Kind)
	}
}

// TestParseFailureLineIsBounded: the collapsed line is one line of 120
// characters even when the failure message is not — a 700-character error used to
// be handed straight to the chip and pushed the row out of shape. The full
// message stays in Error for the opened step.
func TestParseFailureLineIsBounded(t *testing.T) {
	long := strings.Repeat("upstream refused the call; ", 30) // ~780 characters
	got := Parse(`{"ok": false, "error": "` + strings.TrimSpace(long) + `"}`)

	if n := len([]rune(got.Outcome)); n != outcomeTextWidth+1 {
		t.Errorf("outcome runes = %d, want %d (120 + ellipsis)", n, outcomeTextWidth+1)
	}
	if !strings.HasSuffix(got.Outcome, "…") {
		t.Errorf("outcome = %q, want a trailing ellipsis", got.Outcome)
	}
	if strings.ContainsAny(got.Outcome, "\n\r") {
		t.Errorf("outcome = %q, want one line", got.Outcome)
	}
	if len(got.Error) <= outcomeTextWidth {
		t.Errorf("error = %d chars, want the full message", len(got.Error))
	}

	// A message that arrives on several lines still collapses to one line.
	got = Parse(`{"ok": false, "error": "first line\nsecond line"}`)
	if got.Outcome != "first line" {
		t.Errorf("outcome = %q, want %q", got.Outcome, "first line")
	}
}

func TestOutcomeWording(t *testing.T) {
	tests := []struct{ raw, want string }{
		{`{"items": [{"title": "A"}]}`, "1 record"},
		{`{"items": [{"title": "A"}, {"title": "B"}]}`, "2 records"},
		{`{"a": 1}`, "1 field"},
		{`{"a": 1, "b": 2}`, "2 fields"},
	}
	for _, tt := range tests {
		if got := Parse(tt.raw).Outcome; got != tt.want {
			t.Errorf("Parse(%s).Outcome = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
