package processingproblems

import "strings"

// Classify turns a raw terminal error into a stable failure class plus
// user-facing copy. It is intentionally heuristic: queues store provider error
// strings, not error codes, so the only shared signal is the message.
//
// The order of the rules is the precedence. Configuration and permission wins
// over provider because "no api key" must not read as a transient outage;
// output-limit wins over invalid-output because a truncated JSON body is a
// limit problem, not a malformed model.
func Classify(queue, message string) Failure {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return emptyFailure(queue)
	}

	switch {
	case containsAny(msg, "no generative model", "no model configured", "model not configured", "no embedding model",
		"not configured", "missing api key", "api key is not", "no credentials", "credential", "model not found",
		"unknown model", "base url"):
		return Failure{
			Class:       ClassConfiguration,
			Summary:     "The model or provider is not configured correctly.",
			Remediation: "Configure a model in Project settings → Providers, then retry.",
			Retryable:   false,
		}

	case containsAny(msg, "401", "403", "unauthorized", "forbidden", "permission denied", "invalid_grant",
		"invalid grant", "token expired", "access denied", "not authorized"):
		return Failure{
			Class:       ClassPermission,
			Summary:     permissionSummary(queue),
			Remediation: permissionRemediation(queue),
			Retryable:   false,
		}

	case containsAny(msg, "context length", "too many tokens", "maximum context", "max tokens", "output limit",
		"token limit", "truncated", "exceeds the maximum", "request too large"):
		return Failure{
			Class:       ClassLimit,
			Summary:     "The model ran out of context or output space.",
			Remediation: "Retry with a smaller document, or switch to a model with a larger output limit.",
			Retryable:   true,
		}

	case containsAny(msg, "no choices", "no content or tool calls", "tool_calls", "invalid json", "unmarshal",
		"failed to parse", "parse entity", "invalid character", "unexpected end of json", "schema validation",
		"empty response", "response was empty"):
		return Failure{
			Class:       ClassInvalidOutput,
			Summary:     "The model returned output that could not be read.",
			Remediation: "Retry. If it repeats, install an object schema so the model is forced into JSON mode, or switch model.",
			Retryable:   true,
		}

	case containsAny(msg, "429", "rate limit", "quota", "timeout", "timed out", "deadline exceeded",
		"connection refused", "connection reset", "broken pipe", "eof", "502", "503", "504", "500",
		"internal server error", "bad gateway", "service unavailable", "overloaded", "provider", "mailgun",
		"smtp"):
		return Failure{
			Class:       ClassProvider,
			Summary:     providerSummary(queue),
			Remediation: providerRemediation(queue),
			Retryable:   true,
		}

	case containsAny(msg, "load document", "unsupported", "no such file", "invalid file", "corrupt",
		"empty document", "source type requires", "document has no", "missing document", "document not found",
		"deleted"):
		return Failure{
			Class:       ClassInput,
			Summary:     "The input could not be read.",
			Remediation: "Fix or replace the source, then retry.",
			Retryable:   false,
		}

	case containsAny(msg, "conflict", "duplicate", "already exists", "unique constraint", "migration",
		"schema mismatch", "risk"):
		return Failure{
			Class:       ClassConflict,
			Summary:     conflictSummary(queue),
			Remediation: "Review the conflict in the owning page before retrying.",
			Retryable:   false,
		}

	case containsAny(msg, "nil event", "nil pointer", "scan error", "panic", "deadlock", "internal",
		"unexpected", "yielded", "index out of range"):
		return Failure{
			Class:       ClassInternal,
			Summary:     "An internal pipeline error stopped the run.",
			Remediation: "Retry. If it repeats, it is a product bug — open the raw error and report it.",
			Retryable:   true,
		}
	}

	return Failure{
		Class:       ClassUnknown,
		Summary:     "The run stopped with an unclassified error.",
		Remediation: "Retry. If it repeats, open the raw error below and report it.",
		Retryable:   true,
	}
}

func containsAny(msg string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

func emptyFailure(queue string) Failure {
	switch queue {
	case QueueEmail:
		return Failure{Class: ClassProvider, Summary: "The email could not be delivered.", Remediation: "Check the recipient address and email provider, then resend.", Retryable: true}
	case QueueSourceSync:
		return Failure{Class: ClassProvider, Summary: "The source sync stopped without an error message.", Remediation: "Retry the sync; if it repeats, test the source connection.", Retryable: true}
	case QueueAgentRun:
		return Failure{Class: ClassInternal, Summary: "The agent run stopped before finishing.", Remediation: "Retry the run from its session.", Retryable: true}
	case QueueSchemaMigration, QueueBlueprintUpdate:
		return Failure{Class: ClassConflict, Summary: "The schema change stopped before completing.", Remediation: "Review the change in the schema area.", Retryable: false}
	case QueueDiscovery:
		return Failure{Class: ClassInternal, Summary: "Schema discovery stopped before finishing.", Remediation: "Retry discovery from the schema area.", Retryable: true}
	case QueueDocumentParsing:
		return Failure{Class: ClassInput, Summary: "The document could not be converted.", Remediation: "Re-upload the document or check its format.", Retryable: false}
	default:
		return Failure{Class: ClassInternal, Summary: "The run stopped without an error message.", Remediation: "Retry. If it repeats, report it.", Retryable: true}
	}
}

func permissionSummary(queue string) string {
	if queue == QueueSourceSync {
		return "The data source is no longer authorized."
	}
	return "The provider rejected the credentials or permissions."
}

func permissionRemediation(queue string) string {
	if queue == QueueSourceSync {
		return "Reconnect the data source, then retry the sync."
	}
	return "Update the provider credentials or reconnect the account, then retry."
}

func providerSummary(queue string) string {
	switch queue {
	case QueueEmail:
		return "The email provider could not deliver the message."
	case QueueSourceSync:
		return "The source provider rejected or dropped the request."
	default:
		return "The model or embedding provider rejected or dropped the request."
	}
}

func providerRemediation(queue string) string {
	switch queue {
	case QueueEmail:
		return "Check the recipient address and email provider configuration, then resend."
	case QueueSourceSync:
		return "Retry the sync; if it repeats, test the source connection."
	default:
		return "Retry. If it repeats, check the provider status and configuration."
	}
}

func conflictSummary(queue string) string {
	switch queue {
	case QueueSchemaMigration:
		return "The schema migration stopped on a conflict."
	case QueueBlueprintUpdate:
		return "The blueprint update stopped on a conflict."
	default:
		return "The change conflicts with existing data."
	}
}
