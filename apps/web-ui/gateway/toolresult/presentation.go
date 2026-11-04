// Package toolresult turns a tool's raw JSON result into the two things the web
// console actually shows: a category (verb and icon, derived from the tool's
// name) and a presentation of the result (a list of records, one record's
// fields, or a body of text, plus the bounded raw payload).
//
// It exists because the web had two tool renderers — a Go template for recorded
// sessions and a JavaScript chip for the live chat — and no shared answer to
// "what does this result say". Both now render this structure, so the reading
// rules live in one tested place.
//
// A tool result is a JSON document whose shape is the tool's own business, so
// this holds no per-tool knowledge: it looks for the few shapes a person can
// read and otherwise leaves the body empty, so the raw payload speaks for
// itself. Those shapes are a list of records, one record's fields, a body of
// text, and an empty list ("No results"); a batch create's counts travel beside
// the body. Two of them are handled explicitly because the server produces them
// for most tools: the MCP result envelope ({"ok": true, "data": …}) and a list
// element that wraps its record ({"object": {…}, "score": …}).
package toolresult

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits, matching the shipped iOS client so both surfaces tell the reader the
// same thing about the same payload.
const (
	// ItemLimit is how many records a list shows before it summarizes the rest.
	ItemLimit = 5
	// FieldLimit is how many fields a single record shows.
	FieldLimit = 8
	// TextLineLimit is how many lines of body text a result shows.
	TextLineLimit = 8
	// RawLineLimit is how many lines of raw JSON the disclosure reveals.
	RawLineLimit = 40
	// LineWidth bounds one displayed line, so a payload carrying a huge string on
	// one line (a command's output, a document body) cannot form an endless row.
	LineWidth = 400
	// ParseLimit is the size above which a payload is not parsed at all: a
	// quarter-megabyte result is a wall no presentation fixes.
	ParseLimit = 200000
	// textMinimumLength is below which a "long text" is just another field.
	textMinimumLength = 120
	// fieldValueWidth bounds one field's rendered value.
	fieldValueWidth = 240
	// outcomeTextWidth bounds the first line of a text result in the collapsed row.
	outcomeTextWidth = 120
)

// Kind is the shape a presentation took.
type Kind string

const (
	// KindRecords is a list of records: search hits, entities, documents, keys.
	KindRecords Kind = "records"
	// KindFields is one record's scalar fields.
	KindFields Kind = "fields"
	// KindText is a body of text.
	KindText Kind = "text"
	// KindEmpty is a result whose list is empty: "No results" is the whole
	// answer, and the envelope's total: 0 / has_more are noise (#2277).
	KindEmpty Kind = "empty"
)

// EmptyText is what an empty list result says. The copy lives here for the same
// reason Outcome does: the two web surfaces must not word it differently, and it
// is the same decision the iOS client makes with sessions.detail.result.empty.
const EmptyText = "No results"

// Item is one record of a list.
type Item struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	// Link is an http(s) URL the record carries; anything else stays in Detail.
	Link string `json:"link,omitempty"`
}

// Field is one field of a single record.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// BatchTally is a batch create's counts, which the per-record list cannot say: a
// successful record has a name, a failed one is skipped as untitled, and the
// meta tally would otherwise live only in the raw JSON (#2277).
type BatchTally struct {
	Created int `json:"created"`
	Failed  int `json:"failed"`
	Similar int `json:"similar"`
}

// IsFailure reports whether an item failed. A failed item makes the batch read
// as a failure, however clean the list of created records looks.
func (b *BatchTally) IsFailure() bool { return b != nil && b.Failed > 0 }

// Text words the counts as the body's one-line summary. The copy matches the iOS
// client: created and failed always, near-duplicates only when there are any,
// with the count's inflection.
func (b *BatchTally) Text() string {
	if b == nil {
		return ""
	}
	line := fmt.Sprintf("%d created, %d failed", b.Created, b.Failed)
	if b.Similar > 0 {
		line += ", " + plural(b.Similar, "near-duplicate")
	}
	return line
}

// MarshalJSON carries the one-line summary beside the counts, so the live chat
// chip renders the same wording as the recorded page instead of re-deriving it.
func (b BatchTally) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Created int    `json:"created"`
		Failed  int    `json:"failed"`
		Similar int    `json:"similar"`
		Text    string `json:"text"`
	}{b.Created, b.Failed, b.Similar, b.Text()})
}

// UnmarshalJSON reads the counts; the summary is derived, not stored.
func (b *BatchTally) UnmarshalJSON(data []byte) error {
	var counts struct {
		Created int `json:"created"`
		Failed  int `json:"failed"`
		Similar int `json:"similar"`
	}
	if err := json.Unmarshal(data, &counts); err != nil {
		return err
	}
	b.Created, b.Failed, b.Similar = counts.Created, counts.Failed, counts.Similar
	return nil
}

// Presentation is what a renderer needs to show a settled tool's result. It is
// a view model, not the payload: the raw JSON travels beside it, bounded.
type Presentation struct {
	Kind Kind `json:"kind"`

	// Items is set when Kind is KindRecords.
	Items []Item `json:"items,omitempty"`
	// Fields is set when Kind is KindFields.
	Fields []Field `json:"fields,omitempty"`
	// Text is set when Kind is KindText, and carries the copy when Kind is
	// KindEmpty.
	Text string `json:"text,omitempty"`

	// Batch is the counts of a batch create, when the payload is one. A plain
	// search's results array is not a batch.
	Batch *BatchTally `json:"batch,omitempty"`

	// MoreRecords, MoreFields, MoreLines and MoreCharacters count what the caps
	// hid. A renderer that shows a prefix without these turns a cut into a lie.
	MoreRecords    int `json:"moreRecords,omitempty"`
	MoreFields     int `json:"moreFields,omitempty"`
	MoreLines      int `json:"moreLines,omitempty"`
	MoreCharacters int `json:"moreCharacters,omitempty"`

	// Error is the message a failed result carries, already extracted from the
	// envelope. It is the answer when a step failed.
	Error string `json:"error,omitempty"`

	// Outcome is the one line the collapsed step shows: how many records came
	// back, "No results" for an empty list, the first line of the text, the
	// failure message, or empty when the result has no readable shape. It is
	// derived here so the two web surfaces cannot word it differently.
	Outcome string `json:"outcome,omitempty"`

	// Raw is the payload, pretty-printed and bounded, and RawMoreLines /
	// RawMoreCharacters say what that bounding left out.
	Raw               string `json:"raw,omitempty"`
	RawMoreLines      int    `json:"rawMoreLines,omitempty"`
	RawMoreCharacters int    `json:"rawMoreCharacters,omitempty"`
}

// HasBody reports whether the result holds something to read beyond its raw text.
func (p *Presentation) HasBody() bool { return p != nil && p.Kind != "" }

// Parse derives the presentation of a tool result. A nil presentation means
// there was no result at all.
//
// A payload above ParseLimit is never parsed into a readable body: it is
// pretty-printed as-is (when it is valid JSON) and bounded, with the remainder
// counted, so an oversized result still says what it left out.
func Parse(raw string) *Presentation {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	var value any
	parsed := false
	// The limit is counted in characters, not bytes, so a payload of multi-byte
	// text is not refused for being smaller than the reader thinks.
	if utf8.RuneCountInString(trimmed) <= ParseLimit {
		value, parsed = decode(trimmed)
	}

	printable := trimmed
	if parsed {
		if pretty, ok := prettyPrinted(value); ok {
			printable = pretty
		}
	}

	bounded := Lines(printable, RawLineLimit, LineWidth)
	p := &Presentation{
		Raw:               bounded.Text,
		RawMoreLines:      bounded.MoreLines,
		RawMoreCharacters: bounded.MoreCharacters,
	}
	if !parsed {
		// Oversized or not JSON: there is no body to derive, and saying so is the
		// point of the remaining counts.
		return p
	}

	body := bodyFrom(value)
	p.Batch = batchTally(value)
	if body == nil {
		return p
	}
	p.Kind = body.kind
	p.Items = body.items
	p.Fields = body.fields
	p.Text = body.text
	p.MoreRecords = body.moreRecords
	p.MoreFields = body.moreFields
	p.MoreLines = body.moreLines
	p.MoreCharacters = body.moreCharacters
	p.Error = body.err
	p.Outcome = outcomeOf(body)
	return p
}

// decode reads one JSON value, keeping numbers in their written form so a count
// renders as 5 rather than 5.000000.
func decode(raw string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err == nil {
		// Trailing content: more than one value, so this is not the single result
		// the caller believes it is.
		return nil, false
	}
	return value, true
}

// prettyPrinted renders a decoded JSON value the way the reader should see it.
// A bare scalar is left as the original text (quoting it would add nothing), and
// numbers keep their written form.
func prettyPrinted(value any) (string, bool) {
	switch value.(type) {
	case map[string]any, []any:
		out, err := json.MarshalIndent(normalize(value), "", "  ")
		if err != nil {
			return "", false
		}
		return string(out), true
	default:
		return "", false
	}
}

// normalize turns json.Number back into a number json.Marshal can emit as-is.
func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, inner := range v {
			out[key] = normalize(inner)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, inner := range v {
			out[i] = normalize(inner)
		}
		return out
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	default:
		return value
	}
}

// outcomeOf words the collapsed one-line result. Wording lives here so the live
// chat chip and the recorded-session row cannot disagree.
func outcomeOf(body *parsedBody) string {
	if body.err != "" {
		// The failure message is the answer, and it is a collapsed line like any
		// other: one line, 120 characters, ellipsis when cut. The full message
		// stays in Error, which only the opened step shows.
		return firstLine(body.err, outcomeTextWidth)
	}
	switch body.kind {
	case KindRecords:
		total := len(body.items) + body.moreRecords
		return plural(total, "record")
	case KindFields:
		total := len(body.fields) + body.moreFields
		return plural(total, "field")
	case KindText:
		return firstLine(body.text, outcomeTextWidth)
	case KindEmpty:
		return EmptyText
	default:
		return ""
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// firstLine is the text's first non-empty line, bounded, with an ellipsis when
// it had to be cut.
func firstLine(text string, width int) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > width {
			return string(runes[:width]) + "…"
		}
		return line
	}
	return ""
}

// scalarString renders one scalar value for a field list. Null is a value the
// record has ("—"), not a reason to abandon the field list.
func scalarString(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "—", true
	case string:
		if runes := []rune(v); len(runes) > fieldValueWidth {
			return string(runes[:fieldValueWidth]) + "…", true
		}
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return strconv.FormatInt(i, 10), true
		}
		if f, err := v.Float64(); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64), true
		}
		return v.String(), true
	default:
		// A nested structure is not a field value: the record is not a field list.
		return "", false
	}
}
