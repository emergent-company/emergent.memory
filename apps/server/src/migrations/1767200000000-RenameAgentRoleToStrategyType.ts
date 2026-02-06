import { MigrationInterface, QueryRunner } from 'typeorm';

export class RenameAgentRoleToStrategyType1767200000000
  implements MigrationInterface
{
  name = 'RenameAgentRoleToStrategyType1767200000000';

  public async up(queryRunner: QueryRunner): Promise<void> {
    // Drop the unique index on role
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb."IDX_agents_role"
    `);

    // Also try dropping the constraint if it exists as a unique constraint
    await queryRunner.query(`
      ALTER TABLE kb.agents DROP CONSTRAINT IF EXISTS "UQ_agents_role"
    `);

    // Rename the column from role to strategy_type
    await queryRunner.query(`
      ALTER TABLE kb.agents RENAME COLUMN role TO strategy_type
    `);

    // Create a non-unique index on strategy_type
    await queryRunner.query(`
      CREATE INDEX "IDX_agents_strategy_type" ON kb.agents (strategy_type)
    `);
  }

  public async down(queryRunner: QueryRunner): Promise<void> {
    // Drop the non-unique index
    await queryRunner.query(`
      DROP INDEX IF EXISTS kb."IDX_agents_strategy_type"
    `);

    // Rename column back
    await queryRunner.query(`
      ALTER TABLE kb.agents RENAME COLUMN strategy_type TO role
    `);

    // Recreate the unique index
    await queryRunner.query(`
      CREATE UNIQUE INDEX "IDX_agents_role" ON kb.agents (role)
    `);
  }
}
