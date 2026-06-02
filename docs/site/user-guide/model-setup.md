# Model Setup

Before agents, chat, `/remember`, or `/query` can run, your project needs a generative model and an embedding model configured. This guide explains how to set them up.

---

## Concepts

**Provider credentials** and **model selection** are two separate things:

| Thing | What it is | Where stored |
|---|---|---|
| Provider credential | API key or service account for an LLM provider | Org-level (shared) or project-level (override) |
| Model config | Which specific model names to use | Org-level (default) or project-level (override) |

**Resolution order** — when an agent or query runs, the server resolves the model like this:

1. Agent definition's own `--model` override (if set)
2. Project-level model config (`PUT /api/v1/projects/:projectId/model-config`)
3. Org-level model config (`PUT /api/v1/organizations/:orgId/model-config`)
4. Nothing found → error: `no generative model configured for project <id>`

Setting up at the **org level** is the simplest path — all projects inherit it automatically.

---

## Supported Providers

| Provider slug | What you need |
|---|---|
| `google` | Google AI API key |
| `google-vertex` | GCP project ID + location (+ optional service account key file) |
| `openai` | OpenAI API key (+ optional base URL for compatible endpoints) |
| `deepseek` | DeepSeek API key |

---

## CLI Quickstart

### 1. Configure provider credentials at org level

```bash
# Google AI
memory provider configure google --api-key <YOUR_GOOGLE_API_KEY>

# Google Vertex AI
memory provider configure google-vertex --gcp-project <PROJECT_ID> --location us-central1

# OpenAI
memory provider configure openai --api-key <YOUR_OPENAI_API_KEY>

# DeepSeek
memory provider configure deepseek --api-key <YOUR_DEEPSEEK_API_KEY>
```

By default, `configure` auto-selects recommended generative and embedding models. To specify explicitly:

```bash
memory provider configure google \
  --api-key <KEY> \
  --generative-model "google/gemini-2.5-flash" \
  --embedding-model "google/text-embedding-004"
```

> **Model names must include the provider prefix** — e.g. `google/gemini-2.5-flash`, not `gemini-2.5-flash`. The prefix matches the provider slug.

### 2. Verify

```bash
memory provider list                          # show org + all project configs
memory provider test                          # live generate call using org config
memory provider test --project <PROJECT_ID>   # test via a specific project's config
```

### 3. Override at project level (optional)

If one project needs a different model than the org default:

```bash
memory provider configure-project deepseek \
  --api-key <KEY> \
  --generative-model "deepseek/deepseek-chat"
```

To revert a project back to org config:

```bash
memory provider configure-project deepseek --remove
```

---

## API

You can also set model config directly via the REST API (useful if you are managing credentials programmatically and have already stored provider credentials separately).

### Set project model config

```
PUT /api/v1/projects/:projectId/model-config
```

```json
{
  "generativeModel": "google/gemini-2.5-flash",
  "embeddingModel": "google/text-embedding-004"
}
```

### Set org model config

```
PUT /api/v1/organizations/:orgId/model-config
```

Same body shape.

### Check effective config (resolved with source)

```
GET /api/v1/projects/:projectId/model-config/effective
```

Response:

```json
{
  "generativeModel": "google/gemini-2.5-flash",
  "generativeModelSource": "org",
  "embeddingModel": "google/text-embedding-004",
  "embeddingModelSource": "org"
}
```

`generativeModelSource` / `embeddingModelSource` values: `"project"`, `"org"`, or `"none"`.

---

## Browse Available Models

```bash
memory provider models google                          # all google models
memory provider models google --type generative
memory provider models google --type embedding
memory provider models deepseek
```

---

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `no generative model configured for project <id>` | No model config at project or org level | Run `memory provider configure <provider> --api-key <key>` |
| `no project ID in context — cannot resolve generative model` | Auth context missing project ID | Ensure you are calling the endpoint with a valid project-scoped API token |
| `model name has no provider prefix` | Model name like `gemini-2.0-flash` instead of `google/gemini-2.0-flash` | Prefix the model name with the provider slug |
| `model resolver error for project <id>` | Provider credentials not stored or invalid | Run `memory provider test --project <id>` to diagnose |

---

## Next Steps

- [Agents](agents.md) — create agents that use the configured model
- [Chat](chat.md) — run chat sessions against your knowledge graph
