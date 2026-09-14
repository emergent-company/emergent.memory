# Blueprint pinned-model install choice

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-model-availability-warnings](sessions/2026-09-10-model-availability-warnings.md)

## What

When installing/applying a blueprint whose agent pins a model the target project cannot serve
(no configured provider, or no provider matching the model's prefix), offer an install-time
choice instead of only warning afterwards:

- keep the pinned model (current behavior: install proceeds, error warning shows), or
- install the agent unpinned / with a different model, so it falls back to the project default
  (or the resolved model).

## Why

The original report asked to either be asked to change the provider or be warned. Warn-only
shipped in PRs #3/#16 (decision D35: warn, never mutate), but the "ask/choose" path was deferred
because it cannot be implemented purely in the gateway: bundled blueprints are seeded as global,
published records shared across projects, and `ApplyBlueprint(ctx, id)` has no per-project agent
override. A real choice needs either a per-project apply override in memory's blueprint API or a
post-apply agent-definition edit.

## Depends on

- Memory blueprint apply API supporting per-project agent overrides, **or** a gateway-side
  post-apply edit of the created agent definition.
- [agent-effective-model-memory](agent-effective-model-memory.md) — knowing the resolved fallback
  model when installing unpinned.

## Notes

- Related: [verify-agent-model-ui-browser](verify-agent-model-ui-browser.md) (browser pass over the
  warning surfaces), [merge-memory-strip-model-prefix](merge-memory-strip-model-prefix.md) (model
  prefix resolution semantics the availability rule relies on).
- Concrete case: the bundled `operator` blueprint pins `openai/deepseek-v4-pro`.
- Availability rule today: provider-prefix match against configured providers; catalog membership not
  required; bare model with providers present is satisfied.
