import {
  IsString,
  IsBoolean,
  IsOptional,
  IsObject,
  IsEnum,
  IsArray,
  ValidateNested,
  IsUUID,
  ValidateIf,
  IsNotEmpty,
} from 'class-validator';
import { ApiProperty, ApiPropertyOptional } from '@nestjs/swagger';
import { Type } from 'class-transformer';

/**
 * Trigger type for agent execution
 */
export enum AgentTriggerTypeEnum {
  SCHEDULE = 'schedule',
  MANUAL = 'manual',
  REACTION = 'reaction',
}

/**
 * Execution mode for agents
 */
export enum AgentExecutionModeEnum {
  SUGGEST = 'suggest',
  EXECUTE = 'execute',
  HYBRID = 'hybrid',
}

/**
 * Events that can trigger a reaction agent
 */
export enum ReactionEventTypeEnum {
  CREATED = 'created',
  UPDATED = 'updated',
  DELETED = 'deleted',
}

/**
 * Concurrency strategy for reaction agents
 */
export enum ConcurrencyStrategyEnum {
  SKIP = 'skip',
  PARALLEL = 'parallel',
}

/**
 * DTO for reaction trigger configuration
 */
export class ReactionConfigDto {
  @ApiProperty({
    description: 'Object types to react to (empty array = all types)',
    example: ['Person', 'Company'],
    isArray: true,
    type: String,
  })
  @IsArray()
  @IsString({ each: true })
  objectTypes!: string[];

  @ApiProperty({
    description: 'Events to react to',
    enum: ReactionEventTypeEnum,
    isArray: true,
    example: ['created', 'updated'],
  })
  @IsArray()
  @IsEnum(ReactionEventTypeEnum, { each: true })
  events!: ReactionEventTypeEnum[];

  @ApiProperty({
    description: 'How to handle concurrent events for the same object',
    enum: ConcurrencyStrategyEnum,
    default: ConcurrencyStrategyEnum.SKIP,
  })
  @IsEnum(ConcurrencyStrategyEnum)
  concurrencyStrategy!: ConcurrencyStrategyEnum;

  @ApiProperty({
    description: 'Ignore events triggered by any agent',
    default: true,
  })
  @IsBoolean()
  ignoreAgentTriggered!: boolean;

  @ApiProperty({
    description: 'Ignore events triggered by this agent',
    default: true,
  })
  @IsBoolean()
  ignoreSelfTriggered!: boolean;
}

/**
 * DTO for agent capabilities
 */
export class AgentCapabilitiesDto {
  @ApiPropertyOptional({
    description: 'Can the agent create new objects',
    default: true,
  })
  @IsBoolean()
  @IsOptional()
  canCreateObjects?: boolean;

  @ApiPropertyOptional({
    description: 'Can the agent update existing objects',
    default: true,
  })
  @IsBoolean()
  @IsOptional()
  canUpdateObjects?: boolean;

  @ApiPropertyOptional({
    description: 'Can the agent delete objects',
    default: false,
  })
  @IsBoolean()
  @IsOptional()
  canDeleteObjects?: boolean;

  @ApiPropertyOptional({
    description: 'Can the agent create relationships',
    default: true,
  })
  @IsBoolean()
  @IsOptional()
  canCreateRelationships?: boolean;

  @ApiPropertyOptional({
    description: 'Restrict to specific object types (null = all types allowed)',
    isArray: true,
    type: String,
  })
  @IsArray()
  @IsString({ each: true })
  @IsOptional()
  allowedObjectTypes?: string[] | null;
}

/**
 * DTO for creating a new agent
 */
export class CreateAgentDto {
  @ApiProperty({
    description: 'Project ID the agent belongs to',
    example: '123e4567-e89b-12d3-a456-426614174000',
  })
  @IsUUID()
  projectId!: string;

  @ApiProperty({
    description: 'Human-readable name for the agent',
    example: 'My Merge Suggestion Agent',
  })
  @IsString()
  name!: string;

  @ApiProperty({
    description:
      'Strategy type identifier (determines which strategy implementation to use)',
    example: 'merge-suggestion',
  })
  @IsString()
  strategyType!: string;

  @ApiPropertyOptional({
    description:
      'Admin-tunable prompt/configuration for the agent (required for reaction-handler)',
    example: 'Focus on finding duplicate Person entities...',
  })
  @ValidateIf((o) => o.strategyType === 'reaction-handler')
  @IsNotEmpty({ message: 'Prompt is required for reaction-handler agents' })
  @IsString()
  @IsOptional()
  prompt?: string;

  @ApiProperty({
    description:
      'Cron expression for scheduling (required even for non-scheduled agents)',
    example: '*/5 * * * *',
    default: '0 * * * *',
  })
  @IsString()
  cronSchedule!: string;

  @ApiPropertyOptional({
    description: 'Whether the agent is enabled',
    default: true,
  })
  @IsBoolean()
  @IsOptional()
  enabled?: boolean;

  @ApiPropertyOptional({
    description: 'How the agent is triggered',
    enum: AgentTriggerTypeEnum,
    default: AgentTriggerTypeEnum.SCHEDULE,
  })
  @IsEnum(AgentTriggerTypeEnum)
  @IsOptional()
  triggerType?: AgentTriggerTypeEnum;

  @ApiPropertyOptional({
    description:
      'Configuration for reaction triggers (required when triggerType is reaction)',
    type: ReactionConfigDto,
  })
  @ValidateNested()
  @Type(() => ReactionConfigDto)
  @IsOptional()
  reactionConfig?: ReactionConfigDto;

  @ApiPropertyOptional({
    description: 'How the agent executes its actions',
    enum: AgentExecutionModeEnum,
    default: AgentExecutionModeEnum.EXECUTE,
  })
  @IsEnum(AgentExecutionModeEnum)
  @IsOptional()
  executionMode?: AgentExecutionModeEnum;

  @ApiPropertyOptional({
    description: 'Capability restrictions for the agent',
    type: AgentCapabilitiesDto,
  })
  @ValidateNested()
  @Type(() => AgentCapabilitiesDto)
  @IsOptional()
  capabilities?: AgentCapabilitiesDto;

  @ApiPropertyOptional({
    description: 'Agent-specific configuration as JSON',
    example: { similarityThreshold: 0.1, maxPendingNotifications: 5 },
  })
  @IsObject()
  @IsOptional()
  config?: Record<string, any>;

  @ApiPropertyOptional({
    description: 'Optional description of what the agent does',
    example: 'Identifies potential duplicate entities and creates merge tasks',
  })
  @IsString()
  @IsOptional()
  description?: string;
}

/**
 * DTO for updating an existing agent (all fields optional)
 */
export class UpdateAgentDto {
  @ApiPropertyOptional({
    description: 'Human-readable name for the agent',
    example: 'My Merge Suggestion Agent',
  })
  @IsString()
  @IsOptional()
  name?: string;

  @ApiPropertyOptional({
    description: 'Admin-tunable prompt/configuration for the agent',
    example: 'Focus on finding duplicate Person entities...',
  })
  @IsString()
  @IsOptional()
  prompt?: string;

  @ApiPropertyOptional({
    description: 'Cron expression for scheduling',
    example: '*/5 * * * *',
  })
  @IsString()
  @IsOptional()
  cronSchedule?: string;

  @ApiPropertyOptional({
    description: 'Whether the agent is enabled',
  })
  @IsBoolean()
  @IsOptional()
  enabled?: boolean;

  @ApiPropertyOptional({
    description: 'How the agent is triggered',
    enum: AgentTriggerTypeEnum,
  })
  @IsEnum(AgentTriggerTypeEnum)
  @IsOptional()
  triggerType?: AgentTriggerTypeEnum;

  @ApiPropertyOptional({
    description: 'Configuration for reaction triggers',
    type: ReactionConfigDto,
  })
  @ValidateNested()
  @Type(() => ReactionConfigDto)
  @IsOptional()
  reactionConfig?: ReactionConfigDto;

  @ApiPropertyOptional({
    description: 'How the agent executes its actions',
    enum: AgentExecutionModeEnum,
  })
  @IsEnum(AgentExecutionModeEnum)
  @IsOptional()
  executionMode?: AgentExecutionModeEnum;

  @ApiPropertyOptional({
    description: 'Capability restrictions for the agent',
    type: AgentCapabilitiesDto,
  })
  @ValidateNested()
  @Type(() => AgentCapabilitiesDto)
  @IsOptional()
  capabilities?: AgentCapabilitiesDto;

  @ApiPropertyOptional({
    description: 'Agent-specific configuration as JSON',
    example: { similarityThreshold: 0.1, maxPendingNotifications: 5 },
  })
  @IsObject()
  @IsOptional()
  config?: Record<string, any>;

  @ApiPropertyOptional({
    description: 'Optional description of what the agent does',
  })
  @IsString()
  @IsOptional()
  description?: string;
}
