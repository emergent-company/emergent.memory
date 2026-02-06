import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Migration: Add actor tracking columns to graph_objects table
 *
 * Adds columns to track who made changes to graph objects:
 * - actor_type: 'user' | 'agent' | 'system'
 * - actor_id: UUID of the user or agent that made the change
 *
 * This enables reaction agents to prevent infinite loops by filtering
 * out events triggered by themselves or by other agents.
 */
export class AddGraphObjectActorColumns1767199002000
  implements MigrationInterface
{
  name = 'AddGraphObjectActorColumns1767199002000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Add actor_type column with default 'user' for existing records
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      ADD COLUMN IF NOT EXISTS actor_type TEXT DEFAULT 'user'
    `);

    // Add check constraint for actor_type
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      ADD CONSTRAINT chk_graph_objects_actor_type
      CHECK (actor_type IN ('user', 'agent', 'system'))
    `);

    // Add actor_id column (nullable - system actions may not have an actor)
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      ADD COLUMN IF NOT EXISTS actor_id UUID DEFAULT NULL
    `);

    // Add index for efficient querying by actor
    await queryRunner.query(`
      CREATE INDEX IF NOT EXISTS idx_graph_objects_actor
      ON kb.graph_objects(actor_type, actor_id)
      WHERE actor_type IS NOT NULL
    `);

    // Add comment explaining usage
    await queryRunner.query(`
      COMMENT ON COLUMN kb.graph_objects.actor_type IS 
      'Type of actor that created/modified this object: user, agent, or system'
    `);
    await queryRunner.query(`
      COMMENT ON COLUMN kb.graph_objects.actor_id IS 
      'UUID of the user or agent that created/modified this object'
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Remove comments
    await queryRunner.query(`
      COMMENT ON COLUMN kb.graph_objects.actor_type IS NULL
    `);
    await queryRunner.query(`
      COMMENT ON COLUMN kb.graph_objects.actor_id IS NULL
    `);

    // Remove index
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_graph_objects_actor
    `);

    // Remove check constraint
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      DROP CONSTRAINT IF EXISTS chk_graph_objects_actor_type
    `);

    // Remove columns
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      DROP COLUMN IF EXISTS actor_id
    `);
    await queryRunner.query(`
      ALTER TABLE kb.graph_objects
      DROP COLUMN IF EXISTS actor_type
    `);
  }
}
