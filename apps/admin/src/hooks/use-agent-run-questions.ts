/**
 * useAgentRunQuestions hook
 * Fetches questions for a specific agent run via the agents API client.
 */
import { useState, useEffect, useCallback, useMemo } from 'react';
import { useApi } from '@/hooks/use-api';
import { createAgentsClient, type AgentQuestion } from '@/api/agents';

export interface UseAgentRunQuestionsReturn {
  questions: AgentQuestion[];
  isLoading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

export function useAgentRunQuestions(
  projectId: string | undefined,
  runId: string | undefined
): UseAgentRunQuestionsReturn {
  const { apiBase, fetchJson } = useApi();
  const client = useMemo(
    () => createAgentsClient(apiBase, fetchJson),
    [apiBase, fetchJson]
  );

  const [questions, setQuestions] = useState<AgentQuestion[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const refetch = useCallback(async () => {
    if (!projectId || !runId) return;

    try {
      setIsLoading(true);
      setError(null);
      const data = await client.getRunQuestions(projectId, runId);
      setQuestions(data);
    } catch (err) {
      setError(err as Error);
    } finally {
      setIsLoading(false);
    }
  }, [client, projectId, runId]);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { questions, isLoading, error, refetch };
}
