package adk

// context_overflow.go — typed detection of a provider context-window rejection.
//
// Compaction normally happens on the load path, before a run starts. A single
// long run can still grow past the model's context window mid-run, where the
// provider rejects the next model call. Recovering from that requires knowing
// that the failure IS an overflow rather than a generic 400, so the executor can
// summarise the oldest history and retry the step (#1138).
//
// Provider SDKs do not expose a portable typed error for this. Some adapters
// surface the HTTP body verbatim ("This model's maximum context length is …",
// "prompt is too long", "input token count exceeds …"), others use a dedicated
// error code. ContextOverflowError is the typed carrier: the tracking model
// wraps a recognised provider error in it at the model-call boundary, and
// IsContextOverflow also recognises the common message shapes so an unwrapped
// model (another factory, a test stub) is still classified correctly.

import (
	"errors"
	"fmt"
	"strings"
)

// ContextOverflowError reports that a model call failed because the request
// exceeded the provider's context window.
type ContextOverflowError struct {
	// Model is the model name reported by the failing LLM, when known.
	Model string
	// Err is the provider's original error.
	Err error
}

// Error implements error.
func (e *ContextOverflowError) Error() string {
	if e == nil {
		return "context window exceeded"
	}
	if e.Err == nil {
		return fmt.Sprintf("model %s: context window exceeded", e.Model)
	}
	if e.Model == "" {
		return fmt.Sprintf("context window exceeded: %v", e.Err)
	}
	return fmt.Sprintf("model %s: context window exceeded: %v", e.Model, e.Err)
}

// Unwrap exposes the provider error to errors.Is / errors.As.
func (e *ContextOverflowError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// contextOverflowMarkers are the message fragments providers use to reject a
// request that is too large for the model's context window. They are matched
// case-insensitively and deliberately narrow: a generic 400 (bad credential,
// malformed tool schema, unsupported parameter) must not trigger compaction.
var contextOverflowMarkers = []string{
	"context_length_exceeded",
	"context length",
	"context window",
	"maximum context",
	"too many tokens",
	"input is too long",
	"prompt is too long",
	"reduce the length of the messages",
	"exceeds the maximum number of tokens",
	"input token count",
	"token count exceeds",
	"string_above_max_length",
	"request too large",
}

// IsContextOverflow reports whether err is a provider context-window rejection.
// A *ContextOverflowError anywhere in the chain is authoritative; otherwise the
// provider message is matched against the known overflow shapes.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	var overflow *ContextOverflowError
	if errors.As(err, &overflow) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range contextOverflowMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// WrapContextOverflow returns err unchanged when it is not a context-window
// rejection, and a *ContextOverflowError wrapping it when it is. It is
// nil-safe and idempotent: an error already carrying the typed error is
// returned as-is, so wrappers can be stacked without nesting the same error.
func WrapContextOverflow(modelName string, err error) error {
	if err == nil || !IsContextOverflow(err) {
		return err
	}
	var already *ContextOverflowError
	if errors.As(err, &already) {
		return err
	}
	return &ContextOverflowError{Model: modelName, Err: err}
}
