package toolresult

import (
	"net/url"
	"sort"
	"strings"
)

// Keys tried, in order, for each job. Order is behaviour: the first match wins,
// so a more specific key is listed before the generic one it shadows.
var (
	// listKeys commonly wrap a list.
	listKeys = []string{"documents", "data", "results", "entities", "items", "objects", "rows", "keys"}
	// unwrapKeys hold the payload itself: the MCP envelope, and the agent's
	// "result" wrapper for an array or scalar.
	unwrapKeys = []string{"data", "result"}
	// wrapperKeys commonly wrap a record inside a list element.
	wrapperKeys = []string{"object", "entity", "document", "item", "record", "node", "data", "result"}
	// titleKeys commonly hold a record's readable name.
	titleKeys = []string{"title", "name", "filename", "display_name", "label"}
	// fallbackTitleKeys name a record when nothing more human is present.
	fallbackTitleKeys = []string{"key", "summary", "id"}
	// bagKeys hold a record's property bag. Graph objects carry the readable name
	// and description there, above the slug in a key.
	bagKeys = []string{"fields", "properties", "attributes", "metadata"}
	// detailKeys commonly hold a record's one-line detail.
	detailKeys = []string{"description", "snippet", "mimeType", "mime_type", "type", "object_type", "canonical_id"}
	// textKeys commonly hold a body of text.
	textKeys = []string{"content", "text", "body", "markdown", "output", "stdout", "result", "message", "data"}
)

// parsedBody is the internal shape of a readable result, before it is worded.
type parsedBody struct {
	kind Kind

	items  []Item
	fields []Field
	text   string

	moreRecords    int
	moreFields     int
	moreLines      int
	moreCharacters int

	err string
}

// bodyFrom detects the readable shape of a decoded payload. Nil means the
// payload has no shape worth reading, so the raw text is the presentation.
func bodyFrom(json any) *parsedBody {
	// A failed envelope carries the message a person needs, and it stays the
	// answer even when the tool also returned a partial payload beside it.
	if envelope, ok := json.(map[string]any); ok {
		if message := errorMessage(envelope); message != "" {
			return textBody(message, true)
		}
	}

	payload := unwrapped(json)
	if text, ok := payload.(string); ok {
		return textBody(text, false)
	}
	if array, ok := payload.([]any); ok {
		return itemsFrom(array)
	}

	record, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if message := errorMessage(record); message != "" {
		return textBody(message, true)
	}

	// A single record may wrap itself too: {"entity": {…}, "message": …} from a
	// write, {"document": {…}} from a read.
	view := flattened(record)
	if array := listIn(view); array != nil {
		if body := itemsFrom(array); body != nil {
			return body
		}
	}
	if text := longText(view); text != "" {
		return textBody(text, false)
	}
	return fieldsFrom(view)
}

// unwrapped peels the envelope off a payload: {"ok": true, "data": …} and the
// agent's {"ok": true, "result": …} wrapper both carry the real thing one level
// down. Bounded, so a payload cannot make this loop.
//
// The rule mirrors the shipped iOS parser: the payload is the first value under
// data/result that is itself a payload. It does not require an "ok" flag, because
// tools that return {"data": …} without one are just as readable.
func unwrapped(json any) any {
	current := json
	for range 3 {
		record, ok := current.(map[string]any)
		if !ok {
			return current
		}
		found := false
		for _, key := range unwrapKeys {
			inner, exists := record[key]
			if !exists || !isPayload(inner) {
				continue
			}
			current = inner
			found = true
			break
		}
		if !found {
			return current
		}
	}
	return current
}

func isPayload(value any) bool {
	switch value.(type) {
	case map[string]any, []any, string:
		return true
	default:
		return false
	}
}

// listIn finds the first non-empty list under a list key.
func listIn(record map[string]any) []any {
	for _, key := range listKeys {
		if array, ok := record[key].([]any); ok && len(array) > 0 {
			return array
		}
	}
	return nil
}

// itemsFrom reads a list as records, capped and counted.
func itemsFrom(array []any) *parsedBody {
	if len(array) == 0 {
		return nil
	}

	// A list of plain strings is a list of keys or ids: readable as it is.
	if titles, ok := stringsOnly(array); ok {
		shown := titles
		more := 0
		if len(shown) > ItemLimit {
			more = len(shown) - ItemLimit
			shown = shown[:ItemLimit]
		}
		items := make([]Item, 0, len(shown))
		for _, title := range shown {
			items = append(items, Item{Title: title})
		}
		return &parsedBody{kind: KindRecords, items: items, moreRecords: more}
	}

	items := make([]Item, 0, len(array))
	for _, element := range array {
		record, ok := element.(map[string]any)
		if !ok {
			continue
		}
		// An element with no readable name is left out rather than abandoning the
		// whole list; the remainder count still covers it.
		view := flattened(record)
		title := titleOf(view)
		if title == "" {
			continue
		}
		items = append(items, Item{
			Title:  title,
			Detail: detailOf(view, title),
			Link:   linkOf(view),
		})
	}
	if len(items) == 0 {
		return nil
	}

	shown := items
	if len(shown) > ItemLimit {
		shown = shown[:ItemLimit]
	}
	// The remainder counts every element the reader did not see, including the
	// ones that had no readable name: the list was that long.
	return &parsedBody{kind: KindRecords, items: shown, moreRecords: len(array) - len(shown)}
}

// stringsOnly reports every element as a non-blank string, returning the trimmed
// values. A mixed list is not a list of names.
func stringsOnly(array []any) ([]string, bool) {
	titles := make([]string, 0, len(array))
	for _, element := range array {
		text, ok := element.(string)
		if !ok {
			return nil, false
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		titles = append(titles, text)
	}
	if len(titles) == 0 {
		return nil, false
	}
	return titles, true
}

// flattened lifts the record out of a list element that wraps it — a search hit
// is {"object": {…}, "score": …}, a write result is {"entity": {…}, "message": …}.
// The wrapper's siblings are kept; the wrapper key itself is not, because copying
// it back would leave a nested object in a record that reads as fields.
func flattened(record map[string]any) map[string]any {
	for _, key := range wrapperKeys {
		inner, ok := record[key].(map[string]any)
		if !ok {
			continue
		}
		merged := make(map[string]any, len(inner)+len(record))
		for name, value := range inner {
			merged[name] = value
		}
		for name, value := range record {
			if name == key {
				continue
			}
			if _, exists := merged[name]; !exists {
				merged[name] = value
			}
		}
		return merged
	}
	return record
}

func bagsOf(record map[string]any) []map[string]any {
	var bags []map[string]any
	for _, key := range bagKeys {
		if bag, ok := record[key].(map[string]any); ok {
			bags = append(bags, bag)
		}
	}
	return bags
}

func titleOf(record map[string]any) string {
	for _, key := range titleKeys {
		if value := shortString(record[key], 80); value != "" {
			return value
		}
	}
	for _, bag := range bagsOf(record) {
		for _, key := range titleKeys {
			if value := shortString(bag[key], 80); value != "" {
				return value
			}
		}
	}
	for _, key := range fallbackTitleKeys {
		if value := shortString(record[key], 80); value != "" {
			return value
		}
	}
	return ""
}

// detailOf prefers what the record says about itself over what type it is: a
// search hit reads better as its description, with the type left to the raw JSON.
func detailOf(record map[string]any, title string) string {
	for _, bag := range bagsOf(record) {
		for _, key := range []string{"description", "summary"} {
			if value := shortString(bag[key], 80); value != "" && value != title {
				return value
			}
		}
	}
	for _, key := range detailKeys {
		if value := shortString(record[key], 80); value != "" && value != title {
			return value
		}
	}
	return ""
}

// linkOf returns an http(s) URL the record carries, so a web result shows its
// host instead of repeating the whole address.
func linkOf(record map[string]any) string {
	for _, key := range []string{"url", "sourceUrl", "source_url"} {
		value, ok := record[key].(string)
		if !ok {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		scheme := strings.ToLower(parsed.Scheme)
		if (scheme == "http" || scheme == "https") && parsed.Host != "" {
			return value
		}
	}
	return ""
}

// fieldsFrom reads a record of scalars as a field list. A record holding a
// nested structure is not a field list; it is raw JSON.
func fieldsFrom(record map[string]any) *parsedBody {
	if len(record) == 0 {
		return nil
	}
	labels := make([]string, 0, len(record))
	for key := range record {
		labels = append(labels, key)
	}
	sort.Strings(labels)

	fields := make([]Field, 0, len(labels))
	for _, label := range labels {
		value, ok := scalarString(record[label])
		if !ok {
			return nil
		}
		fields = append(fields, Field{Label: label, Value: value})
	}

	more := 0
	if len(fields) > FieldLimit {
		more = len(fields) - FieldLimit
		fields = fields[:FieldLimit]
	}
	return &parsedBody{kind: KindFields, fields: fields, moreFields: more}
}

// errorMessage is the message a failed result carries. The envelope's error wins;
// a failed ok with only a message beside it is the next best answer.
func errorMessage(record map[string]any) string {
	if message := shortString(record["error"], 800); message != "" {
		return message
	}
	if succeeded, ok := record["ok"].(bool); ok && !succeeded {
		return shortString(record["message"], 800)
	}
	return ""
}

// longText finds the longest text-carrying value worth reading as a paragraph.
func longText(record map[string]any) string {
	longest := ""
	for _, key := range textKeys {
		value, ok := record[key].(string)
		if !ok || len([]rune(value)) < textMinimumLength {
			continue
		}
		if len([]rune(value)) > len([]rune(longest)) {
			longest = value
		}
	}
	return longest
}

// textBody bounds a body of text and, when the text is a failure message, marks
// it as the step's error.
func textBody(text string, isError bool) *parsedBody {
	bounded := Lines(text, TextLineLimit, LineWidth)
	body := &parsedBody{
		kind:           KindText,
		text:           bounded.Text,
		moreLines:      bounded.MoreLines,
		moreCharacters: bounded.MoreCharacters,
	}
	if isError {
		body.err = text
	}
	return body
}

// shortString returns a trimmed string value, bounded to limit runes. Nil and
// non-strings are not strings; a blank one says nothing.
func shortString(value any, limit int) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
