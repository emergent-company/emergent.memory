# Verify agent model display + config warnings in a signed-in browser

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-agent-model-default-display](sessions/2026-09-08-agent-model-default-display.md)

## What
Signed-in browser pass over the agent-model UI on alfred-dev:

1. Agents list — at `md` and up the desktop **table** renders the Model column from the resolved `effectiveModel` (explicit override name, else project default; dash when unresolvable). There is **no** "(default)" suffix. Below `md` the page shows the card grid (name + icon + chat, no model).
2. Agent dashboard summary — Model field shows the same resolved value / "(default)".
3. Agent settings — an auto agent with no resolvable model shows the Model-section warning; with a configured provider it reads **warning**, with none it reads **error**, and the action button (Configure a provider / Set a default model) sits inline on the alert's row at the same height, no card box.
4. Configure a provider + a project default generative model (Settings → Providers) and confirm the warnings clear and the exact model name appears.
5. Pinned-model warnings — on a provider-less project, an agent with an explicit model (e.g. the bundled `operator`) shows the **error** notice on the dashboard and settings, the same error banner on the blueprint details page (`/blueprints/operator`), and the pre-send banner in the chat workspace (`/chat?agent=<id>`); configuring a provider matching the model's prefix clears them.
6. Chat picker reactivity — switching the chat agent picker updates the pre-send banner live (shown only for agents whose chats cannot run).

## Why
The gateway behind Zitadel session auth couldn't be screenshotted this session; only rendered-HTML unit tests + live API probes verified the feature.

## Depends on
- none (commits `11183b0`, `9c573b7`, `adc6681`)

## Notes
- Test project: agent `test` (`d6156255-4e9d-4927-b35d-e86f9023c2d6`) on the dev project has zero providers — expect the error warning there.
- Item 1 was written when the Agents page was a table; it then became a card grid (D31) and is now a responsive split — table at `md`+, cards below (D37). Verify both viewports and the `md` breakpoint edge.
- The "(default)" model tag was dropped from the list with D37 (the list summary carries no explicit-vs-resolved signal).
- Pinned-model warning surfaces (items 5–6) were shipped in [2026-09-10-model-availability-warnings](sessions/2026-09-10-model-availability-warnings.md).
- Requires a valid Zitadel session on `http://alfred-dev.tail0358fa.ts.net:8095`.
