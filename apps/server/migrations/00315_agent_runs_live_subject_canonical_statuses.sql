-- +goose NO TRANSACTION
-- +goose Up
-- Rebuild the one-live-run-per-work-object guard on the canonical run-status
-- vocabulary (issue #992).
--
-- 00262 created idx_agent_runs_live_subject with
--   WHERE subject_object_id IS NOT NULL AND status IN ('queued','running','paused')
-- but 00113 (earlier) had already renamed those run statuses to
-- 'submitted','working','input-required'. The predicate therefore matched no
-- row, so the "one live run per subject" claim was never enforced and the
-- isLiveRunConflict path in domain/agents could never fire. 00262's dedupe
-- query had the same dead predicate and collapsed nothing.
--
-- Keep the index name: Repository.isLiveRunConflict keys on it.
--
-- Input-required stays in the live set on purpose: a run waiting on a human
-- must hold its subject's claim so the item is not dispatched twice. Its
-- indefinite-park behaviour is tracked separately in issue #993.
--
-- CONCURRENTLY cannot run inside a transaction, hence NO TRANSACTION: each
-- statement commits on its own, so no statement below may be wrapped in a
-- StatementBegin/End block (the post-condition guard is the one exception,
-- because goose's NO TRANSACTION splitter cannot parse dollar quotes).

-- Step 1 — collapse duplicate live runs for one subject or the unique index
-- build fails. Keep exactly one: an actively executing ('working') run wins,
-- then one parked on a human ('input-required'), then the newest; id is the
-- deterministic tiebreak. Losers are cancelled and their pending/processing
-- jobs retired, the same shape 00262 used. The runs and their history remain.
UPDATE kb.agent_run_jobs
SET status = 'failed',
    completed_at = now()
WHERE run_id IN (
    SELECT id
    FROM (
        SELECT id,
               row_number() OVER (
                   PARTITION BY subject_object_id
                   ORDER BY (status = 'working') DESC,
                            (status = 'input-required') DESC,
                            created_at DESC,
                            id DESC
               ) AS rn
        FROM kb.agent_runs
        WHERE subject_object_id IS NOT NULL
          AND status IN ('submitted', 'working', 'input-required')
    ) ranked
    WHERE rn > 1
)
AND status IN ('pending', 'processing');

UPDATE kb.agent_runs
SET status = 'cancelled',
    completed_at = now()
WHERE id IN (
    SELECT id
    FROM (
        SELECT id,
               row_number() OVER (
                   PARTITION BY subject_object_id
                   ORDER BY (status = 'working') DESC,
                            (status = 'input-required') DESC,
                            created_at DESC,
                            id DESC
               ) AS rn
        FROM kb.agent_runs
        WHERE subject_object_id IS NOT NULL
          AND status IN ('submitted', 'working', 'input-required')
    ) ranked
    WHERE rn > 1
);

-- Step 2 — replace the inert index. A failed CONCURRENTLY build leaves an
-- INVALID index behind and contains no uniqueness guarantee, so drop any
-- leftover target-name index first; a retry then rebuilds from scratch (the
-- dedupe above is idempotent once one live run per subject remains).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_agent_runs_live_subject;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runs_live_subject
    ON kb.agent_runs (subject_object_id)
    WHERE subject_object_id IS NOT NULL
      AND status IN ('submitted', 'working', 'input-required');

-- Step 3 — post-condition guard. With NO TRANSACTION each statement commits on
-- its own, so a run whose CONCURRENTLY build did not take effect (or which left
-- the dead 00262 predicate in place) would still let goose record the version
-- and the invariant would silently not exist. Raise instead. StatementBegin/End
-- is required because goose's NO TRANSACTION splitter does not understand
-- dollar-quoted bodies and would truncate at the first semicolon.
-- +goose StatementBegin
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(e.name, ', ') INTO bad
    FROM (VALUES ('idx_agent_runs_live_subject')) AS e(name)
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
          AND pg_get_indexdef(i.indexrelid) LIKE '%submitted%'
          AND pg_get_indexdef(i.indexrelid) LIKE '%working%'
          AND pg_get_indexdef(i.indexrelid) LIKE '%input-required%'
    );
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00315: agent_runs live-subject unique index missing, invalid, or still uses the pre-ACP statuses: %', bad;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Restore the index 00262 defined. Rollback of this fix restores the (inert)
-- predicate it was fixing; the cancelled duplicate runs are not resurrected.
-- Kept CONCURRENTLY and outside any StatementBegin/End block: goose runs such a
-- block as a single ExecContext, which Postgres treats as a transaction, where
-- DROP/CREATE INDEX CONCURRENTLY is illegal.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_agent_runs_live_subject;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runs_live_subject
    ON kb.agent_runs (subject_object_id)
    WHERE subject_object_id IS NOT NULL
      AND status IN ('queued', 'running', 'paused');
