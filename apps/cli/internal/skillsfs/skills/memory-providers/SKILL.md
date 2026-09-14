---
name: memory-providers
description: Configure LLM provider credentials (Google AI, Vertex AI) and browse available models. Use when setting up or changing the AI provider for a Memory org.
metadata:
  author: emergent
  version: "3.0"
---

Manage LLM provider credentials, model catalog, and usage reporting using `memory provider`.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.

## Commands

### Configure a provider (org-level)
```bash
# Google AI (API key)
memory provider configure google-ai --api-key "AIza..."

# Vertex AI (service account)
memory provider configure vertex-ai \
  --key-file /path/to/sa.json \
  --gcp-project "my-gcp-project" \
  --location "us-central1"
```
Stores encrypted credentials, syncs the model catalog from the live API, auto-selects the best generative and embedding models, and runs a live test — all in one atomic operation. Scoped to the **organization**.

Optional flags:
- `--generative-model string`  Override auto-selected generative model
- `--embedding-model string`   Override auto-selected embedding model
- `--org-id string`            Organization ID (auto-detected from config)

### Configure a provider (project-level override)
```bash
memory provider configure-project vertex-ai \
  --key-file /path/to/sa.json \
  --gcp-project "my-gcp-project" \
  --location "us-central1"
```
Stores a project-specific provider config that overrides the org-level config for this project. Uses the same flags as `configure`.

To remove the project override and fall back to org config:
```bash
memory provider configure-project <provider> --remove
```

### List configured providers
```bash
memory provider list
```
Shows which providers are configured for the org and their model selections.

### Test a provider
```bash
memory provider test <provider>
# provider is one of: google-ai, vertex-ai
```
Runs a live generate call to confirm credentials are valid and the model responds.

### List available models
```bash
memory provider models
memory provider models google-ai --type generative
```
Shows the cached model catalog available from configured providers.

### View usage and estimated cost
```bash
memory provider usage
memory provider usage --project <id>
memory provider usage --since 2024-01-01
```
Shows LLM token consumption and estimated cost breakdown by model and time period.

## Workflow

1. **Initial setup**: `provider configure <provider>` — configures credentials, syncs models, and validates in one step
2. **Verify**: `provider list` to confirm the config is stored
3. **Test live**: `provider test <provider>` to confirm the model responds
4. **Browse models**: `provider models` to see what's available
5. **Monitor costs**: `provider usage` regularly to track token consumption

## Choosing a provider

| Provider | Command | Key format | Requires GCP project? |
|---|---|---|---|
| Google AI | `configure google-ai --api-key` | `AIza...` | No |
| Vertex AI | `configure vertex-ai --key-file --gcp-project --location` | service account JSON | Yes |

## Notes

- Credentials are scoped to the **organization** — `configure` requires no `--project` flag
- Credentials and model selection are stored and updated atomically in one call
- Model auto-selection picks the best available model from the live catalog; override with `--generative-model` / `--embedding-model`
- Credentials are stored server-side (encrypted); never written to the CLI config file
- Vertex AI requires a GCP project with the Vertex AI API enabled and a service account with the `aiplatform.user` role
- Project-level overrides (`configure-project`) inherit from the org when no project row exists; use `--remove` to revert to org config
