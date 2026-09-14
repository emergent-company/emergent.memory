-- PostgreSQL Initialization Script for Spec Server
-- ===================================================
-- Purpose: Create PostgreSQL extensions and roles
-- Schema creation is handled by application migrations (apps/server-go/migrations/)
-- 
-- This keeps Docker init minimal and ensures schema consistency with migrations.
-- Run migrations after container starts: npm run db:migrate
--
-- NOTE: Zitadel is managed externally via emergent-infra/zitadel

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
-- See: apps/server-go/migrations/0001_init.sql
-- Run: npm run db:migrate