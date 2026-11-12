# Model Setup

Before agents, chat, `/remember`, or `/query` can run, your **project** needs a generative model and an embedding model configured. Both the provider credentials and the model selection are per-project: there is no organization-level provider configuration, and no deployment-wide embedding provider to inherit.

This guide explains how to set them up.

---

## Concepts

**Provider credentials** and **model selection** are two separate things, and both belong to a project:

| Thing | What it is | Where stored |
|---|---|---|
| Provider credential | API key or service account for an LLM provider | Project — `PUT /api/v1/projects/:projectId/providers/:provider` |
| Model config | Which specific model names to use | Project — `PUT /api/v1/projects/:projectId/model-config`, or the models auto-selected into the provider config |

**Resolution order** — when an agent, query or embedding runs, the server resolves the model like this:

1. Agent definition's own `--model` override (if set)
2. Project model config (`memory projects set-models`)
3. Model stored on the project's provider credential — set automatically when you run `memory provider configure-project`
4. Nothing found → error: `no generative model configured for project <id>`

The `*Source` reported by the effective-config endpoint is therefore `"project"`, `"provider"`, or `"none"` — never `"org"`.

### Embeddings are project-only

Embedding vectors are generated **only** through the provider configured on the project. There is no environment-variable fallback for embeddings (self-hosted deployments used to have one; it was removed so a project can never embed into a provider its owner did not choose).

If a project has no embedding model, nothing fails loudly — the server logs **one warning per project**:

```
embeddings: embedding model was not set for this project — semantic vectors will not be generated;
configure one with 'memory projects set-models --embedding <provider>/<model>'
```

Documents and objects still extract, but no vectors are written, and search degrades to lexical-only. Set an embedding model (see below) and re-embed the corpus.

### Generative environment fallback (self-hosted only)

Generative calls have one last-resort fallback for operators of self-hosted deployments: when no project credential resolves, `pkg/adk` reads the legacy per-provider environment variables — `GOOGLE_API_KEY`, `GCP_PROJECT_ID` + `VERTEX_AI_LOCATION`, `OPENAI_API_KEY`, `DEEPSEEK_API_KEY`, or `<PREFIX>_API_KEY` for any other registered vendor. This is a deployment-wide convenience and never applies to embeddings. If you are using a hosted deployment, configure the project instead.

---

## Supported Providers

| Provider slug | What you need |
|---|---|
| `google` | Google AI API key (`--api-key`) |
| `google-vertex` | GCP project ID + location (`--gcp-project`, `--location`), optional service account key file (`--key-file`) |
| `openai` | OpenAI API key (`--api-key`), optional base URL for compatible endpoints (`--base-url`) |
| `deepseek` | DeepSeek API key (`--api-key`) |

Any other registered vendor (for example `azure-openai`, `volcengine`, `lkeap`) can be configured with `--api-key`, `--base-url`, and repeatable vendor-specific `--extra key=value` pairs.

---

## CLI Quickstart

### 0. Select the project

Every command below acts on one project. Either set the active project once:

```bash
memory projects set "Product Research"    # writes MEMORY_PROJECT_ID to .env.local
```

or pass `--project <project-id>` to each command.

### 1. Configure the project's provider

```bash
# Google AI
memory provider configure-project google --api-key <YOUR_GOOGLE_API_KEY>

# Google Vertex AI
memory provider configure-project google-vertex \
  --gcp-project <PROJECT_ID> --location us-central1 --key-file <sa.json>

# OpenAI
memory provider configure-project openai --api-key <YOUR_OPENAI_API_KEY>

# DeepSeek
memory provider configure-project deepseek --api-key <YOUR_DEEPSEEK_API_KEY>
```

`configure-project` stores the credentials encrypted, syncs the model catalog, auto-selects a recommended generative and embedding model, and runs a live test — one call. To pin the models instead of accepting the defaults:

```bash
memory provider configure-project google \
  --api-key <KEY> \
  --generative-model "google/gemini-2.5-flash" \
  --embedding-model "google/gemini-embedding-001"
```

> **Model names must include the provider prefix** — e.g. `google/gemini-2.5-flash`, not `gemini-2.5-flash`. The prefix matches the provider slug.

To remove the project's configuration for a provider:

```bash
memory provider configure-project google --remove
```

### 2. Choose the models explicitly (optional)

Auto-selection is usually fine. To override the models without touching credentials:

```bash
memory projects set-models \
  --generative "google/gemini-2.5-flash" \
  --embedding "google/gemini-embedding-001"
```

> `PUT /projects/:id/model-config` replaces the whole model config — pass **both** `--generative` and `--embedding`. A flag you omit is stored as empty, which clears that model.

Because `set-models` writes an explicit project config, it takes precedence over the models stored on the provider credential — including after you later re-run `configure-project`, which updates the provider's models but not this row. Use `memory projects show-models` to see what actually wins.

### 3. Verify

```bash
memory projects show-models               # effective model + where it came from
memory provider test google --project <project-id>   # live generate + embed call
```

`show-models` prints one line per model with its source:

```
Effective model config for project 6f0c…
  Generative: google/gemini-2.5-flash                      [project]
  Embedding:  google/gemini-embedding-001                  [provider]
```

If either source is `none`, that model is not configured — agents (generative) or embeddings (embedding) will not work until you set it.

### 4. Re-embed after changing the embedding model

Vectors from two different embedding models are not comparable, so switching `embeddingModel` on a project with existing content leaves its stored vectors in the old model's space. Search will not fail loudly — it will quietly rank badly. Re-queue the corpus after the switch:

```bash
memory graph reindex-embeddings --force --chunks
```

- `--force` re-queues rows that already have a vector. Without it, only rows with
  a NULL embedding are queued, which is what you want when filling gaps rather
  than migrating.
- `--chunks` also re-queues the project's document chunks. Chunks have no other
  bulk path — they are otherwise only re-embedded when their document is
  reprocessed — so omitting it leaves the segment search corpus behind on the old
  model.
- The command returns immediately with counts of jobs queued; the embedding
  workers drain them in the background.

The same endpoint is available over HTTP: `POST /api/graph/reindex-embeddings`
with query parameters `force` and `chunks`.

### 5. Re-embed Google AI projects after this upgrade

Before this release, stored corpus text and search queries were embedded through
the same code path, so they shared one task format. They are now separated: corpus
text is embedded with the *document* task and only a search string with the *query*
task. Google AI (`google`) projects have stored corpus vectors in the query
format since an earlier release, so those vectors are no longer comparable with
query vectors and must be re-embedded:

```bash
memory graph reindex-embeddings --force --chunks
```

Vertex AI (`google-vertex`) projects do not need this: their corpus was already
document-format. OpenAI-compatible providers are unaffected. See the CHANGELOG
entry for the backend detail.

---

## API

You can also configure a project programmatically. Provider credentials and model
selection are separate endpoints, matching the CLI.

### Store provider credentials for a project

```
PUT /api/v1/projects/:projectId/providers/:provider
```

```json
{
  "apiKey": "<key>",
  "generativeModel": "google/gemini-2.5-flash",
  "embeddingModel": "google/gemini-embedding-001"
}
```

Omit the model fields to accept the auto-selected ones. Removing the config:
`DELETE /api/v1/projects/:projectId/providers/:provider`. Listing what a project
has: `GET /api/v1/projects/:projectId/providers`.

### Set project model config

```
PUT /api/v1/projects/:projectId/model-config
```

```json
{
  "generativeModel": "google/gemini-2.5-flash",
  "embeddingModel": "google/gemini-embedding-001"
}
```

The body replaces the whole row; `DELETE` on the same path clears it.

### Check effective config (resolved with source)

```
GET /api/v1/projects/:projectId/model-config/effective
```

Response:

```json
{
  "generativeModel": "google/gemini-2.5-flash",
  "generativeModelSource": "provider",
  "embeddingModel": "google/gemini-embedding-001",
  "embeddingModelSource": "project"
}
```

`generativeModelSource` / `embeddingModelSource` values: `"project"`, `"provider"`, or `"none"`.

> There is no organization-level model-config endpoint. `PUT /api/v1/organizations/:orgId/model-config` no longer exists; org-level provider configuration is deprecated server-side and returns `org-level provider config is deprecated`.

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
| `no generative model configured for project <id>` | No project model config and no model on the project's provider credential | Run `memory provider configure-project <provider> --api-key <key>`, or `memory projects set-models --generative <provider>/<model> --embedding <provider>/<model>` |
| `embeddings: embedding model was not set for this project …` (logged once per project) | Project provider has no embedding model — embeddings are project-only, with no env fallback | Run `memory projects set-models --embedding <provider>/<model>` (pass `--generative` too), then `memory graph reindex-embeddings --force --chunks` |
| `embedding provider credential is unusable` | The project's embedding provider is configured but its credential cannot be used (e.g. a Google provider with no API key) | Re-run `memory provider configure-project <provider> --api-key <key>` |
| `no project in context for embedding resolution` | The call is not project-scoped (global/org-scoped skill or an API token without a project) | Use a project-scoped token, or run the operation inside a project |
| `model name has no provider prefix` | Model name like `gemini-2.0-flash` instead of `google/gemini-2.0-flash` | Prefix the model name with the provider slug |
| `org-level provider config is deprecated` | A client still calls an org-level provider or model-config endpoint | Use `memory provider configure-project` / `memory projects set-models`; these are project-scoped |
| `memory provider test <provider> --project <id>` reports that no credentials were found | The project has no provider config for that provider | Run `memory provider configure-project <provider> --api-key <key>` for this project |

---

## Full Project Init Example

A complete sequence from zero to a working project with models configured.

```bash
# 1. Create org and project
memory orgs create --name "Acme Corp"
memory projects create --name "Product Research" --org-id <org-id>

# 2. Set the new project as active (writes MEMORY_PROJECT_ID to .env.local)
memory projects set "Product Research"

# 3. Configure this project's provider — stores credentials, syncs the model
#    catalog, auto-selects generative + embedding models, and runs a live test
memory provider configure-project google --api-key $GOOGLE_AI_API_KEY

# 4. Optional: pin specific models (overrides the auto-selected ones)
memory projects set-models \
  --generative "google/gemini-2.5-flash" \
  --embedding "google/gemini-embedding-001"

# 5. Verify the effective config — sources must not be "none"
memory projects show-models

# 6. Smoke test — live generate + embed call through this project's credentials
memory provider test google --project <project-id>
```

### Split providers — generation and embeddings from different vendors

DeepSeek and OpenAI have no embedding API, so a project that generates with them
still needs a separate embedding provider. Store both credentials, then pin the
pair with an explicit project model config:

```bash
# 1. Store the two credentials
memory provider configure-project deepseek --api-key $DEEPSEEK_API_KEY
memory provider configure-project google --api-key $GOOGLE_AI_API_KEY

# 2. Pin generation to DeepSeek and embeddings to Google AI — a project model
#    config wins over the models auto-selected onto each provider config
memory projects set-models \
  --generative "deepseek/deepseek-chat" \
  --embedding "google/gemini-embedding-001"

# 3. Show which provider each model resolved to
memory projects show-models
```

`show-models` reports both models with their sources; there is no org default to
fall back on, so a project missing either one needs it set explicitly.

---

## Next Steps

- [Agents](agents.md) — create agents that use the configured model
- [Chat](chat.md) — run chat sessions against your knowledge graph
