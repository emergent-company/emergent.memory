import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LangGraphService } from '../../../src/modules/chat-ui/services/langgraph.service';

// Create an async iterable for the stream mock
async function* mockAsyncIterable() {
  yield { messages: [{ content: 'Mock response' }] };
}

// Mock all external dependencies before imports are resolved
vi.mock('@langchain/langgraph/prebuilt', () => ({
  createReactAgent: vi.fn().mockReturnValue({
    stream: vi.fn().mockImplementation(() => mockAsyncIterable()),
  }),
}));

vi.mock('@langchain/google-vertexai', () => ({
  ChatVertexAI: vi.fn().mockImplementation(() => ({
    bindTools: vi.fn(),
    invoke: vi.fn().mockResolvedValue({ content: 'Mock response' }),
  })),
}));

vi.mock('@langchain/langgraph-checkpoint-postgres', () => ({
  PostgresSaver: vi.fn().mockImplementation(() => ({
    setup: vi.fn().mockResolvedValue(undefined),
  })),
}));

vi.mock('pg', () => ({
  Pool: vi.fn().mockImplementation(() => ({})),
}));

// Factory function to create mock config service with getter properties
function createMockConfigService(
  overrides: Partial<{
    vertexAiProjectId: string;
    vertexAiLocation: string;
    vertexAiModel: string;
    dbHost: string;
    dbPort: number;
    dbUser: string;
    dbPassword: string;
    dbName: string;
  }> = {}
) {
  const defaults = {
    vertexAiProjectId: 'test-project',
    vertexAiLocation: 'us-central1',
    vertexAiModel: 'gemini-pro',
    dbHost: 'localhost',
    dbPort: 5432,
    dbUser: 'test',
    dbPassword: 'test',
    dbName: 'test',
  };
  const values = { ...defaults, ...overrides };

  return {
    get vertexAiProjectId() {
      return values.vertexAiProjectId;
    },
    get vertexAiLocation() {
      return values.vertexAiLocation;
    },
    get vertexAiModel() {
      return values.vertexAiModel;
    },
    get dbHost() {
      return values.dbHost;
    },
    get dbPort() {
      return values.dbPort;
    },
    get dbUser() {
      return values.dbUser;
    },
    get dbPassword() {
      return values.dbPassword;
    },
    get dbName() {
      return values.dbName;
    },
  };
}

const mockConfigService = createMockConfigService();

describe('LangGraphService', () => {
  let service: LangGraphService;

  beforeEach(async () => {
    // Clear all mocks between tests
    vi.clearAllMocks();

    // Manually instantiate the service to avoid NestJS DI metadata issues
    // (Vitest/esbuild can strip decorator metadata, causing injection to fail)
    service = new LangGraphService(mockConfigService as any);
    await service.onModuleInit();
  });

  it('should be defined', () => {
    expect(service).toBeDefined();
  });

  it('should initialize with default agent', () => {
    expect(service.isReady()).toBe(true);
  });

  it('should stream conversation', async () => {
    const stream = await service.streamConversation({
      message: 'Hello',
      threadId: 'test-thread',
    });
    expect(stream).toBeDefined();

    // Consume the async iterable to verify it works
    const messages: any[] = [];
    for await (const chunk of stream) {
      messages.push(chunk);
    }
    expect(messages.length).toBeGreaterThan(0);
  });

  it('should return false for isReady when model is not initialized', async () => {
    // Create a new service without proper config to test the not-ready state
    const emptyConfigService = createMockConfigService({
      vertexAiProjectId: '',
      vertexAiLocation: '',
      vertexAiModel: '',
    });

    const uninitializedService = new LangGraphService(
      emptyConfigService as any
    );
    await uninitializedService.onModuleInit();

    expect(uninitializedService.isReady()).toBe(false);
  });

  it('should throw error when streaming without initialization', async () => {
    const emptyConfigService = createMockConfigService({
      vertexAiProjectId: '',
      vertexAiLocation: '',
      vertexAiModel: '',
    });

    const uninitializedService = new LangGraphService(
      emptyConfigService as any
    );
    await uninitializedService.onModuleInit();

    await expect(
      uninitializedService.streamConversation({
        message: 'Hello',
        threadId: 'test-thread',
      })
    ).rejects.toThrow('LangGraph not initialized');
  });
});
