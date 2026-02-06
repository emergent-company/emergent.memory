import {
  Injectable,
  Logger,
  OnModuleInit,
  OnModuleDestroy,
} from '@nestjs/common';
import { SchedulerRegistry } from '@nestjs/schedule';
import { CronJob } from 'cron';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { DataSourceIntegration } from '../../entities/data-source-integration.entity';
import { DataSourceSyncJobService } from './data-source-sync-job.service';
import {
  SyncScheduleDto,
  SyncConfigurationDto,
} from './dto/data-source-integration.dto';

/**
 * Job key format for scheduler registry
 */
function getJobKey(integrationId: string, configId: string): string {
  return `sync-${integrationId}-${configId}`;
}

/**
 * Convert interval minutes to a cron expression
 * Returns a cron that runs every N minutes
 */
function intervalToCron(intervalMinutes: number): string {
  if (intervalMinutes < 60) {
    // Every N minutes
    return `*/${intervalMinutes} * * * *`;
  } else if (intervalMinutes < 1440) {
    // Every N hours
    const hours = Math.floor(intervalMinutes / 60);
    return `0 */${hours} * * *`;
  } else {
    // Daily at midnight
    return '0 0 * * *';
  }
}

/**
 * Get the effective cron expression from a schedule
 * Prefers cronSchedule over intervalMinutes
 */
function getEffectiveCron(schedule: SyncScheduleDto): string | null {
  if (!schedule.enabled) {
    return null;
  }

  if (schedule.cronSchedule) {
    return schedule.cronSchedule;
  }

  if (schedule.intervalMinutes) {
    return intervalToCron(schedule.intervalMinutes);
  }

  return null;
}

/**
 * IntegrationSchedulerService
 *
 * Manages cron-based scheduling for data source sync configurations.
 * Loads scheduled sync configurations from the database on startup and
 * creates/updates cron jobs as configurations are modified.
 */
@Injectable()
export class IntegrationSchedulerService
  implements OnModuleInit, OnModuleDestroy
{
  private readonly logger = new Logger(IntegrationSchedulerService.name);
  private readonly runningJobs = new Map<string, boolean>();

  constructor(
    private readonly schedulerRegistry: SchedulerRegistry,
    @InjectRepository(DataSourceIntegration)
    private readonly integrationRepo: Repository<DataSourceIntegration>,
    private readonly syncJobService: DataSourceSyncJobService
  ) {}

  async onModuleInit(): Promise<void> {
    this.logger.log('Initializing integration scheduler...');
    await this.loadSchedules();
  }

  onModuleDestroy(): void {
    this.logger.log('Shutting down integration scheduler...');
    this.stopAllJobs();
  }

  /**
   * Load all scheduled sync configurations and create cron jobs
   */
  async loadSchedules(): Promise<void> {
    try {
      // Find all active integrations with sync configurations
      const integrations = await this.integrationRepo.find({
        where: { status: 'active' },
      });

      this.logger.log(`Found ${integrations.length} active integration(s)`);

      let scheduledCount = 0;
      for (const integration of integrations) {
        const configs = this.getSyncConfigurationsFromMetadata(integration);

        for (const config of configs) {
          if (config.schedule?.enabled) {
            await this.scheduleConfig(integration, config);
            scheduledCount++;
          }
        }
      }

      this.logger.log(`Scheduled ${scheduledCount} sync configuration(s)`);
    } catch (error) {
      this.logger.error('Failed to load schedules', (error as Error).stack);
    }
  }

  /**
   * Schedule a single sync configuration
   */
  async scheduleConfig(
    integration: DataSourceIntegration,
    config: SyncConfigurationDto
  ): Promise<void> {
    const { id: integrationId, name: integrationName, projectId } = integration;
    const { id: configId, name: configName, schedule } = config;

    if (!schedule?.enabled) {
      this.logger.debug(
        `Config "${configName}" on "${integrationName}" is not scheduled, skipping`
      );
      this.unscheduleConfig(integrationId, configId);
      return;
    }

    const cronExpr = getEffectiveCron(schedule);
    if (!cronExpr) {
      this.logger.warn(
        `Config "${configName}" on "${integrationName}" has no valid schedule, skipping`
      );
      this.unscheduleConfig(integrationId, configId);
      return;
    }

    // Remove existing job if any
    this.unscheduleConfig(integrationId, configId);

    try {
      const jobKey = getJobKey(integrationId, configId);

      const job = new CronJob(cronExpr, () => {
        this.executeSyncConfig(
          integrationId,
          configId,
          projectId,
          configName
        ).catch((err) => {
          this.logger.error(
            `Scheduled sync for config "${configName}" failed: ${err.message}`,
            err.stack
          );
        });
      });

      // Use type assertion to handle cron version mismatch
      this.schedulerRegistry.addCronJob(jobKey, job as any);
      job.start();

      this.logger.log(
        `Scheduled sync config "${configName}" on "${integrationName}" with cron: ${cronExpr}`
      );
    } catch (error) {
      this.logger.error(
        `Failed to schedule config "${configName}": ${(error as Error).message}`
      );
    }
  }

  /**
   * Unschedule a sync configuration's cron job
   */
  unscheduleConfig(integrationId: string, configId: string): void {
    const jobKey = getJobKey(integrationId, configId);
    try {
      if (this.schedulerRegistry.doesExist('cron', jobKey)) {
        this.schedulerRegistry.deleteCronJob(jobKey);
        this.logger.debug(`Unscheduled sync job: ${jobKey}`);
      }
    } catch {
      // Job doesn't exist, ignore
    }
  }

  /**
   * Stop all scheduled sync jobs
   */
  stopAllJobs(): void {
    const jobs = this.schedulerRegistry.getCronJobs();
    jobs.forEach((job, name) => {
      if (name.startsWith('sync-')) {
        job.stop();
        this.logger.debug(`Stopped job: ${name}`);
      }
    });
  }

  /**
   * Execute a scheduled sync
   */
  private async executeSyncConfig(
    integrationId: string,
    configId: string,
    projectId: string,
    configName: string
  ): Promise<void> {
    const jobKey = getJobKey(integrationId, configId);

    // Prevent concurrent runs of the same config
    if (this.runningJobs.get(jobKey)) {
      this.logger.debug(
        `Sync for config "${configName}" is already running, skipping`
      );
      return;
    }

    this.runningJobs.set(jobKey, true);

    try {
      // Re-fetch integration and config to get latest settings
      const integration = await this.integrationRepo.findOne({
        where: { id: integrationId },
      });

      if (!integration || integration.status !== 'active') {
        this.logger.debug(
          `Integration ${integrationId} is disabled or deleted, skipping scheduled sync`
        );
        this.unscheduleConfig(integrationId, configId);
        return;
      }

      const configs = this.getSyncConfigurationsFromMetadata(integration);
      const config = configs.find((c) => c.id === configId);

      if (!config || !config.schedule?.enabled) {
        this.logger.debug(
          `Config ${configId} is disabled or deleted, skipping scheduled sync`
        );
        this.unscheduleConfig(integrationId, configId);
        return;
      }

      this.logger.log(
        `Starting scheduled sync for config "${configName}" on integration "${integration.name}"`
      );

      // Create a sync job with the configuration's options
      await this.syncJobService.createAndStart({
        integrationId,
        projectId,
        triggerType: 'scheduled',
        configurationId: configId,
        configurationName: configName,
        syncOptions: config.options,
      });

      // Update lastRunAt in the schedule
      await this.updateScheduleLastRun(integrationId, configId);
    } finally {
      this.runningJobs.set(jobKey, false);
    }
  }

  /**
   * Update the lastRunAt timestamp for a schedule
   */
  private async updateScheduleLastRun(
    integrationId: string,
    configId: string
  ): Promise<void> {
    try {
      const integration = await this.integrationRepo.findOne({
        where: { id: integrationId },
      });

      if (!integration) return;

      const configs = this.getSyncConfigurationsFromMetadata(integration);
      const configIndex = configs.findIndex((c) => c.id === configId);

      if (configIndex === -1 || !configs[configIndex].schedule) return;

      configs[configIndex].schedule!.lastRunAt = new Date().toISOString();

      // Calculate next run time
      const cronExpr = getEffectiveCron(configs[configIndex].schedule!);
      if (cronExpr) {
        try {
          const job = new CronJob(cronExpr, () => {});
          const nextDate = job.nextDate();
          configs[configIndex].schedule!.nextRunAt =
            nextDate.toISO() ?? undefined;
        } catch {
          // Ignore errors calculating next run
        }
      }

      // Save back to metadata
      await this.integrationRepo
        .createQueryBuilder()
        .update()
        .set({
          metadata: () =>
            `metadata || '${JSON.stringify({
              syncConfigurations: configs,
            })}'::jsonb`,
        })
        .where('id = :id', { id: integrationId })
        .execute();
    } catch (error) {
      this.logger.error(
        `Failed to update schedule lastRunAt: ${(error as Error).message}`
      );
    }
  }

  /**
   * Reload schedules for a specific integration (called after admin updates)
   */
  async reloadIntegration(integrationId: string): Promise<void> {
    const integration = await this.integrationRepo.findOne({
      where: { id: integrationId },
    });

    if (!integration) {
      // Integration deleted, unschedule all its configs
      const jobs = this.schedulerRegistry.getCronJobs();
      jobs.forEach((_, name) => {
        if (name.startsWith(`sync-${integrationId}-`)) {
          this.schedulerRegistry.deleteCronJob(name);
        }
      });
      return;
    }

    // First, unschedule all existing jobs for this integration
    const jobs = this.schedulerRegistry.getCronJobs();
    jobs.forEach((_, name) => {
      if (name.startsWith(`sync-${integrationId}-`)) {
        this.schedulerRegistry.deleteCronJob(name);
      }
    });

    // Then re-schedule active configs
    if (integration.status === 'active') {
      const configs = this.getSyncConfigurationsFromMetadata(integration);
      for (const config of configs) {
        if (config.schedule?.enabled) {
          await this.scheduleConfig(integration, config);
        }
      }
    }
  }

  /**
   * Reload a specific sync configuration's schedule
   */
  async reloadConfig(integrationId: string, configId: string): Promise<void> {
    const integration = await this.integrationRepo.findOne({
      where: { id: integrationId },
    });

    if (!integration || integration.status !== 'active') {
      this.unscheduleConfig(integrationId, configId);
      return;
    }

    const configs = this.getSyncConfigurationsFromMetadata(integration);
    const config = configs.find((c) => c.id === configId);

    if (!config || !config.schedule?.enabled) {
      this.unscheduleConfig(integrationId, configId);
      return;
    }

    await this.scheduleConfig(integration, config);
  }

  /**
   * Extract sync configurations from integration metadata
   */
  private getSyncConfigurationsFromMetadata(
    integration: DataSourceIntegration
  ): SyncConfigurationDto[] {
    const configs = integration.metadata?.syncConfigurations;
    if (!Array.isArray(configs)) {
      return [];
    }
    return configs as SyncConfigurationDto[];
  }
}
