package adk

import (
	"strings"

	"google.golang.org/genai"
)

// Native tool name constants — these are the string identifiers used in
// AgentDefinition.Model.NativeTools and the capability registry below.
const (
	// NativeToolGoogleSearch enables real-time web search grounding via Google Search.
	// Supported on: Gemini 2.0 Flash, Gemini 2.5+, Gemini 3+ (text/reasoning models).
	NativeToolGoogleSearch = "google_search"

	// NativeToolURLContext enables the model to fetch and read URLs mentioned in the prompt.
	// Supported on: Gemini 2.5+, Gemini 3+ (text/reasoning models).
	// NOTE: Not available on Vertex AI — Gemini API only.
	NativeToolURLContext = "url_context"

	// NativeToolCodeExecution enables sandboxed Python code generation and execution.
	// Supported on: Gemini 2.0 Flash, Gemini 2.5+, Gemini 3+ (text/reasoning models).
	NativeToolCodeExecution = "code_execution"
)

// modelNativeToolSupport maps model name prefixes to the native tools they support.
//
// Matching is done via strings.HasPrefix, so "gemini-2.5-flash-preview-0514" correctly
// matches the "gemini-2.5-flash" entry. More specific prefixes must appear before
// broader ones when ambiguity could arise (e.g., "gemini-2.0-flash-lite" before
// "gemini-2.0-flash").
//
// Source: https://ai.google.dev/gemini-api/docs (March 2026)
var modelNativeToolSupport = []modelCapability{
	// --- Gemini 3.1 series ---
	{
		prefix: "gemini-3.1-pro",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},
	{
		prefix: "gemini-3.1-flash-lite",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},
	{
		// Image variant: google_search only (no url_context, no code_execution)
		prefix: "gemini-3.1-flash-image",
		tools:  []string{NativeToolGoogleSearch},
	},
	{
		prefix: "gemini-3.1-flash",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},

	// --- Gemini 3 series ---
	{
		// Image variant: google_search only
		prefix: "gemini-3-pro-image",
		tools:  []string{NativeToolGoogleSearch},
	},
	{
		prefix: "gemini-3-pro",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},
	{
		// Image variant: google_search only
		prefix: "gemini-3-flash-image",
		tools:  []string{NativeToolGoogleSearch},
	},
	{
		prefix: "gemini-3-flash",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},

	// --- Gemini 2.5 series ---
	{
		prefix: "gemini-2.5-pro",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},
	{
		prefix: "gemini-2.5-flash-lite",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},
	{
		prefix: "gemini-2.5-flash",
		tools:  []string{NativeToolGoogleSearch, NativeToolURLContext, NativeToolCodeExecution},
	},

	// --- Gemini 2.0 series ---
	// gemini-2.0-flash-lite: no native tools supported
	{
		// Must appear before "gemini-2.0-flash" to avoid prefix ambiguity
		prefix: "gemini-2.0-flash-lite",
		tools:  nil,
	},
	{
		// google_search + code_execution; url_context not supported on 2.0
		prefix: "gemini-2.0-flash",
		tools:  []string{NativeToolGoogleSearch, NativeToolCodeExecution},
	},
}

// modelCapability pairs a model name prefix with its supported native tools.
type modelCapability struct {
	prefix string
	tools  []string
}

// SupportedNativeTools returns the native tools supported by the given model.
//
// Matching is prefix-based and case-insensitive: "gemini-2.5-flash-preview-0514"
// matches the "gemini-2.5-flash" entry. Returns nil if the model is unknown or
// supports no native tools.
func SupportedNativeTools(modelName string) []string {
	lower := strings.ToLower(modelName)
	// Strip common path prefixes (e.g. "models/gemini-2.5-flash")
	if idx := strings.LastIndex(lower, "/"); idx != -1 {
		lower = lower[idx+1:]
	}
	for _, cap := range modelNativeToolSupport {
		if strings.HasPrefix(lower, cap.prefix) {
			return cap.tools
		}
	}
	return nil
}

// BuildNativeGenaiTools converts a list of native tool names into []*genai.Tool
// suitable for appending to GenerateContentConfig.Tools.
//
// Unknown tool names are silently ignored. Returns nil if toolNames is empty
// or contains only unrecognized names.
func BuildNativeGenaiTools(toolNames []string) []*genai.Tool {
	var tools []*genai.Tool
	for _, name := range toolNames {
		switch name {
		case NativeToolGoogleSearch:
			tools = append(tools, &genai.Tool{
				GoogleSearch: &genai.GoogleSearch{},
			})
		case NativeToolURLContext:
			tools = append(tools, &genai.Tool{
				URLContext: &genai.URLContext{},
			})
		case NativeToolCodeExecution:
			tools = append(tools, &genai.Tool{
				CodeExecution: &genai.ToolCodeExecution{},
			})
		}
	}
	return tools
}

// IntersectNativeTools returns only the tool names that appear in both
// requested and supported slices. Order follows requested.
func IntersectNativeTools(requested, supported []string) []string {
	if len(requested) == 0 || len(supported) == 0 {
		return nil
	}
	supportedSet := make(map[string]bool, len(supported))
	for _, s := range supported {
		supportedSet[s] = true
	}
	var result []string
	for _, r := range requested {
		if supportedSet[r] {
			result = append(result, r)
		}
	}
	return result
}
