import { Injectable, Logger, OnModuleInit, Optional } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository, DataSource } from 'typeorm';
import {
  AgentStrategy,
  AgentExecutionContext,
  AgentRunResult,
} from './agent-strategy.interface';
import { AgentStrategyRegistry } from './agent-strategy.registry';
import { GraphObject } from '../../../entities/graph-object.entity';
import { LangfuseService } from '../../langfuse/langfuse.service';
import { AppConfigService } from '../../../common/config/config.service';
import { createReactionAgentTools } from './reaction-handler-tools';

// LangGraph types (lazy-loaded at runtime to avoid blocking server startup)
import type { ChatVertexAI } from '@langchain/google-vertexai';
import type { BaseMessage } from '@langchain/core/messages';

/**
 * ReactionHandlerStrategy
 *
 * Agent strategy that uses LangGraph to execute a customizable AI agent
 * in response to graph object events (created, updated, deleted).
 *
 * The agent has access to tools for searching and modifying the graph,
 * gated by the agent's configured capabilities.
 *
 * Note: LangChain/LangGraph imports are lazy-loaded at runtime to avoid
 * blocking server startup (the packages can take several seconds to load).
 */
@Injectable()
export class ReactionHandlerStrategy implements AgentStrategy, OnModuleInit {
  private readonly logger = new Logger(ReactionHandlerStrategy.name);

  readonly role = 'reaction-handler';

  // Max iterations for the LangGraph agent (prevents infinite loops)
  private readonly MAX_ITERATIONS = 10;

  // Lazy-loaded modules (cached after first use)
  private langchainModules: {
    ChatVertexAI: typeof ChatVertexAI;
    createReactAgent: typeof import('@langchain/langgraph/prebuilt').createReactAgent;
    HumanMessage: typeof import('@langchain/core/messages').HumanMessage;
    SystemMessage: typeof import('@langchain/core/messages').SystemMessage;
  } | null = null;

  constructor(
    private readonly registry: AgentStrategyRegistry,
    private readonly dataSource: DataSource,
    private readonly config: AppConfigService,
    @InjectRepository(GraphObject)
    private readonly graphObjectRepo: Repository<GraphObject>,
    @Optional() private readonly langfuseService?: LangfuseService
  ) {}

  onModuleInit(): void {
    this.registry.register(this);
    this.logger.log('ReactionHandlerStrategy registered');
  }

  /**
   * Lazy-load LangChain modules to avoid blocking server startup.
   * These modules can take several seconds to import.
   */
  private async loadLangchainModules(): Promise<typeof this.langchainModules> {
    if (this.langchainModules) {
      return this.langchainModules;
    }

    this.logger.debug('Loading LangChain modules...');
    const loadStart = Date.now();

    const [vertexAiModule, langGraphModule, messagesModule] = await Promise.all(
      [
        import('@langchain/google-vertexai'),
        import('@langchain/langgraph/prebuilt'),
        import('@langchain/core/messages'),
      ]
    );

    this.langchainModules = {
      ChatVertexAI: vertexAiModule.ChatVertexAI,
      createReactAgent: langGraphModule.createReactAgent,
      HumanMessage: messagesModule.HumanMessage,
      SystemMessage: messagesModule.SystemMessage,
    };

    this.logger.debug(
      `LangChain modules loaded in ${Date.now() - loadStart}ms`
    );
    return this.langchainModules;
  }

  /**
   * Execute the reaction handler logic
   */
  async execute(context: AgentExecutionContext): Promise<AgentRunResult> {
    const { agent, reaction, traceId } = context;

    // Validate we have a reaction context
    if (!reaction) {
      return {
        success: false,
        summary: {},
        errorMessage:
          'ReactionHandlerStrategy requires a reaction context. This agent should only be triggered by graph object events.',
      };
    }

    // Validate agent has a prompt
    if (!agent.prompt?.trim()) {
      return {
        success: false,
        summary: {},
        errorMessage:
          'Agent prompt is required for reaction-handler strategy. Please configure a prompt that describes what the agent should do.',
      };
    }

    const startTime = Date.now();

    try {
      // Lazy-load LangChain modules (avoid blocking server startup)
      const langchain = await this.loadLangchainModules();
      if (!langchain) {
        return {
          success: false,
          summary: {},
          errorMessage: 'Failed to load LangChain modules',
        };
      }

      // Create Langfuse span for execution
      const executeSpan = traceId
        ? this.langfuseService?.createSpan(traceId, 'reactionHandlerExecute', {
            agentId: agent.id,
            objectId: reaction.graphObjectId,
            objectType: reaction.objectType,
            eventType: reaction.eventType,
          }) ?? null
        : null;

      // Get the triggering object
      const triggeringObject = await this.graphObjectRepo.findOne({
        where: { id: reaction.graphObjectId },
      });

      if (!triggeringObject) {
        this.langfuseService?.endSpan(
          executeSpan,
          { error: 'Triggering object not found' },
          'error'
        );
        return {
          success: false,
          summary: { objectId: reaction.graphObjectId },
          errorMessage: `Triggering object not found: ${reaction.graphObjectId}`,
        };
      }

      // Initialize Vertex AI model
      const model = this.createModel(langchain.ChatVertexAI);
      if (!model) {
        this.langfuseService?.endSpan(
          executeSpan,
          { error: 'Vertex AI not configured' },
          'error'
        );
        return {
          success: false,
          summary: {},
          errorMessage:
            'Vertex AI not configured. Please set GCP_PROJECT_ID, VERTEX_AI_LOCATION, and VERTEX_AI_MODEL.',
        };
      }

      // Create tools based on agent capabilities
      const tools = await createReactionAgentTools({
        dataSource: this.dataSource,
        graphObjectRepo: this.graphObjectRepo,
        capabilities: agent.capabilities || {},
        actorContext: {
          actorType: 'agent',
          actorId: agent.id,
          source: 'agent_reaction',
        },
        projectId: reaction.projectId,
      });

      this.logger.debug(
        `Created ${tools.length} tools for agent: ${tools
          .map((t) => t.name)
          .join(', ')}`
      );

      // Create the React agent
      const reactAgent = langchain.createReactAgent({
        llm: model,
        tools,
      });

      // Build the conversation messages
      const messages = this.buildMessages(
        langchain,
        agent.prompt,
        triggeringObject,
        reaction
      );

      // Run the agent with iteration limit
      const result = await reactAgent.invoke(
        { messages },
        {
          configurable: { thread_id: `reaction-${context.runId}` },
          recursionLimit: this.MAX_ITERATIONS,
        }
      );

      // Extract the final response
      const finalMessages = result.messages as BaseMessage[];
      const lastMessage = finalMessages[finalMessages.length - 1];
      const agentResponse =
        typeof lastMessage?.content === 'string'
          ? lastMessage.content
          : JSON.stringify(lastMessage?.content ?? '');

      // Count tool calls made
      const toolCallCount = finalMessages.filter(
        (m) => m.constructor.name === 'ToolMessage'
      ).length;

      const durationMs = Date.now() - startTime;

      this.langfuseService?.endSpan(
        executeSpan,
        {
          success: true,
          toolCallCount,
          durationMs,
          response: agentResponse.substring(0, 500), // Truncate for logging
        },
        'success'
      );

      this.logger.log(
        `Agent "${agent.name}" completed reaction for ${reaction.objectType}:${reaction.graphObjectId} ` +
          `(${toolCallCount} tool calls, ${durationMs}ms)`
      );

      return {
        success: true,
        summary: {
          objectId: reaction.graphObjectId,
          objectType: reaction.objectType,
          eventType: reaction.eventType,
          toolCallCount,
          durationMs,
          response: agentResponse,
        },
      };
    } catch (error) {
      const err = error as Error;
      this.logger.error(
        `Agent "${agent.name}" failed: ${err.message}`,
        err.stack
      );

      return {
        success: false,
        summary: {
          objectId: reaction?.graphObjectId,
          objectType: reaction?.objectType,
          eventType: reaction?.eventType,
        },
        errorMessage: err.message,
      };
    }
  }

  /**
   * Create Vertex AI model instance
   */
  private createModel(
    ChatVertexAIClass: typeof ChatVertexAI
  ): InstanceType<typeof ChatVertexAI> | null {
    const projectId = this.config.vertexAiProjectId;
    const location = this.config.vertexAiLocation;
    const modelName = this.config.vertexAiModel;

    if (!projectId || !location || !modelName) {
      this.logger.warn(
        'Vertex AI not configured: missing GCP_PROJECT_ID, VERTEX_AI_LOCATION, or VERTEX_AI_MODEL'
      );
      return null;
    }

    return new ChatVertexAIClass({
      model: modelName,
      apiKey: '', // Empty string bypasses GOOGLE_API_KEY env var, forces ADC auth
      authOptions: {
        projectId: projectId,
      },
      location: location,
      temperature: 0.3,
      maxOutputTokens: 4096,
    });
  }

  /**
   * Build the messages array for the agent
   */
  private buildMessages(
    langchain: NonNullable<typeof this.langchainModules>,
    agentPrompt: string,
    triggeringObject: GraphObject,
    reaction: NonNullable<AgentExecutionContext['reaction']>
  ): BaseMessage[] {
    const { SystemMessage, HumanMessage } = langchain;

    // System message with agent's configured prompt
    const systemMessage = new SystemMessage(agentPrompt);

    // Human message describing the triggering event
    const eventDescription = this.buildEventDescription(
      triggeringObject,
      reaction
    );
    const humanMessage = new HumanMessage(eventDescription);

    return [systemMessage, humanMessage];
  }

  /**
   * Build a description of the triggering event for the agent
   */
  private buildEventDescription(
    triggeringObject: GraphObject,
    reaction: NonNullable<AgentExecutionContext['reaction']>
  ): string {
    const { eventType, objectType, graphObjectId } = reaction;

    let eventVerb: string;
    switch (eventType) {
      case 'created':
        eventVerb = 'was created';
        break;
      case 'updated':
        eventVerb = 'was updated';
        break;
      case 'deleted':
        eventVerb = 'was deleted';
        break;
      default:
        eventVerb = `had event "${eventType}"`;
    }

    const objectInfo = {
      id: graphObjectId,
      type: objectType,
      key: triggeringObject.key,
      properties: triggeringObject.properties,
      version: triggeringObject.version,
    };

    return `A graph object ${eventVerb}. Here are the details:

**Event Type:** ${eventType}
**Object Type:** ${objectType}
**Object ID:** ${graphObjectId}

**Object Data:**
\`\`\`json
${JSON.stringify(objectInfo, null, 2)}
\`\`\`

Please analyze this event and take appropriate action based on your instructions.`;
  }
}
