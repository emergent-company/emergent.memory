package processingproblems

import (
	"regexp"
	"strings"
)

// Classify turns a raw terminal error into a stable failure class plus
// user-facing copy. It is intentionally heuristic: queues store provider error
// strings, not error codes, so the only shared signal is the message.
//
// The order of the rules is the precedence. A dead OAuth grant (reconnect
// required) is decided first because retrying it is never the answer;
// configuration and permission win over provider because "no api key" must not
// read as a transient outage; output-limit wins over invalid-output because a
// truncated JSON body is a limit problem, not a malformed model.
func Classify(queue, message string) Failure {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return emptyFailure(queue)
	}

	switch {
	// A rejected OAuth *client* is not a dead grant: the app configuration (or
	// the client registration) is what the authorization server refused, so
	// reconnecting does not help — an administrator has to restore or replace it.
	// The rule sits above the reconnect rule because the connector's copy for
	// this case also says "reconnect the source" (it is the second step, after
	// the client is fixed); classifying it as a plain permission problem would
	// hide who owns the fix (#2407).
	case containsAny(msg, "oauth app", "oauth client", "invalid_client", "unauthorized_client", "client registration"):
		return Failure{
			Class:       ClassPermission,
			Summary:     "The OAuth app for this connection was rejected.",
			Remediation: "An administrator must restore or replace the OAuth client this connection uses, then reconnect it.",
			Retryable:   false,
		}

	// A rejected OAuth grant is a dead end for retries, not an outage: the
	// connector has already decided the stored credential can never work again,
	// and only a fresh consent recovers it. The connectors agree on one stable
	// phrase ("reconnect the source"), which therefore has to win over every
	// other rule below. It is checked first because the most damaging shape is
	// the silent one: a Google Drive source whose grant Google revoked after the
	// app's testing window landed in ClassUnknown, so the panel told the user to
	// retry a sync that could not possibly succeed.
	case containsAny(msg, "reconnect the source", "reconnect the data source"):
		return Failure{
			Class:       ClassPermission,
			Summary:     permissionSummary(queue),
			Remediation: permissionRemediation(queue),
			Retryable:   false,
		}

	case containsAny(msg, "no generative model", "no model configured", "model not configured", "no embedding model",
		"embedding model not configured"):
		// The error text says a model is missing, but not which element is
		// missing. The service resolves the project's effective model state and
		// replaces this fallback copy with the real one (no provider, no default
		// model, or a configured model the run could not use). Until then this
		// copy must not assert a cause it has not checked.
		return Failure{
			Class:       ClassConfiguration,
			Summary:     configurationFallbackSummary,
			Remediation: configurationFallbackRemediation,
			Fix:         providerFix("Review model settings"),
			Retryable:   false,
		}

	// A model name the provider does not serve is its own failure: the provider
	// is configured and reachable, so "not configured" would send the user to a
	// page that already looks correct.
	case modelNotFound(msg):
		return Failure{
			Class:       ClassConfiguration,
			Cause:       CauseModelUnavailable,
			Summary:     "The provider does not offer the configured model.",
			Remediation: "Pick a model the provider serves (Project settings → Providers → model catalog), then retry.",
			Fix:         providerFix("Choose a model"),
			Retryable:   false,
		}

	case containsAny(msg, "base url", "baseurl", "invalid url", "unsupported protocol scheme"):
		return Failure{
			Class:       ClassConfiguration,
			Cause:       CauseBaseURL,
			Summary:     "The provider endpoint (base URL) is not usable.",
			Remediation: "Fix the provider's base URL in Project settings → Providers, then retry.",
			Fix:         providerFix("Fix base URL"),
			Retryable:   false,
		}

	// A refusal to read a document or another resource is still a permission
	// problem, but it is not about the model credential: naming a model and
	// offering to test it would send the user to the wrong fix. The rule sits
	// above the credential rule because "permission denied reading document X"
	// contains the same words as a rejected key.
	case resourceAccessDenied(msg):
		return Failure{
			Class:       ClassPermission,
			Cause:       CauseResourceAccess,
			Summary:     permissionSummary(queue),
			Remediation: permissionRemediation(queue),
			Retryable:   false,
		}

	case containsAny(msg, "401", "403", "unauthorized", "forbidden", "permission denied", "invalid_grant",
		"invalid grant", "token expired", "access denied", "not authorized",
		"credential", "api key is not", "invalid api key", "incorrect api key", "authentication fails",
		"unauthenticated"):
		// A rejected or missing credential is a permission problem, not a model
		// selection one: the provider is configured, and the fix is its key. The
		// cause is recorded so applyModelState only names a model for this shape,
		// not for a resource-access refusal above.
		return Failure{
			Class:       ClassPermission,
			Cause:       CauseCredential,
			Summary:     permissionSummary(queue),
			Remediation: permissionRemediation(queue),
			Retryable:   false,
		}

	case containsAny(msg, "response had no choices", "had no choices"):
		// An empty 200 from the provider is a transient provider response, not
		// malformed model output: no schema advice applies.
		return Failure{
			Class:       ClassProvider,
			Summary:     providerSummary(queue),
			Remediation: providerRemediation(queue),
			Retryable:   true,
		}

	case isModelQueue(queue) && containsAny(msg, "unexpected eof", "unexpected end of json"):
		// A model response cut off mid-object. Restrict to model queues so an
		// SMTP "unexpected EOF" is not reported as an output limit.
		return Failure{
			Class:       ClassLimit,
			Summary:     "The model ran out of context or output space.",
			Remediation: limitRemediation(queue),
			Retryable:   true,
		}

	case containsAny(msg, "context length", "too many tokens", "maximum context", "max tokens", "output limit",
		"token limit", "truncated", "exceeds the maximum", "request too large"):
		return Failure{
			Class:       ClassLimit,
			Summary:     "The model ran out of context or output space.",
			Remediation: limitRemediation(queue),
			Retryable:   true,
		}

	case containsAny(msg, "no content or tool calls", "tool_calls", "invalid json", "unmarshal",
		"failed to parse", "parse entity", "invalid character", "schema validation",
		"empty response", "response was empty"):
		return Failure{
			Class:       ClassInvalidOutput,
			Summary:     "The model returned output that could not be read.",
			Remediation: invalidOutputRemediation(queue),
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

	case queue == QueueDiscovery && containsAny(msg, "no entity types were discovered", "found no entity types"):
		return Failure{
			Class:       ClassInput,
			Summary:     "No entity types were discovered in these documents.",
			Remediation: "Add documents with substantive entities, or choose a different model, then retry discovery.",
			Retryable:   false,
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

	case containsAny(msg, "yielded a nil event", "nil event"):
		return Failure{
			Class:       ClassInternal,
			Summary:     "The extraction pipeline stopped before saving results.",
			Remediation: "This is a bug in Memory, not your document. Retrying the same input will not fix it — report it with the technical detail.",
			Retryable:   false,
		}

	case containsAny(msg, "nil pointer", "scan error", "panic", "deadlock", "internal",
		"unexpected", "yielded", "index out of range"):
		return Failure{
			Class:       ClassInternal,
			Summary:     "An internal pipeline error stopped the run.",
			Remediation: "This is a bug in Memory, not the input. Retry once; if it repeats, report it with the technical detail.",
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

// resourceAccessDenied reports whether a permission failure is about access to
// a document or another resource rather than the provider credential. Both
// shapes use the same words ("permission denied", "forbidden"), so the deciding
// signal is a resource noun or an access verb. The distinction matters because
// only a credential failure makes a model test meaningful.
func resourceAccessDenied(msg string) bool {
	if !containsAny(msg, "permission denied", "permissions denied", "access denied", "forbidden",
		"401", "403", "unauthorized", "not authorized", "does not have permission", "no permission",
		"insufficient permission", "lacks permission", "missing permission") {
		return false
	}
	return containsAny(msg,
		"reading", "accessing", "opening", "listing",
		"document", "file", "bucket", "object", "folder", "source",
		"resource", "blob", "dataset", "path",
	)
}

// modelNotFoundRe catches the vendor phrasings that put words between "model"
// and the failure: `model "qwen3:8b" not found`, `the model 'gpt-5' does not
// exist or you do not have access to it`, `model is not available`. A plain
// substring list cannot express that, and "not found" on its own would also
// swallow missing documents and data sources.
var modelNotFoundRe = regexp.MustCompile(`\bmodel\b[^.\n]{0,80}\b(not found|not available|does not exist|is not supported|unknown)\b`)

// modelNotFound reports whether the error says the provider does not serve the
// requested model.
func modelNotFound(msg string) bool {
	if containsAny(msg, "no such model", "invalid model name", "unknown model", "model not found") {
		return true
	}
	return modelNotFoundRe.MatchString(msg)
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

func isModelQueue(queue string) bool {
	switch queue {
	case QueueObjectExtraction, QueueDiscovery, QueueChunkEmbedding, QueueGraphEmbedding,
		QueueGraphRelationshipEmbedding, QueueAgentRun:
		return true
	default:
		return false
	}
}

func limitRemediation(queue string) string {
	if queue == QueueDiscovery {
		return "Retry. If it repeats, reduce the documents per discovery batch, shorten the source documents, or switch to a model with a larger output limit."
	}
	return "Retry with a smaller document batch, or switch to a model with a larger output limit."
}

func invalidOutputRemediation(queue string) string {
	switch queue {
	case QueueDiscovery:
		// Discovery produces the schema, so "install an object schema" is the
		// wrong advice here.
		return "Retry. If it repeats, reduce the documents per discovery batch, shorten the source documents, or switch model."
	case QueueObjectExtraction:
		return "Retry. If it repeats, install or repair the project's object schema so extraction runs in JSON mode, or switch model."
	default:
		return "Retry. If it repeats, check the model configuration or switch model."
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
