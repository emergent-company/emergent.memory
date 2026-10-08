# web-ui-unavailable-not-empty

Fixes the gateway web UI conflating a backend failure (5xx, timeout, or auth `503
service_unavailable`) with the empty "create your first org/project" onboarding state. A backend
failure must render an explicit "unavailable, your data is safe" state; the onboarding/create screen is
reserved for a successful authenticated response that reports zero orgs/projects.
