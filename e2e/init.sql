-- PostgreSQL Initialization Script for Spec Server
-- ===================================================
-- Purpose: Create PostgreSQL extensions and roles
-- Schema creation is handled by application migrations (apps/server/migrations/)
--
-- This keeps Docker init minimal. The one object migrations do not create is
-- the `app_rls` role below; extensions are also created by the first migration
-- (00001_baseline.sql), so a database initialised either way ends up the same.
-- The self-hosted server image runs `emergent-migrate -c up` on start, and
-- running the server from a checkout needs `task migrate:up` first.
--
-- NOTE: Zitadel is not part of this stack. Authentication is standalone API-key
-- or an OIDC issuer you host yourself; nothing here creates Zitadel objects.

-- Enable required PostgreSQL extensions
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Create app_rls role used for Row-Level Security policies
-- This role is referenced by RLS policies to grant access to the application
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