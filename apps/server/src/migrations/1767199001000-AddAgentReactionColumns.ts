import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Migration: Add reaction-related columns to agents table
 *
 * Adds columns for:
 * - reaction_config: JSONB configuration for reaction triggers
 * - execution_mode: How the agent executes (suggest/execute/hybrid)
 * - capabilities: JSONB defining what operations the agent can perform
 */
export class AddAgentReactionColumns1767199001000
  implements MigrationInterface
{
  name = 'AddAgentReactionColumns1767199001000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Add reaction_config column for reaction trigger configuration
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD COLUMN IF NOT EXISTS reaction_config JSONB DEFAULT NULL
    `);

    // Add execution_mode column
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD COLUMN IF NOT EXISTS execution_mode TEXT DEFAULT 'execute'
    `);

    // Add check constraint for execution_mode
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD CONSTRAINT chk_agents_execution_mode
      CHECK (execution_mode IN ('suggest', 'execute', 'hybrid'))
    `);

    // Add capabilities column for operation restrictions
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD COLUMN IF NOT EXISTS capabilities JSONB DEFAULT NULL
    `);

    // Add comment explaining reaction_config structure
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agents.reaction_config IS 
      'Configuration for reaction triggers: {objectTypes: string[], events: ("created"|"updated"|"deleted")[], concurrencyStrategy: "skip"|"parallel", ignoreAgentTriggered: boolean, ignoreSelfTriggered: boolean}'
    `);

    // Add comment explaining capabilities structure
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agents.capabilities IS 
      'Capability restrictions: {canCreateObjects: boolean, canUpdateObjects: boolean, canDeleteObjects: boolean, canCreateRelationships: boolean, allowedObjectTypes: string[]}'
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Remove comments
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agents.reaction_config IS NULL
    `);
    await queryRunner.query(`
      COMMENT ON COLUMN kb.agents.capabilities IS NULL
    `);

    // Remove check constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP CONSTRAINT IF EXISTS chk_agents_execution_mode
    `);

    // Remove columns
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP COLUMN IF EXISTS capabilities
    `);
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP COLUMN IF EXISTS execution_mode
    `);
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP COLUMN IF EXISTS reaction_config
    `);
  }
}
