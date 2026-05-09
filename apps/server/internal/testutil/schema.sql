
SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;
CREATE SCHEMA core;

CREATE SCHEMA kb;

CREATE TYPE kb.document_conversion_status AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed',
    'not_required'
);

CREATE TYPE kb.email_delivery_status AS ENUM (
    'pending',
    'delivered',
    'opened',
    'clicked',
    'bounced',
    'soft_bounced',
    'complained',
    'unsubscribed',
    'failed'
);

CREATE FUNCTION core.update_email_preferences_timestamp() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
      BEGIN
        NEW.updated_at = NOW();
        RETURN NEW;
      END;
      $$;

CREATE FUNCTION kb.get_object_revision_count(p_object_id uuid) RETURNS integer
    LANGUAGE plpgsql STABLE
    AS $$
      DECLARE 
        v_canonical_id UUID;
        v_count INTEGER;
      BEGIN 
        -- Get canonical_id for the object
        SELECT canonical_id INTO v_canonical_id
        FROM kb.graph_objects
        WHERE id = p_object_id
        LIMIT 1;
        
        IF v_canonical_id IS NULL THEN 
          RETURN 0;
        END IF;
        
        -- Get count from materialized view (fast)
        SELECT revision_count INTO v_count
        FROM kb.graph_object_revision_counts
        WHERE canonical_id = v_canonical_id;
        
        -- Fallback to live count if not in materialized view
        IF v_count IS NULL THEN
          SELECT COUNT(*)::INTEGER INTO v_count
          FROM kb.graph_objects
          WHERE canonical_id = v_canonical_id
            AND deleted_at IS NULL;
        END IF;
        
        RETURN COALESCE(v_count, 0);
      END;
      $$;

CREATE FUNCTION kb.refresh_revision_counts() RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $$
      DECLARE 
        refresh_start TIMESTAMPTZ;
        refresh_end TIMESTAMPTZ;
        refresh_duration INTERVAL;
        is_populated BOOLEAN;
      BEGIN 
        refresh_start := clock_timestamp();
        
        -- Check if materialized view has been populated by checking pg_class
        SELECT relispopulated INTO is_populated
        FROM pg_class
        WHERE relname = 'graph_object_revision_counts'
        AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'kb');
        
        -- Use CONCURRENTLY only if already populated, otherwise do regular refresh
        IF is_populated THEN
          REFRESH MATERIALIZED VIEW CONCURRENTLY kb.graph_object_revision_counts;
        ELSE
          REFRESH MATERIALIZED VIEW kb.graph_object_revision_counts;
        END IF;
        
        refresh_end := clock_timestamp();
        refresh_duration := refresh_end - refresh_start;
        
        RAISE NOTICE 'Revision counts refreshed in %', refresh_duration;
        
        RETURN (
          SELECT COUNT(*)::INTEGER
          FROM kb.graph_object_revision_counts
        );
      END;
      $$;

CREATE FUNCTION kb.update_data_source_sync_jobs_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
      BEGIN
        NEW.updated_at = NOW();
        RETURN NEW;
      END;
      $$;

CREATE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- On UPDATE, skip the expensive recompute if the source fields are unchanged.
    -- On INSERT, TG_OP = 'INSERT' so OLD is undefined — always compute.
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts :=
        setweight(to_tsvector('simple', coalesce(NEW.key, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.type, '')), 'B') ||
        setweight(to_tsvector('simple', coalesce(
            (SELECT string_agg(value::text, ' ')
             FROM jsonb_each_text(CASE WHEN jsonb_typeof(NEW.properties) = 'object' THEN NEW.properties ELSE '{}'::jsonb END)),
            ''
        )), 'C');
    RETURN NEW;
END;
$$;

CREATE FUNCTION kb.update_tsv() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- On UPDATE, only recompute when text actually changed.
    IF TG_OP = 'UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text THEN
        RETURN NEW;
    END IF;

    NEW.tsv := to_tsvector('simple', NEW.text);
    RETURN NEW;
END;
$$;
SET default_tablespace = '';

SET default_table_access_method = heap;
CREATE TABLE core.api_tokens (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid,
    user_id uuid,
    name character varying(255) NOT NULL,
    token_hash character varying(64) NOT NULL,
    token_prefix character varying(12) NOT NULL,
    scopes text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone,
    token_encrypted text,
    expires_at timestamp with time zone
);

CREATE TABLE core.superadmins (
    user_id uuid NOT NULL,
    granted_by uuid,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    revoked_by uuid,
    notes text,
    role character varying(50) DEFAULT 'superadmin_full'::character varying NOT NULL,
    CONSTRAINT superadmins_role_check CHECK (((role)::text = ANY ((ARRAY['superadmin_full'::character varying, 'superadmin_readonly'::character varying])::text[])))
);

CREATE TABLE core.user_email_preferences (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    release_emails_enabled boolean DEFAULT true NOT NULL,
    marketing_emails_enabled boolean DEFAULT true NOT NULL,
    unsubscribe_token character varying(64) DEFAULT encode(public.gen_random_bytes(32), 'hex'::text) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE core.user_emails (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    user_id uuid NOT NULL,
    email text NOT NULL,
    verified boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE core.user_profiles (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    zitadel_user_id text NOT NULL,
    first_name text,
    last_name text,
    display_name text,
    phone_e164 text,
    avatar_object_key text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    welcome_email_sent_at timestamp with time zone,
    deleted_at timestamp with time zone,
    deleted_by uuid,
    last_synced_at timestamp with time zone,
    last_activity_at timestamp with time zone
);

CREATE TABLE kb.acp_run_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    event_type text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.acp_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    agent_name text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.adk_events (
    id text NOT NULL,
    app_name text NOT NULL,
    user_id text NOT NULL,
    session_id text NOT NULL,
    invocation_id text,
    author text,
    actions jsonb,
    long_running_tool_ids_json jsonb,
    branch text,
    "timestamp" timestamp with time zone DEFAULT now() NOT NULL,
    content jsonb,
    grounding_metadata jsonb,
    custom_metadata jsonb,
    usage_metadata jsonb,
    citation_metadata jsonb,
    partial boolean,
    turn_complete boolean,
    error_code text,
    error_message text,
    interrupted boolean
);

CREATE TABLE kb.adk_sessions (
    id text NOT NULL,
    app_name text NOT NULL,
    user_id text NOT NULL,
    state jsonb DEFAULT '{}'::jsonb,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.adk_states (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    scope text NOT NULL,
    app_name text NOT NULL,
    user_id text,
    session_id text,
    state jsonb DEFAULT '{}'::jsonb,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.agent_definitions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    product_id uuid,
    project_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    system_prompt text,
    model jsonb DEFAULT '{}'::jsonb,
    tools text[] DEFAULT '{}'::text[],
    flow_type character varying(50) DEFAULT 'single'::character varying NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    max_steps integer,
    default_timeout integer,
    visibility character varying(50) DEFAULT 'project'::character varying NOT NULL,
    acp_config jsonb,
    config jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    sandbox_config jsonb,
    dispatch_mode text DEFAULT 'sync'::text NOT NULL,
    skills text[] DEFAULT '{}'::text[] NOT NULL,
    banned_tools text[]
);

CREATE TABLE kb.agent_processing_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    graph_object_id uuid NOT NULL,
    object_version integer NOT NULL,
    event_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    error_message text,
    result_summary jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_agent_processing_log_event_type CHECK ((event_type = ANY (ARRAY['created'::text, 'updated'::text, 'deleted'::text]))),
    CONSTRAINT chk_agent_processing_log_status CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text, 'failed'::text, 'abandoned'::text, 'skipped'::text])))
);

CREATE TABLE kb.agent_questions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    project_id uuid NOT NULL,
    question text NOT NULL,
    options jsonb DEFAULT '[]'::jsonb NOT NULL,
    response text,
    responded_by uuid,
    responded_at timestamp with time zone,
    status text DEFAULT 'pending'::text NOT NULL,
    notification_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.agent_run_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 1 NOT NULL,
    next_run_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT agent_run_jobs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text, 'failed'::text])))
);

CREATE TABLE kb.agent_run_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    role text NOT NULL,
    content jsonb DEFAULT '{}'::jsonb NOT NULL,
    step_number integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE kb.agent_run_tool_calls (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    message_id uuid,
    tool_name character varying(255) NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(20) DEFAULT 'completed'::character varying NOT NULL,
    duration_ms integer,
    step_number integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE kb.agent_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    status text NOT NULL,
    started_at timestamp with time zone NOT NULL,
    completed_at timestamp with time zone,
    duration_ms integer,
    summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_message text,
    skip_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    parent_run_id uuid,
    step_count integer DEFAULT 0 NOT NULL,
    max_steps integer,
    resumed_from uuid,
    session_status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    trigger_source text,
    trigger_metadata jsonb,
    trigger_message text,
    trace_id text,
    root_run_id uuid,
    model text,
    acp_session_id uuid,
    provider text,
    tools text[] DEFAULT '{}'::text[] NOT NULL,
    agent_definition_id uuid
);

CREATE TABLE kb.agent_sandboxes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_session_id uuid,
    container_type text NOT NULL,
    provider text NOT NULL,
    provider_workspace_id text DEFAULT ''::text NOT NULL,
    repository_url text,
    branch text,
    deployment_mode text DEFAULT 'self-hosted'::text NOT NULL,
    lifecycle text DEFAULT 'ephemeral'::text NOT NULL,
    status text DEFAULT 'creating'::text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_used_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    expires_at timestamp with time zone,
    resource_limits jsonb DEFAULT '{}'::jsonb,
    snapshot_id text,
    mcp_config jsonb,
    metadata jsonb DEFAULT '{}'::jsonb,
    base_image text,
    image_digest text
);

CREATE TABLE kb.agent_webhook_hooks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    project_id text NOT NULL,
    label text NOT NULL,
    token_hash text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    rate_limit_config jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.agents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    strategy_type text NOT NULL,
    prompt text,
    cron_schedule text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    description text,
    last_run_at timestamp with time zone,
    last_run_status text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    trigger_type text DEFAULT 'schedule'::text NOT NULL,
    reaction_config jsonb,
    execution_mode text DEFAULT 'execute'::text,
    capabilities jsonb,
    project_id uuid NOT NULL,
    consecutive_failures integer DEFAULT 0 NOT NULL,
    disabled_reason text,
    agent_definition_id uuid,
    CONSTRAINT chk_agents_execution_mode CHECK ((execution_mode = ANY (ARRAY['suggest'::text, 'execute'::text, 'hybrid'::text]))),
    CONSTRAINT chk_agents_trigger_type CHECK ((trigger_type = ANY (ARRAY['schedule'::text, 'manual'::text, 'reaction'::text])))
);

CREATE TABLE kb.audit_log (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    "timestamp" timestamp with time zone DEFAULT now() NOT NULL,
    event_type text NOT NULL,
    outcome text NOT NULL,
    user_id uuid,
    user_email text,
    resource_type text,
    resource_id text,
    action text NOT NULL,
    endpoint text NOT NULL,
    http_method text NOT NULL,
    status_code integer,
    error_code text,
    error_message text,
    ip_address text,
    user_agent text,
    request_id text,
    details jsonb
);

CREATE TABLE kb.auth_introspection_cache (
    token_hash character varying(128) NOT NULL,
    introspection_data jsonb NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.backups (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    project_name text NOT NULL,
    storage_key text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    status text DEFAULT 'creating'::text NOT NULL,
    progress integer DEFAULT 0 NOT NULL,
    error_message text,
    backup_type text DEFAULT 'full'::text NOT NULL,
    includes jsonb DEFAULT '{}'::jsonb NOT NULL,
    stats jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    completed_at timestamp with time zone,
    expires_at timestamp with time zone,
    deleted_at timestamp with time zone,
    manifest_checksum text,
    content_checksum text,
    parent_backup_id uuid,
    baseline_backup_id uuid,
    change_window jsonb,
    CONSTRAINT backups_backup_type_check CHECK ((backup_type = ANY (ARRAY['full'::text, 'incremental'::text]))),
    CONSTRAINT backups_progress_check CHECK (((progress >= 0) AND (progress <= 100))),
    CONSTRAINT backups_status_check CHECK ((status = ANY (ARRAY['creating'::text, 'ready'::text, 'failed'::text, 'deleted'::text])))
);

CREATE TABLE kb.branch_lineage (
    branch_id uuid NOT NULL,
    ancestor_branch_id uuid NOT NULL,
    depth integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.branches (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid,
    name text NOT NULL,
    parent_branch_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    description text,
    merged_at timestamp with time zone
);

CREATE TABLE kb.chat_conversations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    title text NOT NULL,
    owner_user_id uuid,
    is_private boolean DEFAULT true NOT NULL,
    project_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    object_id uuid,
    draft_text text,
    canonical_id uuid,
    enabled_tools text[],
    agent_definition_id uuid
);

CREATE TABLE kb.chat_messages (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    conversation_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    citations jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    context_summary text,
    retrieval_context jsonb
);

CREATE TABLE kb.chunk_embedding_jobs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    chunk_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text,
    priority integer DEFAULT 0 NOT NULL,
    scheduled_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.chunks (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    document_id uuid NOT NULL,
    chunk_index integer NOT NULL,
    text text NOT NULL,
    embedding public.vector(768),
    tsv tsvector,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    metadata jsonb,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.database_backups (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    storage_key text,
    size_bytes bigint,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.document_artifacts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    artifact_type text NOT NULL,
    content jsonb,
    storage_key text,
    position_in_document integer,
    page_number integer,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.document_parsing_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid,
    project_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    source_type text NOT NULL,
    source_filename text,
    mime_type text,
    file_size_bytes bigint,
    storage_key text,
    document_id uuid,
    extraction_job_id uuid,
    parsed_content text,
    metadata jsonb DEFAULT '{}'::jsonb,
    error_message text,
    retry_count integer DEFAULT 0,
    max_retries integer DEFAULT 3,
    next_retry_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.documents (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid,
    source_url text,
    filename text,
    mime_type text,
    content text,
    content_hash text,
    parent_document_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_type text DEFAULT 'upload'::text NOT NULL,
    storage_key text,
    storage_url text,
    metadata jsonb DEFAULT '{}'::jsonb,
    file_size_bytes bigint,
    conversion_status text DEFAULT 'not_required'::text,
    conversion_error text,
    conversion_completed_at timestamp with time zone,
    file_hash text
);

CREATE TABLE kb.email_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    template_name character varying(100) NOT NULL,
    to_email character varying(320) NOT NULL,
    to_name character varying(255),
    subject character varying(500) NOT NULL,
    template_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    last_error text,
    mailgun_message_id character varying(255),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    next_retry_at timestamp with time zone,
    source_type character varying(50),
    source_id uuid,
    delivery_status kb.email_delivery_status,
    delivery_status_at timestamp with time zone,
    delivery_status_synced_at timestamp with time zone,
    CONSTRAINT email_jobs_status_check CHECK (((status)::text = ANY (ARRAY[('pending'::character varying)::text, ('processing'::character varying)::text, ('sent'::character varying)::text, ('failed'::character varying)::text, ('dead_letter'::character varying)::text])))
);

CREATE TABLE kb.email_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_job_id uuid,
    event_type character varying(50) NOT NULL,
    mailgun_event_id character varying(255),
    details jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.email_template_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    template_id uuid NOT NULL,
    version_number integer NOT NULL,
    subject_template character varying(500) NOT NULL,
    mjml_content text NOT NULL,
    variables jsonb DEFAULT '[]'::jsonb NOT NULL,
    sample_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    change_summary character varying(500),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

CREATE TABLE kb.email_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(100) NOT NULL,
    description text,
    subject_template character varying(500) NOT NULL,
    mjml_content text NOT NULL,
    variables jsonb DEFAULT '[]'::jsonb NOT NULL,
    sample_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    current_version_id uuid,
    is_customized boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);

CREATE TABLE kb.embedding_policies (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    object_type text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    max_property_size integer,
    required_labels text[] DEFAULT '{}'::text[] NOT NULL,
    excluded_labels text[] DEFAULT '{}'::text[] NOT NULL,
    relevant_paths text[] DEFAULT '{}'::text[] NOT NULL,
    excluded_statuses text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    is_system boolean DEFAULT false NOT NULL
);

CREATE TABLE kb.external_sources (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    provider_type text NOT NULL,
    external_id text NOT NULL,
    original_url text NOT NULL,
    normalized_url text NOT NULL,
    display_name text,
    mime_type text,
    sync_policy text DEFAULT 'manual'::text NOT NULL,
    sync_interval_minutes integer,
    last_checked_at timestamp with time zone,
    last_synced_at timestamp with time zone,
    last_etag text,
    status text DEFAULT 'active'::text NOT NULL,
    error_count integer DEFAULT 0 NOT NULL,
    last_error text,
    last_error_at timestamp with time zone,
    provider_metadata jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.graph_embedding_jobs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    object_id uuid NOT NULL,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text,
    priority integer DEFAULT 0 NOT NULL,
    scheduled_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.graph_objects (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    type text NOT NULL,
    key text,
    status text,
    version integer DEFAULT 1 NOT NULL,
    supersedes_id uuid,
    canonical_id uuid NOT NULL,
    properties jsonb DEFAULT '{}'::jsonb NOT NULL,
    labels text[] DEFAULT '{}'::text[] NOT NULL,
    deleted_at timestamp with time zone,
    change_summary jsonb,
    content_hash bytea,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    branch_id uuid,
    fts tsvector,
    embedding_updated_at timestamp with time zone,
    extraction_job_id uuid,
    extraction_confidence real,
    needs_review boolean DEFAULT false,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    embedding_v2 public.vector(768),
    schema_version text,
    actor_type text DEFAULT 'user'::text,
    actor_id uuid,
    migration_archive jsonb DEFAULT '[]'::jsonb,
    last_accessed_at timestamp with time zone,
    CONSTRAINT chk_graph_objects_actor_type CHECK ((actor_type = ANY (ARRAY['user'::text, 'agent'::text, 'system'::text])))
);

ALTER TABLE ONLY kb.graph_objects FORCE ROW LEVEL SECURITY;

CREATE MATERIALIZED VIEW kb.graph_object_revision_counts AS
 SELECT canonical_id,
    project_id,
    count(*) AS revision_count,
    max(version) AS latest_version,
    min(created_at) AS first_created_at,
    max(created_at) AS last_updated_at
   FROM kb.graph_objects
  WHERE (deleted_at IS NULL)
  GROUP BY canonical_id, project_id
  WITH NO DATA;

CREATE TABLE kb.graph_relationship_embedding_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    relationship_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text,
    scheduled_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT graph_relationship_embedding_jobs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text, 'failed'::text])))
);

CREATE TABLE kb.graph_relationships (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    type text NOT NULL,
    src_id uuid NOT NULL,
    dst_id uuid NOT NULL,
    properties jsonb DEFAULT '{}'::jsonb NOT NULL,
    weight real,
    valid_from timestamp with time zone,
    valid_to timestamp with time zone,
    deleted_at timestamp with time zone,
    change_summary jsonb,
    content_hash bytea,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    canonical_id uuid NOT NULL,
    supersedes_id uuid,
    version integer DEFAULT 1 NOT NULL,
    branch_id uuid,
    embedding public.vector(768),
    embedding_updated_at timestamp with time zone
);

ALTER TABLE ONLY kb.graph_relationships FORCE ROW LEVEL SECURITY;

CREATE TABLE kb.graph_schemas (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    description text,
    author text,
    license text,
    repository_url text,
    documentation_url text,
    source text DEFAULT 'manual'::text,
    discovery_job_id uuid,
    pending_review boolean DEFAULT false NOT NULL,
    object_type_schemas jsonb NOT NULL,
    relationship_type_schemas jsonb DEFAULT '{}'::jsonb NOT NULL,
    ui_configs jsonb DEFAULT '{}'::jsonb NOT NULL,
    extraction_prompts jsonb DEFAULT '{}'::jsonb NOT NULL,
    sql_views jsonb DEFAULT '[]'::jsonb NOT NULL,
    signature text,
    checksum text,
    published_at timestamp with time zone DEFAULT now() NOT NULL,
    deprecated_at timestamp with time zone,
    superseded_by text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    parent_version_id uuid,
    draft boolean DEFAULT false,
    project_id uuid,
    org_id uuid,
    visibility text DEFAULT 'project'::text NOT NULL,
    migrations jsonb
);

CREATE TABLE kb.invites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    email text NOT NULL,
    role text NOT NULL,
    token text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    expires_at timestamp with time zone,
    accepted_at timestamp with time zone,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    invited_by_user_id uuid
);

CREATE TABLE kb.llm_call_logs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    process_id text NOT NULL,
    process_type text NOT NULL,
    model_name text NOT NULL,
    request_payload jsonb,
    response_payload jsonb,
    status text NOT NULL,
    error_message text,
    input_tokens integer,
    output_tokens integer,
    total_tokens integer,
    cost_usd numeric(10,6),
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    duration_ms integer
);

CREATE TABLE kb.llm_usage_events (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    org_id uuid NOT NULL,
    provider character varying(50) NOT NULL,
    model character varying(255) NOT NULL,
    operation character varying(50) DEFAULT 'generate'::character varying NOT NULL,
    text_input_tokens bigint DEFAULT 0 NOT NULL,
    image_input_tokens bigint DEFAULT 0 NOT NULL,
    video_input_tokens bigint DEFAULT 0 NOT NULL,
    audio_input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    estimated_cost_usd numeric(12,8) DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    run_id uuid,
    root_run_id uuid,
    cached_tokens bigint DEFAULT 0 NOT NULL
);

CREATE TABLE kb.mcp_server_tools (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    server_id uuid NOT NULL,
    tool_name character varying(255) NOT NULL,
    description text,
    input_schema jsonb DEFAULT '{}'::jsonb,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    config jsonb,
    config_keys text[] DEFAULT '{}'::text[]
);

CREATE TABLE kb.mcp_servers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    type character varying(50) NOT NULL,
    command text,
    args text[] DEFAULT '{}'::text[],
    env jsonb DEFAULT '{}'::jsonb,
    url text,
    headers jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    description text
);

CREATE TABLE kb.merge_provenance (
    child_version_id uuid NOT NULL,
    parent_version_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.notifications (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid,
    user_id uuid NOT NULL,
    title text NOT NULL,
    message text NOT NULL,
    type text,
    severity text DEFAULT 'info'::text NOT NULL,
    related_resource_type text,
    related_resource_id uuid,
    read boolean DEFAULT false NOT NULL,
    dismissed boolean DEFAULT false NOT NULL,
    dismissed_at timestamp with time zone,
    actions jsonb DEFAULT '[]'::jsonb NOT NULL,
    expires_at timestamp with time zone,
    read_at timestamp with time zone,
    importance text DEFAULT 'other'::text NOT NULL,
    cleared_at timestamp with time zone,
    snoozed_until timestamp with time zone,
    category text,
    source_type text,
    source_id text,
    action_url text,
    action_label text,
    group_key text,
    details jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    action_status text,
    action_status_at timestamp with time zone,
    action_status_by uuid,
    task_id uuid
);

CREATE TABLE kb.object_chunks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    object_id uuid NOT NULL,
    chunk_id uuid NOT NULL,
    extraction_job_id uuid,
    confidence real,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.object_extraction_jobs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    document_id uuid,
    chunk_id uuid,
    job_type text DEFAULT 'full_extraction'::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    enabled_types text[] DEFAULT '{}'::text[] NOT NULL,
    extraction_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    objects_created integer DEFAULT 0 NOT NULL,
    relationships_created integer DEFAULT 0 NOT NULL,
    suggestions_created integer DEFAULT 0 NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    error_message text,
    retry_count integer DEFAULT 0 NOT NULL,
    max_retries integer DEFAULT 3 NOT NULL,
    created_by uuid,
    reprocessing_of uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_type text,
    source_id text,
    source_metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    debug_info jsonb,
    total_items integer DEFAULT 0 NOT NULL,
    processed_items integer DEFAULT 0 NOT NULL,
    successful_items integer DEFAULT 0 NOT NULL,
    failed_items integer DEFAULT 0 NOT NULL,
    logs jsonb DEFAULT '[]'::jsonb NOT NULL,
    discovered_types jsonb DEFAULT '[]'::jsonb,
    created_objects jsonb DEFAULT '[]'::jsonb,
    error_details jsonb
);

CREATE TABLE kb.object_extraction_logs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    extraction_job_id uuid NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    step_index integer NOT NULL,
    operation_type text NOT NULL,
    operation_name text,
    step text NOT NULL,
    status text NOT NULL,
    message text,
    input_data jsonb,
    output_data jsonb,
    error_message text,
    error_stack text,
    error_details jsonb,
    duration_ms integer,
    tokens_used integer,
    entity_count integer,
    relationship_count integer
);

CREATE TABLE kb.object_type_schemas (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid,
    type text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    supersedes_id uuid,
    canonical_id uuid,
    json_schema jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.org_provider_configs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    org_id uuid NOT NULL,
    provider character varying(50) NOT NULL,
    encrypted_credential bytea NOT NULL,
    encryption_nonce bytea NOT NULL,
    gcp_project character varying(255),
    location character varying(100),
    generative_model character varying(255),
    embedding_model character varying(255),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    base_url text
);

CREATE TABLE kb.org_tool_settings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    tool_name text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    config jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE kb.organization_custom_pricing (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    org_id uuid NOT NULL,
    provider character varying(50) NOT NULL,
    model character varying(255) NOT NULL,
    text_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    image_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    video_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    audio_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    output_price numeric(12,8) DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.organization_memberships (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.orgs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    deleted_by uuid
);

CREATE TABLE kb.product_version_members (
    product_version_id uuid NOT NULL,
    object_canonical_id uuid NOT NULL,
    object_version_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.product_versions (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    description text,
    base_product_version_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.project_journal (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    event_type text NOT NULL,
    entity_type text,
    entity_id uuid,
    object_type text,
    actor_type text DEFAULT 'system'::text NOT NULL,
    actor_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    branch_id uuid
);

CREATE TABLE kb.project_journal_notes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    journal_id uuid,
    body text NOT NULL,
    actor_type text DEFAULT 'user'::text NOT NULL,
    actor_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    branch_id uuid
);

CREATE TABLE kb.project_memberships (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.project_object_schema_registry (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    type_name text NOT NULL,
    source text NOT NULL,
    schema_id uuid,
    schema_version integer DEFAULT 1 NOT NULL,
    json_schema jsonb NOT NULL,
    ui_config jsonb,
    extraction_config jsonb,
    enabled boolean DEFAULT true NOT NULL,
    discovery_confidence double precision,
    description text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    namespace text
);

CREATE TABLE kb.project_provider_configs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    provider character varying(50) NOT NULL,
    encrypted_credential bytea NOT NULL,
    encryption_nonce bytea NOT NULL,
    gcp_project character varying(255),
    location character varying(100),
    generative_model character varying(255),
    embedding_model character varying(255),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    base_url text
);

CREATE TABLE kb.project_schemas (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    schema_id uuid NOT NULL,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    installed_by uuid,
    active boolean DEFAULT true NOT NULL,
    customizations jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    removed_at timestamp with time zone
);

CREATE TABLE kb.project_settings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    category text NOT NULL,
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.projects (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    organization_id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    auto_extract_objects boolean DEFAULT false NOT NULL,
    auto_extract_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    chat_prompt_template text,
    chunking_config jsonb,
    allow_parallel_extraction boolean DEFAULT false NOT NULL,
    extraction_config jsonb,
    deleted_at timestamp with time zone,
    deleted_by uuid,
    project_info text,
    budget_usd numeric(10,4),
    budget_alert_threshold numeric(3,2) DEFAULT 0.80 NOT NULL
);

CREATE TABLE kb.provider_pricing (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    provider character varying(50) NOT NULL,
    model character varying(255) NOT NULL,
    text_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    image_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    video_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    audio_input_price numeric(12,8) DEFAULT 0 NOT NULL,
    output_price numeric(12,8) DEFAULT 0 NOT NULL,
    last_synced timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.provider_supported_models (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    provider character varying(50) NOT NULL,
    model_name character varying(255) NOT NULL,
    model_type character varying(50) NOT NULL,
    display_name character varying(255),
    last_synced timestamp with time zone DEFAULT now() NOT NULL,
    max_output_tokens integer
);

CREATE TABLE kb.release_notification_recipients (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    release_notification_id uuid NOT NULL,
    user_id uuid NOT NULL,
    email_sent boolean DEFAULT false NOT NULL,
    email_sent_at timestamp with time zone,
    mailgun_message_id character varying(255),
    email_status character varying(50) DEFAULT 'pending'::character varying,
    email_status_updated_at timestamp with time zone,
    in_app_notification_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    email_job_id uuid,
    CONSTRAINT chk_email_status CHECK (((email_status)::text = ANY (ARRAY[('pending'::character varying)::text, ('delivered'::character varying)::text, ('opened'::character varying)::text, ('failed'::character varying)::text])))
);

CREATE TABLE kb.release_notification_state (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    branch character varying(255) DEFAULT 'main'::character varying NOT NULL,
    last_notified_commit character varying(40) NOT NULL,
    last_notified_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.release_notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    version character varying(50) NOT NULL,
    from_commit character varying(40) NOT NULL,
    to_commit character varying(40) NOT NULL,
    commit_count integer NOT NULL,
    changelog_json jsonb NOT NULL,
    target_mode character varying(20) NOT NULL,
    target_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    CONSTRAINT chk_release_status CHECK (((status)::text = ANY (ARRAY[('draft'::character varying)::text, ('published'::character varying)::text]))),
    CONSTRAINT chk_target_mode CHECK (((target_mode)::text = ANY (ARRAY[('single'::character varying)::text, ('project'::character varying)::text, ('all'::character varying)::text])))
);

CREATE TABLE kb.sandbox_images (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(100) NOT NULL,
    type character varying(20) DEFAULT 'custom'::character varying NOT NULL,
    docker_ref text,
    provider character varying(20) DEFAULT 'firecracker'::character varying NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    error_msg text,
    project_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE kb.schema_migration_jobs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    from_schema_id uuid NOT NULL,
    to_schema_id uuid NOT NULL,
    chain jsonb DEFAULT '[]'::jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    risk_level text,
    objects_migrated integer DEFAULT 0 NOT NULL,
    objects_failed integer DEFAULT 0 NOT NULL,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone
);

CREATE TABLE kb.schema_migration_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    from_version text NOT NULL,
    to_version text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    status text NOT NULL,
    total_objects integer DEFAULT 0 NOT NULL,
    successful integer DEFAULT 0 NOT NULL,
    failed integer DEFAULT 0 NOT NULL,
    skipped integer DEFAULT 0 NOT NULL,
    with_warnings integer DEFAULT 0 NOT NULL,
    risk_level text DEFAULT 'unknown'::text NOT NULL,
    dry_run boolean DEFAULT false NOT NULL,
    forced boolean DEFAULT false NOT NULL,
    confirmed_data_loss boolean DEFAULT false NOT NULL,
    error_summary jsonb,
    CONSTRAINT schema_migration_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'completed'::text, 'failed'::text, 'rolled_back'::text])))
);

CREATE TABLE kb.schema_studio_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    session_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    suggestions jsonb DEFAULT '[]'::jsonb,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT template_pack_studio_messages_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text, 'system'::text])))
);

CREATE TABLE kb.schema_studio_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id text NOT NULL,
    project_id uuid NOT NULL,
    pack_id uuid,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT template_pack_studio_sessions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'completed'::text, 'discarded'::text])))
);

CREATE TABLE kb.settings (
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.skills (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    description_embedding public.vector(768),
    project_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    org_id uuid,
    CONSTRAINT chk_skills_scope CHECK ((NOT ((project_id IS NOT NULL) AND (org_id IS NOT NULL)))),
    CONSTRAINT skills_name_check CHECK (((name ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text) AND ((char_length(name) >= 1) AND (char_length(name) <= 64))))
);

CREATE TABLE kb.system_process_logs (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    process_id text NOT NULL,
    process_type text NOT NULL,
    level text NOT NULL,
    message text NOT NULL,
    metadata jsonb,
    "timestamp" timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.tags (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    product_version_id uuid NOT NULL,
    name text NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE kb.tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    resolved_at timestamp with time zone,
    resolved_by uuid,
    resolution_notes text,
    source_type text,
    source_id text,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now()
);

CREATE TABLE kb.user_recent_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id text NOT NULL,
    project_id uuid NOT NULL,
    resource_type character varying(20) NOT NULL,
    resource_id uuid NOT NULL,
    resource_name text,
    resource_subtype text,
    action_type character varying(20) NOT NULL,
    accessed_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_recent_items_action_type_check CHECK (((action_type)::text = ANY (ARRAY[('viewed'::character varying)::text, ('edited'::character varying)::text]))),
    CONSTRAINT user_recent_items_resource_type_check CHECK (((resource_type)::text = ANY (ARRAY[('document'::character varying)::text, ('object'::character varying)::text])))
);

ALTER TABLE ONLY core.user_profiles
    ADD CONSTRAINT "PK_1ec6662219f4605723f1e41b6cb" PRIMARY KEY (id);

ALTER TABLE ONLY core.user_emails
    ADD CONSTRAINT "PK_3ef6c4be97ba94ea3ba65362ad0" PRIMARY KEY (id);

ALTER TABLE ONLY core.api_tokens
    ADD CONSTRAINT api_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY core.api_tokens
    ADD CONSTRAINT api_tokens_token_hash_unique UNIQUE (token_hash);

ALTER TABLE ONLY core.superadmins
    ADD CONSTRAINT superadmins_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY core.user_email_preferences
    ADD CONSTRAINT user_email_preferences_pkey PRIMARY KEY (id);

ALTER TABLE ONLY core.user_email_preferences
    ADD CONSTRAINT user_email_preferences_token_unique UNIQUE (unsubscribe_token);

ALTER TABLE ONLY core.user_email_preferences
    ADD CONSTRAINT user_email_preferences_user_unique UNIQUE (user_id);

ALTER TABLE ONLY kb.graph_objects
    ADD CONSTRAINT "PK_078aacf1069493166009e2f1f5d" PRIMARY KEY (id);

ALTER TABLE ONLY kb.audit_log
    ADD CONSTRAINT "PK_07fefa57f7f5ab8fc3f52b3ed0b" PRIMARY KEY (id);

ALTER TABLE ONLY kb.object_type_schemas
    ADD CONSTRAINT "PK_10b0ea5bce13b0404825a0c94cd" PRIMARY KEY (id);

ALTER TABLE ONLY kb.branch_lineage
    ADD CONSTRAINT "PK_1f87552be159d70c1e49bc394d4" PRIMARY KEY (branch_id, ancestor_branch_id);

ALTER TABLE ONLY kb.graph_embedding_jobs
    ADD CONSTRAINT "PK_29374bc3691491e73c6170ff8e3" PRIMARY KEY (id);

ALTER TABLE ONLY kb.chat_messages
    ADD CONSTRAINT "PK_40c55ee0e571e268b0d3cd37d10" PRIMARY KEY (id);

ALTER TABLE ONLY kb.projects
    ADD CONSTRAINT "PK_6271df0a7aed1d6c0691ce6ac50" PRIMARY KEY (id);

ALTER TABLE ONLY kb.notifications
    ADD CONSTRAINT "PK_6a72c3c0f683f6462415e653c3a" PRIMARY KEY (id);

ALTER TABLE ONLY kb.system_process_logs
    ADD CONSTRAINT "PK_734385c231b8c9ce4b9157913ae" PRIMARY KEY (id);

ALTER TABLE ONLY kb.branches
    ADD CONSTRAINT "PK_7f37d3b42defea97f1df0d19535" PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_memberships
    ADD CONSTRAINT "PK_856d7bae2d9bddc94861d41eded" PRIMARY KEY (id);

ALTER TABLE ONLY kb.embedding_policies
    ADD CONSTRAINT "PK_923c15ce099ae3991a1d1a6b6b0" PRIMARY KEY (id);

ALTER TABLE ONLY kb.object_extraction_jobs
    ADD CONSTRAINT "PK_946f0b690e0a0972ebd0e6222d5" PRIMARY KEY (id);

ALTER TABLE ONLY kb.auth_introspection_cache
    ADD CONSTRAINT "PK_95b04c40e975a4b426cd21a07f5" PRIMARY KEY (token_hash);

ALTER TABLE ONLY kb.object_extraction_logs
    ADD CONSTRAINT "PK_9ea0a4d02ba4f16f7f390589503" PRIMARY KEY (id);

ALTER TABLE ONLY kb.orgs
    ADD CONSTRAINT "PK_9eed8bfad4c9e0dc8648e090efe" PRIMARY KEY (id);

ALTER TABLE ONLY kb.chunks
    ADD CONSTRAINT "PK_a306e60b8fdf6e7de1be4be1e6a" PRIMARY KEY (id);

ALTER TABLE ONLY kb.invites
    ADD CONSTRAINT "PK_aa52e96b44a714372f4dd31a0af" PRIMARY KEY (id);

ALTER TABLE ONLY kb.documents
    ADD CONSTRAINT "PK_ac51aa5181ee2036f5ca482857c" PRIMARY KEY (id);

ALTER TABLE ONLY kb.llm_call_logs
    ADD CONSTRAINT "PK_ad84866fef0164fcee07558a67d" PRIMARY KEY (id);

ALTER TABLE ONLY kb.product_version_members
    ADD CONSTRAINT "PK_b5b8707471c0c5c16f64f95f75c" PRIMARY KEY (product_version_id, object_canonical_id);

ALTER TABLE ONLY kb.merge_provenance
    ADD CONSTRAINT "PK_c6759cdb97dce23f85bb11cb5c1" PRIMARY KEY (child_version_id, parent_version_id);

ALTER TABLE ONLY kb.settings
    ADD CONSTRAINT "PK_c8639b7626fa94ba8265628f214" PRIMARY KEY (key);

ALTER TABLE ONLY kb.organization_memberships
    ADD CONSTRAINT "PK_cd7be805730a4c778a5f45364af" PRIMARY KEY (id);

ALTER TABLE ONLY kb.product_versions
    ADD CONSTRAINT "PK_dbd6ab6ae9343c6c6f2df5e76db" PRIMARY KEY (id);

ALTER TABLE ONLY kb.tags
    ADD CONSTRAINT "PK_e7dc17249a1148a1970748eda99" PRIMARY KEY (id);

ALTER TABLE ONLY kb.graph_relationships
    ADD CONSTRAINT "PK_e858a7876b4b8a382c481bded76" PRIMARY KEY (id);

ALTER TABLE ONLY kb.chat_conversations
    ADD CONSTRAINT "PK_ff117d9f57807c4f2e3034a39f3" PRIMARY KEY (id);

ALTER TABLE ONLY kb.acp_run_events
    ADD CONSTRAINT acp_run_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.acp_sessions
    ADD CONSTRAINT acp_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.adk_events
    ADD CONSTRAINT adk_events_pkey PRIMARY KEY (id, app_name, user_id, session_id);

ALTER TABLE ONLY kb.adk_sessions
    ADD CONSTRAINT adk_sessions_pkey PRIMARY KEY (app_name, user_id, id);

ALTER TABLE ONLY kb.adk_states
    ADD CONSTRAINT adk_states_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_definitions
    ADD CONSTRAINT agent_definitions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_processing_log
    ADD CONSTRAINT agent_processing_log_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_questions
    ADD CONSTRAINT agent_questions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_run_jobs
    ADD CONSTRAINT agent_run_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_run_messages
    ADD CONSTRAINT agent_run_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_run_tool_calls
    ADD CONSTRAINT agent_run_tool_calls_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_sandboxes
    ADD CONSTRAINT agent_sandboxes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agent_webhook_hooks
    ADD CONSTRAINT agent_webhook_hooks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.agents
    ADD CONSTRAINT agents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.chunk_embedding_jobs
    ADD CONSTRAINT chunk_embedding_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.database_backups
    ADD CONSTRAINT database_backups_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.document_artifacts
    ADD CONSTRAINT document_artifacts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.document_parsing_jobs
    ADD CONSTRAINT document_parsing_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.email_jobs
    ADD CONSTRAINT email_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.email_logs
    ADD CONSTRAINT email_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.email_template_versions
    ADD CONSTRAINT email_template_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.email_template_versions
    ADD CONSTRAINT email_template_versions_template_id_version_number_key UNIQUE (template_id, version_number);

ALTER TABLE ONLY kb.email_templates
    ADD CONSTRAINT email_templates_name_key UNIQUE (name);

ALTER TABLE ONLY kb.email_templates
    ADD CONSTRAINT email_templates_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.external_sources
    ADD CONSTRAINT external_sources_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.graph_relationship_embedding_jobs
    ADD CONSTRAINT graph_relationship_embedding_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.graph_schemas
    ADD CONSTRAINT graph_schemas_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.llm_usage_events
    ADD CONSTRAINT llm_usage_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.mcp_server_tools
    ADD CONSTRAINT mcp_server_tools_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.mcp_servers
    ADD CONSTRAINT mcp_servers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.object_chunks
    ADD CONSTRAINT object_chunks_object_id_chunk_id_key UNIQUE (object_id, chunk_id);

ALTER TABLE ONLY kb.object_chunks
    ADD CONSTRAINT object_chunks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.org_provider_configs
    ADD CONSTRAINT org_provider_configs_org_id_provider_key UNIQUE (org_id, provider);

ALTER TABLE ONLY kb.org_provider_configs
    ADD CONSTRAINT org_provider_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.org_tool_settings
    ADD CONSTRAINT org_tool_settings_org_id_tool_name_key UNIQUE (org_id, tool_name);

ALTER TABLE ONLY kb.org_tool_settings
    ADD CONSTRAINT org_tool_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.organization_custom_pricing
    ADD CONSTRAINT organization_custom_pricing_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_journal_notes
    ADD CONSTRAINT project_journal_notes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_journal
    ADD CONSTRAINT project_journal_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_memberships
    ADD CONSTRAINT project_memberships_project_id_user_id_key UNIQUE (project_id, user_id);

ALTER TABLE ONLY kb.project_object_schema_registry
    ADD CONSTRAINT project_object_schema_registry_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_provider_configs
    ADD CONSTRAINT project_provider_configs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_provider_configs
    ADD CONSTRAINT project_provider_configs_project_id_provider_key UNIQUE (project_id, provider);

ALTER TABLE ONLY kb.project_schemas
    ADD CONSTRAINT project_schemas_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_settings
    ADD CONSTRAINT project_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.project_settings
    ADD CONSTRAINT project_settings_project_id_category_key_key UNIQUE (project_id, category, key);

ALTER TABLE ONLY kb.provider_pricing
    ADD CONSTRAINT provider_pricing_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.provider_supported_models
    ADD CONSTRAINT provider_supported_models_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipien_release_notification_id_user__key UNIQUE (release_notification_id, user_id);

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.release_notification_state
    ADD CONSTRAINT release_notification_state_branch_key UNIQUE (branch);

ALTER TABLE ONLY kb.release_notification_state
    ADD CONSTRAINT release_notification_state_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.release_notifications
    ADD CONSTRAINT release_notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.sandbox_images
    ADD CONSTRAINT sandbox_images_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.schema_migration_jobs
    ADD CONSTRAINT schema_migration_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.schema_migration_runs
    ADD CONSTRAINT schema_migration_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.schema_studio_messages
    ADD CONSTRAINT schema_studio_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.schema_studio_sessions
    ADD CONSTRAINT schema_studio_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.skills
    ADD CONSTRAINT skills_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kb.organization_custom_pricing
    ADD CONSTRAINT uq_org_custom_pricing UNIQUE (org_id, provider, model);

ALTER TABLE ONLY kb.provider_supported_models
    ADD CONSTRAINT uq_provider_model UNIQUE (provider, model_name);

ALTER TABLE ONLY kb.provider_pricing
    ADD CONSTRAINT uq_provider_pricing UNIQUE (provider, model);

ALTER TABLE ONLY kb.user_recent_items
    ADD CONSTRAINT user_recent_items_pkey PRIMARY KEY (id);

CREATE INDEX "IDX_2e88b95787b903d46ab3cc3eb9" ON core.user_emails USING btree (user_id);

CREATE UNIQUE INDEX "IDX_3ef997e65ad4f83f35356a1a6e" ON core.user_profiles USING btree (zitadel_user_id);

CREATE UNIQUE INDEX "IDX_6594597afde633cfeab9a806e4" ON core.user_emails USING btree (email);

CREATE UNIQUE INDEX api_tokens_user_name_unique ON core.api_tokens USING btree (user_id, name) WHERE (revoked_at IS NULL);

CREATE INDEX idx_api_tokens_expires_at ON core.api_tokens USING btree (expires_at) WHERE (expires_at IS NOT NULL);

CREATE INDEX idx_api_tokens_project_id ON core.api_tokens USING btree (project_id);

CREATE INDEX idx_api_tokens_token_hash ON core.api_tokens USING btree (token_hash);

CREATE INDEX idx_api_tokens_user_id ON core.api_tokens USING btree (user_id);

CREATE INDEX idx_superadmins_active ON core.superadmins USING btree (user_id) WHERE (revoked_at IS NULL);

CREATE INDEX idx_user_email_preferences_token ON core.user_email_preferences USING btree (unsubscribe_token);

CREATE INDEX idx_user_email_preferences_user ON core.user_email_preferences USING btree (user_id);

CREATE INDEX idx_user_profiles_deleted_at ON core.user_profiles USING btree (deleted_at) WHERE (deleted_at IS NULL);

CREATE INDEX idx_user_profiles_last_activity_at ON core.user_profiles USING btree (last_activity_at DESC NULLS LAST) WHERE (deleted_at IS NULL);

CREATE INDEX idx_user_profiles_last_synced_at ON core.user_profiles USING btree (last_synced_at) WHERE (deleted_at IS NULL);

CREATE INDEX "IDX_1c7f91f13d7e1a438519d37ec3" ON kb.object_extraction_jobs USING btree (project_id);

CREATE UNIQUE INDEX "IDX_26573c7e713682c72216747770" ON kb.embedding_policies USING btree (project_id, object_type);

CREATE INDEX "IDX_3844c9efd6d2e06105a117f90c" ON kb.object_extraction_jobs USING btree (status);

CREATE INDEX "IDX_38a73cbcc58fbed8e62a66d79b" ON kb.project_memberships USING btree (project_id);

CREATE UNIQUE INDEX "IDX_3bbf4ea30357bf556110f034d4" ON kb.documents USING btree (project_id, content_hash) WHERE (content_hash IS NOT NULL);

CREATE INDEX "IDX_5352fc550034d507d6c76dd290" ON kb.organization_memberships USING btree (user_id);

CREATE UNIQUE INDEX "IDX_6f5a7e4467cdc44037f209122e" ON kb.chunks USING btree (document_id, chunk_index);

CREATE INDEX "IDX_7cb6c36ad5bf1bd4a413823ace" ON kb.project_memberships USING btree (user_id);

CREATE INDEX "IDX_86ae2efbb9ce84dd652e0c96a4" ON kb.organization_memberships USING btree (organization_id);

CREATE INDEX "IDX_95464140d7dc04d7efb0afd6be" ON kb.notifications USING btree (project_id);

CREATE INDEX "IDX_9a8a82462cab47c73d25f49261" ON kb.notifications USING btree (user_id);

CREATE INDEX "IDX_a0dadc1ffc4ee153226f786e99" ON kb.graph_relationships USING btree (project_id);

CREATE INDEX "IDX_a970f04cced6336cb2b1ad1f4e" ON kb.graph_relationships USING btree (src_id);

CREATE INDEX "IDX_agents_strategy_type" ON kb.agents USING btree (strategy_type);

CREATE UNIQUE INDEX "IDX_b877acbf8d466f2889a2eeb147" ON kb.project_memberships USING btree (project_id, user_id);

CREATE INDEX "IDX_b8c7752534a444c2f16ebf3d91" ON kb.graph_objects USING btree (type);

CREATE INDEX "IDX_c04db004625a1c8be8abb6c046" ON kb.graph_objects USING btree (canonical_id);

CREATE UNIQUE INDEX "IDX_caa73db1b161fa6b3a042290fe" ON kb.organization_memberships USING btree (organization_id, user_id);

CREATE UNIQUE INDEX "IDX_chat_conversations_canonical_id" ON kb.chat_conversations USING btree (canonical_id) WHERE (canonical_id IS NOT NULL);

CREATE UNIQUE INDEX "IDX_chat_conversations_object_id_unique" ON kb.chat_conversations USING btree (object_id) WHERE (object_id IS NOT NULL);

CREATE INDEX "IDX_d05c07bafeabc0850f94db035b" ON kb.auth_introspection_cache USING btree (expires_at);

CREATE INDEX "IDX_d841de45a719fe1f35213d7920" ON kb.chunks USING btree (document_id);

CREATE INDEX "IDX_df895a2e1799c53ef660d0aae6" ON kb.graph_embedding_jobs USING btree (object_id);

CREATE INDEX "IDX_e156b298c20873e14c362e789b" ON kb.documents USING btree (project_id);

CREATE INDEX "IDX_f0021c2230e47af51928f35975" ON kb.graph_embedding_jobs USING btree (status);

CREATE INDEX "IDX_f35de415032037ea629b1772e4" ON kb.graph_relationships USING btree (type);

CREATE INDEX "IDX_f8b7ed75170d2d7dca4477cc94" ON kb.notifications USING btree (read);

CREATE INDEX "IDX_f8d6b0b40d75cdabb27cf81084" ON kb.graph_relationships USING btree (dst_id);

CREATE INDEX "IDX_graph_objects_embedding_v2_ivfflat" ON kb.graph_objects USING ivfflat (embedding_v2 public.vector_cosine_ops) WITH (lists='100');

CREATE UNIQUE INDEX "IDX_graph_objects_upsert_branch" ON kb.graph_objects USING btree (project_id, branch_id, type, key) WHERE ((key IS NOT NULL) AND (supersedes_id IS NULL) AND (deleted_at IS NULL) AND (branch_id IS NOT NULL));

CREATE UNIQUE INDEX "IDX_graph_objects_upsert_main" ON kb.graph_objects USING btree (project_id, type, key) WHERE ((key IS NOT NULL) AND (supersedes_id IS NULL) AND (deleted_at IS NULL) AND (branch_id IS NULL));

CREATE INDEX "IDX_graph_schemas_draft" ON kb.graph_schemas USING btree (draft) WHERE (draft = true);

CREATE INDEX "IDX_graph_schemas_parent_version_id" ON kb.graph_schemas USING btree (parent_version_id) WHERE (parent_version_id IS NOT NULL);

CREATE INDEX "IDX_object_chunks_chunk_id" ON kb.object_chunks USING btree (chunk_id);

CREATE INDEX "IDX_object_chunks_extraction_job_id" ON kb.object_chunks USING btree (extraction_job_id);

CREATE INDEX "IDX_object_chunks_object_id" ON kb.object_chunks USING btree (object_id);

CREATE INDEX "IDX_schema_studio_messages_session_id" ON kb.schema_studio_messages USING btree (session_id);

CREATE INDEX "IDX_schema_studio_sessions_schema_id" ON kb.schema_studio_sessions USING btree (pack_id) WHERE (pack_id IS NOT NULL);

CREATE INDEX "IDX_schema_studio_sessions_status" ON kb.schema_studio_sessions USING btree (status) WHERE (status = 'active'::text);

CREATE INDEX "IDX_schema_studio_sessions_user_id" ON kb.schema_studio_sessions USING btree (user_id);

CREATE INDEX idx_acp_run_events_run_id_created ON kb.acp_run_events USING btree (run_id, created_at);

CREATE INDEX idx_acp_sessions_project_id ON kb.acp_sessions USING btree (project_id);

CREATE INDEX idx_adk_events_session ON kb.adk_events USING btree (app_name, user_id, session_id);

CREATE INDEX idx_adk_events_timestamp ON kb.adk_events USING btree ("timestamp");

CREATE UNIQUE INDEX idx_adk_states_unique ON kb.adk_states USING btree (scope, app_name, COALESCE(user_id, ''::text), COALESCE(session_id, ''::text));

CREATE INDEX idx_agent_definitions_product_id ON kb.agent_definitions USING btree (product_id) WHERE (product_id IS NOT NULL);

CREATE INDEX idx_agent_definitions_project_default ON kb.agent_definitions USING btree (project_id, is_default) WHERE (is_default = true);

CREATE UNIQUE INDEX idx_agent_definitions_project_name ON kb.agent_definitions USING btree (project_id, name);

CREATE INDEX idx_agent_processing_log_agent ON kb.agent_processing_log USING btree (agent_id, created_at DESC);

CREATE INDEX idx_agent_processing_log_lookup ON kb.agent_processing_log USING btree (agent_id, graph_object_id, object_version, event_type);

CREATE INDEX idx_agent_processing_log_object ON kb.agent_processing_log USING btree (graph_object_id, created_at DESC);

CREATE INDEX idx_agent_processing_log_stuck ON kb.agent_processing_log USING btree (status, started_at) WHERE (status = 'processing'::text);

CREATE INDEX idx_agent_questions_agent_id ON kb.agent_questions USING btree (agent_id);

CREATE INDEX idx_agent_questions_project_status ON kb.agent_questions USING btree (project_id, status);

CREATE INDEX idx_agent_questions_run_id ON kb.agent_questions USING btree (run_id);

CREATE INDEX idx_agent_run_jobs_poll ON kb.agent_run_jobs USING btree (status, next_run_at) WHERE (status = 'pending'::text);

CREATE INDEX idx_agent_run_jobs_run_id ON kb.agent_run_jobs USING btree (run_id);

CREATE INDEX idx_agent_run_messages_run_step ON kb.agent_run_messages USING btree (run_id, step_number);

CREATE INDEX idx_agent_run_tool_calls_run_step ON kb.agent_run_tool_calls USING btree (run_id, step_number);

CREATE INDEX idx_agent_run_tool_calls_run_tool ON kb.agent_run_tool_calls USING btree (run_id, tool_name);

CREATE INDEX idx_agent_runs_acp_session_id ON kb.agent_runs USING btree (acp_session_id);

CREATE INDEX idx_agent_runs_agent_id ON kb.agent_runs USING btree (agent_id);

CREATE INDEX idx_agent_runs_parent_run_id ON kb.agent_runs USING btree (parent_run_id) WHERE (parent_run_id IS NOT NULL);

CREATE INDEX idx_agent_runs_resumed_from ON kb.agent_runs USING btree (resumed_from) WHERE (resumed_from IS NOT NULL);

CREATE INDEX idx_agent_runs_root_run_id ON kb.agent_runs USING btree (root_run_id) WHERE (root_run_id IS NOT NULL);

CREATE INDEX idx_agent_runs_started_at ON kb.agent_runs USING btree (started_at);

CREATE INDEX idx_agent_runs_status ON kb.agent_runs USING btree (status);

CREATE INDEX idx_agent_runs_trace_id ON kb.agent_runs USING btree (trace_id) WHERE (trace_id IS NOT NULL);

CREATE INDEX idx_agent_sandboxes_expires ON kb.agent_sandboxes USING btree (expires_at) WHERE (expires_at IS NOT NULL);

CREATE INDEX idx_agent_sandboxes_persistent_mcp ON kb.agent_sandboxes USING btree (container_type, lifecycle, status) WHERE ((container_type = 'mcp_server'::text) AND (lifecycle = 'persistent'::text));

CREATE INDEX idx_agent_sandboxes_session ON kb.agent_sandboxes USING btree (agent_session_id) WHERE (agent_session_id IS NOT NULL);

CREATE INDEX idx_agent_sandboxes_status ON kb.agent_sandboxes USING btree (status);

CREATE INDEX idx_agent_webhook_hooks_agent_id ON kb.agent_webhook_hooks USING btree (agent_id);

CREATE INDEX idx_agents_consecutive_failures ON kb.agents USING btree (consecutive_failures) WHERE (consecutive_failures > 0);

CREATE INDEX idx_agents_enabled ON kb.agents USING btree (enabled);

CREATE INDEX idx_agents_project_id ON kb.agents USING btree (project_id);

CREATE INDEX idx_agents_role ON kb.agents USING btree (strategy_type);

CREATE INDEX idx_backups_baseline ON kb.backups USING btree (baseline_backup_id) WHERE (baseline_backup_id IS NOT NULL);

CREATE INDEX idx_backups_created ON kb.backups USING btree (created_at DESC) WHERE (deleted_at IS NULL);

CREATE INDEX idx_backups_expires ON kb.backups USING btree (expires_at) WHERE ((deleted_at IS NULL) AND (expires_at IS NOT NULL));

CREATE INDEX idx_backups_org_project ON kb.backups USING btree (organization_id, project_id) WHERE (deleted_at IS NULL);

CREATE INDEX idx_backups_parent ON kb.backups USING btree (parent_backup_id) WHERE (parent_backup_id IS NOT NULL);

CREATE INDEX idx_backups_status ON kb.backups USING btree (status) WHERE (deleted_at IS NULL);

CREATE INDEX idx_chat_messages_conversation_history ON kb.chat_messages USING btree (conversation_id, created_at DESC);

CREATE INDEX idx_chunk_embedding_jobs_chunk_id ON kb.chunk_embedding_jobs USING btree (chunk_id);

CREATE INDEX idx_chunk_embedding_jobs_dequeue ON kb.chunk_embedding_jobs USING btree (status, scheduled_at, priority DESC) WHERE (status = 'pending'::text);

CREATE INDEX idx_chunk_embedding_jobs_status ON kb.chunk_embedding_jobs USING btree (status);

CREATE INDEX idx_chunks_embedding ON kb.chunks USING ivfflat (embedding public.vector_cosine_ops) WITH (lists='100');

CREATE INDEX idx_chunks_tsv ON kb.chunks USING gin (tsv);

CREATE INDEX idx_database_backups_created_at ON kb.database_backups USING btree (created_at DESC);

CREATE INDEX idx_database_backups_status ON kb.database_backups USING btree (status);

CREATE INDEX idx_document_artifacts_document ON kb.document_artifacts USING btree (document_id);

CREATE INDEX idx_document_artifacts_type ON kb.document_artifacts USING btree (document_id, artifact_type);

CREATE INDEX idx_document_parsing_jobs_document ON kb.document_parsing_jobs USING btree (document_id) WHERE (document_id IS NOT NULL);

CREATE INDEX idx_document_parsing_jobs_orphaned ON kb.document_parsing_jobs USING btree (status, updated_at) WHERE (status = 'processing'::text);

CREATE INDEX idx_document_parsing_jobs_pending ON kb.document_parsing_jobs USING btree (status, created_at) WHERE (status = 'pending'::text);

CREATE INDEX idx_document_parsing_jobs_project ON kb.document_parsing_jobs USING btree (project_id);

CREATE INDEX idx_document_parsing_jobs_retry ON kb.document_parsing_jobs USING btree (status, next_retry_at) WHERE ((status = 'retry_pending'::text) AND (next_retry_at IS NOT NULL));

CREATE INDEX idx_documents_conversion_status ON kb.documents USING btree (conversion_status) WHERE (conversion_status = ANY (ARRAY['pending'::text, 'failed'::text]));

CREATE UNIQUE INDEX idx_documents_email_message_id ON kb.documents USING btree (project_id, ((metadata ->> 'messageId'::text))) WHERE ((source_type = 'email'::text) AND ((metadata ->> 'messageId'::text) IS NOT NULL));

CREATE INDEX idx_documents_email_message_id_lookup ON kb.documents USING btree (((metadata ->> 'messageId'::text))) WHERE ((source_type = 'email'::text) AND ((metadata ->> 'messageId'::text) IS NOT NULL));

CREATE INDEX idx_documents_metadata ON kb.documents USING gin (metadata) WHERE ((metadata IS NOT NULL) AND (metadata <> '{}'::jsonb));

CREATE INDEX idx_documents_parent_document_id ON kb.documents USING btree (parent_document_id);

CREATE UNIQUE INDEX idx_documents_project_file_hash ON kb.documents USING btree (project_id, file_hash) WHERE (file_hash IS NOT NULL);

CREATE INDEX idx_documents_source_type ON kb.documents USING btree (source_type);

CREATE INDEX idx_documents_storage_key ON kb.documents USING btree (storage_key) WHERE (storage_key IS NOT NULL);

CREATE INDEX idx_email_jobs_mailgun_id ON kb.email_jobs USING btree (mailgun_message_id) WHERE (mailgun_message_id IS NOT NULL);

CREATE INDEX idx_email_jobs_needs_status_sync ON kb.email_jobs USING btree (processed_at) WHERE (((status)::text = 'sent'::text) AND (mailgun_message_id IS NOT NULL) AND (delivery_status IS NULL));

CREATE INDEX idx_email_jobs_source ON kb.email_jobs USING btree (source_type, source_id);

CREATE INDEX idx_email_jobs_status_next_retry ON kb.email_jobs USING btree (status, next_retry_at) WHERE ((status)::text = ANY (ARRAY[('pending'::character varying)::text, ('processing'::character varying)::text]));

CREATE INDEX idx_email_logs_event_type ON kb.email_logs USING btree (event_type);

CREATE INDEX idx_email_logs_job ON kb.email_logs USING btree (email_job_id);

CREATE INDEX idx_email_template_versions_created ON kb.email_template_versions USING btree (created_at DESC);

CREATE INDEX idx_email_template_versions_template ON kb.email_template_versions USING btree (template_id);

CREATE INDEX idx_email_templates_name ON kb.email_templates USING btree (name);

CREATE INDEX idx_embedding_policies_is_system ON kb.embedding_policies USING btree (project_id, is_system) WHERE (is_system = true);

CREATE INDEX idx_external_sources_normalized_url ON kb.external_sources USING btree (project_id, normalized_url);

CREATE UNIQUE INDEX idx_external_sources_project_provider_external_id ON kb.external_sources USING btree (project_id, provider_type, external_id);

CREATE INDEX idx_external_sources_sync_status ON kb.external_sources USING btree (status, sync_policy, last_checked_at) WHERE (status = 'active'::text);

CREATE UNIQUE INDEX idx_graph_object_revision_counts_unique ON kb.graph_object_revision_counts USING btree (canonical_id, project_id);

CREATE INDEX idx_graph_objects_actor ON kb.graph_objects USING btree (actor_type, actor_id) WHERE (actor_type IS NOT NULL);

CREATE INDEX idx_graph_objects_fts ON kb.graph_objects USING gin (fts);

CREATE INDEX idx_graph_objects_has_archive ON kb.graph_objects USING btree (((migration_archive <> '[]'::jsonb))) WHERE (migration_archive <> '[]'::jsonb);

CREATE INDEX idx_graph_objects_last_accessed ON kb.graph_objects USING btree (last_accessed_at DESC) WHERE (last_accessed_at IS NOT NULL);

CREATE INDEX idx_graph_objects_schema_version ON kb.graph_objects USING btree (schema_version);

CREATE INDEX idx_graph_rel_emb_jobs_relationship_id ON kb.graph_relationship_embedding_jobs USING btree (relationship_id);

CREATE INDEX idx_graph_rel_emb_jobs_scheduled ON kb.graph_relationship_embedding_jobs USING btree (scheduled_at) WHERE (status = 'pending'::text);

CREATE INDEX idx_graph_rel_emb_jobs_status ON kb.graph_relationship_embedding_jobs USING btree (status);

CREATE INDEX idx_graph_relationships_embedding_ivfflat ON kb.graph_relationships USING ivfflat (embedding public.vector_cosine_ops) WITH (lists='100');

CREATE INDEX idx_graph_schemas_org_id ON kb.graph_schemas USING btree (org_id) WHERE (org_id IS NOT NULL);

CREATE INDEX idx_graph_schemas_project_id ON kb.graph_schemas USING btree (project_id) WHERE (project_id IS NOT NULL);

CREATE INDEX idx_graph_schemas_visibility ON kb.graph_schemas USING btree (visibility);

CREATE INDEX idx_llm_usage_events_model ON kb.llm_usage_events USING btree (provider, model, created_at);

CREATE INDEX idx_llm_usage_events_org ON kb.llm_usage_events USING btree (org_id, created_at);

CREATE INDEX idx_llm_usage_events_project ON kb.llm_usage_events USING btree (project_id, created_at);

CREATE INDEX idx_llm_usage_events_run_id ON kb.llm_usage_events USING btree (run_id) WHERE (run_id IS NOT NULL);

CREATE INDEX idx_mcp_server_tools_enabled ON kb.mcp_server_tools USING btree (server_id, enabled) WHERE (enabled = true);

CREATE INDEX idx_mcp_server_tools_server_id ON kb.mcp_server_tools USING btree (server_id);

CREATE UNIQUE INDEX idx_mcp_server_tools_server_name ON kb.mcp_server_tools USING btree (server_id, tool_name);

CREATE INDEX idx_mcp_servers_project_enabled ON kb.mcp_servers USING btree (project_id, enabled) WHERE (enabled = true);

CREATE INDEX idx_mcp_servers_project_id ON kb.mcp_servers USING btree (project_id);

CREATE UNIQUE INDEX idx_mcp_servers_project_name ON kb.mcp_servers USING btree (project_id, name);

CREATE INDEX idx_migration_runs_project ON kb.schema_migration_runs USING btree (project_id);

CREATE INDEX idx_migration_runs_started ON kb.schema_migration_runs USING btree (started_at DESC);

CREATE INDEX idx_migration_runs_status ON kb.schema_migration_runs USING btree (status);

CREATE INDEX idx_notifications_action_status ON kb.notifications USING btree (action_status) WHERE (action_status IS NOT NULL);

CREATE INDEX idx_notifications_task ON kb.notifications USING btree (task_id);

CREATE INDEX idx_notifications_type_action_status ON kb.notifications USING btree (type, action_status) WHERE (type IS NOT NULL);

CREATE INDEX idx_org_custom_pricing_org_id ON kb.organization_custom_pricing USING btree (org_id);

CREATE INDEX idx_org_tool_settings_org_id ON kb.org_tool_settings USING btree (org_id);

CREATE INDEX idx_orgs_deleted_at ON kb.orgs USING btree (deleted_at) WHERE (deleted_at IS NULL);

CREATE INDEX idx_project_journal_branch ON kb.project_journal USING btree (project_id, branch_id, created_at DESC);

CREATE INDEX idx_project_journal_notes_journal ON kb.project_journal_notes USING btree (journal_id);

CREATE INDEX idx_project_journal_notes_project ON kb.project_journal_notes USING btree (project_id, created_at DESC);

CREATE INDEX idx_project_journal_project_created ON kb.project_journal USING btree (project_id, created_at DESC);

CREATE INDEX idx_project_settings_project_category ON kb.project_settings USING btree (project_id, category);

CREATE INDEX idx_projects_deleted_at ON kb.projects USING btree (deleted_at) WHERE (deleted_at IS NULL);

CREATE INDEX idx_provider_supported_models_provider ON kb.provider_supported_models USING btree (provider);

CREATE INDEX idx_provider_supported_models_type ON kb.provider_supported_models USING btree (provider, model_type);

CREATE INDEX idx_release_notification_recipients_email_job_id ON kb.release_notification_recipients USING btree (email_job_id) WHERE (email_job_id IS NOT NULL);

CREATE INDEX idx_release_notifications_created_at ON kb.release_notifications USING btree (created_at DESC);

CREATE INDEX idx_release_notifications_status ON kb.release_notifications USING btree (status);

CREATE INDEX idx_release_notifications_to_commit ON kb.release_notifications USING btree (to_commit);

CREATE INDEX idx_release_notifications_version ON kb.release_notifications USING btree (version);

CREATE INDEX idx_release_recipients_mailgun ON kb.release_notification_recipients USING btree (mailgun_message_id) WHERE (mailgun_message_id IS NOT NULL);

CREATE INDEX idx_release_recipients_release ON kb.release_notification_recipients USING btree (release_notification_id);

CREATE INDEX idx_release_recipients_user ON kb.release_notification_recipients USING btree (user_id);

CREATE UNIQUE INDEX idx_revision_counts_canonical ON kb.graph_object_revision_counts USING btree (canonical_id);

CREATE INDEX idx_revision_counts_count ON kb.graph_object_revision_counts USING btree (revision_count DESC);

CREATE INDEX idx_sandbox_images_project_id ON kb.sandbox_images USING btree (project_id);

CREATE UNIQUE INDEX idx_sandbox_images_project_name ON kb.sandbox_images USING btree (project_id, name);

CREATE INDEX idx_schema_migration_jobs_project_id ON kb.schema_migration_jobs USING btree (project_id);

CREATE INDEX idx_schema_migration_jobs_status ON kb.schema_migration_jobs USING btree (status);

CREATE INDEX idx_skills_embedding_ivfflat ON kb.skills USING ivfflat (description_embedding public.vector_cosine_ops) WITH (lists='100');

CREATE UNIQUE INDEX idx_skills_name_global ON kb.skills USING btree (name) WHERE (project_id IS NULL);

CREATE UNIQUE INDEX idx_skills_name_org ON kb.skills USING btree (name, org_id) WHERE ((project_id IS NULL) AND (org_id IS NOT NULL));

CREATE UNIQUE INDEX idx_skills_name_project ON kb.skills USING btree (name, project_id) WHERE (project_id IS NOT NULL);

CREATE INDEX idx_skills_org_id ON kb.skills USING btree (org_id);

CREATE INDEX idx_skills_project_id ON kb.skills USING btree (project_id);

CREATE INDEX idx_tasks_pending ON kb.tasks USING btree (status) WHERE (status = 'pending'::text);

CREATE INDEX idx_tasks_project_status ON kb.tasks USING btree (project_id, status);

CREATE INDEX idx_tasks_type ON kb.tasks USING btree (type);

CREATE UNIQUE INDEX idx_user_recent_items_unique_resource ON kb.user_recent_items USING btree (user_id, project_id, resource_type, resource_id);

CREATE INDEX idx_user_recent_items_user_project_accessed ON kb.user_recent_items USING btree (user_id, project_id, accessed_at DESC);

CREATE UNIQUE INDEX uq_graph_relationships_head_branch ON kb.graph_relationships USING btree (project_id, branch_id, type, src_id, dst_id) WHERE ((supersedes_id IS NULL) AND (branch_id IS NOT NULL));

CREATE UNIQUE INDEX uq_graph_relationships_head_main ON kb.graph_relationships USING btree (project_id, type, src_id, dst_id) WHERE ((supersedes_id IS NULL) AND (branch_id IS NULL));

CREATE TRIGGER trg_user_email_preferences_updated BEFORE UPDATE ON core.user_email_preferences FOR EACH ROW EXECUTE FUNCTION core.update_email_preferences_timestamp();

CREATE TRIGGER trg_chunks_tsv BEFORE INSERT OR UPDATE ON kb.chunks FOR EACH ROW EXECUTE FUNCTION kb.update_tsv();

CREATE TRIGGER trg_graph_objects_fts BEFORE INSERT OR UPDATE ON kb.graph_objects FOR EACH ROW EXECUTE FUNCTION kb.update_graph_objects_fts();

ALTER TABLE ONLY core.user_emails
    ADD CONSTRAINT "FK_2e88b95787b903d46ab3cc3eb91" FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY core.api_tokens
    ADD CONSTRAINT api_tokens_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY core.api_tokens
    ADD CONSTRAINT api_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY core.superadmins
    ADD CONSTRAINT superadmins_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY core.superadmins
    ADD CONSTRAINT superadmins_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY core.superadmins
    ADD CONSTRAINT superadmins_user_id_fkey FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY core.user_email_preferences
    ADD CONSTRAINT user_email_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY core.user_profiles
    ADD CONSTRAINT user_profiles_deleted_by_fkey FOREIGN KEY (deleted_by) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.embedding_policies
    ADD CONSTRAINT "FK_057b973371cc00d7df2e95a6d57" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chat_conversations
    ADD CONSTRAINT "FK_14ad2d35eccbe22a4bc61a9a065" FOREIGN KEY (owner_user_id) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.object_extraction_jobs
    ADD CONSTRAINT "FK_1c7f91f13d7e1a438519d37ec3b" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_memberships
    ADD CONSTRAINT "FK_38a73cbcc58fbed8e62a66d79b8" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chat_messages
    ADD CONSTRAINT "FK_3d623662d4ee1219b23cf61e649" FOREIGN KEY (conversation_id) REFERENCES kb.chat_conversations(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.organization_memberships
    ADD CONSTRAINT "FK_5352fc550034d507d6c76dd2901" FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.object_extraction_jobs
    ADD CONSTRAINT "FK_543b356bd6204a84bc8c038d309" FOREIGN KEY (document_id) REFERENCES kb.documents(id);

ALTER TABLE ONLY kb.projects
    ADD CONSTRAINT "FK_585c8ce06628c70b70100bfb842" FOREIGN KEY (organization_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.branches
    ADD CONSTRAINT "FK_6dab82d7024195ac691c50f6942" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.tags
    ADD CONSTRAINT "FK_7ab852bb0ada09a0fc3adb7e5de" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_memberships
    ADD CONSTRAINT "FK_7cb6c36ad5bf1bd4a413823acec" FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.organization_memberships
    ADD CONSTRAINT "FK_86ae2efbb9ce84dd652e0c96a49" FOREIGN KEY (organization_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.notifications
    ADD CONSTRAINT "FK_95464140d7dc04d7efb0afd6be0" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.invites
    ADD CONSTRAINT "FK_9a75a544ecb579c8203efab71d9" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.notifications
    ADD CONSTRAINT "FK_9a8a82462cab47c73d25f49261f" FOREIGN KEY (user_id) REFERENCES core.user_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.graph_relationships
    ADD CONSTRAINT "FK_a0dadc1ffc4ee153226f786e99a" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.product_versions
    ADD CONSTRAINT "FK_befe8619b468202250e33d16bd0" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chunks
    ADD CONSTRAINT "FK_d841de45a719fe1f35213d79207" FOREIGN KEY (document_id) REFERENCES kb.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.documents
    ADD CONSTRAINT "FK_e156b298c20873e14c362e789bf" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chat_conversations
    ADD CONSTRAINT "FK_e49dcd93d3f2653f21dff81e180" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.object_type_schemas
    ADD CONSTRAINT "FK_f9b1a295fa838a7b20d80f084bb" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.graph_objects
    ADD CONSTRAINT "FK_ff6be6062964f2462ee8e8b2ac1" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.notifications
    ADD CONSTRAINT "FK_notifications_project_id" FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.acp_run_events
    ADD CONSTRAINT acp_run_events_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.acp_sessions
    ADD CONSTRAINT acp_sessions_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.adk_events
    ADD CONSTRAINT adk_events_session_fk FOREIGN KEY (app_name, user_id, session_id) REFERENCES kb.adk_sessions(app_name, user_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.adk_states
    ADD CONSTRAINT adk_states_session_fk FOREIGN KEY (app_name, user_id, session_id) REFERENCES kb.adk_sessions(app_name, user_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_definitions
    ADD CONSTRAINT agent_definitions_product_id_fkey FOREIGN KEY (product_id) REFERENCES kb.product_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_processing_log
    ADD CONSTRAINT agent_processing_log_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES kb.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_processing_log
    ADD CONSTRAINT agent_processing_log_graph_object_id_fkey FOREIGN KEY (graph_object_id) REFERENCES kb.graph_objects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_questions
    ADD CONSTRAINT agent_questions_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES kb.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_questions
    ADD CONSTRAINT agent_questions_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_run_jobs
    ADD CONSTRAINT agent_run_jobs_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_run_messages
    ADD CONSTRAINT agent_run_messages_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_run_tool_calls
    ADD CONSTRAINT agent_run_tool_calls_message_id_fkey FOREIGN KEY (message_id) REFERENCES kb.agent_run_messages(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_run_tool_calls
    ADD CONSTRAINT agent_run_tool_calls_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_acp_session_id_fkey FOREIGN KEY (acp_session_id) REFERENCES kb.acp_sessions(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_agent_definition_id_fkey FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES kb.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_parent_run_id_fkey FOREIGN KEY (parent_run_id) REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_resumed_from_fkey FOREIGN KEY (resumed_from) REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_runs
    ADD CONSTRAINT agent_runs_root_run_id_fkey FOREIGN KEY (root_run_id) REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.agent_webhook_hooks
    ADD CONSTRAINT agent_webhook_hooks_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES kb.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agents
    ADD CONSTRAINT agents_agent_definition_id_fkey FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_baseline_backup_id_fkey FOREIGN KEY (baseline_backup_id) REFERENCES kb.backups(id);

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_created_by_fkey FOREIGN KEY (created_by) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_parent_backup_id_fkey FOREIGN KEY (parent_backup_id) REFERENCES kb.backups(id);

ALTER TABLE ONLY kb.backups
    ADD CONSTRAINT backups_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chat_conversations
    ADD CONSTRAINT chat_conversations_agent_definition_fk FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id);

ALTER TABLE ONLY kb.chat_conversations
    ADD CONSTRAINT chat_conversations_object_id_fkey FOREIGN KEY (object_id) REFERENCES kb.graph_objects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.chunk_embedding_jobs
    ADD CONSTRAINT chunk_embedding_jobs_chunk_id_fkey FOREIGN KEY (chunk_id) REFERENCES kb.chunks(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.document_artifacts
    ADD CONSTRAINT document_artifacts_document_id_fkey FOREIGN KEY (document_id) REFERENCES kb.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.document_parsing_jobs
    ADD CONSTRAINT document_parsing_jobs_document_id_fkey FOREIGN KEY (document_id) REFERENCES kb.documents(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.document_parsing_jobs
    ADD CONSTRAINT document_parsing_jobs_extraction_job_id_fkey FOREIGN KEY (extraction_job_id) REFERENCES kb.object_extraction_jobs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.document_parsing_jobs
    ADD CONSTRAINT document_parsing_jobs_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.email_logs
    ADD CONSTRAINT email_logs_email_job_id_fkey FOREIGN KEY (email_job_id) REFERENCES kb.email_jobs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.email_template_versions
    ADD CONSTRAINT email_template_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.email_template_versions
    ADD CONSTRAINT email_template_versions_template_id_fkey FOREIGN KEY (template_id) REFERENCES kb.email_templates(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.email_templates
    ADD CONSTRAINT email_templates_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.external_sources
    ADD CONSTRAINT external_sources_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.agents
    ADD CONSTRAINT fk_agents_project FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.email_templates
    ADD CONSTRAINT fk_email_templates_current_version FOREIGN KEY (current_version_id) REFERENCES kb.email_template_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.graph_relationship_embedding_jobs
    ADD CONSTRAINT graph_relationship_embedding_jobs_relationship_id_fkey FOREIGN KEY (relationship_id) REFERENCES kb.graph_relationships(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.graph_schemas
    ADD CONSTRAINT graph_schemas_parent_version_id_fkey FOREIGN KEY (parent_version_id) REFERENCES kb.graph_schemas(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.invites
    ADD CONSTRAINT invites_invited_by_user_id_fkey FOREIGN KEY (invited_by_user_id) REFERENCES core.user_profiles(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.llm_usage_events
    ADD CONSTRAINT llm_usage_events_org_id_fkey FOREIGN KEY (org_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.llm_usage_events
    ADD CONSTRAINT llm_usage_events_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.llm_usage_events
    ADD CONSTRAINT llm_usage_events_run_id_fkey FOREIGN KEY (run_id) REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.mcp_server_tools
    ADD CONSTRAINT mcp_server_tools_server_id_fkey FOREIGN KEY (server_id) REFERENCES kb.mcp_servers(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.notifications
    ADD CONSTRAINT notifications_task_id_fkey FOREIGN KEY (task_id) REFERENCES kb.tasks(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.object_chunks
    ADD CONSTRAINT object_chunks_chunk_id_fkey FOREIGN KEY (chunk_id) REFERENCES kb.chunks(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.object_chunks
    ADD CONSTRAINT object_chunks_extraction_job_id_fkey FOREIGN KEY (extraction_job_id) REFERENCES kb.object_extraction_jobs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.object_chunks
    ADD CONSTRAINT object_chunks_object_id_fkey FOREIGN KEY (object_id) REFERENCES kb.graph_objects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.org_provider_configs
    ADD CONSTRAINT org_provider_configs_org_id_fkey FOREIGN KEY (org_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.org_tool_settings
    ADD CONSTRAINT org_tool_settings_org_id_fkey FOREIGN KEY (org_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.organization_custom_pricing
    ADD CONSTRAINT organization_custom_pricing_org_id_fkey FOREIGN KEY (org_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.orgs
    ADD CONSTRAINT orgs_deleted_by_fkey FOREIGN KEY (deleted_by) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.project_journal_notes
    ADD CONSTRAINT project_journal_notes_journal_id_fkey FOREIGN KEY (journal_id) REFERENCES kb.project_journal(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_object_schema_registry
    ADD CONSTRAINT project_object_schema_registry_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_provider_configs
    ADD CONSTRAINT project_provider_configs_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_schemas
    ADD CONSTRAINT project_schemas_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.project_schemas
    ADD CONSTRAINT project_schemas_schema_id_fkey FOREIGN KEY (schema_id) REFERENCES kb.graph_schemas(id);

ALTER TABLE ONLY kb.project_settings
    ADD CONSTRAINT project_settings_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.projects
    ADD CONSTRAINT projects_deleted_by_fkey FOREIGN KEY (deleted_by) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipients_email_job_id_fkey FOREIGN KEY (email_job_id) REFERENCES kb.email_jobs(id) ON DELETE SET NULL;

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipients_in_app_notification_id_fkey FOREIGN KEY (in_app_notification_id) REFERENCES kb.notifications(id);

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipients_release_notification_id_fkey FOREIGN KEY (release_notification_id) REFERENCES kb.release_notifications(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.release_notification_recipients
    ADD CONSTRAINT release_notification_recipients_user_id_fkey FOREIGN KEY (user_id) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.release_notifications
    ADD CONSTRAINT release_notifications_created_by_fkey FOREIGN KEY (created_by) REFERENCES core.user_profiles(id);

ALTER TABLE ONLY kb.schema_studio_messages
    ADD CONSTRAINT schema_studio_messages_session_id_fkey FOREIGN KEY (session_id) REFERENCES kb.schema_studio_sessions(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.schema_studio_sessions
    ADD CONSTRAINT schema_studio_sessions_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.schema_studio_sessions
    ADD CONSTRAINT schema_studio_sessions_schema_id_fkey FOREIGN KEY (pack_id) REFERENCES kb.graph_schemas(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.skills
    ADD CONSTRAINT skills_org_id_fkey FOREIGN KEY (org_id) REFERENCES kb.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.skills
    ADD CONSTRAINT skills_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY kb.tasks
    ADD CONSTRAINT tasks_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

ALTER TABLE kb.branches ENABLE ROW LEVEL SECURITY;
CREATE POLICY branches_delete ON kb.branches FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY branches_insert ON kb.branches FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY branches_select ON kb.branches FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY branches_update ON kb.branches FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.chat_conversations ENABLE ROW LEVEL SECURITY;
CREATE POLICY chat_conversations_delete ON kb.chat_conversations FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY chat_conversations_insert ON kb.chat_conversations FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY chat_conversations_select ON kb.chat_conversations FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY chat_conversations_update ON kb.chat_conversations FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.chat_messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY chat_messages_delete_policy ON kb.chat_messages FOR DELETE USING ((EXISTS ( SELECT 1
   FROM kb.chat_conversations c
  WHERE ((c.id = chat_messages.conversation_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((c.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chat_messages_insert_policy ON kb.chat_messages FOR INSERT WITH CHECK ((EXISTS ( SELECT 1
   FROM kb.chat_conversations c
  WHERE ((c.id = chat_messages.conversation_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((c.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chat_messages_select_policy ON kb.chat_messages FOR SELECT USING ((EXISTS ( SELECT 1
   FROM kb.chat_conversations c
  WHERE ((c.id = chat_messages.conversation_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((c.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chat_messages_update_policy ON kb.chat_messages FOR UPDATE USING ((EXISTS ( SELECT 1
   FROM kb.chat_conversations c
  WHERE ((c.id = chat_messages.conversation_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((c.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.chunk_embedding_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY chunk_embedding_jobs_project_access ON kb.chunk_embedding_jobs USING ((EXISTS ( SELECT 1
   FROM (kb.chunks c
     JOIN kb.documents d ON ((c.document_id = d.id)))
  WHERE ((c.id = chunk_embedding_jobs.chunk_id) AND ((current_setting('app.current_project_id'::text, true) = ''::text) OR ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.chunks ENABLE ROW LEVEL SECURITY;
CREATE POLICY chunks_delete ON kb.chunks FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR (EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = chunks.document_id) AND ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chunks_insert ON kb.chunks FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR (EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = chunks.document_id) AND ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chunks_select ON kb.chunks FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR (EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = chunks.document_id) AND ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY chunks_update ON kb.chunks FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR (EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = chunks.document_id) AND ((d.project_id)::text = current_setting('app.current_project_id'::text, true))))))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR (EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = chunks.document_id) AND ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.document_artifacts ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_artifacts_delete_policy ON kb.document_artifacts FOR DELETE USING ((EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = document_artifacts.document_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY document_artifacts_insert_policy ON kb.document_artifacts FOR INSERT WITH CHECK ((EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = document_artifacts.document_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY document_artifacts_select_policy ON kb.document_artifacts FOR SELECT USING ((EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = document_artifacts.document_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY document_artifacts_update_policy ON kb.document_artifacts FOR UPDATE USING ((EXISTS ( SELECT 1
   FROM kb.documents d
  WHERE ((d.id = document_artifacts.document_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((d.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.document_parsing_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_parsing_jobs_delete_policy ON kb.document_parsing_jobs FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY document_parsing_jobs_insert_policy ON kb.document_parsing_jobs FOR INSERT WITH CHECK (true);

CREATE POLICY document_parsing_jobs_select_policy ON kb.document_parsing_jobs FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY document_parsing_jobs_update_policy ON kb.document_parsing_jobs FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.documents ENABLE ROW LEVEL SECURITY;
CREATE POLICY documents_delete ON kb.documents FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY documents_insert ON kb.documents FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY documents_select ON kb.documents FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY documents_update ON kb.documents FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.embedding_policies ENABLE ROW LEVEL SECURITY;
CREATE POLICY embedding_policies_delete ON kb.embedding_policies FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY embedding_policies_insert ON kb.embedding_policies FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY embedding_policies_select ON kb.embedding_policies FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY embedding_policies_update ON kb.embedding_policies FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.external_sources ENABLE ROW LEVEL SECURITY;
CREATE POLICY external_sources_delete ON kb.external_sources FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY external_sources_insert ON kb.external_sources FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY external_sources_select ON kb.external_sources FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY external_sources_update ON kb.external_sources FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.graph_embedding_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY graph_embedding_jobs_delete_policy ON kb.graph_embedding_jobs FOR DELETE USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = graph_embedding_jobs.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY graph_embedding_jobs_insert_policy ON kb.graph_embedding_jobs FOR INSERT WITH CHECK ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = graph_embedding_jobs.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY graph_embedding_jobs_select_policy ON kb.graph_embedding_jobs FOR SELECT USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = graph_embedding_jobs.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY graph_embedding_jobs_update_policy ON kb.graph_embedding_jobs FOR UPDATE USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = graph_embedding_jobs.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.graph_objects ENABLE ROW LEVEL SECURITY;
CREATE POLICY graph_objects_delete ON kb.graph_objects FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_objects_insert ON kb.graph_objects FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_objects_select ON kb.graph_objects FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_objects_update ON kb.graph_objects FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.graph_relationships ENABLE ROW LEVEL SECURITY;
CREATE POLICY graph_relationships_delete ON kb.graph_relationships FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_relationships_insert ON kb.graph_relationships FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_relationships_select ON kb.graph_relationships FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY graph_relationships_update ON kb.graph_relationships FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.invites ENABLE ROW LEVEL SECURITY;
CREATE POLICY invites_delete ON kb.invites FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY invites_insert ON kb.invites FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY invites_select ON kb.invites FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY invites_update ON kb.invites FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY notifications_delete ON kb.notifications FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY notifications_insert ON kb.notifications FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY notifications_select ON kb.notifications FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY notifications_update ON kb.notifications FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.object_chunks ENABLE ROW LEVEL SECURITY;
CREATE POLICY object_chunks_delete_policy ON kb.object_chunks FOR DELETE USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = object_chunks.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_chunks_insert_policy ON kb.object_chunks FOR INSERT WITH CHECK ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = object_chunks.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_chunks_select_policy ON kb.object_chunks FOR SELECT USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = object_chunks.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_chunks_update_policy ON kb.object_chunks FOR UPDATE USING ((EXISTS ( SELECT 1
   FROM kb.graph_objects o
  WHERE ((o.id = object_chunks.object_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((o.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.object_extraction_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY object_extraction_jobs_delete ON kb.object_extraction_jobs FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_extraction_jobs_insert ON kb.object_extraction_jobs FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_extraction_jobs_select ON kb.object_extraction_jobs FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_extraction_jobs_update ON kb.object_extraction_jobs FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.object_extraction_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY object_extraction_logs_delete_policy ON kb.object_extraction_logs FOR DELETE USING ((EXISTS ( SELECT 1
   FROM kb.object_extraction_jobs j
  WHERE ((j.id = object_extraction_logs.extraction_job_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((j.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_extraction_logs_insert_policy ON kb.object_extraction_logs FOR INSERT WITH CHECK ((EXISTS ( SELECT 1
   FROM kb.object_extraction_jobs j
  WHERE ((j.id = object_extraction_logs.extraction_job_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((j.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_extraction_logs_select_policy ON kb.object_extraction_logs FOR SELECT USING ((EXISTS ( SELECT 1
   FROM kb.object_extraction_jobs j
  WHERE ((j.id = object_extraction_logs.extraction_job_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((j.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

CREATE POLICY object_extraction_logs_update_policy ON kb.object_extraction_logs FOR UPDATE USING ((EXISTS ( SELECT 1
   FROM kb.object_extraction_jobs j
  WHERE ((j.id = object_extraction_logs.extraction_job_id) AND ((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((j.project_id)::text = current_setting('app.current_project_id'::text, true)))))));

ALTER TABLE kb.object_type_schemas ENABLE ROW LEVEL SECURITY;
CREATE POLICY object_type_schemas_delete ON kb.object_type_schemas FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_type_schemas_insert ON kb.object_type_schemas FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_type_schemas_select ON kb.object_type_schemas FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY object_type_schemas_update ON kb.object_type_schemas FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.product_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_versions_delete ON kb.product_versions FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY product_versions_insert ON kb.product_versions FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY product_versions_select ON kb.product_versions FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY product_versions_update ON kb.product_versions FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.project_memberships ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_memberships_delete ON kb.project_memberships FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_memberships_insert ON kb.project_memberships FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_memberships_select ON kb.project_memberships FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_memberships_update ON kb.project_memberships FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.project_object_schema_registry ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_object_schema_registry_delete ON kb.project_object_schema_registry FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_object_schema_registry_insert ON kb.project_object_schema_registry FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_object_schema_registry_select ON kb.project_object_schema_registry FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_object_schema_registry_update ON kb.project_object_schema_registry FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.project_schemas ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_schemas_delete ON kb.project_schemas FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_schemas_insert ON kb.project_schemas FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_schemas_select ON kb.project_schemas FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY project_schemas_update ON kb.project_schemas FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.schema_studio_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY schema_studio_sessions_delete ON kb.schema_studio_sessions FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY schema_studio_sessions_insert ON kb.schema_studio_sessions FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY schema_studio_sessions_select ON kb.schema_studio_sessions FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY schema_studio_sessions_update ON kb.schema_studio_sessions FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.tags ENABLE ROW LEVEL SECURITY;
CREATE POLICY tags_delete ON kb.tags FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tags_insert ON kb.tags FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tags_select ON kb.tags FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tags_update ON kb.tags FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.tasks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tasks_delete ON kb.tasks FOR DELETE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tasks_insert ON kb.tasks FOR INSERT WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tasks_select ON kb.tasks FOR SELECT USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

CREATE POLICY tasks_update ON kb.tasks FOR UPDATE USING (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true)))) WITH CHECK (((COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text) OR ((project_id)::text = current_setting('app.current_project_id'::text, true))));

ALTER TABLE kb.user_recent_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_recent_items_isolation ON kb.user_recent_items USING ((user_id = current_setting('app.user_id'::text, true))) WITH CHECK ((user_id = current_setting('app.user_id'::text, true)));
-- Migration: 00095_add_title_to_acp_sessions.sql

-- Add title field to ACP sessions for set_session_title built-in MCP tool
ALTER TABLE kb.acp_sessions ADD COLUMN IF NOT EXISTS title TEXT;


-- Migration: 00096_add_auto_load_skills_to_agent_definitions.sql
ALTER TABLE kb.agent_definitions ADD COLUMN IF NOT EXISTS auto_load_skills BOOLEAN NOT NULL DEFAULT FALSE;


-- Migration: 00097_add_max_session_events_to_agent_definitions.sql
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS max_session_events integer;


-- Migration: 00098_add_max_input_tokens_to_provider_supported_models.sql
ALTER TABLE kb.provider_supported_models
    ADD COLUMN IF NOT EXISTS max_input_tokens integer;


-- Migration: 00099_add_agent_question_interaction_fields.sql
-- Add interaction_type, placeholder, and max_length columns to kb.agent_questions
ALTER TABLE kb.agent_questions
  ADD COLUMN IF NOT EXISTS interaction_type text NOT NULL DEFAULT 'buttons',
  ADD COLUMN IF NOT EXISTS placeholder text,
  ADD COLUMN IF NOT EXISTS max_length integer;


-- Migration: 00100_create_project_edge_schema_registry.sql
-- Create the project_edge_schema_registry table for relationship (edge) type registrations.
-- This was added to the Go code but the migration was never written.
-- See: schema_tools.go executeCreateSchema (INSERT), executeGetSchema (SELECT), executeSchemaCompiledTypes (SELECT)

CREATE TABLE kb.project_edge_schema_registry (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    project_id uuid NOT NULL,
    schema_id uuid NOT NULL,
    type_name text NOT NULL,
    json_schema jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Primary key
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_pkey PRIMARY KEY (id);

-- Foreign key to projects
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

-- Foreign key to graph_schemas
ALTER TABLE ONLY kb.project_edge_schema_registry
    ADD CONSTRAINT project_edge_schema_registry_schema_id_fkey
    FOREIGN KEY (schema_id) REFERENCES kb.graph_schemas(id) ON DELETE CASCADE;

-- Index for project-scoped lookups
CREATE INDEX idx_project_edge_schema_registry_project_id
    ON kb.project_edge_schema_registry(project_id);

-- Index for schema-scoped lookups
CREATE INDEX idx_project_edge_schema_registry_schema_id
    ON kb.project_edge_schema_registry(schema_id);

-- Unique constraint: one type_name per project per schema (schema_id is NOT NULL so NULLs are not an issue)
CREATE UNIQUE INDEX idx_project_edge_schema_registry_unique
    ON kb.project_edge_schema_registry(project_id, schema_id, type_name);

-- Row-Level Security
ALTER TABLE kb.project_edge_schema_registry ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_edge_schema_registry_select ON kb.project_edge_schema_registry
    FOR SELECT USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_insert ON kb.project_edge_schema_registry
    FOR INSERT WITH CHECK (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_update ON kb.project_edge_schema_registry
    FOR UPDATE USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    ) WITH CHECK (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );

CREATE POLICY project_edge_schema_registry_delete ON kb.project_edge_schema_registry
    FOR DELETE USING (
        (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
        OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
    );


-- Migration: 00101_create_session_todos.sql
-- Create session_todo_status enum
CREATE TYPE kb.session_todo_status AS ENUM ('draft', 'pending', 'in_progress', 'completed', 'cancelled');

-- Create session_todos table
CREATE TABLE kb.session_todos (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id       uuid        NOT NULL REFERENCES kb.acp_sessions(id) ON DELETE CASCADE,
    content          text        NOT NULL,
    status           kb.session_todo_status NOT NULL DEFAULT 'draft',
    author           text,
    "order"          integer     NOT NULL DEFAULT 0,
    context_snapshot text,
    created_at       timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at       timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE INDEX idx_session_todos_session_id ON kb.session_todos(session_id);
CREATE INDEX idx_session_todos_status ON kb.session_todos(status);


-- Migration: 00102_session_todos_drop_fk.sql
-- Drop the FK constraint on session_todos.session_id.
-- session_todos should work with any session ID (acp, adk, graph, etc.)
-- not just those that exist in kb.acp_sessions.
ALTER TABLE kb.session_todos DROP CONSTRAINT IF EXISTS session_todos_session_id_fkey;


-- Migration: 00103_extraction_staging_branch.sql
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS staging_branch_id uuid REFERENCES kb.branches(id) ON DELETE SET NULL;

COMMENT ON COLUMN kb.object_extraction_jobs.staging_branch_id IS
    'Staging branch where extracted objects land pending review. NULL = legacy (objects on main) or no branch isolation.';


-- Migration: 00104_extraction_created_object_ids.sql
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS created_object_ids uuid[] NOT NULL DEFAULT '{}';


-- Migration: 00105_project_budget_default.sql

-- Set a column-level default so all INSERT paths (including raw SQL in standalone
-- bootstrap and MCP tool) automatically get $10 without any application-layer change.
ALTER TABLE kb.projects
    ALTER COLUMN budget_usd SET DEFAULT 10.0;

-- Backfill existing projects that were created before migration 00063 or via paths
-- that bypassed the service layer (standalone bootstrap, MCP project-create tool).
UPDATE kb.projects
SET budget_usd = 10.0
WHERE budget_usd IS NULL;



