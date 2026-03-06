---
name: env-editor
description: Environment file conventions for twentyfirst - which files to edit, override rules, and variable reference per app
---

## Golden Rule

**NEVER modify `.env` files directly. Always use `.env.local` for overrides.**

| File         | Purpose                                       | Git Status  |
| ------------ | --------------------------------------------- | ----------- |
| `.env`       | Default/example values, new variables         | Committed   |
| `.env.local` | Local overrides, secrets, deployment-specific | Git-ignored |

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
/.env                    → Global defaults (docker, shared vars)
/.env.local              → Global local overrides
/apps/{app}/.env         → App-specific defaults
/apps/{app}/.env.local   → App-specific local overrides
```

---

## All Environment Files

### Global (Root)

| File           | Purpose                                                 |
| -------------- | ------------------------------------------------------- |
| `/.env`        | Docker compose vars, Tolgee, Sentry placeholders        |
| `/.env.local`  | Local overrides + Vertex AI, CircleCI tokens            |
| `/.env.docker` | Docker-specific host overrides (`host.docker.internal`) |

### API (Main Backend) - `apps/api/`

| File         | Purpose                                                            |
| ------------ | ------------------------------------------------------------------ |
| `.env`       | All API config: DB, Auth0, Mailgun, Redis, ECIT Sign, Google Cloud |
| `.env.local` | Secrets: Mailgun keys, Google credentials, Auth0 secrets, ECIT key |

**Key Variables:**

- `API_PORT` - HTTP port (default: 3000)
- `DB_*` - Database connection (port 5434, user: root)
- `AUTH_*` - Auth0 configuration
- `MAILGUN_*` - Email service
- `GOOGLE_CLOUD_*` - GCS storage
- `ECIT_SIGN_*` - Document signing
- `AI_WORKER_URL` - AI service endpoint

### API-ID (Identity Service) - `apps/api-id/`

| File         | Purpose                                                      |
| ------------ | ------------------------------------------------------------ |
| `.env`       | Identity API config: DB (port 5435), Auth0, Signicat, Twilio |
| `.env.local` | Secrets: Signicat, Auth0, Twilio credentials                 |

**Key Variables:**

- `API_HTTP_PORT` - HTTP port (default: 3001)
- `DB_*` - Database connection (port 5435, user: 21st-id)
- `SIGNICAT_*` - Norwegian BankID/eID verification
- `TWILIO_*` - SMS service
- `CATALOG_API_SECRET` - Internal API auth

### API-AI (AI Worker) - `apps/api-ai/`

| File         | Purpose                                     |
| ------------ | ------------------------------------------- |
| `.env`       | AI worker config: DB, Redis, AI service URL |
| `.env.local` | Google Cloud credentials                    |

**Key Variables:**

- `API_HTTP_PORT` - HTTP port (default: 3002)
- `AI_SERVICE_URL` - Python AI service endpoint
- `MCP_AUTH_KEY` - MCP authentication

### Web (Main Frontend) - `apps/web/`

| File         | Purpose                                      |
| ------------ | -------------------------------------------- |
| `.env`       | Frontend config: API URLs, Google Maps, Auth |
| `.env.local` | Intercom, Auth client ID, external URLs      |

**Key Variables:**

- `API_URL` - Backend API endpoint
- `WEB_ID_URL` - Identity app URL
- `AUTH_CLIENT_ID` - Auth0 client ID
- `GOOGLE_MAPS_KEY` - Maps API key

### Web-ID (Identity Frontend) - `apps/web-id/`

| File         | Purpose                                        |
| ------------ | ---------------------------------------------- |
| `.env`       | Identity frontend config: API URLs, GTM        |
| `.env.local` | GTM credentials, Auth client ID, external URLs |

**Key Variables:**

- `IDENTITY_API_URL` - Identity API endpoint
- `APP_API_URL` - Main API endpoint
- `GTM_*` - Google Tag Manager
- `AUTH_CLIENT_ID` - Auth0 client ID

### Cache Manager - `apps/cache-manager/`

| File   | Purpose                                                   |
| ------ | --------------------------------------------------------- |
| `.env` | Cache service config: own DB (port 5436), Redis, API keys |

**Key Variables:**

- `DB_PORT` - Database port (5436 - separate DB)
- `REDIS_PORT` - Redis port (6377 - separate instance)
- `CACHE_API_KEY` - API authentication
- `CF_*` - Cloudflare integration

---

## Database Connections

| App           | Port | User          | Database      |
| ------------- | ---- | ------------- | ------------- |
| api           | 5434 | root          | root          |
| api-ai        | 5434 | root          | root          |
| api-id        | 5435 | 21st-id       | 21st-id       |
| cache-manager | 5436 | cache-manager | cache-manager |

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
# Override API URL for local tunnel
API_URL=https://your-ngrok-url.ngrok-free.app
```

### Checking Current Values

```bash
# See effective value (after all overrides)
grep VAR_NAME apps/api/.env apps/api/.env.local

# Or source and echo
(source apps/api/.env && source apps/api/.env.local 2>/dev/null && echo $VAR_NAME)
```

---

## External Service Credentials

These should **only** exist in `.env.local` files:

| Service      | Variables                                | Location                                        |
| ------------ | ---------------------------------------- | ----------------------------------------------- |
| Auth0        | `AUTH_CLIENT_ID_*`, `AUTH_SECRET_*`      | `apps/api/.env.local`, `apps/api-id/.env.local` |
| Mailgun      | `MAILGUN_API_KEY`, `MAILGUN_SENDING_KEY` | `apps/api/.env.local`                           |
| Google Cloud | `GOOGLE_CLOUD_CREDENTIALS`               | `apps/api/.env.local`, `apps/api-ai/.env.local` |
| Signicat     | `SIGNICAT_CLIENT_ID`, `SIGNICAT_SECRET`  | `apps/api-id/.env.local`                        |
| Twilio       | `TWILIO_*`                               | `apps/api-id/.env.local`                        |
| ECIT Sign    | `ECIT_SIGN_API_KEY`                      | `apps/api/.env.local`                           |
| Intercom     | `INTERCOM_APP_ID`, `INTERCOM_SECRET`     | `apps/api/.env.local`, `apps/web/.env.local`    |

---

## Troubleshooting

### Variable Not Taking Effect

1. Check load order - `.env.local` overrides `.env`
2. Restart the app after changes
3. Check for typos in variable names

### Secrets Appearing in Git

1. Move to `.env.local` immediately
2. Verify `.env.local` is in `.gitignore`
3. Consider rotating the exposed credential

### App Can't Connect to Service

1. Check if URL needs `http://` vs `https://`
2. Verify port matches running service
3. Check if `localhost` vs `127.0.0.1` vs `host.docker.internal` is needed
