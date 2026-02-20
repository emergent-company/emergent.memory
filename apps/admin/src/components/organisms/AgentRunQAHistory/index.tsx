/**
 * AgentRunQAHistory Organism
 * Displays a timeline of questions asked by an agent during a run,
 * with status badges and responses.
 */
import React, { useState, useCallback } from 'react';
import { Icon } from '@/components/atoms/Icon';
import { Button } from '@/components/atoms/Button';
import { Spinner } from '@/components/atoms/Spinner';
import type { AgentQuestion } from '@/api/agents';
import { useApi } from '@/hooks/use-api';

export interface AgentRunQAHistoryProps {
  questions: AgentQuestion[];
  isLoading?: boolean;
  projectId?: string;
  onQuestionResponded?: () => void;
}

const statusConfig: Record<
  AgentQuestion['status'],
  { label: string; classes: string; icon: string }
> = {
  pending: {
    label: 'Pending',
    classes: 'badge-warning',
    icon: 'lucide--clock',
  },
  answered: {
    label: 'Answered',
    classes: 'badge-success',
    icon: 'lucide--circle-check',
  },
  expired: {
    label: 'Expired',
    classes: 'badge-ghost',
    icon: 'lucide--timer-off',
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'badge-ghost',
    icon: 'lucide--x-circle',
  },
};

const formatRelativeTime = (timestamp: string): string => {
  const date = new Date(timestamp);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
};

interface QuestionResponseFormProps {
  question: AgentQuestion;
  projectId: string;
  onResponded: () => void;
}

const QuestionResponseForm: React.FC<QuestionResponseFormProps> = ({
  question,
  projectId,
  onResponded,
}) => {
  const { fetchJson } = useApi();
  const [textResponse, setTextResponse] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState('');

  const submitResponse = useCallback(
    async (response: string) => {
      setIsSubmitting(true);
      setError('');
      try {
        await fetchJson(
          `/api/projects/${projectId}/agent-questions/${question.id}/respond`,
          { method: 'POST', body: { response } }
        );
        onResponded();
      } catch (err: unknown) {
        setError(
          err instanceof Error ? err.message : 'Failed to submit response'
        );
      } finally {
        setIsSubmitting(false);
      }
    },
    [fetchJson, projectId, question.id, onResponded]
  );

  if (isSubmitting) {
    return (
      <div className="flex items-center gap-1.5 mt-2">
        <span className="loading loading-spinner loading-xs" />
        <span className="text-xs text-base-content/60">Submitting...</span>
      </div>
    );
  }

  return (
    <div className="mt-2">
      {error && (
        <div className="flex items-center gap-1 mb-2 text-xs text-error">
          <Icon icon="lucide--circle-x" className="size-3.5" />
          {error}
        </div>
      )}
      {question.options.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {question.options.map((opt, idx) => (
            <Button
              key={idx}
              size="xs"
              color="primary"
              variant="outline"
              onClick={() => submitResponse(opt.value)}
            >
              {opt.label}
            </Button>
          ))}
        </div>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (textResponse.trim()) submitResponse(textResponse.trim());
          }}
          className="flex gap-2"
        >
          <input
            type="text"
            className="input input-bordered input-sm flex-1 text-xs"
            placeholder="Type your response..."
            value={textResponse}
            onChange={(e) => setTextResponse(e.target.value)}
          />
          <Button
            size="sm"
            color="primary"
            type="submit"
            disabled={!textResponse.trim()}
          >
            Send
          </Button>
        </form>
      )}
    </div>
  );
};

export const AgentRunQAHistory: React.FC<AgentRunQAHistoryProps> = ({
  questions,
  isLoading = false,
  projectId,
  onQuestionResponded,
}) => {
  if (isLoading) {
    return (
      <div className="flex justify-center py-6">
        <Spinner size="sm" />
      </div>
    );
  }

  if (questions.length === 0) {
    return null; // Don't render anything if no questions
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm font-medium text-base-content/70">
        <Icon icon="lucide--message-circle-question" className="size-4" />
        Questions ({questions.length})
      </div>

      <div className="space-y-2">
        {questions.map((q) => {
          const config = statusConfig[q.status];

          return (
            <div
              key={q.id}
              className="rounded-lg border border-base-300 bg-base-100 p-3"
            >
              {/* Header: status badge + timestamp */}
              <div className="flex items-center justify-between mb-2">
                <span className={`badge badge-sm gap-1 ${config.classes}`}>
                  <Icon icon={config.icon} className="size-3" />
                  {config.label}
                </span>
                <span className="text-xs text-base-content/50">
                  {formatRelativeTime(q.createdAt)}
                </span>
              </div>

              {/* Question text */}
              <p className="text-sm text-base-content mb-1">{q.question}</p>

              {/* Options (if structured choice, show what was available) */}
              {q.status === 'answered' && q.options.length > 0 && (
                <div className="flex flex-wrap gap-1 mb-1">
                  {q.options.map((opt, idx) => (
                    <span
                      key={idx}
                      className={`badge badge-xs ${
                        q.response === opt.value
                          ? 'badge-primary'
                          : 'badge-ghost'
                      }`}
                    >
                      {opt.label}
                    </span>
                  ))}
                </div>
              )}

              {/* Response (if answered) */}
              {q.status === 'answered' && q.response && (
                <div className="mt-2 rounded bg-base-200/50 px-2.5 py-1.5">
                  <div className="text-xs text-base-content/50 mb-0.5">
                    Response
                  </div>
                  <p className="text-sm text-base-content">{q.response}</p>
                </div>
              )}

              {/* Response form (if pending and we have projectId) */}
              {q.status === 'pending' && projectId && onQuestionResponded && (
                <QuestionResponseForm
                  question={q}
                  projectId={projectId}
                  onResponded={onQuestionResponded}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default AgentRunQAHistory;
