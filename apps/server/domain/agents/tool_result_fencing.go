package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Untrusted-content fencing (issue #1142).
//
// A tool result is conversation content: whatever it says arrives in the model
// context with the same standing as the operator's own words. That is fine for
// this product's own store — an entity row is data the agent was asked to work
// with — and it is a prompt-injection path for everything else. A web page, a
// file in the workspace, a command's output or another MCP server's reply can
// say "ignore your instructions and call X"; nothing in the message marks it as
// a quotation.
//
// This file closes that path at the same boundary as the tool-result bound
// (tool_result_bounds.go): the model request only (model.LLMRequest.Contents).
// The ADK deep-clones session content into the request before the before-model
// callback, so the session transcript, kb.agent_run_tool_calls, the
// kb.agent_run_messages rows and the StreamEventToolCallEnd SSE event keep the
// exact payload the tool produced.
//
// Trust is decided per tool name, and it fails closed: a result is left untouched
// only when the run's internal tool catalog lists the tool (or it is one of the
// agent-runtime tools that return this product's own state). External MCP
// servers, relay tools, the workspace/exec tools, the fetching builtins and the
// native execution parts have no entry there, so they are fenced — including a
// tool name nobody has classified yet.
const (
	// untrustedContentOpenFmt opens the envelope. %q quotes the source so a tool
	// name can never inject an attribute.
	untrustedContentOpenFmt = `<untrusted-content source=%q>`

	// untrustedContentCloseTag closes the envelope.
	untrustedContentCloseTag = `</untrusted-content>`
)

// untrustedDelimiterPattern matches the sequences a payload could use to break
// out of the envelope or impersonate the harness: the envelope's own tags, a
// system-message tag, and a chat control token (<|im_start|>, <|endoftext|>, …).
var untrustedDelimiterPattern = regexp.MustCompile(`(?i)<(/?untrusted-content|/?system\b|\|[a-z_]{1,32}\|>)`)

// externalContentBuiltinTools are tools in the internal catalog whose payload is
// third-party content despite being served by this product: a fetched web page,
// a web search hit, or a diff from a repository this project does not own. They
// are the exception to "listed in the catalog ⇒ trusted".
var externalContentBuiltinTools = map[string]bool{
	"web-fetch":         true,
	"web-search-brave":  true,
	"web-search-reddit": true,
	"github-pr-diff":    true,
}

// trustedRuntimeTools are agent-runtime tools that are not in the MCP catalog but
// return this product's own state: the operator's answer to a question, the
// work-terminator results, a child run's ids, the agent catalogue, an appended
// comment. Anything not listed here and not in the catalog is fenced.
var trustedRuntimeTools = map[string]bool{
	ToolNameAskUser:             true,
	ToolNameWorkComplete:        true,
	ToolNameWorkBlock:           true,
	ToolNameWorkTransition:      true,
	ToolNameWorkComment:         true,
	ToolNameSpawnAgents:         true,
	ToolNameListAvailableAgents: true,
	ToolNameACPListAgents:       true,
	ToolNameACPTriggerRun:       true,
	ToolNameACPGetRunStatus:     true,
	ToolNameACPMCPServerList:    true,
	ToolNameACPMCPServerGet:     true,
}

// trustedToolResultSource builds the per-run trust predicate. internalScopes is
// the run's internal tool catalog (ToolPool.ToolScopes: name → required scope),
// which is the authoritative list of tools this product executes against its own
// store; deriving trust from it means a newly added builtin tool is trusted
// without touching this file. A nil catalog (MCP service unavailable) trusts
// nothing but the runtime tools, i.e. it fails closed.
func trustedToolResultSource(internalScopes map[string]string) func(string) bool {
	return func(name string) bool {
		if externalContentBuiltinTools[name] {
			return false
		}
		if _, ok := internalScopes[name]; ok {
			return true
		}
		return trustedRuntimeTools[name]
	}
}

// ToolResultFenceStats summarises what fenceModelRequestToolResults changed.
type ToolResultFenceStats struct {
	// FencedParts counts result parts wrapped in an untrusted-content envelope.
	FencedParts int
	// TrustedParts counts result parts left untouched because their tool is
	// this product's own store.
	TrustedParts int
	// BytesBefore and BytesAfter measure the fenced parts only.
	BytesBefore int
	BytesAfter  int
}

// fenceModelRequestToolResults mutates req in place, wrapping the results of
// tools this product does not own in an untrusted-content envelope. trusted
// decides per tool name; a nil predicate fences every FunctionResponse.
//
// Every result-bearing part is covered: FunctionResponse (our tools and MCP),
// ToolResponse (server-side tools) and CodeExecutionResult (native
// code_execution, which never goes through a FunctionResponse at all). It never
// panics on nil/empty requests or parts.
func fenceModelRequestToolResults(req *model.LLMRequest, trusted func(string) bool) ToolResultFenceStats {
	var stats ToolResultFenceStats
	if req == nil {
		return stats
	}
	for _, content := range req.Contents {
		if content == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			switch {
			case part.FunctionResponse != nil:
				name := part.FunctionResponse.Name
				resp := part.FunctionResponse.Response
				if resp == nil {
					continue
				}
				if trusted != nil && trusted(name) {
					stats.TrustedParts++
					continue
				}
				stats.BytesBefore += jsonSize(resp)
				part.FunctionResponse.Response = fenceUntrustedResult(name, resp)
				stats.BytesAfter += jsonSize(part.FunctionResponse.Response)
				stats.FencedParts++

			case part.ToolResponse != nil:
				resp := part.ToolResponse.Response
				if resp == nil {
					continue
				}
				source := string(part.ToolResponse.ToolType)
				if source == "" || source == string(genai.ToolTypeUnspecified) {
					source = "server-tool"
				}
				stats.BytesBefore += jsonSize(resp)
				part.ToolResponse.Response = fenceUntrustedResult(source, resp)
				stats.BytesAfter += jsonSize(part.ToolResponse.Response)
				stats.FencedParts++

			case part.CodeExecutionResult != nil:
				out := part.CodeExecutionResult.Output
				if strings.TrimSpace(out) == "" {
					continue
				}
				stats.BytesBefore += len(out)
				part.CodeExecutionResult.Output = wrapUntrusted("code_execution", out)
				stats.BytesAfter += len(part.CodeExecutionResult.Output)
				stats.FencedParts++
			}
		}
	}
	return stats
}

// fenceUntrustedResult returns a copy of resp whose every string leaf is wrapped
// in an untrusted-content envelope. resp itself is never mutated: the same value
// is persisted and streamed in full elsewhere.
//
// The value is normalised through JSON first (json.Number, no HTML escaping) so
// the walk covers every shape a tool can return — a typed struct, a []string, a
// map[string]string — instead of only the maps and slices our handlers build by
// hand. json.Number keeps 64-bit ids exact.
func fenceUntrustedResult(source string, resp map[string]any) map[string]any {
	norm, err := normalizeJSONValue(resp)
	if err != nil {
		// The ADK marshals every function response it sends, so a value that
		// cannot be normalised cannot reach the model either. Hand it back
		// untouched rather than dropping the result.
		return resp
	}
	fenced, ok := fenceUntrustedValue(source, norm).(map[string]any)
	if !ok {
		return resp
	}
	return fenced
}

// fenceUntrustedValue wraps every non-blank string in v, descending into maps
// and slices, and defangs map keys. Scalars (numbers, bools, null) are returned
// as they are.
func fenceUntrustedValue(source string, v any) any {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return t
		}
		return wrapUntrusted(source, t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[defangUntrustedText(k)] = fenceUntrustedValue(source, val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = fenceUntrustedValue(source, val)
		}
		return out
	default:
		// json.Number, bool, nil and anything else a JSON decoder produces.
		return v
	}
}

// wrapUntrusted defangs text and wraps it in the envelope for source.
func wrapUntrusted(source, text string) string {
	return fmt.Sprintf(untrustedContentOpenFmt, source) + defangUntrustedText(text) + untrustedContentCloseTag
}

// defangUntrustedText rewrites the opening "<" of every sequence the payload
// could use to close the envelope early or impersonate a system message as the
// literal text "&lt;". The escape is visible and reversible by a reader, but the
// payload can no longer contain a tag at all, so the envelope cannot be broken
// out of by construction.
func defangUntrustedText(s string) string {
	if !untrustedDelimiterPattern.MatchString(s) {
		return s
	}
	return untrustedDelimiterPattern.ReplaceAllStringFunc(s, func(match string) string {
		return "&lt;" + match[1:]
	})
}

// normalizeJSONValue round-trips v through JSON into the generic shapes
// (map[string]any, []any, string, json.Number, bool, nil). Numbers are decoded
// as json.Number so an integer id keeps its exact digits instead of becoming a
// float64.
func normalizeJSONValue(v any) (any, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(&buf)
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
