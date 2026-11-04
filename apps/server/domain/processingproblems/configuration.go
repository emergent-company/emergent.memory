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
//  2. a provider is connected but no default generative model is selected;
//  3. a model resolves, and the run failed anyway.
//
// Only the first two are configuration work. The third is the shape that made
// the panel tell a user to configure a provider that was already configured —
// a dead end, because the page it linked to looked correct. So a configuration
// failure is written once the project's model state is known, and it says which
// element is missing instead of restating the error.

const (
	configurationFallbackSummary     = "The run reported a model configuration problem."
	configurationFallbackRemediation = "Check the technical detail below against Project settings → Providers, then retry."
)

// providerFix is the link to the provider settings page, which is where a model
// or provider problem is fixed.
func providerFix(label string) *FixTarget {
	return &FixTarget{Href: "/settings/providers", Label: label}
}

// resolveConfiguration rewrites a configuration failure's copy for the
// project's real model state. It is the only class whose copy depends on state;
// every other class describes the error itself.
//
// The resolved copy always marks the failure Verified, because it was checked
// against configuration rather than inferred from the queue's error text.
func resolveConfiguration(f Failure, state modelstate.State) Failure {
	if f.Class != ClassConfiguration {
		return f
	}

	f.Verified = true
	f.Model = state.GenerativeModel

	switch {
	case f.Cause == CauseBaseURL:
		// Already specific, and independent of whether a model resolves.
		return f

	case state.Configured() && f.Cause == CauseModelUnavailable:
		f.Summary = fmt.Sprintf("The provider does not offer the configured model (%s).", state.GenerativeModel)
		return f

	case state.Configured():
		// The user's provider page looks correct because it is correct. Say so
		// and hand back the evidence, instead of sending them back to it.
		f.Cause = CauseModelUnusable
		f.Summary = fmt.Sprintf("A model is configured (%s), but this run could not use it.", state.GenerativeModel)
		f.Remediation = "Open the technical detail below. If the model or the provider instance looks wrong, update Project settings → Providers, then retry."
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

	default:
		f.Cause = CauseNoDefaultModel
		f.Summary = "A provider is connected, but no default generative model is selected."
		f.Remediation = "Choose a default generative model in Project settings → Providers → Default models, then retry."
		f.Fix = &FixTarget{Href: "/settings/providers", Label: "Select a model"}
		return f
	}
}
