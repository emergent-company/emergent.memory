import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Migration: Add 'reaction' to agents trigger_type constraint
 *
 * Updates the trigger_type check constraint to allow 'reaction' in addition
 * to existing 'schedule' and 'manual' types.
 */
export class AddReactionTriggerType1767199000000 implements MigrationInterface {
  name = 'AddReactionTriggerType1767199000000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Drop existing check constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP CONSTRAINT IF EXISTS chk_agents_trigger_type
    `);

    // Add updated check constraint with 'reaction' option
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD CONSTRAINT chk_agents_trigger_type
      CHECK (trigger_type IN ('schedule', 'manual', 'reaction'))
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // First, update any 'reaction' agents back to 'manual'
    await queryRunner.query(`
      UPDATE kb.agents
      SET trigger_type = 'manual'
      WHERE trigger_type = 'reaction'
    `);

    // Drop updated constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP CONSTRAINT IF EXISTS chk_agents_trigger_type
    `);

    // Restore original constraint without 'reaction'
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD CONSTRAINT chk_agents_trigger_type
      CHECK (trigger_type IN ('schedule', 'manual'))
    `);
  }
}
