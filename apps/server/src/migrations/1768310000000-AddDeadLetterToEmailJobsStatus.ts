import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Adds 'dead_letter' status to email_jobs table for dead letter queue support.
 * Jobs that fail permanently after max retries are moved to dead_letter status
 * rather than just 'failed', enabling better monitoring and manual retry.
 */
export class AddDeadLetterToEmailJobsStatus1768310000000
  implements MigrationInterface
{
  name = 'AddDeadLetterToEmailJobsStatus1768310000000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Drop the old constraint
    await queryRunner.query(`
      ALTER TABLE kb.email_jobs DROP CONSTRAINT IF EXISTS email_jobs_status_check
    `);

    // Add the new constraint with dead_letter
    await queryRunner.query(`
      ALTER TABLE kb.email_jobs 
      ADD CONSTRAINT email_jobs_status_check 
      CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'dead_letter'))
    `);

    // Add comment for documentation
    await queryRunner.query(`
      COMMENT ON COLUMN kb.email_jobs.status IS 
        'Job status: pending (queued), processing (being sent), sent (delivered), failed (temporary failure), dead_letter (permanently failed after max retries)'
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Move any dead_letter jobs back to failed
    await queryRunner.query(`
      UPDATE kb.email_jobs SET status = 'failed' WHERE status = 'dead_letter'
    `);

    // Drop the new constraint
    await queryRunner.query(`
      ALTER TABLE kb.email_jobs DROP CONSTRAINT IF EXISTS email_jobs_status_check
    `);

    // Restore the old constraint
    await queryRunner.query(`
      ALTER TABLE kb.email_jobs 
      ADD CONSTRAINT email_jobs_status_check 
      CHECK (status IN ('pending', 'processing', 'sent', 'failed'))
    `);
  }
}
