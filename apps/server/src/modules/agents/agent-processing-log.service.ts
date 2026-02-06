import { Injectable, Logger } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository, LessThan, In } from 'typeorm';
import {
  AgentProcessingLog,
  AgentProcessingStatus,
  ProcessingEventType,
} from '../../entities/agent-processing-log.entity';

/**
 * Input for creating a new processing log entry
 */
export interface CreateProcessingLogInput {
  agentId: string;
  graphObjectId: string;
  objectVersion: number;
  eventType: ProcessingEventType;
}

/**
 * Result summary to store when processing completes
 */
export interface ProcessingResultSummary {
  suggestionsCreated?: number;
  objectsCreated?: number;
  objectsUpdated?: number;
  objectsDeleted?: number;
  relationshipsCreated?: number;
  [key: string]: any;
}

/**
 * AgentProcessingLogService
 *
 * Manages the agent processing log for reaction agents.
 * Used to:
 * - Track which graph objects have been processed by which agents
 * - Prevent duplicate processing of the same object version
 * - Support concurrency control (skip vs parallel)
 * - Detect and recover from stuck jobs
 */
@Injectable()
export class AgentProcessingLogService {
  private readonly logger = new Logger(AgentProcessingLogService.name);

  /** Timeout in milliseconds for stuck job detection (5 minutes) */
  private readonly STUCK_JOB_TIMEOUT_MS = 5 * 60 * 1000;

  constructor(
    @InjectRepository(AgentProcessingLog)
    private readonly processingLogRepo: Repository<AgentProcessingLog>
  ) {}

  /**
   * Create a new processing log entry in 'pending' status
   *
   * @param input The processing log input data
   * @returns The created processing log entry
   */
  async createEntry(
    input: CreateProcessingLogInput
  ): Promise<AgentProcessingLog> {
    const entry = this.processingLogRepo.create({
      agentId: input.agentId,
      graphObjectId: input.graphObjectId,
      objectVersion: input.objectVersion,
      eventType: input.eventType,
      status: 'pending',
    });

    const saved = await this.processingLogRepo.save(entry);
    this.logger.debug(
      `Created processing log entry ${saved.id} for agent ${input.agentId}, object ${input.graphObjectId} v${input.objectVersion}`
    );
    return saved;
  }

  /**
   * Mark an entry as 'processing' and set the started_at timestamp
   *
   * @param entryId The processing log entry ID
   * @returns The updated entry or null if not found
   */
  async markProcessing(entryId: string): Promise<AgentProcessingLog | null> {
    const entry = await this.processingLogRepo.findOne({
      where: { id: entryId },
    });
    if (!entry) {
      this.logger.warn(`Cannot mark processing: entry ${entryId} not found`);
      return null;
    }

    entry.status = 'processing';
    entry.startedAt = new Date();

    const saved = await this.processingLogRepo.save(entry);
    this.logger.debug(`Marked entry ${entryId} as processing`);
    return saved;
  }

  /**
   * Mark an entry as 'completed' and set the completed_at timestamp
   *
   * @param entryId The processing log entry ID
   * @param resultSummary Optional summary of processing results
   * @returns The updated entry or null if not found
   */
  async markCompleted(
    entryId: string,
    resultSummary?: ProcessingResultSummary
  ): Promise<AgentProcessingLog | null> {
    const entry = await this.processingLogRepo.findOne({
      where: { id: entryId },
    });
    if (!entry) {
      this.logger.warn(`Cannot mark completed: entry ${entryId} not found`);
      return null;
    }

    entry.status = 'completed';
    entry.completedAt = new Date();
    if (resultSummary) {
      entry.resultSummary = resultSummary;
    }

    const saved = await this.processingLogRepo.save(entry);
    this.logger.debug(`Marked entry ${entryId} as completed`);
    return saved;
  }

  /**
   * Mark an entry as 'failed' with an error message
   *
   * @param entryId The processing log entry ID
   * @param errorMessage The error message describing the failure
   * @returns The updated entry or null if not found
   */
  async markFailed(
    entryId: string,
    errorMessage: string
  ): Promise<AgentProcessingLog | null> {
    const entry = await this.processingLogRepo.findOne({
      where: { id: entryId },
    });
    if (!entry) {
      this.logger.warn(`Cannot mark failed: entry ${entryId} not found`);
      return null;
    }

    entry.status = 'failed';
    entry.completedAt = new Date();
    entry.errorMessage = errorMessage;

    const saved = await this.processingLogRepo.save(entry);
    this.logger.warn(`Marked entry ${entryId} as failed: ${errorMessage}`);
    return saved;
  }

  /**
   * Mark an entry as 'skipped' (used when concurrency strategy is 'skip')
   *
   * @param entryId The processing log entry ID
   * @param reason Optional reason for skipping
   * @returns The updated entry or null if not found
   */
  async markSkipped(
    entryId: string,
    reason?: string
  ): Promise<AgentProcessingLog | null> {
    const entry = await this.processingLogRepo.findOne({
      where: { id: entryId },
    });
    if (!entry) {
      this.logger.warn(`Cannot mark skipped: entry ${entryId} not found`);
      return null;
    }

    entry.status = 'skipped';
    entry.completedAt = new Date();
    if (reason) {
      entry.errorMessage = reason;
    }

    const saved = await this.processingLogRepo.save(entry);
    this.logger.debug(
      `Marked entry ${entryId} as skipped: ${reason || 'no reason'}`
    );
    return saved;
  }

  /**
   * Find an existing entry that is pending or processing for the same
   * agent, object, version, and event type.
   *
   * Used to check if we should skip processing due to concurrency control.
   *
   * @param agentId The agent ID
   * @param graphObjectId The graph object ID
   * @param objectVersion The object version
   * @param eventType The event type
   * @returns The existing entry or null if none found
   */
  async findPendingOrProcessing(
    agentId: string,
    graphObjectId: string,
    objectVersion: number,
    eventType: ProcessingEventType
  ): Promise<AgentProcessingLog | null> {
    return this.processingLogRepo.findOne({
      where: {
        agentId,
        graphObjectId,
        objectVersion,
        eventType,
        status: In(['pending', 'processing'] as AgentProcessingStatus[]),
      },
    });
  }

  /**
   * Check if an agent is currently processing any entry for a specific object
   * (regardless of version). Used for 'skip' concurrency strategy.
   *
   * @param agentId The agent ID
   * @param graphObjectId The graph object ID
   * @returns True if there's an active processing entry
   */
  async isAgentProcessingObject(
    agentId: string,
    graphObjectId: string
  ): Promise<boolean> {
    const count = await this.processingLogRepo.count({
      where: {
        agentId,
        graphObjectId,
        status: In(['pending', 'processing'] as AgentProcessingStatus[]),
      },
    });
    return count > 0;
  }

  /**
   * Find and mark stuck jobs as 'abandoned'.
   *
   * Jobs in 'processing' status for more than 5 minutes are considered stuck.
   * This allows them to be retried or investigated.
   *
   * @returns The number of jobs marked as abandoned
   */
  async markStuckJobsAsAbandoned(): Promise<number> {
    const cutoffTime = new Date(Date.now() - this.STUCK_JOB_TIMEOUT_MS);

    // Find stuck jobs
    const stuckJobs = await this.processingLogRepo.find({
      where: {
        status: 'processing',
        startedAt: LessThan(cutoffTime),
      },
    });

    if (stuckJobs.length === 0) {
      return 0;
    }

    // Mark them as abandoned
    const now = new Date();
    for (const job of stuckJobs) {
      job.status = 'abandoned';
      job.completedAt = now;
      job.errorMessage = `Job stuck for more than ${
        this.STUCK_JOB_TIMEOUT_MS / 1000 / 60
      } minutes`;
    }

    await this.processingLogRepo.save(stuckJobs);

    this.logger.warn(
      `Marked ${stuckJobs.length} stuck jobs as abandoned: ${stuckJobs
        .map((j) => j.id)
        .join(', ')}`
    );

    return stuckJobs.length;
  }

  /**
   * Get processing history for an agent
   *
   * @param agentId The agent ID
   * @param limit Maximum number of entries to return
   * @returns Recent processing log entries
   */
  async getAgentHistory(
    agentId: string,
    limit = 50
  ): Promise<AgentProcessingLog[]> {
    return this.processingLogRepo.find({
      where: { agentId },
      order: { createdAt: 'DESC' },
      take: limit,
    });
  }

  /**
   * Get processing history for a graph object
   *
   * @param graphObjectId The graph object ID
   * @param limit Maximum number of entries to return
   * @returns Recent processing log entries
   */
  async getObjectHistory(
    graphObjectId: string,
    limit = 50
  ): Promise<AgentProcessingLog[]> {
    return this.processingLogRepo.find({
      where: { graphObjectId },
      order: { createdAt: 'DESC' },
      take: limit,
    });
  }

  /**
   * Get processing statistics for an agent
   *
   * @param agentId The agent ID
   * @returns Statistics about processing entries
   */
  async getAgentStats(agentId: string): Promise<{
    totalEntries: number;
    pendingCount: number;
    processingCount: number;
    completedCount: number;
    failedCount: number;
    abandonedCount: number;
    skippedCount: number;
  }> {
    const result = await this.processingLogRepo
      .createQueryBuilder('log')
      .select([
        'COUNT(*) as total',
        `COUNT(*) FILTER (WHERE status = 'pending') as pending`,
        `COUNT(*) FILTER (WHERE status = 'processing') as processing`,
        `COUNT(*) FILTER (WHERE status = 'completed') as completed`,
        `COUNT(*) FILTER (WHERE status = 'failed') as failed`,
        `COUNT(*) FILTER (WHERE status = 'abandoned') as abandoned`,
        `COUNT(*) FILTER (WHERE status = 'skipped') as skipped`,
      ])
      .where('log.agent_id = :agentId', { agentId })
      .getRawOne();

    return {
      totalEntries: parseInt(result.total, 10) || 0,
      pendingCount: parseInt(result.pending, 10) || 0,
      processingCount: parseInt(result.processing, 10) || 0,
      completedCount: parseInt(result.completed, 10) || 0,
      failedCount: parseInt(result.failed, 10) || 0,
      abandonedCount: parseInt(result.abandoned, 10) || 0,
      skippedCount: parseInt(result.skipped, 10) || 0,
    };
  }

  /**
   * Delete old completed/failed entries for cleanup (retention policy)
   *
   * @param olderThanDays Delete entries older than this many days
   * @returns Number of deleted entries
   */
  async cleanupOldEntries(olderThanDays = 30): Promise<number> {
    const cutoffDate = new Date();
    cutoffDate.setDate(cutoffDate.getDate() - olderThanDays);

    const result = await this.processingLogRepo
      .createQueryBuilder()
      .delete()
      .where('status IN (:...statuses)', {
        statuses: ['completed', 'failed', 'abandoned', 'skipped'],
      })
      .andWhere('created_at < :cutoffDate', { cutoffDate })
      .execute();

    const deleted = result.affected || 0;
    if (deleted > 0) {
      this.logger.log(`Cleaned up ${deleted} old processing log entries`);
    }
    return deleted;
  }
}
