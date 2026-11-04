package agents

import "github.com/emergent-company/emergent.memory/pkg/llmjson"

// decodeModelJSON is the extraction domain's alias for the shared LLM JSON
// parser. Keeping the wrapper local lets the two extraction parsers share one
// implementation without exporting agent internals.
func decodeModelJSON(raw string, dest interface{}) error {
	return llmjson.Decode(raw, dest)
}

// decodeModelJSONMeta also reports whether the response was repaired or
// truncated, so callers can reject a truncation that salvaged zero entities.
func decodeModelJSONMeta(raw string, dest interface{}) (repaired, truncated bool, err error) {
	return llmjson.DecodeWithMeta(raw, dest)
}

// previewModelOutput bounds model output for error messages.
func previewModelOutput(raw string, limit int) string {
	return llmjson.Preview(raw, limit)
}
