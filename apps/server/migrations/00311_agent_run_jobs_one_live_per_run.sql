-- +goose NO TRANSACTION
-- +goose Up
-- Exactly one live dispatch job per agent run (issue #905).
--
-- kb.agent_run_jobs only ever had a plain index on run_id (00048), so the
-- "one job per run" invariant was enforced solely by the read-then-insert in
-- RequeueOrphanedQueuedRuns: it selects queued runs with no live job and then
-- inserts one. That heal runs on every process boot and every five minutes per
-- process, so concurrent instances all observe "no live job" and all insert —
-- six concurrent heals for one run measured two pending rows. A run cannot
-- execute twice (the claim is a guarded UPDATE of status='queued'), but the
-- duplicates inflate the row count that CountPendingJobsForAgent feeds to the
-- AGENT_MAX_PENDING_JOBS guard, producing spurious QueueFullError for an agent
-- whose real backlog is smaller, and the inflation persists until the job is
-- claimed or the run fails.
--
-- A partial unique index on the live rows makes the insert idempotent in the
-- database: the requeue INSERT carries ON CONFLICT DO NOTHING (see
-- Repository.CreateRunJob), so a loser of the race is a no-op instead of an
-- extra row. A NOT EXISTS guard inside a single statement is not sufficient
-- under READ COMMITTED: the two inserters do not see each other's uncommitted
-- row.
--
-- CONCURRENTLY cannot run inside a transaction, hence NO TRANSACTION: each
-- statement commits on its own, so no statement below can be wrapped in a
-- StatementBegin/End block.

-- Step 1 — collapse the duplicates the race already created, or the unique
-- index build fails. Keep one live row per run: an actively executing
-- ('processing') row wins over a pending duplicate, otherwise the oldest
-- (FIFO) row, with id only as a deterministic tiebreak. Losers are retired as
-- 'completed' — the same shape the claim and the undispatchable-run sweep use
-- to retire a job without executing it — so no worker can pick them up and they
-- stop counting against the queue-depth guard. Non-destructive: the run and its
-- history are untouched.
UPDATE kb.agent_run_jobs
SET status = 'completed',
    completed_at = now()
WHERE id IN (
    SELECT id
    FROM (
        SELECT id,
               row_number() OVER (
                   PARTITION BY run_id
                   ORDER BY (status = 'processing') DESC,
                            created_at ASC,
                            id ASC
               ) AS rn
        FROM kb.agent_run_jobs
        WHERE status IN ('pending', 'processing')
    ) ranked
    WHERE ranked.rn > 1
);

-- Step 2 — build CONCURRENTLY so a populated kb.agent_run_jobs is not locked
-- against writes for the whole build. A failed CONCURRENTLY build leaves an
-- INVALID index behind and contains no uniqueness guarantee, so drop any
-- leftover target-name index first; a retry then rebuilds from scratch
-- (the dedupe above is idempotent once one live row per run remains).
DROP INDEX CONCURRENTLY IF EXISTS kb.uq_agent_run_jobs_one_live_per_run;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_agent_run_jobs_one_live_per_run
    ON kb.agent_run_jobs (run_id)
    WHERE status IN ('pending', 'processing');

-- Step 3 — post-condition guard. With NO TRANSACTION each statement commits on
-- its own, so a run whose CONCURRENTLY build did not take effect would still
-- let goose record the version, and the invariant would silently not exist
-- (the 00164 failure mode, issue #734). Raise instead, so the version is not
-- recorded and the idempotent retry above rebuilds the index.
-- StatementBegin/End is required because goose's NO TRANSACTION splitter does
-- not understand dollar-quoted bodies and would truncate at the first
-- semicolon.
-- +goose StatementBegin
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(e.name, ', ') INTO bad
    FROM (VALUES ('uq_agent_run_jobs_one_live_per_run')) AS e(name)
    WHERE NOT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = e.name
          AND i.indisunique
          AND i.indisvalid
          AND i.indisready
    );
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00311: agent_run_jobs live-job unique index missing or invalid: %', bad;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Kept CONCURRENTLY and outside any StatementBegin/End block: goose runs such a
-- block as a single ExecContext, which Postgres treats as a transaction, where
-- DROP INDEX CONCURRENTLY is illegal. Dropping the index re-opens the duplicate
-- race; the retired duplicate rows are not restored (that state is not
-- recoverable and was never meaningful).
DROP INDEX CONCURRENTLY IF EXISTS kb.uq_agent_run_jobs_one_live_per_run;
