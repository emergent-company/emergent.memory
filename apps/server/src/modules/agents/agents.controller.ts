import {
  Controller,
  Get,
  Patch,
  Post,
  Delete,
  Param,
  Body,
  Query,
  UseGuards,
  ParseUUIDPipe,
  HttpCode,
  HttpStatus,
  NotFoundException,
  BadRequestException,
} from '@nestjs/common';
import {
  ApiTags,
  ApiOkResponse,
  ApiOperation,
  ApiParam,
  ApiBearerAuth,
  ApiCreatedResponse,
  ApiNoContentResponse,
  ApiHeader,
} from '@nestjs/swagger';
import { AuthGuard } from '../auth/auth.guard';
import { ScopesGuard } from '../auth/scopes.guard';
import { Scopes } from '../auth/scopes.decorator';
import { AgentService } from './agents.service';
import { AgentSchedulerService } from './agent-scheduler.service';
import { ReactionDispatcherService } from './reaction-dispatcher.service';
import { ApiStandardErrors } from '../../common/decorators/api-standard-errors';
import { CreateAgentDto, UpdateAgentDto, BatchTriggerDto } from './dto';
import {
  RequireProjectId,
  ProjectContext,
} from '../../common/decorators/project-context.decorator';

/**
 * AgentsController
 *
 * Admin API for managing agents and their schedules.
 */
@ApiTags('Agents')
@Controller('admin/agents')
@UseGuards(AuthGuard, ScopesGuard)
@ApiBearerAuth()
export class AgentsController {
  constructor(
    private readonly agentService: AgentService,
    private readonly schedulerService: AgentSchedulerService,
    private readonly reactionDispatcher: ReactionDispatcherService
  ) {}

  @Get()
  @ApiOperation({ summary: 'List all agents for the current project' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiOkResponse({ description: 'List of agent configurations' })
  @ApiStandardErrors()
  @Scopes('admin:read')
  async listAgents(@RequireProjectId() ctx: ProjectContext) {
    const agents = await this.agentService.findAll(ctx.projectId);
    return {
      success: true,
      data: agents,
    };
  }

  @Get(':id')
  @ApiOperation({ summary: 'Get agent by ID' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Agent configuration' })
  @ApiStandardErrors()
  @Scopes('admin:read')
  async getAgent(
    @Param('id', ParseUUIDPipe) id: string,
    @RequireProjectId() ctx: ProjectContext
  ) {
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      return {
        success: false,
        error: 'Agent not found',
      };
    }
    return {
      success: true,
      data: agent,
    };
  }

  @Get(':id/runs')
  @ApiOperation({ summary: 'Get recent runs for an agent' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'List of agent runs' })
  @ApiStandardErrors()
  @Scopes('admin:read')
  async getAgentRuns(
    @Param('id', ParseUUIDPipe) id: string,
    @RequireProjectId() ctx: ProjectContext
  ) {
    // Verify agent belongs to project
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }
    const runs = await this.agentService.getRecentRuns(id, 50);
    return {
      success: true,
      data: runs,
    };
  }

  @Post()
  @ApiOperation({ summary: 'Create a new agent' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiCreatedResponse({ description: 'Agent created successfully' })
  @ApiStandardErrors()
  @Scopes('admin:write')
  async createAgent(
    @Body() dto: CreateAgentDto,
    @RequireProjectId() ctx: ProjectContext
  ) {
    // Override projectId from header to ensure it matches
    dto.projectId = ctx.projectId;

    const agent = await this.agentService.create(dto);

    // Schedule the agent if it's enabled and has schedule trigger type
    if (agent.enabled && agent.triggerType === 'schedule') {
      await this.schedulerService.scheduleAgent(agent);
    }

    return {
      success: true,
      data: agent,
    };
  }

  @Patch(':id')
  @ApiOperation({ summary: 'Update agent configuration' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Updated agent configuration' })
  @ApiStandardErrors()
  @Scopes('admin:write')
  async updateAgent(
    @Param('id', ParseUUIDPipe) id: string,
    @Body() dto: UpdateAgentDto,
    @RequireProjectId() ctx: ProjectContext
  ) {
    // Verify agent belongs to project
    const existingAgent = await this.agentService.findById(id, ctx.projectId);
    if (!existingAgent) {
      return {
        success: false,
        error: 'Agent not found',
      };
    }

    const agent = await this.agentService.update(id, dto);
    if (!agent) {
      return {
        success: false,
        error: 'Agent not found',
      };
    }

    // Reload the agent schedule
    await this.schedulerService.reloadAgent(id);

    return {
      success: true,
      data: agent,
    };
  }

  @Delete(':id')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({ summary: 'Delete an agent' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Agent deleted successfully' })
  @ApiStandardErrors()
  @Scopes('admin:write')
  async deleteAgent(
    @Param('id', ParseUUIDPipe) id: string,
    @RequireProjectId() ctx: ProjectContext
  ) {
    // Verify agent belongs to project
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }

    // Unschedule the agent first
    this.schedulerService.unscheduleAgent(id);

    const deleted = await this.agentService.delete(id);
    if (!deleted) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }

    return {
      success: true,
      message: 'Agent deleted successfully',
    };
  }

  @Post(':id/trigger')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({ summary: 'Trigger an immediate run of the agent' })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Agent triggered successfully' })
  @ApiStandardErrors()
  @Scopes('admin:write')
  async triggerAgent(
    @Param('id', ParseUUIDPipe) id: string,
    @RequireProjectId() ctx: ProjectContext
  ) {
    // Verify agent belongs to project
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }

    try {
      await this.schedulerService.triggerAgent(id);
      return {
        success: true,
        message: 'Agent triggered successfully',
      };
    } catch (error) {
      return {
        success: false,
        error: (error as Error).message,
      };
    }
  }

  @Get(':id/pending-events')
  @ApiOperation({
    summary: 'Get pending events for a reaction agent',
    description:
      'Returns graph objects that match the agent reaction config but have not been processed yet',
  })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Pending events for the agent' })
  @ApiStandardErrors()
  @Scopes('admin:read')
  async getPendingEvents(
    @Param('id', ParseUUIDPipe) id: string,
    @Query('limit') limit: string | undefined,
    @RequireProjectId() ctx: ProjectContext
  ) {
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }

    if (agent.triggerType !== 'reaction') {
      throw new BadRequestException(
        'Pending events are only available for reaction agents'
      );
    }

    const parsedLimit = limit ? parseInt(limit, 10) : 100;
    const result = await this.reactionDispatcher.getPendingEvents(
      agent,
      parsedLimit
    );

    return {
      success: true,
      data: result,
    };
  }

  @Post(':id/batch-trigger')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({
    summary: 'Batch trigger a reaction agent for multiple objects',
    description:
      'Queues multiple graph objects for processing by the reaction agent',
  })
  @ApiHeader({ name: 'x-project-id', required: true })
  @ApiParam({ name: 'id', type: 'string', format: 'uuid' })
  @ApiOkResponse({ description: 'Batch trigger result' })
  @ApiStandardErrors()
  @Scopes('admin:write')
  async batchTrigger(
    @Param('id', ParseUUIDPipe) id: string,
    @Body() dto: BatchTriggerDto,
    @RequireProjectId() ctx: ProjectContext
  ) {
    const agent = await this.agentService.findById(id, ctx.projectId);
    if (!agent) {
      throw new NotFoundException(`Agent not found: ${id}`);
    }

    if (agent.triggerType !== 'reaction') {
      throw new BadRequestException(
        'Batch trigger is only available for reaction agents'
      );
    }

    if (!agent.enabled) {
      throw new BadRequestException('Cannot trigger a disabled agent');
    }

    const result = await this.reactionDispatcher.batchTrigger(
      agent,
      dto.objectIds
    );

    return {
      success: true,
      data: result,
    };
  }
}
