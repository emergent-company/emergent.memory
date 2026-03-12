---
name: env-editor
description: Environment file conventions for emergent.memory - which files to edit, override rules, and variable reference per app
---

## Golden Rule

**NEVER put secrets in committed files. Always use `.env.local` for overrides and secrets.**

| File                      | Purpose                                          | Git Status  |
| ------------------------- | ------------------------------------------------ | ----------- |
| `.env`                    | Safe workspace defaults (no secrets)             | Committed   |
| `.env.local`              | Local overrides, secrets, deployment-specific    | Git-ignored |
| `.env.example`            | Documentation of all available variables         | Committed   |
| `.env.production.example` | Production variable reference                    | Committed   |
| `apps/server/.env`        | Go server app-specific defaults                  | Committed   |

### When to Edit `.env`

- Adding a **new** variable (with empty or example value)
- Updating documentation comments
- Changing non-sensitive defaults

### When to Edit `.env.local`

- **Always** for secrets (API keys, passwords, tokens)
- **Always** for deployment-specific values (URLs, ports)
- **Always** for overriding any existing variable

---

## File Hierarchy

Environment files are loaded with later files overriding earlier ones:

```
/.env                    → Workspace-level defaults
/.env.local              → Workspace-level local overrides (gitignored)
/apps/server/.env        → Go server defaults
/apps/server/.env.local  → Go server local overrides (gitignored)
```

---

## Key Variables

### Workspace (Root `.env`)

| Variable                  | Purpose                                           | Default         |
| ------------------------- | ------------------------------------------------- | --------------- |
| `NAMESPACE`               | PM2 process namespace                             | `emergent`      |
| `ADMIN_PORT`              | Admin frontend port                               | `5200`          |
| `SERVER_PORT`             | Go API server port                                | `5300`          |
| `STORAGE_PROVIDER`        | Storage backend (`minio` / `s3` / `gcs`)         | `minio`         |
| `STORAGE_ENDPOINT`        | MinIO/S3 endpoint URL                             | `localhost:9000`|
| `STORAGE_ACCESS_KEY`      | Storage access key                                | `minio`         |
| `STORAGE_SECRET_KEY`      | Storage secret (**use `.env.local`**)             | —               |
| `KREUZBERG_SERVICE_URL`   | Document parsing service URL                      | `localhost:8000`|
| `WHISPER_SERVICE_URL`     | Audio transcription service URL                   | `localhost:9876`|
| `GCP_PROJECT_ID`          | GCP project ID (Vertex AI)                        | —               |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry endpoint (opt-in)              | unset           |

### Go Server (`apps/server/`)

See `apps/server/AGENT.md` for the full variable list. Key ones:

| Variable                  | Purpose                                           |
| ------------------------- | ------------------------------------------------- |
| `POSTGRES_HOST`           | Database host                                     |
| `POSTGRES_PORT`           | Database port (default: `5436`)                   |
| `POSTGRES_USER`           | Database user                                     |
| `POSTGRES_PASSWORD`       | Database password (**use `.env.local`**)          |
| `POSTGRES_DB`             | Database name                                     |
| `ZITADEL_DOMAIN`          | Zitadel auth domain                               |
| `ZITADEL_CLIENT_ID`       | Backend OAuth client ID                           |
| `ZITADEL_CLIENT_SECRET`   | Backend OAuth client secret (**use `.env.local`**)|
| `GOOGLE_API_KEY`          | Gemini API key (**use `.env.local`**)             |

---

## Common Patterns

### Adding a New Environment Variable

1. Add to appropriate `.env` file with empty/example value:

   ```env
   # Description of what this does
   NEW_VARIABLE=
   ```

2. Add actual value to `.env.local`:
   ```env
   NEW_VARIABLE=actual-secret-value
   ```

### Overriding for Local Development

Only edit `.env.local`:

```env
# Override server port
SERVER_PORT=3002
```

### Checking Current Values

```bash
# See what's set in root .env
grep VAR_NAME .env .env.local 2>/dev/null

# See what's set in server .env
grep VAR_NAME apps/server/.env apps/server/.env.local 2>/dev/null
```

---

## Troubleshooting

### Variable Not Taking Effect

1. Check load order — `.env.local` overrides `.env`
2. Restart the server after changes (or let air hot-reload pick it up)
3. Check for typos in variable names

### Secrets Appearing in Git

1. Move to `.env.local` immediately
2. Verify `.env.local` is in `.gitignore`
3. Rotate the exposed credential
4. If already committed: purge from history with `git filter-repo`
