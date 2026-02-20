/**
 * AgentQuestionNotification Organism
 * Renders an agent question notification with response controls.
 *
 * For structured choices: renders option buttons.
 * For open-ended questions: renders a text input with submit.
 * Handles loading/success/error states after responding.
 */
import React, { useState, useCallback } from 'react';
import { Button } from '@/components/atoms/Button';
import { Icon } from '@/components/atoms/Icon';
import type { Notification } from '@/types/notification';
import { useApi } from '@/hooks/use-api';

export interface AgentQuestionNotificationProps {
  notification: Notification;
  /** Called after a successful response, so parent can refetch */
  onResponded?: () => void;
  /** Compact mode for bell dropdown vs full inbox */
  compact?: boolean;
}

type ResponseStatus = 'idle' | 'loading' | 'success' | 'error';

export const AgentQuestionNotification: React.FC<
  AgentQuestionNotificationProps
> = ({ notification, onResponded, compact = false }) => {
  const { fetchJson } = useApi();
  const [status, setStatus] = useState<ResponseStatus>('idle');
  const [textResponse, setTextResponse] = useState('');
  const [errorMessage, setErrorMessage] = useState('');

  const questionId = notification.relatedResourceId;
  const projectId = notification.projectId;
  const isAnswered = notification.actionStatus === 'completed';

  // Options come from notification.actions with {label, value} shape
  const options = (notification.actions || []).filter((a) => a.value);
  const isOpenEnded = options.length === 0;

  const submitResponse = useCallback(
    async (response: string) => {
      if (!questionId || !projectId) return;
      setStatus('loading');
      setErrorMessage('');

      try {
        await fetchJson(
          `/api/projects/${projectId}/agent-questions/${questionId}/respond`,
          {
            method: 'POST',
            body: { response },
          }
        );
        setStatus('success');
        onResponded?.();
      } catch (err: unknown) {
        const message =
          err instanceof Error ? err.message : 'Failed to submit response';
        setErrorMessage(message);
        setStatus('error');
      }
    },
    [fetchJson, questionId, projectId, onResponded]
  );

  const handleOptionClick = useCallback(
    (e: React.MouseEvent, value: string) => {
      e.stopPropagation();
      submitResponse(value);
    },
    [submitResponse]
  );

  const handleTextSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (textResponse.trim()) {
        submitResponse(textResponse.trim());
      }
    },
    [textResponse, submitResponse]
  );

  // Already answered (from backend state)
  if (isAnswered) {
    return (
      <div className="flex items-center gap-1.5 mt-2">
        <Icon
          icon="lucide--circle-check"
          className="size-3.5 text-success flex-shrink-0"
        />
        <span className="text-xs text-success">Answered</span>
      </div>
    );
  }

  // Successfully responded (just now)
  if (status === 'success') {
    return (
      <div className="flex items-center gap-1.5 mt-2">
        <Icon
          icon="lucide--circle-check"
          className="size-3.5 text-success flex-shrink-0"
        />
        <span className="text-xs text-success">
          Response sent, agent resuming...
        </span>
      </div>
    );
  }

  // Error state with retry
  if (status === 'error') {
    return (
      <div className="mt-2 space-y-1.5">
        <div className="flex items-center gap-1.5">
          <Icon
            icon="lucide--circle-x"
            className="size-3.5 text-error flex-shrink-0"
          />
          <span className="text-xs text-error">{errorMessage}</span>
        </div>
        <Button
          size="xs"
          variant="outline"
          onClick={(e) => {
            e.stopPropagation();
            setStatus('idle');
            setErrorMessage('');
          }}
        >
          Try again
        </Button>
      </div>
    );
  }

  // Loading state
  if (status === 'loading') {
    return (
      <div className="flex items-center gap-1.5 mt-2">
        <span className="loading loading-spinner loading-xs" />
        <span className="text-xs text-base-content/60">Submitting...</span>
      </div>
    );
  }

  // Idle state: show response controls
  return (
    <div className="mt-2" onClick={(e) => e.stopPropagation()}>
      {/* Agent question indicator */}
      <div className="flex items-center gap-1 mb-2">
        <Icon
          icon="lucide--message-circle-question"
          className="size-3.5 text-primary flex-shrink-0"
        />
        <span className="text-xs font-medium text-primary">
          Agent needs your input
        </span>
      </div>

      {isOpenEnded ? (
        /* Open-ended: text input with submit */
        <form onSubmit={handleTextSubmit} className="flex gap-2">
          <input
            type="text"
            className={`input input-bordered input-sm flex-1 ${
              compact ? 'text-xs' : ''
            }`}
            placeholder="Type your response..."
            value={textResponse}
            onChange={(e) => setTextResponse(e.target.value)}
            autoFocus={!compact}
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
      ) : (
        /* Structured choices: option buttons */
        <div className="flex flex-wrap gap-1.5">
          {options.map((option, idx) => (
            <Button
              key={idx}
              size="xs"
              color="primary"
              variant="outline"
              onClick={(e) => handleOptionClick(e, option.value!)}
            >
              {option.label}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
};

export default AgentQuestionNotification;
