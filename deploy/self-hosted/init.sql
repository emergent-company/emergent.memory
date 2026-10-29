-- PostgreSQL Initialization Script for Memory
-- ===================================================
-- Purpose: Create PostgreSQL extensions and roles
-- Schema creation is handled by application migrations (apps/server/migrations/)
--
-- This keeps Docker init minimal. The one object migrations do not create is
-- the `app_rls` role below; the extensions are also created by the first
-- migration (00001_baseline.sql), so a database initialised either way gets them.
-- The self-hosted server image runs migrations on start (`emergent-migrate -c up`);
-- running from a checkout instead needs `task migrate:up` first.
--
-- NOTE: Zitadel is not part of this stack. Self-hosted installs authenticate with
-- a standalone API key; an OIDC deployment is yours to run separately. Nothing
-- here creates Zitadel objects.

-- Enable required PostgreSQL extensions
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Create the app_rls role (NOLOGIN). No policy or grant in this repo references
-- it — the migrations create their policies without a role clause, and key almost
-- all of them on app.current_project_id — so treat it as reserved rather than
-- load-bearing.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_rls') THEN
        CREATE ROLE app_rls WITH NOLOGIN;
    END IF;
END
$$;

-- Note: Schema (kb, core) and tables are created by migrations
-- See: apps/server/migrations/00001_baseline.sql
-- Run: task migrate:up (or start the self-hosted server image, which does it)