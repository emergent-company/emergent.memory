---
name: memory-providers
description: Configure LLM provider credentials (Google AI, Vertex AI) and browse available models. Use when setting up or changing the AI provider for a Memory project.
metadata:
  author: emergent
  version: "3.1"
---

Manage LLM provider credentials, model catalog, and usage reporting using `memory provider`.

Provider credentials and model selection are **per project**. There is no organization-level provider configuration; an org-level call now fails with `org-level provider config is deprecated`. Embeddings are generated only through the project's configured provider — the server has no deployment-wide embedding fallback.

## Rules

- **Project context is auto-discovered** — the CLI walks up the directory tree to find `.env.local` containing `MEMORY_PROJECT` or `MEMORY_PROJECT_ID`. If `.env.local` is present anywhere above the current directory, `--project` is not needed. Only pass `--project <id>` explicitly when overriding or when no `.env.local` exists.
- **Model names carry the provider prefix** — `google/gemini-2.5-flash`, not `gemini-2.5-flash`. The prefix is the provider slug.
- **DeepSeek and OpenAI serve no embeddings** — a project that generates with them needs a second provider (Google AI or Vertex AI) for embeddings.

## Commands

### Configure the project's provider
```bash
# Google AI (API key)
memory provider configure-project google --api-key "AIza..."

# Vertex AI (service account)
memory provider configure-project google-vertex \
  --key-file /path/to/sa.json \
  --gcp-project "my-gcp-project" \
  --location "us-central1"

# OpenAI
memory provider configure-project openai --api-key "sk-..."

# DeepSeek
memory provider configure-project deepseek --api-key "sk-..."
```
Stores encrypted credentials, syncs the model catalog from the live API, auto-selects the best generative and embedding models, and runs a live test — all in one operation, scoped to the **project**.

Optional flags:
- `--generative-model string`  Override the auto-selected generative model
- `--embedding-model string`   Override the auto-selected embedding model
- `--project string`           Project ID (otherwise `MEMORY_PROJECT_ID`)
- `--base-url string`          For OpenAI-compatible endpoints
- `--extra key=value`          Repeatable, vendor-specific fields (e.g. `api_version=2024-06-01`)

To remove a provider's config for the project:
```bash
memory provider configure-project <provider> --remove
```

Any registered vendor (`azure-openai`, `volcengine`, `lkeap`, …) can also be configured with `--api-key`, `--base-url`, and `--extra`.

### Choose models explicitly (optional)
```bash
memory projects set-models \
  --generative "google/gemini-2.5-flash" \
  --embedding "google/gemini-embedding-001"
```
`PUT /projects/:id/model-config` replaces the whole model config — pass **both** flags, because an omitted one is stored as empty and clears that model. A project model config wins over the models auto-selected onto the provider config.

### Show the effective models
```bash
memory projects show-models
memory projects show-models --project <id> --output json
```
Prints each model with its source — `project`, `provider`, or `none`. `none` means that model is not configured: agents cannot run without a generative model, and no vectors are generated without an embedding model.

### Test a provider
```bash
memory provider test google --project <project-id>
# provider is one of: google, google-vertex, openai, deepseek
```
Runs a live generate call — plus an embedding call where the provider supports it — through the project's credentials. Always pass `--project` (or have `MEMORY_PROJECT_ID` set); credential resolution is project-scoped.

### List available models
```bash
memory provider models google --type generative
memory provider models google --type embedding
memory provider models
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

1. **Initial setup**: `provider configure-project <provider> --api-key …` — stores credentials, syncs models, and validates in one step
2. **Verify**: `projects show-models` — both sources must not be `none`
3. **Test live**: `provider test <provider> --project <id>` to confirm the model responds
4. **Browse models**: `provider models` to see what's available
5. **Monitor costs**: `provider usage` regularly to track token consumption

## Choosing a provider

| Provider | Command | Key format | Requires GCP project? |
|---|---|---|---|
| Google AI | `configure-project google --api-key` | `AIza...` | No |
| Vertex AI | `configure-project google-vertex --key-file --gcp-project --location` | service account JSON | Yes |
| OpenAI | `configure-project openai --api-key` | `sk-...` | No |
| DeepSeek | `configure-project deepseek --api-key` | `sk-...` | No |

## Notes

- Credentials are scoped to the **project** — there is no `memory provider configure` (org-level) command any more
- Credentials and model selection are stored and updated atomically in one call; re-saving credentials without model flags keeps the stored models
- Model auto-selection picks the best available model from the live catalog; override with `--generative-model` / `--embedding-model`, or with `memory projects set-models`
- Embeddings resolve **only** from the project's configured provider — no environment-variable fallback exists. A project with no embedding model logs one warning per project and writes no vectors
- Credentials are stored server-side (encrypted); never written to the CLI config file
- Vertex AI requires a GCP project with the Vertex AI API enabled and a service account with the `aiplatform.user` role
