import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Migration: Create agent_processing_log table
 *
 * Tracks which objects have been processed by each agent to:
 * - Avoid duplicate processing of the same object version
 * - Support concurrency control (skip vs parallel)
 * - Track processing status and errors
 * - Detect stuck jobs for recovery
 */
export class CreateAgentProcessingLog1767199003000
  implements MigrationInterface
{
  name = 'CreateAgentProcessingLog1767199003000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Create agent_processing_log table
    await queryRunner.query(`
      CREATE TABLE IF NOT EXISTS kb.agent_processing_log (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        agent_id UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
        graph_object_id UUID NOT NULL REFERENCES kb.graph_objects(id) ON DELETE CASCADE,
        object_version INTEGER NOT NULL,
        event_type TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'pending',
        started_at TIMESTAMPTZ,
        completed_at TIMESTAMPTZ,
        error_message TEXT,
        result_summary JSONB,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
      )
    `);

    // Add check constraint for event_type
    await queryRunner.query(`
      ALTER TABLE kb.agent_processing_log
      ADD CONSTRAINT chk_agent_processing_log_event_type
      CHECK (event_type IN ('created', 'updated', 'deleted'))
    `);

    // Add check constraint for status
    await queryRunner.query(`
      ALTER TABLE kb.agent_processing_log
      ADD CONSTRAINT chk_agent_processing_log_status
      CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'abandoned', 'skipped'))
    `);

    // Add index for looking up existing processing entries
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_agent_processing_log_lookup
      ON kb.agent_processing_log(agent_id, graph_object_id, object_version, event_type)
    `);

    // Add index for finding stuck jobs (processing status with old started_at)
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_agent_processing_log_stuck
      ON kb.agent_processing_log(status, started_at)
      WHERE status = 'processing'
    `);

    // Add index for agent run history
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_agent_processing_log_agent
      ON kb.agent_processing_log(agent_id, created_at DESC)
    `);

    // Add index for object processing history
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_agent_processing_log_object
      ON kb.agent_processing_log(graph_object_id, created_at DESC)
    `);

    // Add comments
    await queryRunner.query(`
      COMMENT ON TABLE kb.agent_processing_log IS 
      'Tracks which graph objects have been processed by reaction agents'
    `);
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agent_processing_log.object_version IS 
      'Version of the graph object when processing was triggered'
    `);
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agent_processing_log.status IS 
      'Processing status: pending, processing, completed, failed, abandoned (stuck), skipped'
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Drop indexes
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_agent_processing_log_object
    `);
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_agent_processing_log_agent
    `);
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_agent_processing_log_stuck
    `);
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_agent_processing_log_lookup
    `);

    // Drop table (constraints are dropped automatically)
    await queryRunner.query(`
      DROP TABLE IF EXISTS kb.agent_processing_log
    `);
  }
}
