# Provider Setup

Memory uses LLM providers for text generation (chat, agents) and embedding (vector search). Credentials and model selection are configured **per project**: each project stores its own provider credential and the models it uses. There is no organization-level provider configuration — the old org-level endpoints are deprecated and returned an error.

## Supported providers

| Provider | `provider` value | Auth method |
|---|---|---|
| Google AI (Gemini API) | `google` | API key |
| Google Cloud Vertex AI | `google-vertex` | GCP project + location (+ optional service account JSON) |
| OpenAI | `openai` | API key (+ optional base URL) |
| DeepSeek | `deepseek` | API key |

Any other vendor registered in the server's vendor registry (for example `azure-openai`, `volcengine`, `lkeap`) can be configured with an API key, a base URL, and vendor-specific extra fields.

---

## Credential security

Credentials are **AES-GCM encrypted at rest** and never returned by any API response. Responses carry a `configured` flag indicating whether a credential is stored, without revealing the value.

---

## Project configuration

Every project configures its own credential. `PUT /api/v1/projects/:projectId/providers/:provider` stores the credential, syncs the provider's model catalog, auto-selects a generative and an embedding model, and runs a live test — one call.

### Configure Google AI (Gemini API)

```bash
curl -X PUT https://api.example.com/api/v1/projects/<projectId>/providers/google \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "apiKey": "AIza...",
    "generativeModel": "google/gemini-2.5-flash",
    "embeddingModel": "google/gemini-embedding-001"
  }'
```

The model fields are optional — omit them to accept the auto-selected models. Model names include the provider prefix.

### Configure Vertex AI

```bash
curl -X PUT https://api.example.com/api/v1/projects/<projectId>/providers/google-vertex \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceAccountJson": "{\"type\":\"service_account\", ...}",
    "gcpProject": "my-gcp-project",
    "location": "us-central1",
    "generativeModel": "google-vertex/gemini-2.5-flash",
    "embeddingModel": "google-vertex/gemini-embedding-001"
  }'
```

### Check configuration status

```bash
curl https://api.example.com/api/v1/projects/<projectId>/providers/google \
  -H "Authorization: Bearer <token>"
```

```json
{
  "provider": "google",
  "configured": true,
  "generativeModel": "gemini-2.5-flash",
  "embeddingModel": "gemini-embedding-001",
  "updatedAt": "2026-11-12T12:00:00Z"
}
```

### List a project's configured providers

```bash
curl https://api.example.com/api/v1/projects/<projectId>/providers \
  -H "Authorization: Bearer <token>"
```

### Remove a provider

```bash
curl -X DELETE https://api.example.com/api/v1/projects/<projectId>/providers/google \
  -H "Authorization: Bearer <token>"
```

---

## Model selection and resolution

A provider credential can carry a default generative and embedding model (auto-selected on configure, or set with the `generativeModel` / `embeddingModel` fields). A project can also pin the models explicitly through its model config:

```bash
curl -X PUT https://api.example.com/api/v1/projects/<projectId>/model-config \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "generativeModel": "google/gemini-2.5-flash",
    "embeddingModel": "google/gemini-embedding-001"
  }'
```

The body replaces the whole model config — send both fields; an omitted one is stored as empty and clears that model.

The resolution order is: **project model config → the project's provider credential → error**. Nothing is inherited from an organization, and there is no environment-variable fallback at the request level.

### Why this matters for embeddings

Embedding vectors are generated only through the provider configured on the project. A project with no embedding model logs one warning per project —

```
embeddings: embedding model was not set for this project — semantic vectors will not be generated
```

— and writes no vectors; search degrades to lexical-only. Set the embedding model, then re-embed the corpus:

```bash
memory graph reindex-embeddings --force --chunks
```

---

## Testing a provider

Test the project's credentials end-to-end:

```bash
curl -X POST "https://api.example.com/api/v1/providers/google/test?projectId=<projectId>" \
  -H "Authorization: Bearer <token>"
```

Returns `200 OK` with the model that answered, the reply, latency, and the embedding model (or an `embeddingError` when the provider cannot embed). The equivalent CLI command is `memory provider test google --project <projectId>`.

---

## Listing available models

Retrieve the cached model catalog for a provider:

```bash
curl "https://api.example.com/api/v1/providers/google/models?type=generative" \
  -H "Authorization: Bearer <token>"
```

The `type` query parameter accepts `generative` or `embedding`.

---

## Request / response reference

### `PUT /api/v1/projects/:projectId/providers/:provider`

| Field | Type | Required | Description |
|---|---|---|---|
| `apiKey` | string | Google AI, OpenAI, DeepSeek | API key |
| `serviceAccountJson` | string | No | Full service account JSON as a string (Vertex AI) |
| `gcpProject` | string | Vertex AI | GCP project ID |
| `location` | string | Vertex AI | Region, e.g. `us-central1` |
| `baseUrl` | string | No | Override the provider endpoint (OpenAI-compatible) |
| `generativeModel` | string | No | Generative model, `provider/model` form |
| `embeddingModel` | string | No | Embedding model, `provider/model` form |
| `extraConfig` | object | No | Vendor-specific fields (e.g. `api_version`) |

Omitting credential fields on an update reuses the stored credential, and omitting the model fields keeps the stored models.

---

## Usage tracking

Every LLM call is recorded asynchronously. Events are buffered and bulk-inserted every 5 seconds (or at 100 events, whichever comes first).

### View org usage

```bash
curl "https://api.example.com/api/v1/organizations/<orgId>/usage?since=2026-03-01" \
  -H "Authorization: Bearer <token>"
```

### View project usage

```bash
curl "https://api.example.com/api/v1/projects/<projectId>/usage" \
  -H "Authorization: Bearer <token>"
```

### Usage event fields

| Field | Description |
|---|---|
| `provider` | Provider slug (`google`, `google-vertex`, `openai`, `deepseek`, …) |
| `model` | Model name |
| `operation` | `generate` or `embed` |
| `textInputTokens` | Text tokens sent to the model |
| `imageInputTokens` | Image tokens (multimodal) |
| `outputTokens` | Tokens in the response |
| `estimatedCostUsd` | Estimated cost in USD |
| `createdAt` | Timestamp |

Cost estimates use org-configured custom rates when available, falling back to global retail rates per 1M tokens.

---

## Model name examples

| Use case | Model |
|---|---|
| General generation (fast) | `google/gemini-2.5-flash` |
| Extended context / reasoning | `google/gemini-2.5-pro` |
| Embeddings | `google/gemini-embedding-001` |
