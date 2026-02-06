import { MigrationInterface, QueryRunner } from 'typeorm';

/**
 * Adds project_id column to kb.agents table.
 *
 * Agents are now project-scoped. This migration:
 * 1. Deletes existing agents (they don't have projects assigned)
 * 2. Adds required project_id column with FK to kb.projects
 * 3. Adds index for project filtering
 *
 * Note: Agent runs are cascade deleted when agents are deleted.
 */
export class AddProjectIdToAgents1768209470022 implements MigrationInterface {
  name = 'AddProjectIdToAgents1768209470022';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Delete existing agents (they don't have project context)
    // Agent runs will be cascade deleted via the FK constraint
    await queryRunner.query(`DELETE FROM kb.agents`);

    // Add project_id column as NOT NULL (required)
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD COLUMN project_id UUID NOT NULL
    `);

    // Add foreign key constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents
      ADD CONSTRAINT fk_agents_project
      FOREIGN KEY (project_id) REFERENCES kb.projects(id)
      ON DELETE CASCADE
    `);

    // Add index for project filtering
    await queryRunner.query(`
      CREATE INDEX idx_agents_project_id ON kb.agents(project_id)
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Remove index
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb.idx_agents_project_id
    `);

    // Remove foreign key constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP CONSTRAINT IF EXISTS fk_agents_project
    `);

    // Remove column
    await queryRunner.query(`
      ALTER TABLE kb.agents
      DROP COLUMN IF EXISTS project_id
    `);
  }
}
