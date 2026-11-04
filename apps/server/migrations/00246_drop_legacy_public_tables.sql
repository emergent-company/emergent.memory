-- +goose Up
-- +goose StatementBegin
-- Drop the dead public-schema leftovers from the retired NestJS/TypeORM era.
--
-- The four checkpoint_* tables are LangGraph checkpointer tables. LangGraph was
-- removed together with the legacy NestJS backend (80f4ef461), no code path
-- writes or reads them, and the Go server keeps agent state in
-- kb.adk_sessions / kb.adk_states / kb.adk_events. typeorm_migrations is the
-- old TypeORM ledger, superseded by goose_db_version.
--
-- All five tables were carried into 00001_baseline.sql by the full-schema
-- export and have never been used: they are empty and show zero inserts in
-- pg_stat_user_tables, and an empty checkpoint_migrations proves LangGraph's
-- own setup() migrator never ran. Dropping them keeps fresh installs from
-- recreating them and leaves public with only the live goose ledger.
--
-- IF EXISTS makes this a no-op on an environment that already lacks them.
DROP TABLE IF EXISTS public.checkpoint_writes;
DROP TABLE IF EXISTS public.checkpoint_blobs;
DROP TABLE IF EXISTS public.checkpoints;
DROP TABLE IF EXISTS public.checkpoint_migrations;
DROP TABLE IF EXISTS public.typeorm_migrations;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Recreate the removed tables empty, matching the 00001_baseline.sql shape.
CREATE TABLE public.checkpoint_blobs (
    thread_id text NOT NULL,
    checkpoint_ns text DEFAULT ''::text NOT NULL,
    channel text NOT NULL,
    version text NOT NULL,
    type text NOT NULL,
    blob bytea,
    CONSTRAINT checkpoint_blobs_pkey PRIMARY KEY (thread_id, checkpoint_ns, channel, version)
);

CREATE TABLE public.checkpoint_migrations (
    v integer NOT NULL,
    CONSTRAINT checkpoint_migrations_pkey PRIMARY KEY (v)
);

CREATE TABLE public.checkpoint_writes (
    thread_id text NOT NULL,
    checkpoint_ns text DEFAULT ''::text NOT NULL,
    checkpoint_id text NOT NULL,
    task_id text NOT NULL,
    idx integer NOT NULL,
    channel text NOT NULL,
    type text,
    blob bytea NOT NULL,
    CONSTRAINT checkpoint_writes_pkey PRIMARY KEY (thread_id, checkpoint_ns, checkpoint_id, task_id, idx)
);

CREATE TABLE public.checkpoints (
    thread_id text NOT NULL,
    checkpoint_ns text DEFAULT ''::text NOT NULL,
    checkpoint_id text NOT NULL,
    parent_checkpoint_id text,
    type text,
    checkpoint jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT checkpoints_pkey PRIMARY KEY (thread_id, checkpoint_ns, checkpoint_id)
);

CREATE SEQUENCE public.typeorm_migrations_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.typeorm_migrations (
    id integer NOT NULL,
    "timestamp" bigint NOT NULL,
    name character varying NOT NULL,
    CONSTRAINT "PK_bb2f075707dd300ba86d0208923" PRIMARY KEY (id)
);

ALTER SEQUENCE public.typeorm_migrations_id_seq OWNED BY public.typeorm_migrations.id;

ALTER TABLE ONLY public.typeorm_migrations
    ALTER COLUMN id SET DEFAULT nextval('public.typeorm_migrations_id_seq'::regclass);
-- +goose StatementEnd
