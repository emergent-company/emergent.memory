import { useEffect, useState, useMemo } from 'react';
import { useNavigate } from 'react-router';
import { Icon } from '@/components/atoms/Icon';
import { Spinner } from '@/components/atoms/Spinner';
import { PageContainer } from '@/components/organisms';
import { Modal } from '@/components/organisms/Modal/Modal';
import { useApi } from '@/hooks/use-api';
import { useToast } from '@/hooks/use-toast';
import { createAgentsClient, type Agent, type AgentRun } from '@/api/agents';

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
    return `Every ${n}m`;
  }

  if (
    minute !== '*' &&
    hour === '*' &&
    dayOfMonth === '*' &&
    month === '*' &&
    dayOfWeek === '*'
  ) {
    return `Hourly`;
  }

  if (
    minute !== '*' &&
    hour !== '*' &&
    dayOfMonth === '*' &&
    month === '*' &&
    dayOfWeek === '*'
  ) {
    return `Daily`;
  }

  return cron;
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

const STATUS_CONFIG = {
  enabled: { label: 'Enabled', color: 'success', icon: 'lucide--check-circle' },
  disabled: { label: 'Disabled', color: 'ghost', icon: 'lucide--pause-circle' },
} as const;

const TRIGGER_ICONS: Record<string, string> = {
  schedule: 'lucide--clock',
  manual: 'lucide--hand',
  reaction: 'lucide--zap',
};

/**
 * Agents management page - table-based list
 */
export default function AgentsPage() {
  const navigate = useNavigate();
  const { apiBase, fetchJson } = useApi();
  const { showToast } = useToast();

  const [agents, setAgents] = useState<Agent[]>([]);
  const [runsMap, setRunsMap] = useState<Record<string, AgentRun[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  // Trigger state
  const [triggering, setTriggering] = useState<Set<string>>(new Set());

  // Delete modal state
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [agentToDelete, setAgentToDelete] = useState<Agent | null>(null);
  const [deleting, setDeleting] = useState(false);

  const client = useMemo(
    () => createAgentsClient(apiBase, fetchJson),
    [apiBase, fetchJson]
  );

  // Load agents and their latest run
  const loadData = async () => {
    setLoading(true);
    setError(null);

    try {
      const agentsList = await client.listAgents();
      setAgents(agentsList);

      // Load runs for each agent (in parallel)
      const runsPromises = agentsList.map(async (agent) => {
        try {
          const runs = await client.getAgentRuns(agent.id);
          return { id: agent.id, runs };
        } catch {
          return { id: agent.id, runs: [] };
        }
      });

      const runsResults = await Promise.all(runsPromises);
      const newRunsMap: Record<string, AgentRun[]> = {};
      runsResults.forEach(({ id, runs }) => {
        newRunsMap[id] = runs;
      });
      setRunsMap(newRunsMap);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load agents');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client]);

  // Handle trigger
  const handleTrigger = async (agent: Agent) => {
    setTriggering((prev) => new Set(prev).add(agent.id));

    try {
      const result = await client.triggerAgent(agent.id);
      if (result.success) {
        showToast({
          message: `Agent "${agent.name}" triggered`,
          variant: 'success',
        });
        // Refresh runs after a short delay
        setTimeout(async () => {
          const runs = await client.getAgentRuns(agent.id);
          setRunsMap((prev) => ({ ...prev, [agent.id]: runs }));
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
      setTriggering((prev) => {
        const next = new Set(prev);
        next.delete(agent.id);
        return next;
      });
    }
  };

  // Handle toggle enabled
  const handleToggle = async (agent: Agent) => {
    try {
      const updated = await client.updateAgent(agent.id, {
        enabled: !agent.enabled,
      });
      setAgents((prev) => prev.map((a) => (a.id === agent.id ? updated : a)));
      showToast({
        message: `Agent ${updated.enabled ? 'enabled' : 'disabled'}`,
        variant: 'success',
      });
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to toggle agent',
        variant: 'error',
      });
    }
  };

  // Handle delete
  const handleDelete = (agent: Agent) => {
    setAgentToDelete(agent);
    setDeleteModalOpen(true);
  };

  const confirmDelete = async () => {
    if (!agentToDelete) return;

    setDeleting(true);
    try {
      await client.deleteAgent(agentToDelete.id);
      showToast({
        message: `"${agentToDelete.name}" deleted successfully`,
        variant: 'success',
      });
      setDeleteModalOpen(false);
      setAgentToDelete(null);
      await loadData();
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Delete failed',
        variant: 'error',
      });
    } finally {
      setDeleting(false);
    }
  };

  // Filter agents by search query
  const filteredAgents = useMemo(() => {
    if (!searchQuery.trim()) return agents;

    const q = searchQuery.toLowerCase();
    return agents.filter(
      (a) =>
        a.name.toLowerCase().includes(q) ||
        a.strategyType.toLowerCase().includes(q) ||
        (a.description && a.description.toLowerCase().includes(q))
    );
  }, [agents, searchQuery]);

  // Render status badge
  const renderStatus = (agent: Agent) => {
    const status = agent.enabled ? 'enabled' : 'disabled';
    const config = STATUS_CONFIG[status];
    return (
      <div className="flex items-center gap-2">
        <Icon icon={config.icon} className={`size-4 text-${config.color}`} />
        <span className={`badge badge-${config.color} badge-sm`}>
          {config.label}
        </span>
      </div>
    );
  };

  // Get last run info
  const getLastRunInfo = (agentId: string) => {
    const runs = runsMap[agentId] || [];
    if (runs.length === 0) return null;
    return runs[0];
  };

  return (
    <PageContainer maxWidth="full" className="px-4" testId="page-agents">
      {/* Header */}
      <div className="flex justify-between items-start mb-6">
        <div>
          <h1 className="font-bold text-2xl inline-flex items-center gap-2">
            <Icon icon="lucide--bot" className="size-6" />
            Agents
            {!loading && (
              <span className="badge badge-ghost badge-lg font-normal">
                {agents.length}
              </span>
            )}
          </h1>
          <p className="mt-1 text-base-content/70">
            Manage automated background tasks that run on a schedule or react to
            events
          </p>
        </div>
        <button
          className="btn btn-primary"
          onClick={() => navigate('/admin/agents/new')}
        >
          <Icon icon="lucide--plus" className="size-4" />
          Add Agent
        </button>
      </div>

      {/* Error state */}
      {error && (
        <div className="alert alert-error mb-4">
          <Icon icon="lucide--alert-circle" className="size-5" />
          <span>{error}</span>
          <button className="btn btn-sm btn-ghost" onClick={loadData}>
            Retry
          </button>
        </div>
      )}

      {/* Search */}
      <div className="mb-4">
        <div className="relative w-72">
          <Icon
            icon="lucide--search"
            className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-base-content/50"
          />
          <input
            type="text"
            placeholder="Search agents..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="input input-bordered input-sm w-full pl-9"
          />
          {searchQuery && (
            <button
              className="absolute right-2 top-1/2 -translate-y-1/2 btn btn-ghost btn-xs"
              onClick={() => setSearchQuery('')}
            >
              <Icon icon="lucide--x" className="size-3" />
            </button>
          )}
        </div>
      </div>

      {/* Loading State */}
      {loading && (
        <div className="flex justify-center items-center py-12">
          <Spinner size="lg" />
        </div>
      )}

      {/* Table */}
      {!loading && !error && (
        <div className="overflow-x-auto">
          <table className="table table-zebra">
            <thead>
              <tr>
                <th className="w-64">Agent</th>
                <th className="w-28">Status</th>
                <th className="w-28">Trigger</th>
                <th className="w-32">Schedule</th>
                <th className="w-40">Last Run</th>
                <th className="w-28">Created</th>
                <th className="w-28">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filteredAgents.length === 0 && (
                <tr>
                  <td
                    colSpan={7}
                    className="text-center py-12 text-base-content/70"
                  >
                    <Icon
                      icon="lucide--bot"
                      className="size-10 mx-auto mb-3 opacity-30"
                    />
                    <div>
                      {searchQuery ? 'No matching agents' : 'No agents yet'}
                    </div>
                    {!searchQuery && (
                      <button
                        className="btn btn-primary btn-sm mt-4"
                        onClick={() => navigate('/admin/agents/new')}
                      >
                        <Icon icon="lucide--plus" className="size-4" />
                        Create your first agent
                      </button>
                    )}
                  </td>
                </tr>
              )}
              {filteredAgents.map((agent) => {
                const lastRun = getLastRunInfo(agent.id);
                const isTriggering = triggering.has(agent.id);

                return (
                  <tr key={agent.id} className="hover">
                    <td>
                      <div
                        className="flex items-center gap-3 cursor-pointer"
                        onClick={() => navigate(`/admin/agents/${agent.id}`)}
                      >
                        <div className="p-2 rounded-lg bg-base-200">
                          <Icon icon="lucide--bot" className="size-5" />
                        </div>
                        <div>
                          <div className="font-medium">{agent.name}</div>
                          <div className="text-sm text-base-content/60">
                            <code className="text-xs">
                              {agent.strategyType}
                            </code>
                          </div>
                        </div>
                      </div>
                    </td>
                    <td>{renderStatus(agent)}</td>
                    <td>
                      <div className="flex items-center gap-2">
                        <Icon
                          icon={
                            TRIGGER_ICONS[agent.triggerType] || 'lucide--circle'
                          }
                          className="size-4 text-base-content/60"
                        />
                        <span className="text-sm capitalize">
                          {agent.triggerType}
                        </span>
                      </div>
                    </td>
                    <td>
                      {agent.triggerType === 'schedule' ? (
                        <span
                          className="text-sm text-base-content/70"
                          title={agent.cronSchedule}
                        >
                          {formatCron(agent.cronSchedule)}
                        </span>
                      ) : (
                        <span className="text-base-content/40">-</span>
                      )}
                    </td>
                    <td>
                      {lastRun ? (
                        <div className="text-sm">
                          <span
                            className={`badge badge-xs mr-2 ${
                              lastRun.status === 'success' ||
                              lastRun.status === 'completed'
                                ? 'badge-success'
                                : lastRun.status === 'error' ||
                                  lastRun.status === 'failed'
                                ? 'badge-error'
                                : lastRun.status === 'running'
                                ? 'badge-info'
                                : 'badge-warning'
                            }`}
                          >
                            {lastRun.status}
                          </span>
                          <span className="text-base-content/60">
                            {formatRelativeTime(lastRun.startedAt)}
                          </span>
                        </div>
                      ) : (
                        <span className="text-base-content/40">Never</span>
                      )}
                    </td>
                    <td>
                      <span className="text-sm text-base-content/70">
                        {new Date(agent.createdAt).toLocaleDateString()}
                      </span>
                    </td>
                    <td>
                      <div className="flex items-center gap-1">
                        {/* Quick trigger for non-reaction agents */}
                        {agent.triggerType !== 'reaction' && (
                          <button
                            className="btn btn-ghost btn-xs"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleTrigger(agent);
                            }}
                            disabled={!agent.enabled || isTriggering}
                            title="Run now"
                          >
                            {isTriggering ? (
                              <Icon
                                icon="lucide--loader-2"
                                className="size-4 animate-spin"
                              />
                            ) : (
                              <Icon icon="lucide--play" className="size-4" />
                            )}
                          </button>
                        )}

                        {/* Actions dropdown */}
                        <div className="dropdown dropdown-end">
                          <div
                            tabIndex={0}
                            role="button"
                            className="btn btn-ghost btn-xs"
                          >
                            <Icon
                              icon="lucide--more-vertical"
                              className="size-4"
                            />
                          </div>
                          <ul
                            tabIndex={0}
                            className="dropdown-content z-[1] menu p-2 shadow bg-base-200 rounded-box w-44"
                          >
                            <li>
                              <button
                                onClick={() =>
                                  navigate(`/admin/agents/${agent.id}`)
                                }
                              >
                                <Icon icon="lucide--edit" className="size-4" />
                                Edit
                              </button>
                            </li>
                            <li>
                              <button onClick={() => handleToggle(agent)}>
                                <Icon
                                  icon={
                                    agent.enabled
                                      ? 'lucide--pause'
                                      : 'lucide--play'
                                  }
                                  className="size-4"
                                />
                                {agent.enabled ? 'Disable' : 'Enable'}
                              </button>
                            </li>
                            <div className="divider my-1"></div>
                            <li>
                              <button
                                className="text-error"
                                onClick={() => handleDelete(agent)}
                              >
                                <Icon
                                  icon="lucide--trash-2"
                                  className="size-4"
                                />
                                Delete
                              </button>
                            </li>
                          </ul>
                        </div>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Refresh button */}
      {!loading && (
        <div className="flex justify-center mt-6">
          <button className="gap-2 btn btn-ghost btn-sm" onClick={loadData}>
            <Icon icon="lucide--refresh-cw" className="w-4 h-4" />
            Refresh
          </button>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      <Modal
        open={deleteModalOpen}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteModalOpen(false);
            setAgentToDelete(null);
          }
        }}
        title="Delete Agent"
        sizeClassName="max-w-md"
        actions={[
          {
            label: 'Cancel',
            variant: 'ghost',
            onClick: () => {
              setDeleteModalOpen(false);
              setAgentToDelete(null);
            },
          },
          {
            label: deleting ? 'Deleting...' : 'Delete',
            variant: 'error' as const,
            onClick: confirmDelete,
            disabled: deleting,
          },
        ]}
      >
        <div className="space-y-4">
          <p>
            Are you sure you want to delete{' '}
            <strong>&quot;{agentToDelete?.name}&quot;</strong>?
          </p>
          <div className="alert alert-warning">
            <Icon icon="lucide--alert-triangle" className="size-5" />
            <span>This action cannot be undone.</span>
          </div>
        </div>
      </Modal>
    </PageContainer>
  );
}
