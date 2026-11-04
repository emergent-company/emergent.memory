package processingproblems

import (
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/modelstate"
)

// A configuration failure cannot be explained from the error text alone. The
// same message ("no generative model configured", "credential", a provider's
// 400) means three different things depending on what the project actually has:
//
//  1. no provider is connected at all;
//  2. a provider is connected but no default model of that kind is selected;
//  3. a model resolves, and the run failed anyway.
//
// Only the first two are configuration work. The third is the shape that made
// the panel tell a user to configure a provider that was already configured —
// a dead end, because the page it linked to looked correct. So a configuration
// failure is written once the project's model state is known, and it says which
// element is missing instead of restating the error.
//
// The copy names the model family the failure is about. Generative and
// embedding models resolve independently: an embedding queue that reports "no
// embedding model configured" must not be told to select a default generative
// model, which the project may already have.

const (
	configurationFallbackSummary     = "The run reported a model configuration problem."
	configurationFallbackRemediation = "Check the technical detail below against Project settings → Providers, then retry."
)

// providerFix is the link to the provider settings page, which is where a model
// or provider problem is fixed.
func providerFix(label string) *FixTarget {
	return &FixTarget{Href: "/settings/providers", Label: label}
}

// modelKindOfQueue reports which model family a queue's failure is about. Only
// the embedding queues are embedding-specific; every other model consumer
// (extraction, discovery, agent runs) uses the generative model.
func modelKindOfQueue(queue string) modelstate.Kind {
	switch queue {
	case QueueChunkEmbedding, QueueGraphEmbedding, QueueGraphRelationshipEmbedding:
		return modelstate.KindEmbedding
	default:
		return modelstate.KindGenerative
	}
}

// resolveConfiguration rewrites a configuration failure's copy for the
// project's real model state. It is the only class whose copy depends on state;
// every other class describes the error itself.
//
// The resolved copy always marks the failure Verified, because it was checked
// against configuration rather than inferred from the queue's error text.
func resolveConfiguration(f Failure, state modelstate.State, kind modelstate.Kind) Failure {
	if f.Class != ClassConfiguration {
		return f
	}

	model := state.Model(kind)
	f.Verified = true
	f.Model = model

	switch {
	case f.Cause == CauseBaseURL:
		// Already specific, and independent of whether a model resolves.
		return f

	case state.Configured(kind) && f.Cause == CauseModelUnavailable:
		if kind == modelstate.KindEmbedding {
			f.Summary = fmt.Sprintf("The provider does not offer the configured embedding model (%s).", model)
			return f
		}
		f.Summary = fmt.Sprintf("The provider does not offer the configured model (%s).", model)
		return f

	case state.Configured(kind):
		// The user's provider page looks correct because it is correct. Say so
		// and hand back the evidence, instead of sending them back to it.
		f.Cause = CauseModelUnusable
		if kind == modelstate.KindEmbedding {
			f.Summary = fmt.Sprintf("An embedding model is configured (%s), but this run could not use it.", model)
			f.Remediation = "Open the technical detail below. If the embedding model or the provider instance looks wrong, update Project settings → Providers, then retry."
		} else {
			f.Summary = fmt.Sprintf("A model is configured (%s), but this run could not use it.", model)
			f.Remediation = "Open the technical detail below. If the model or the provider instance looks wrong, update Project settings → Providers, then retry."
		}
		f.Fix = providerFix("Review model settings")
		// A model exists, so a fresh run is worth offering: the failure may have
		// been transient or provider-side, not a configuration gap.
		f.Retryable = true
		return f

	case state.ProviderCount == 0:
		f.Cause = CauseNoProvider
		f.Summary = "No model provider is connected to this project."
		f.Remediation = "Connect a provider in Project settings → Providers, then retry."
		f.Fix = &FixTarget{Href: "/settings/providers/new", Label: "Connect a provider"}
		return f

	case kind == modelstate.KindEmbedding:
		f.Cause = CauseNoDefaultModel
		f.Summary = "A provider is connected, but no default embedding model is selected."
		f.Remediation = "Choose a default embedding model in Project settings → Providers → Default models, then retry."
		f.Fix = &FixTarget{Href: "/settings/providers", Label: "Select an embedding model"}
		return f

	default:
		f.Cause = CauseNoDefaultModel
		f.Summary = "A provider is connected, but no default generative model is selected."
		f.Remediation = "Choose a default generative model in Project settings → Providers → Default models, then retry."
		f.Fix = &FixTarget{Href: "/settings/providers", Label: "Select a model"}
		return f
	}
}

// applyModelState rewrites the failures that depend on the project's model
// state: configuration copy, and the model a permission failure is about (an
// invalid key is a model problem when the queue needs one). Every other class is
// left exactly as the classifier wrote it.
//
// It is separate from List so the selection rule — which problem gets which
// model family — is testable without a database.
func applyModelState(byArea map[Area][]Problem, state modelstate.State) {
	for area, problems := range byArea {
		for i := range problems {
			kind := modelKindOfQueue(problems[i].Queue)
			if problems[i].Failure.Class == ClassConfiguration {
				problems[i].Failure = resolveConfiguration(problems[i].Failure, state, kind)
				continue
			}
			// A rejected credential on a model-dependent queue is a model
			// problem: name the model so the row can offer a live test, but
			// leave the classifier's summary and remediation alone.
			if problems[i].Failure.Class == ClassPermission &&
				isModelQueue(problems[i].Queue) && state.Configured(kind) {
				problems[i].Failure.Model = state.Model(kind)
			}
		}
		byArea[area] = problems
	}
}
