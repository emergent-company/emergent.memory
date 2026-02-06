import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Adds dead letter queue support to data_source_sync_jobs table.
 * - retry_count: Tracks current retry attempt
 * - max_retries: Maximum retry attempts before moving to dead_letter (default 3)
 * - next_retry_at: When to retry next (for exponential backoff)
 */
export class AddDataSourceSyncJobsRetryColumns1768300000000
  implements MigrationInterface
{
  name = 'AddDataSourceSyncJobsRetryColumns1768300000000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Add retry tracking columns
    await queryRunner.query(`
      ALTER TABLE kb.data_source_sync_jobs
      ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0,
      ADD COLUMN IF NOT EXISTS max_retries INTEGER NOT NULL DEFAULT 3,
      ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ
    `);

    // Add index for retry processing
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_data_source_sync_jobs_retry
      ON kb.data_source_sync_jobs (status, next_retry_at)
      WHERE status = 'pending' AND next_retry_at IS NOT NULL
    `);

    // Add comments
    await queryRunner.query(`
      COMMENT ON COLUMN kb.data_source_sync_jobs.retry_count IS 'Current retry attempt number';
      COMMENT ON COLUMN kb.data_source_sync_jobs.max_retries IS 'Maximum retry attempts before moving to dead_letter status';
      COMMENT ON COLUMN kb.data_source_sync_jobs.next_retry_at IS 'When to retry a failed job (exponential backoff)'
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_data_source_sync_jobs_retry
    `);

    await queryRunner.query(`
      ALTER TABLE kb.data_source_sync_jobs
      DROP COLUMN IF EXISTS retry_count,
      DROP COLUMN IF EXISTS max_retries,
      DROP COLUMN IF EXISTS next_retry_at
    `);
  }
}
