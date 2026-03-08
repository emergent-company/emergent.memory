# Provider Setup

Memory uses LLM providers for text generation (chat, agents) and embedding (vector search). Credentials are configured at the **org level** as a default, and can be overridden at the **project level** when a project needs a different model or billing account.

## Supported providers

| Provider | `provider` value | Auth method |
|---|---|---|
| Google AI (Gemini API) | `google-ai` | API key |
| Google Cloud Vertex AI | `vertex-ai` | Service account JSON |

---

## Credential security

Credentials are **AES-GCM encrypted at rest** and never returned by any API response. The `configured` flag in responses indicates whether a credential is stored without revealing the value.

---

## Org-level configuration

Org-level credentials are the default fallback for all projects in the org.

### Configure Google AI (Gemini API)

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/providers/google-ai \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "apiKey": "AIza...",
    "generativeModel": "gemini-2.0-flash",
    "embeddingModel": "text-embedding-004"
  }'
```

### Configure Vertex AI

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/providers/vertex-ai \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceAccountJson": "{\"type\":\"service_account\", ...}",
    "gcpProject": "my-gcp-project",
    "location": "us-central1",
    "generativeModel": "gemini-2.0-flash",
    "embeddingModel": "text-embedding-004"
  }'
```

### Check configuration status

```bash
curl https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/providers/google-ai \
  -H "Authorization: Bearer <token>"
```

```json
{
  "provider": "google-ai",
  "configured": true,
  "generativeModel": "gemini-2.0-flash",
  "embeddingModel": "text-embedding-004",
  "updatedAt": "2026-03-01T12:00:00Z"
}
```

### List all configured providers

```bash
curl https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/providers \
  -H "Authorization: Bearer <token>"
```

### Remove a provider

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/providers/google-ai \
  -H "Authorization: Bearer <token>"
```

---

## Project-level overrides

A project-level config overrides the org default for that project only. Useful when:

- A project needs a different billing account
- A project requires a different model (e.g. a larger context model for long documents)
- You are running A/B tests across projects

```bash
curl -X PUT https://api.dev.emergent-company.ai/api/v1/projects/<projectId>/providers/google-ai \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "apiKey": "AIza...",
    "generativeModel": "gemini-2.5-pro",
    "embeddingModel": "text-embedding-004"
  }'
```

The resolution order is: **project config → org config → error**.

---

## Testing a provider

Before relying on a provider for production workloads, test the credentials:

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/providers/google-ai/test \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"apiKey": "AIza..."}'
```

Returns `200 OK` with a `{"success": true}` body on success, or an error message on failure.

---

## Listing available models

Retrieve the model catalog for a provider:

```bash
curl "https://api.dev.emergent-company.ai/api/v1/providers/google-ai/models?type=generative" \
  -H "Authorization: Bearer <token>"
```

The `type` query parameter accepts `generative` or `embedding`.

---

## Request / response reference

### `PUT /api/v1/organizations/:orgId/providers/:provider`

| Field | Type | Required | Description |
|---|---|---|---|
| `apiKey` | string | Google AI only | Gemini API key |
| `serviceAccountJson` | string | Vertex AI only | Full service account JSON as a string |
| `gcpProject` | string | Vertex AI only | GCP project ID |
| `location` | string | Vertex AI only | Region, e.g. `us-central1` |
| `generativeModel` | string | No | Override default generative model name |
| `embeddingModel` | string | No | Override default embedding model name |

---

## Usage tracking

Every LLM call is recorded asynchronously. Events are buffered and bulk-inserted every 5 seconds (or at 100 events, whichever comes first).

### View org usage

```bash
curl "https://api.dev.emergent-company.ai/api/v1/organizations/<orgId>/usage?since=2026-03-01" \
  -H "Authorization: Bearer <token>"
```

### View project usage

```bash
curl "https://api.dev.emergent-company.ai/api/v1/projects/<projectId>/usage" \
  -H "Authorization: Bearer <token>"
```

### Usage event fields

| Field | Description |
|---|---|
| `provider` | `google-ai` or `vertex-ai` |
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
| General generation (fast) | `gemini-2.0-flash` |
| Extended context / reasoning | `gemini-2.5-pro` |
| Embeddings | `text-embedding-004` |
