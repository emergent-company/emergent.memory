import { Injectable, Logger } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { Agent } from '../../entities/agent.entity';
import { AgentRun } from '../../entities/agent-run.entity';
import { CreateAgentDto, UpdateAgentDto } from './dto';

/**
 * AgentService
 *
 * Manages agent configurations and records agent runs.
 * Used by the scheduler service and admin API.
 */
@Injectable()
export class AgentService {
  private readonly logger = new Logger(AgentService.name);

  constructor(
    @InjectRepository(Agent)
    private readonly agentRepo: Repository<Agent>,
    @InjectRepository(AgentRun)
    private readonly agentRunRepo: Repository<AgentRun>
  ) {}

  /**
   * Get all agents for a project
   */
  async findAll(projectId?: string): Promise<Agent[]> {
    if (projectId) {
      return this.agentRepo.find({
        where: { projectId },
        order: { name: 'ASC' },
      });
    }
    return this.agentRepo.find({
      order: { name: 'ASC' },
    });
  }

  /**
   * Get all enabled agents (for scheduler - no project filter)
   */
  async findEnabled(): Promise<Agent[]> {
    return this.agentRepo.find({
      where: { enabled: true },
      order: { name: 'ASC' },
    });
  }

  /**
   * Get an agent by ID, optionally validating project ownership
   */
  async findById(id: string, projectId?: string): Promise<Agent | null> {
    const where: any = { id };
    if (projectId) {
      where.projectId = projectId;
    }
    return this.agentRepo.findOne({ where });
  }

  /**
   * Get an agent by strategyType
   * Note: Multiple agents can share the same strategyType, this returns the first match
   */
  async findByStrategyType(strategyType: string): Promise<Agent | null> {
    return this.agentRepo.findOne({ where: { strategyType } });
  }

  /**
   * Create a new agent
   */
  async create(dto: CreateAgentDto): Promise<Agent> {
    const agent = this.agentRepo.create({
      projectId: dto.projectId,
      name: dto.name,
      strategyType: dto.strategyType,
      prompt: dto.prompt || null,
      cronSchedule: dto.cronSchedule,
      enabled: dto.enabled ?? true,
      triggerType: dto.triggerType || 'schedule',
      reactionConfig: dto.reactionConfig || null,
      executionMode: dto.executionMode || 'execute',
      capabilities: dto.capabilities || null,
      config: dto.config || {},
      description: dto.description || null,
    });
    return this.agentRepo.save(agent);
  }

  /**
   * Update an agent's configuration
   */
  async update(id: string, dto: UpdateAgentDto): Promise<Agent | null> {
    // Build update object with only defined fields
    const updates: Partial<Agent> = {};
    if (dto.name !== undefined) updates.name = dto.name;
    if (dto.prompt !== undefined) updates.prompt = dto.prompt;
    if (dto.cronSchedule !== undefined) updates.cronSchedule = dto.cronSchedule;
    if (dto.enabled !== undefined) updates.enabled = dto.enabled;
    if (dto.triggerType !== undefined) updates.triggerType = dto.triggerType;
    if (dto.reactionConfig !== undefined)
      updates.reactionConfig = dto.reactionConfig;
    if (dto.executionMode !== undefined)
      updates.executionMode = dto.executionMode;
    if (dto.capabilities !== undefined) updates.capabilities = dto.capabilities;
    if (dto.config !== undefined) updates.config = dto.config;
    if (dto.description !== undefined) updates.description = dto.description;

    if (Object.keys(updates).length > 0) {
      await this.agentRepo.update(id, updates);
    }
    return this.findById(id);
  }

  /**
   * Delete an agent and all its runs
   */
  async delete(id: string): Promise<boolean> {
    // First delete all runs for this agent
    await this.agentRunRepo.delete({ agentId: id });
    // Then delete the agent
    const result = await this.agentRepo.delete(id);
    return (result.affected ?? 0) > 0;
  }

  /**
   * Record the start of an agent run
   */
  async startRun(agentId: string): Promise<AgentRun> {
    const run = this.agentRunRepo.create({
      agentId,
      status: 'running',
      startedAt: new Date(),
      summary: {},
    });
    return this.agentRunRepo.save(run);
  }

  /**
   * Complete an agent run with success
   */
  async completeRun(
    runId: string,
    summary: Record<string, any>
  ): Promise<void> {
    const now = new Date();
    const run = await this.agentRunRepo.findOne({ where: { id: runId } });
    if (!run) {
      this.logger.warn(`Cannot complete run ${runId}: not found`);
      return;
    }

    const durationMs = now.getTime() - run.startedAt.getTime();

    await this.agentRunRepo.update(runId, {
      status: 'success',
      completedAt: now,
      durationMs,
      summary,
    });

    // Update agent's last run info
    await this.agentRepo.update(run.agentId, {
      lastRunAt: now,
      lastRunStatus: 'success',
    });
  }

  /**
   * Mark an agent run as skipped
   */
  async skipRun(runId: string, skipReason: string): Promise<void> {
    const now = new Date();
    const run = await this.agentRunRepo.findOne({ where: { id: runId } });
    if (!run) {
      this.logger.warn(`Cannot skip run ${runId}: not found`);
      return;
    }

    const durationMs = now.getTime() - run.startedAt.getTime();

    await this.agentRunRepo.update(runId, {
      status: 'skipped',
      completedAt: now,
      durationMs,
      skipReason,
    });

    // Update agent's last run info
    await this.agentRepo.update(run.agentId, {
      lastRunAt: now,
      lastRunStatus: 'skipped',
    });
  }

  /**
   * Mark an agent run as failed
   */
  async failRun(runId: string, errorMessage: string): Promise<void> {
    const now = new Date();
    const run = await this.agentRunRepo.findOne({ where: { id: runId } });
    if (!run) {
      this.logger.warn(`Cannot fail run ${runId}: not found`);
      return;
    }

    const durationMs = now.getTime() - run.startedAt.getTime();

    await this.agentRunRepo.update(runId, {
      status: 'error',
      completedAt: now,
      durationMs,
      errorMessage,
    });

    // Update agent's last run info
    await this.agentRepo.update(run.agentId, {
      lastRunAt: now,
      lastRunStatus: 'error',
    });
  }

  /**
   * Get recent runs for an agent
   */
  async getRecentRuns(agentId: string, limit = 20): Promise<AgentRun[]> {
    return this.agentRunRepo.find({
      where: { agentId },
      order: { startedAt: 'DESC' },
      take: limit,
    });
  }

  /**
   * Get run statistics for an agent
   */
  async getRunStats(agentId: string): Promise<{
    totalRuns: number;
    successRuns: number;
    skippedRuns: number;
    errorRuns: number;
    avgDurationMs: number;
  }> {
    const result = await this.agentRunRepo
      .createQueryBuilder('r')
      .select([
        'COUNT(*) as total',
        `COUNT(*) FILTER (WHERE status = 'success') as success`,
        `COUNT(*) FILTER (WHERE status = 'skipped') as skipped`,
        `COUNT(*) FILTER (WHERE status = 'error') as error`,
        `AVG(duration_ms) FILTER (WHERE status = 'success') as avg_duration`,
      ])
      .where('r.agent_id = :agentId', { agentId })
      .getRawOne();

    return {
      totalRuns: parseInt(result.total, 10) || 0,
      successRuns: parseInt(result.success, 10) || 0,
      skippedRuns: parseInt(result.skipped, 10) || 0,
      errorRuns: parseInt(result.error, 10) || 0,
      avgDurationMs: parseFloat(result.avg_duration) || 0,
    };
  }
}
