import { useEffect, useState, useMemo, useCallback } from 'react';
import { useNavigate, useParams } from 'react-router';
import { Icon } from '@/components/atoms/Icon';
import { Spinner } from '@/components/atoms/Spinner';
import { PageContainer } from '@/components/organisms';
import { Modal } from '@/components/organisms/Modal/Modal';
import { useApi } from '@/hooks/use-api';
import { useToast } from '@/hooks/use-toast';
import {
  createAgentsClient,
  type Agent,
  type AgentRun,
  type AgentTriggerType,
  type AgentExecutionMode,
  type ReactionConfig,
  type ReactionEventType,
  type AgentCapabilities,
  type UpdateAgentPayload,
  type PendingEventsResponse,
  type BatchTriggerResponse,
} from '@/api/agents';

/**
 * Format a cron expression to human-readable text
 */
function formatCron(cron: string): string {
  const parts = cron.split(' ');
  if (parts.length !== 5) return cron;

  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;

  if (
    minute.startsWith('*/') &&
    hour === '*' &&
    dayOfMonth === '*' &&
    month === '*' &&
    dayOfWeek === '*'
  ) {
    const n = minute.slice(2);
    return `Every ${n} minute${n === '1' ? '' : 's'}`;
  }

  if (
    minute !== '*' &&
    hour === '*' &&
    dayOfMonth === '*' &&
    month === '*' &&
    dayOfWeek === '*'
  ) {
    return `Every hour at :${minute.padStart(2, '0')}`;
  }

  if (
    minute !== '*' &&
    hour !== '*' &&
    dayOfMonth === '*' &&
    month === '*' &&
    dayOfWeek === '*'
  ) {
    return `Daily at ${hour}:${minute.padStart(2, '0')}`;
  }

  return cron;
}

/**
 * Format duration in milliseconds to human-readable
 */
function formatDuration(ms: number | null): string {
  if (ms === null) return '-';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

/**
 * Format date to relative time
 */
function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffSec < 60) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  return `${diffDay}d ago`;
}

/**
 * Status badge component
 */
function StatusBadge({ status }: { status: AgentRun['status'] }) {
  const config = {
    running: { class: 'badge-info', icon: 'lucide--loader-2', animate: true },
    success: { class: 'badge-success', icon: 'lucide--check', animate: false },
    completed: {
      class: 'badge-success',
      icon: 'lucide--check',
      animate: false,
    },
    failed: { class: 'badge-error', icon: 'lucide--x', animate: false },
    error: { class: 'badge-error', icon: 'lucide--x', animate: false },
    skipped: {
      class: 'badge-warning',
      icon: 'lucide--skip-forward',
      animate: false,
    },
  };

  const { class: badgeClass, icon, animate } = config[status] || config.success;

  return (
    <span className={`badge badge-sm gap-1 ${badgeClass}`}>
      <Icon
        icon={icon}
        className={`w-3 h-3 ${animate ? 'animate-spin' : ''}`}
      />
      {status}
    </span>
  );
}

/**
 * Agent detail page
 */
export default function AgentDetailPage() {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const { apiBase, fetchJson } = useApi();
  const { showToast } = useToast();

  const client = useMemo(
    () => createAgentsClient(apiBase, fetchJson),
    [apiBase, fetchJson]
  );

  const [agent, setAgent] = useState<Agent | null>(null);
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [triggering, setTriggering] = useState(false);

  // Delete modal
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Run Now modal for reaction agents
  const [runNowModalOpen, setRunNowModalOpen] = useState(false);
  const [pendingEvents, setPendingEvents] =
    useState<PendingEventsResponse | null>(null);
  const [loadingPendingEvents, setLoadingPendingEvents] = useState(false);
  const [selectedObjectIds, setSelectedObjectIds] = useState<Set<string>>(
    new Set()
  );
  const [batchTriggering, setBatchTriggering] = useState(false);
  const [batchResult, setBatchResult] = useState<BatchTriggerResponse | null>(
    null
  );

  // Expanded sections
  const [configExpanded, setConfigExpanded] = useState(false);
  const [reactionExpanded, setReactionExpanded] = useState(false);

  // Load agent data
  const loadAgent = useCallback(async () => {
    if (!id) return;

    setLoading(true);
    setError(null);

    try {
      const [agentData, runsData] = await Promise.all([
        client.getAgent(id),
        client.getAgentRuns(id),
      ]);

      if (!agentData) {
        setError('Agent not found');
        return;
      }

      setAgent(agentData);
      setRuns(runsData);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load agent');
    } finally {
      setLoading(false);
    }
  }, [id, client]);

  useEffect(() => {
    loadAgent();
  }, [loadAgent]);

  // Update agent
  const updateAgent = async (payload: UpdateAgentPayload) => {
    if (!agent) return;

    setSaving(true);
    try {
      const updated = await client.updateAgent(agent.id, payload);
      setAgent(updated);
      showToast({ message: 'Agent updated', variant: 'success' });
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to update agent',
        variant: 'error',
      });
    } finally {
      setSaving(false);
    }
  };

  // Trigger agent
  const handleTrigger = async () => {
    if (!agent) return;

    setTriggering(true);
    try {
      const result = await client.triggerAgent(agent.id);
      if (result.success) {
        showToast({
          message: 'Agent triggered successfully',
          variant: 'success',
        });
        // Refresh runs after a delay
        setTimeout(async () => {
          const newRuns = await client.getAgentRuns(agent.id);
          setRuns(newRuns);
        }, 2000);
      } else {
        showToast({
          message: result.error || 'Failed to trigger agent',
          variant: 'error',
        });
      }
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to trigger agent',
        variant: 'error',
      });
    } finally {
      setTriggering(false);
    }
  };

  // Delete agent
  const handleDelete = async () => {
    if (!agent) return;

    setDeleting(true);
    try {
      await client.deleteAgent(agent.id);
      showToast({ message: 'Agent deleted', variant: 'success' });
      navigate('/admin/agents');
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to delete agent',
        variant: 'error',
      });
    } finally {
      setDeleting(false);
      setDeleteModalOpen(false);
    }
  };

  // Handle reaction config changes
  const handleReactionConfigChange = (config: ReactionConfig) => {
    updateAgent({ reactionConfig: config });
  };

  // Handle capabilities change
  const handleCapabilitiesChange = (capabilities: AgentCapabilities | null) => {
    updateAgent({ capabilities });
  };

  // Handle config key change
  const handleConfigChange = (key: string, value: any) => {
    if (!agent) return;
    const newConfig = { ...agent.config, [key]: value };
    updateAgent({ config: newConfig });
  };

  // Open Run Now modal and fetch pending events
  const handleOpenRunNowModal = async () => {
    if (!agent) return;
    setRunNowModalOpen(true);
    setLoadingPendingEvents(true);
    setBatchResult(null);
    setSelectedObjectIds(new Set());

    try {
      const events = await client.getPendingEvents(agent.id, 100);
      setPendingEvents(events);
    } catch (e) {
      showToast({
        message:
          e instanceof Error ? e.message : 'Failed to load pending events',
        variant: 'error',
      });
      setPendingEvents(null);
    } finally {
      setLoadingPendingEvents(false);
    }
  };

  // Toggle object selection
  const toggleObjectSelection = (id: string) => {
    setSelectedObjectIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // Select/deselect all objects
  const toggleSelectAll = () => {
    if (!pendingEvents) return;
    if (selectedObjectIds.size === pendingEvents.objects.length) {
      setSelectedObjectIds(new Set());
    } else {
      setSelectedObjectIds(new Set(pendingEvents.objects.map((o) => o.id)));
    }
  };

  // Handle batch trigger
  const handleBatchTrigger = async () => {
    if (!agent || selectedObjectIds.size === 0) return;

    setBatchTriggering(true);
    try {
      const result = await client.batchTrigger(
        agent.id,
        Array.from(selectedObjectIds)
      );
      setBatchResult(result);
      showToast({
        message: `Queued ${result.queued} objects for processing`,
        variant: 'success',
      });
      // Refresh pending events
      const events = await client.getPendingEvents(agent.id, 100);
      setPendingEvents(events);
      setSelectedObjectIds(new Set());
      // Refresh runs after a delay
      setTimeout(async () => {
        const newRuns = await client.getAgentRuns(agent.id);
        setRuns(newRuns);
      }, 2000);
    } catch (e) {
      showToast({
        message:
          e instanceof Error ? e.message : 'Failed to trigger batch processing',
        variant: 'error',
      });
    } finally {
      setBatchTriggering(false);
    }
  };

  if (loading) {
    return (
      <PageContainer maxWidth="4xl" testId="page-agents-detail">
        <div className="flex justify-center items-center py-20">
          <Spinner size="lg" />
        </div>
      </PageContainer>
    );
  }

  if (error || !agent) {
    return (
      <PageContainer maxWidth="4xl" testId="page-agents-detail">
        <div className="alert alert-error">
          <Icon icon="lucide--alert-circle" className="size-5" />
          <span>{error || 'Agent not found'}</span>
        </div>
        <button
          className="btn btn-ghost mt-4"
          onClick={() => navigate('/admin/agents')}
        >
          <Icon icon="lucide--arrow-left" className="size-4" />
          Back to Agents
        </button>
      </PageContainer>
    );
  }

  // Default values
  const reactionConfig: ReactionConfig = agent.reactionConfig || {
    objectTypes: [],
    events: ['created', 'updated'],
    concurrencyStrategy: 'skip',
    ignoreAgentTriggered: true,
    ignoreSelfTriggered: true,
  };

  const capabilities: AgentCapabilities = agent.capabilities || {
    canCreateObjects: true,
    canUpdateObjects: true,
    canDeleteObjects: false,
    canCreateRelationships: true,
    allowedObjectTypes: null,
  };

  return (
    <PageContainer maxWidth="4xl" testId="page-agents-detail">
      {/* Header */}
      <div className="mb-6">
        <button
          className="btn btn-ghost btn-sm mb-2"
          onClick={() => navigate('/admin/agents')}
        >
          <Icon icon="lucide--arrow-left" className="size-4" />
          Back to Agents
        </button>

        <div className="flex justify-between items-start">
          <div>
            <h1 className="flex items-center gap-3 font-bold text-2xl">
              <Icon icon="lucide--bot" className="size-6 text-primary" />
              {agent.name}
            </h1>
            <p className="mt-1 text-base-content/70">
              Strategy:{' '}
              <code className="bg-base-200 px-1 rounded">
                {agent.strategyType}
              </code>
            </p>
          </div>

          <div className="flex items-center gap-2">
            {/* Status toggle */}
            <label className="swap">
              <input
                type="checkbox"
                checked={agent.enabled}
                onChange={() => updateAgent({ enabled: !agent.enabled })}
              />
              <span
                className={`badge ${
                  agent.enabled ? 'badge-success' : 'badge-ghost'
                }`}
              >
                {agent.enabled ? 'Enabled' : 'Disabled'}
              </span>
            </label>

            {/* Actions dropdown */}
            <div className="dropdown dropdown-end">
              <div tabIndex={0} role="button" className="btn btn-ghost btn-sm">
                <Icon icon="lucide--more-vertical" className="size-4" />
              </div>
              <ul
                tabIndex={0}
                className="dropdown-content z-[1] menu p-2 shadow bg-base-200 rounded-box w-48"
              >
                <li>
                  <button
                    className="text-error"
                    onClick={() => setDeleteModalOpen(true)}
                  >
                    <Icon icon="lucide--trash-2" className="size-4" />
                    Delete Agent
                  </button>
                </li>
              </ul>
            </div>
          </div>
        </div>
      </div>

      {/* Saving indicator */}
      {saving && (
        <div className="fixed top-4 right-4 z-50">
          <div className="alert alert-info py-2 shadow-lg">
            <Icon icon="lucide--loader-2" className="size-4 animate-spin" />
            <span className="text-sm">Saving...</span>
          </div>
        </div>
      )}

      <div className="space-y-6">
        {/* Description */}
        {agent.description && (
          <div className="card bg-base-100 border border-base-300">
            <div className="card-body py-4">
              <p className="text-base-content/70">{agent.description}</p>
            </div>
          </div>
        )}

        {/* Trigger Configuration */}
        <div className="card bg-base-100 shadow-md border border-base-300">
          <div className="card-body">
            <h2 className="card-title text-lg">
              <Icon icon="lucide--settings-2" className="size-5" />
              Trigger Configuration
            </h2>

            <div className="space-y-4 mt-4">
              {/* Trigger Type */}
              <div className="flex justify-between items-center">
                <div>
                  <p className="font-medium">Trigger Mode</p>
                  <p className="text-sm text-base-content/60">
                    How the agent is triggered to run
                  </p>
                </div>
                <select
                  className="select select-bordered w-36"
                  value={agent.triggerType}
                  onChange={(e) =>
                    updateAgent({
                      triggerType: e.target.value as AgentTriggerType,
                    })
                  }
                >
                  <option value="schedule">Schedule</option>
                  <option value="manual">Manual</option>
                  <option value="reaction">Reaction</option>
                </select>
              </div>

              {/* Schedule settings */}
              {agent.triggerType === 'schedule' && (
                <div className="p-4 bg-base-200 rounded-lg space-y-3">
                  <div className="flex justify-between items-center">
                    <div>
                      <p className="font-medium text-sm">Cron Schedule</p>
                      <p className="text-xs text-base-content/60">
                        {formatCron(agent.cronSchedule)}
                      </p>
                    </div>
                    <input
                      type="text"
                      className="input input-sm input-bordered w-40 font-mono"
                      value={agent.cronSchedule}
                      onChange={(e) =>
                        updateAgent({ cronSchedule: e.target.value })
                      }
                    />
                  </div>

                  <button
                    className={`btn btn-primary btn-sm ${
                      triggering ? 'loading' : ''
                    }`}
                    onClick={handleTrigger}
                    disabled={triggering || !agent.enabled}
                  >
                    {!triggering && (
                      <Icon icon="lucide--play" className="size-4" />
                    )}
                    Run Now
                  </button>
                </div>
              )}

              {/* Manual mode */}
              {agent.triggerType === 'manual' && (
                <div className="p-4 bg-base-200 rounded-lg">
                  <p className="text-sm text-base-content/70 mb-3">
                    This agent only runs when manually triggered
                  </p>
                  <button
                    className={`btn btn-primary btn-sm ${
                      triggering ? 'loading' : ''
                    }`}
                    onClick={handleTrigger}
                    disabled={triggering || !agent.enabled}
                  >
                    {!triggering && (
                      <Icon icon="lucide--play" className="size-4" />
                    )}
                    Run Now
                  </button>
                </div>
              )}

              {/* Reaction mode */}
              {agent.triggerType === 'reaction' && (
                <div className="p-4 bg-base-200 rounded-lg space-y-4">
                  <p className="text-sm text-base-content/70">
                    Runs automatically when graph objects are modified
                  </p>

                  {/* Execution Mode */}
                  <div className="flex justify-between items-center">
                    <div>
                      <p className="font-medium text-sm">Execution Mode</p>
                      <p className="text-xs text-base-content/50">
                        How actions are applied
                      </p>
                    </div>
                    <select
                      className="select select-sm select-bordered w-28"
                      value={agent.executionMode || 'execute'}
                      onChange={(e) =>
                        updateAgent({
                          executionMode: e.target.value as AgentExecutionMode,
                        })
                      }
                    >
                      <option value="suggest">Suggest</option>
                      <option value="execute">Execute</option>
                      <option value="hybrid">Hybrid</option>
                    </select>
                  </div>

                  {/* Events */}
                  <div>
                    <p className="font-medium text-sm mb-2">Events</p>
                    <div className="flex gap-3">
                      {(
                        ['created', 'updated', 'deleted'] as ReactionEventType[]
                      ).map((event) => (
                        <label
                          key={event}
                          className="flex items-center gap-2 cursor-pointer"
                        >
                          <input
                            type="checkbox"
                            className="checkbox checkbox-sm"
                            checked={reactionConfig.events.includes(event)}
                            onChange={(e) => {
                              const newEvents = e.target.checked
                                ? [...reactionConfig.events, event]
                                : reactionConfig.events.filter(
                                    (ev) => ev !== event
                                  );
                              handleReactionConfigChange({
                                ...reactionConfig,
                                events: newEvents,
                              });
                            }}
                          />
                          <span className="text-sm capitalize">{event}</span>
                        </label>
                      ))}
                    </div>
                  </div>

                  {/* Object Types */}
                  <div>
                    <p className="font-medium text-sm mb-1">Object Types</p>
                    <input
                      type="text"
                      className="input input-sm input-bordered w-full"
                      placeholder="All types (leave empty) or comma-separated"
                      value={reactionConfig.objectTypes.join(', ')}
                      onChange={(e) => {
                        const types = e.target.value
                          .split(',')
                          .map((t) => t.trim())
                          .filter(Boolean);
                        handleReactionConfigChange({
                          ...reactionConfig,
                          objectTypes: types,
                        });
                      }}
                    />
                  </div>

                  {/* Advanced */}
                  <button
                    className="flex items-center gap-1 text-sm text-base-content/60 hover:text-base-content"
                    onClick={() => setReactionExpanded(!reactionExpanded)}
                  >
                    <Icon
                      icon={
                        reactionExpanded
                          ? 'lucide--chevron-down'
                          : 'lucide--chevron-right'
                      }
                      className="size-4"
                    />
                    Advanced Settings
                  </button>

                  {reactionExpanded && (
                    <div className="space-y-3 pl-4 border-l-2 border-base-300">
                      <div className="flex justify-between items-center">
                        <span className="text-sm">Concurrency Strategy</span>
                        <select
                          className="select select-xs select-bordered w-28"
                          value={reactionConfig.concurrencyStrategy}
                          onChange={(e) =>
                            handleReactionConfigChange({
                              ...reactionConfig,
                              concurrencyStrategy: e.target.value as
                                | 'skip'
                                | 'parallel',
                            })
                          }
                        >
                          <option value="skip">Skip</option>
                          <option value="parallel">Parallel</option>
                        </select>
                      </div>

                      <label className="flex justify-between items-center cursor-pointer">
                        <span className="text-sm">Ignore agent-triggered</span>
                        <input
                          type="checkbox"
                          className="toggle toggle-sm"
                          checked={reactionConfig.ignoreAgentTriggered}
                          onChange={(e) =>
                            handleReactionConfigChange({
                              ...reactionConfig,
                              ignoreAgentTriggered: e.target.checked,
                            })
                          }
                        />
                      </label>

                      <label className="flex justify-between items-center cursor-pointer">
                        <span className="text-sm">Ignore self-triggered</span>
                        <input
                          type="checkbox"
                          className="toggle toggle-sm"
                          checked={reactionConfig.ignoreSelfTriggered}
                          onChange={(e) =>
                            handleReactionConfigChange({
                              ...reactionConfig,
                              ignoreSelfTriggered: e.target.checked,
                            })
                          }
                        />
                      </label>

                      {/* Capabilities */}
                      <div className="pt-3 border-t border-base-300">
                        <p className="font-medium text-sm mb-2">Capabilities</p>
                        <div className="space-y-2">
                          {[
                            {
                              key: 'canCreateObjects',
                              label: 'Create objects',
                            },
                            {
                              key: 'canUpdateObjects',
                              label: 'Update objects',
                            },
                            {
                              key: 'canDeleteObjects',
                              label: 'Delete objects',
                            },
                            {
                              key: 'canCreateRelationships',
                              label: 'Create relationships',
                            },
                          ].map(({ key, label }) => (
                            <label
                              key={key}
                              className="flex justify-between items-center cursor-pointer"
                            >
                              <span className="text-sm">{label}</span>
                              <input
                                type="checkbox"
                                className="checkbox checkbox-sm"
                                checked={
                                  (capabilities[
                                    key as keyof AgentCapabilities
                                  ] as boolean | undefined) ?? false
                                }
                                onChange={(e) =>
                                  handleCapabilitiesChange({
                                    ...capabilities,
                                    [key]: e.target.checked,
                                  })
                                }
                              />
                            </label>
                          ))}
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Run Now button for reaction agents */}
                  <div className="pt-2 border-t border-base-300">
                    <button
                      className="btn btn-primary btn-sm"
                      onClick={handleOpenRunNowModal}
                      disabled={!agent.enabled}
                    >
                      <Icon icon="lucide--play" className="size-4" />
                      Run Now
                    </button>
                    <p className="text-xs text-base-content/50 mt-1">
                      Manually trigger processing for unprocessed objects
                    </p>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Agent Config */}
        {Object.keys(agent.config || {}).length > 0 && (
          <div className="card bg-base-100 shadow-md border border-base-300">
            <div className="card-body">
              <button
                className="flex items-center gap-2 font-semibold text-lg"
                onClick={() => setConfigExpanded(!configExpanded)}
              >
                <Icon
                  icon={
                    configExpanded
                      ? 'lucide--chevron-down'
                      : 'lucide--chevron-right'
                  }
                  className="size-5"
                />
                Agent Configuration
              </button>

              {configExpanded && (
                <div className="space-y-3 mt-4">
                  {Object.entries(agent.config || {}).map(([key, value]) => (
                    <div
                      key={key}
                      className="flex justify-between items-center"
                    >
                      <label className="text-sm text-base-content/70">
                        {key}
                      </label>
                      <input
                        type={typeof value === 'number' ? 'number' : 'text'}
                        className="input input-sm input-bordered w-40"
                        value={value}
                        onChange={(e) =>
                          handleConfigChange(
                            key,
                            typeof value === 'number'
                              ? parseFloat(e.target.value)
                              : e.target.value
                          )
                        }
                      />
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Recent Runs */}
        <div className="card bg-base-100 shadow-md border border-base-300">
          <div className="card-body">
            <div className="flex justify-between items-center">
              <h2 className="card-title text-lg">
                <Icon icon="lucide--history" className="size-5" />
                Recent Runs
              </h2>
              <button
                className="btn btn-ghost btn-sm"
                onClick={async () => {
                  const newRuns = await client.getAgentRuns(agent.id);
                  setRuns(newRuns);
                }}
              >
                <Icon icon="lucide--refresh-cw" className="size-4" />
                Refresh
              </button>
            </div>

            {runs.length === 0 ? (
              <div className="py-8 text-center text-base-content/50">
                <Icon
                  icon="lucide--clock"
                  className="size-8 mx-auto mb-2 opacity-50"
                />
                <p>No runs yet</p>
              </div>
            ) : (
              <div className="overflow-x-auto mt-4">
                <table className="table table-sm">
                  <thead>
                    <tr>
                      <th>Status</th>
                      <th>Started</th>
                      <th>Duration</th>
                      <th>Summary</th>
                    </tr>
                  </thead>
                  <tbody>
                    {runs.slice(0, 20).map((run) => (
                      <tr key={run.id}>
                        <td>
                          <StatusBadge status={run.status} />
                        </td>
                        <td className="text-base-content/70">
                          {formatRelativeTime(run.startedAt)}
                        </td>
                        <td className="text-base-content/70">
                          {formatDuration(run.durationMs)}
                        </td>
                        <td className="max-w-xs text-base-content/70 truncate">
                          {run.status === 'skipped' && run.skipReason}
                          {(run.status === 'failed' ||
                            run.status === 'error') && (
                            <span className="text-error">
                              {run.errorMessage}
                            </span>
                          )}
                          {(run.status === 'completed' ||
                            run.status === 'success') &&
                            run.summary && (
                              <span>
                                {Object.entries(run.summary)
                                  .filter(
                                    ([, v]) => v !== null && v !== undefined
                                  )
                                  .map(([k, v]) => `${k}: ${v}`)
                                  .join(' | ')}
                              </span>
                            )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Metadata */}
        <div className="card bg-base-100 border border-base-300">
          <div className="card-body py-4">
            <div className="flex gap-6 text-sm text-base-content/60">
              <div>
                <span className="font-medium">Created:</span>{' '}
                {new Date(agent.createdAt).toLocaleString()}
              </div>
              <div>
                <span className="font-medium">Updated:</span>{' '}
                {new Date(agent.updatedAt).toLocaleString()}
              </div>
              <div>
                <span className="font-medium">ID:</span>{' '}
                <code className="bg-base-200 px-1 rounded text-xs">
                  {agent.id}
                </code>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Delete Modal */}
      <Modal
        open={deleteModalOpen}
        onOpenChange={(open) => !open && setDeleteModalOpen(false)}
        title="Delete Agent"
        sizeClassName="max-w-md"
        actions={[
          {
            label: 'Cancel',
            variant: 'ghost',
            onClick: () => setDeleteModalOpen(false),
          },
          {
            label: deleting ? 'Deleting...' : 'Delete',
            variant: 'error' as const,
            onClick: handleDelete,
            disabled: deleting,
          },
        ]}
      >
        <div className="space-y-4">
          <p>
            Are you sure you want to delete{' '}
            <strong>&quot;{agent.name}&quot;</strong>?
          </p>
          <div className="alert alert-warning">
            <Icon icon="lucide--alert-triangle" className="size-5" />
            <span>This action cannot be undone.</span>
          </div>
        </div>
      </Modal>

      {/* Run Now Modal for Reaction Agents */}
      <Modal
        open={runNowModalOpen}
        onOpenChange={(open) => !open && setRunNowModalOpen(false)}
        title="Run Agent on Unprocessed Objects"
        sizeClassName="max-w-3xl"
        actions={[
          {
            label: 'Close',
            variant: 'ghost',
            onClick: () => setRunNowModalOpen(false),
          },
          {
            label: batchTriggering
              ? 'Processing...'
              : `Process ${selectedObjectIds.size} Selected`,
            variant: 'primary' as const,
            onClick: handleBatchTrigger,
            disabled: batchTriggering || selectedObjectIds.size === 0,
          },
        ]}
      >
        <div className="space-y-4">
          {loadingPendingEvents ? (
            <div className="flex justify-center py-8">
              <Spinner size="lg" />
            </div>
          ) : pendingEvents === null ? (
            <div className="alert alert-error">
              <Icon icon="lucide--alert-circle" className="size-5" />
              <span>Failed to load pending events</span>
            </div>
          ) : (
            <>
              {/* Summary */}
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-lg font-semibold">
                    {pendingEvents.totalCount} unprocessed objects
                  </p>
                  <p className="text-sm text-base-content/60">
                    {pendingEvents.reactionConfig.objectTypes.length > 0
                      ? `Types: ${pendingEvents.reactionConfig.objectTypes.join(
                          ', '
                        )}`
                      : 'All object types'}
                  </p>
                </div>
                {pendingEvents.objects.length > 0 && (
                  <button
                    className="btn btn-ghost btn-sm"
                    onClick={toggleSelectAll}
                  >
                    {selectedObjectIds.size === pendingEvents.objects.length
                      ? 'Deselect All'
                      : 'Select All'}
                  </button>
                )}
              </div>

              {/* Batch Result */}
              {batchResult && (
                <div className="alert alert-info">
                  <Icon icon="lucide--info" className="size-5" />
                  <div>
                    <p className="font-medium">
                      Queued: {batchResult.queued} | Skipped:{' '}
                      {batchResult.skipped}
                    </p>
                    {batchResult.skippedDetails.length > 0 && (
                      <details className="mt-1">
                        <summary className="cursor-pointer text-sm">
                          View skipped details
                        </summary>
                        <ul className="text-xs mt-1 list-disc list-inside">
                          {batchResult.skippedDetails.map((d, i) => (
                            <li key={i}>
                              {d.objectId.slice(0, 8)}...: {d.reason}
                            </li>
                          ))}
                        </ul>
                      </details>
                    )}
                  </div>
                </div>
              )}

              {/* Objects Table */}
              {pendingEvents.objects.length === 0 ? (
                <div className="py-8 text-center text-base-content/50">
                  <Icon
                    icon="lucide--check-circle"
                    className="size-8 mx-auto mb-2 opacity-50"
                  />
                  <p>All objects have been processed</p>
                </div>
              ) : (
                <div className="overflow-x-auto max-h-96">
                  <table className="table table-sm table-pin-rows">
                    <thead>
                      <tr>
                        <th className="w-8">
                          <input
                            type="checkbox"
                            className="checkbox checkbox-sm"
                            checked={
                              selectedObjectIds.size ===
                              pendingEvents.objects.length
                            }
                            onChange={toggleSelectAll}
                          />
                        </th>
                        <th>Type</th>
                        <th>Key</th>
                        <th>Version</th>
                        <th>Created</th>
                      </tr>
                    </thead>
                    <tbody>
                      {pendingEvents.objects.map((obj) => (
                        <tr
                          key={obj.id}
                          className={
                            selectedObjectIds.has(obj.id) ? 'bg-base-200' : ''
                          }
                        >
                          <td>
                            <input
                              type="checkbox"
                              className="checkbox checkbox-sm"
                              checked={selectedObjectIds.has(obj.id)}
                              onChange={() => toggleObjectSelection(obj.id)}
                            />
                          </td>
                          <td>
                            <span className="badge badge-sm badge-ghost">
                              {obj.type}
                            </span>
                          </td>
                          <td className="max-w-xs truncate font-mono text-xs">
                            {obj.key}
                          </td>
                          <td className="text-base-content/70">
                            v{obj.version}
                          </td>
                          <td className="text-base-content/70 text-xs">
                            {formatRelativeTime(obj.createdAt)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {pendingEvents.totalCount > pendingEvents.objects.length && (
                <p className="text-xs text-base-content/50 text-center">
                  Showing {pendingEvents.objects.length} of{' '}
                  {pendingEvents.totalCount} objects
                </p>
              )}
            </>
          )}
        </div>
      </Modal>
    </PageContainer>
  );
}
