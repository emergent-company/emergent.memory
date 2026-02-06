import { IsArray, IsUUID, ArrayMinSize, ArrayMaxSize } from 'class-validator';
import { ApiProperty } from '@nestjs/swagger';

/**
 * DTO for batch triggering a reaction agent with specific graph objects
 */
export class BatchTriggerDto {
  @ApiProperty({
    description: 'Array of graph object IDs to process',
    example: ['123e4567-e89b-12d3-a456-426614174000'],
    isArray: true,
    type: String,
    minItems: 1,
    maxItems: 100,
  })
  @IsArray()
  @IsUUID('4', { each: true })
  @ArrayMinSize(1)
  @ArrayMaxSize(100)
  objectIds!: string[];
}

/**
 * Response for pending events query
 */
export interface PendingEventsResponse {
  /** Total count of objects matching the agent's filters */
  totalCount: number;
  /** Sample of unprocessed objects (limited to 100) */
  objects: {
    id: string;
    type: string;
    key: string;
    version: number;
    createdAt: string;
    updatedAt: string;
  }[];
  /** Agent's reaction config for reference */
  reactionConfig: {
    objectTypes: string[];
    events: string[];
  };
}

/**
 * Response for batch trigger
 */
export interface BatchTriggerResponse {
  /** Number of objects queued for processing */
  queued: number;
  /** Number of objects skipped (already processed or processing) */
  skipped: number;
  /** Details about skipped objects */
  skippedDetails: {
    objectId: string;
    reason: string;
  }[];
}
