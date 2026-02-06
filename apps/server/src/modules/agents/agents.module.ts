import { Module } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { ScheduleModule } from '@nestjs/schedule';
import { Agent, AgentRun, Task } from '../../entities';
import { GraphObject } from '../../entities/graph-object.entity';
import { AgentProcessingLog } from '../../entities/agent-processing-log.entity';
import { AgentService } from './agents.service';
import { AgentSchedulerService } from './agent-scheduler.service';
import { AgentProcessingLogService } from './agent-processing-log.service';
import { ReactionDispatcherService } from './reaction-dispatcher.service';
import { CapabilityCheckerService } from './capability-checker.service';
import { SuggestionService } from './suggestion.service';
import { AgentStrategyRegistry } from './strategies/agent-strategy.registry';
import { MergeSuggestionStrategy } from './strategies/merge-suggestion.strategy';
import { ReactionHandlerStrategy } from './strategies/reaction-handler.strategy';
import { AgentsController } from './agents.controller';
import { AuthModule } from '../auth/auth.module';
import { GraphModule } from '../graph/graph.module';
import { NotificationsModule } from '../notifications/notifications.module';
import { TasksModule } from '../tasks/tasks.module';
import { LangfuseModule } from '../langfuse/langfuse.module';

/**
 * AgentsModule
 *
 * Provides the agent system for automated background tasks.
 * Agents are configured in the database and run on cron schedules or react to events.
 *
 * Components:
 * - AgentService: CRUD operations for agents and run tracking
 * - AgentSchedulerService: Dynamic cron scheduling for 'schedule' trigger agents
 * - AgentProcessingLogService: Tracks reaction agent processing state
 * - ReactionDispatcherService: Subscribes to graph events and dispatches to reaction agents
 * - CapabilityCheckerService: Validates agent capabilities for graph operations
 * - SuggestionService: Creates suggestion tasks for 'suggest' mode agents
 * - AgentStrategyRegistry: Strategy lookup by role
 * - Strategies: Implement AgentStrategy interface (e.g., MergeSuggestionStrategy)
 */
@Module({
  imports: [
    // Register entities
    TypeOrmModule.forFeature([
      Agent,
      AgentRun,
      GraphObject,
      Task,
      AgentProcessingLog,
    ]),
    // Enable @nestjs/schedule for cron management
    ScheduleModule.forRoot(),
    // Dependencies
    AuthModule,
    GraphModule,
    NotificationsModule,
    TasksModule,
    LangfuseModule,
    // Note: EventsModule is @Global, so EventsService is available without explicit import
  ],
  controllers: [AgentsController],
  providers: [
    AgentService,
    AgentSchedulerService,
    AgentProcessingLogService,
    ReactionDispatcherService,
    CapabilityCheckerService,
    SuggestionService,
    AgentStrategyRegistry,
    // Agent strategies
    MergeSuggestionStrategy,
    ReactionHandlerStrategy,
  ],
  exports: [
    AgentService,
    AgentSchedulerService,
    AgentProcessingLogService,
    ReactionDispatcherService,
    CapabilityCheckerService,
    SuggestionService,
  ],
})
export class AgentsModule {}
