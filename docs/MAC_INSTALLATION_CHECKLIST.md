# Mac Installation - Quick Checklist

Use this checklist to track your installation progress.

## ✅ Prerequisites

- [ ] Node.js >= 20.19 installed (`node --version`)
- [ ] Docker Desktop installed and running (`docker --version`)
- [ ] Repository cloned to your Mac
- [ ] Terminal open in repository root

## ✅ Part 1: Dependencies

- [ ] Run `npm install` (takes 2-5 minutes)
- [ ] No errors in installation output

## ✅ Part 2: Environment Configuration

- [ ] Created `.env` from `.env.example`
- [ ] Set `GOOGLE_API_KEY` in `.env`
- [ ] Verified database settings in `.env`
- [ ] Set `ZITADEL_ISSUER_URL=http://localhost:8080`
- [ ] Created `apps/admin/.env` from `apps/admin/.env.example`
- [ ] Set `VITE_API_BASE=http://localhost:3001`
- [ ] Set `VITE_ZITADEL_ISSUER=http://localhost:8080`
- [ ] Set redirect URIs in `apps/admin/.env`

## ✅ Part 3: Infrastructure Services

- [ ] Navigate to `../emergent-infra/zitadel`
- [ ] Created `.env` from `.env.example` (in zitadel directory)
- [ ] Run `docker compose up -d` (in zitadel directory)
- [ ] Verify Zitadel running: `curl http://localhost:8080/debug/ready`
- [ ] Return to emergent repository
- [ ] Run `npm run workspace:deps:start`
- [ ] Verify database: `npm run workspace:status` shows db healthy

## ✅ Part 4: Zitadel Application Setup

- [ ] Open Zitadel Console: http://localhost:8080
- [ ] Create organization (or use existing)
- [ ] Create project (or use existing)
- [ ] Create PKCE application "Admin SPA"
- [ ] Set redirect URI: `http://localhost:5175/auth/callback`
- [ ] Set post-logout URI: `http://localhost:5175/`
- [ ] Copy Client ID
- [ ] Update `VITE_ZITADEL_CLIENT_ID` in `apps/admin/.env`
- [ ] Add allowed origin: `http://localhost:5175`

## ✅ Part 5: Run Application

- [ ] Run `npm run workspace:start`
- [ ] Verify backend: `curl http://localhost:3001/health`
- [ ] Verify frontend: Open http://localhost:5175
- [ ] See login page with "Login with Zitadel" button

## ✅ Part 6: Test Authentication

- [ ] Click "Login with Zitadel"
- [ ] Create test user (or use existing)
- [ ] Complete login flow
- [ ] Redirected to `/admin` dashboard
- [ ] User profile visible in UI
- [ ] Navigation sidebar works

## ✅ Part 7: Test Core Functionality

- [ ] Run `npm run test:smoke` successfully
- [ ] Or: Ingest test document via API
- [ ] Verify data in database (`docker exec -it emergent-db-1 psql -U spec -d spec`)
- [ ] Check documents: `SELECT COUNT(*) FROM kb.documents;`
- [ ] Check chunks: `SELECT COUNT(*) FROM kb.chunks;`

## 🎉 Installation Complete!

**Your services are running at:**

- Frontend: http://localhost:5175
- Backend: http://localhost:3001
- Zitadel: http://localhost:8080
- Database: localhost:5432

**Daily startup:**

1. `cd ../emergent-infra/zitadel && docker compose up -d`
2. `cd ../../emergent`
3. `npm run workspace:deps:start`
4. `npm run workspace:start`
5. Open http://localhost:5175

**Daily shutdown:**

1. `npm run workspace:stop`
2. `npm run workspace:deps:stop` (optional)
3. `cd ../emergent-infra/zitadel && docker compose down` (optional)

## Common Issues

**Port conflicts?**

```bash
lsof -ti:3001   # Backend
lsof -ti:5175   # Frontend
lsof -ti:5432   # Database
```

**Database not connecting?**

```bash
npm run workspace:status
npm run workspace:deps:restart
```

**Frontend not updating?**

- Hard refresh: `Cmd+Shift+R`
- Or restart: `npm run workspace:restart -- --service admin`

**Backend not updating?**

- Usually automatic (hot reload)
- Or restart: `npm run workspace:restart -- --service server`

**View logs:**

```bash
npm run workspace:logs -- --follow
```

---

**Next:** See `docs/MAC_STANDALONE_INSTALLATION.md` for detailed instructions and troubleshooting.
