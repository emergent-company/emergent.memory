import { useState, useEffect } from 'react';
import { Icon } from '@/components/atoms/Icon';
import { Modal } from '@/components/organisms/Modal/Modal';
import { useApi } from '@/hooks/use-api';
import { useToast } from '@/hooks/use-toast';
import { createAgentsClient, type AgentWebhookHook } from '@/api/agents';

interface WebhookHooksListProps {
  agentId: string;
}

export function WebhookHooksList({ agentId }: WebhookHooksListProps) {
  const { apiBase, fetchJson } = useApi();
  const { showToast } = useToast();
  const client = createAgentsClient(apiBase, fetchJson);

  const [hooks, setHooks] = useState<AgentWebhookHook[]>([]);
  const [loading, setLoading] = useState(true);

  // Create Modal
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [newLabel, setNewLabel] = useState('');
  const [rpm, setRpm] = useState(60);
  const [burst, setBurst] = useState(10);
  const [creating, setCreating] = useState(false);

  // Token Reveal Modal
  const [newToken, setNewToken] = useState<string | null>(null);
  const [tokenModalOpen, setTokenModalOpen] = useState(false);

  // Delete Modal
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [hookToDelete, setHookToDelete] = useState<AgentWebhookHook | null>(
    null
  );
  const [deleting, setDeleting] = useState(false);

  const loadHooks = async () => {
    setLoading(true);
    try {
      const data = await client.listWebhookHooks(agentId);
      setHooks(data);
    } catch (e) {
      showToast({ message: 'Failed to load webhook hooks', variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadHooks();
  }, [agentId]);

  const handleCreate = async () => {
    if (!newLabel.trim()) return;
    setCreating(true);
    try {
      const hook = await client.createWebhookHook(agentId, {
        label: newLabel,
        rateLimitConfig: {
          requestsPerMinute: rpm,
          burstSize: burst,
        },
      });
      setHooks([hook, ...hooks]);
      setCreateModalOpen(false);
      setNewLabel('');

      // Show token
      setNewToken(hook.token || null);
      setTokenModalOpen(true);
      showToast({ message: 'Webhook hook created', variant: 'success' });
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to create hook',
        variant: 'error',
      });
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async () => {
    if (!hookToDelete) return;
    setDeleting(true);
    try {
      await client.deleteWebhookHook(agentId, hookToDelete.id);
      setHooks(hooks.filter((h) => h.id !== hookToDelete.id));
      showToast({ message: 'Webhook hook deleted', variant: 'success' });
      setDeleteModalOpen(false);
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to delete hook',
        variant: 'error',
      });
    } finally {
      setDeleting(false);
    }
  };

  const copyToken = () => {
    if (newToken) {
      navigator.clipboard.writeText(newToken);
      showToast({ message: 'Token copied to clipboard', variant: 'success' });
    }
  };

  return (
    <div className="card bg-base-100 shadow-md border border-base-300 mt-6">
      <div className="card-body">
        <div className="flex justify-between items-center">
          <h2 className="card-title text-lg">
            <Icon icon="lucide--webhook" className="size-5" />
            Webhook Triggers
          </h2>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => setCreateModalOpen(true)}
          >
            <Icon icon="lucide--plus" className="size-4" />
            New Webhook
          </button>
        </div>

        {loading ? (
          <div className="flex justify-center p-4">
            <span className="loading loading-spinner" />
          </div>
        ) : hooks.length === 0 ? (
          <div className="py-8 text-center text-base-content/50">
            <p>No webhooks configured.</p>
          </div>
        ) : (
          <div className="overflow-x-auto mt-4">
            <table className="table table-sm">
              <thead>
                <tr>
                  <th>Label</th>
                  <th>Rate Limit</th>
                  <th>Created</th>
                  <th className="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {hooks.map((hook) => (
                  <tr key={hook.id}>
                    <td className="font-medium">{hook.label}</td>
                    <td>
                      <span className="text-xs text-base-content/70">
                        {hook.rateLimitConfig?.requestsPerMinute || 60} req/min
                      </span>
                    </td>
                    <td className="text-sm text-base-content/70">
                      {new Date(hook.createdAt).toLocaleDateString()}
                    </td>
                    <td className="text-right">
                      <button
                        className="btn btn-ghost btn-xs text-error"
                        onClick={() => {
                          setHookToDelete(hook);
                          setDeleteModalOpen(true);
                        }}
                      >
                        <Icon icon="lucide--trash-2" className="size-4" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Create Modal */}
      <Modal
        open={createModalOpen}
        onOpenChange={(open) => !open && setCreateModalOpen(false)}
        title="Create Webhook Trigger"
        sizeClassName="max-w-md"
        actions={[
          {
            label: 'Cancel',
            variant: 'ghost',
            onClick: () => setCreateModalOpen(false),
          },
          {
            label: creating ? 'Creating...' : 'Create',
            variant: 'primary',
            onClick: handleCreate,
            disabled: creating || !newLabel.trim(),
          },
        ]}
      >
        <div className="space-y-4">
          <div className="form-control">
            <label className="label">
              <span className="label-text">Label</span>
            </label>
            <input
              type="text"
              className="input input-bordered w-full"
              placeholder="e.g. GitHub Production Events"
              value={newLabel}
              onChange={(e) => setNewLabel(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="form-control">
              <label className="label">
                <span className="label-text">Requests / Minute</span>
              </label>
              <input
                type="number"
                className="input input-bordered w-full"
                value={rpm}
                onChange={(e) => setRpm(Number(e.target.value))}
                min={1}
              />
            </div>
            <div className="form-control">
              <label className="label">
                <span className="label-text">Burst Size</span>
              </label>
              <input
                type="number"
                className="input input-bordered w-full"
                value={burst}
                onChange={(e) => setBurst(Number(e.target.value))}
                min={1}
              />
            </div>
          </div>
        </div>
      </Modal>

      {/* Token Modal */}
      <Modal
        open={tokenModalOpen}
        onOpenChange={(open) => !open && setTokenModalOpen(false)}
        title="Webhook Token"
        sizeClassName="max-w-lg"
        actions={[
          {
            label: 'Close',
            variant: 'primary',
            onClick: () => setTokenModalOpen(false),
          },
        ]}
      >
        <div className="space-y-4">
          <div className="alert alert-warning">
            <Icon icon="lucide--alert-triangle" className="size-5" />
            <span>Copy this token now! You won't be able to see it again.</span>
          </div>
          <div className="form-control">
            <div className="input-group flex w-full">
              <input
                type="text"
                readOnly
                value={newToken || ''}
                className="input input-bordered flex-1 font-mono text-sm"
              />
              <button className="btn btn-square" onClick={copyToken}>
                <Icon icon="lucide--copy" className="size-5" />
              </button>
            </div>
          </div>
          <div className="text-sm text-base-content/70 mt-4">
            <p className="font-medium mb-1">Webhook URL:</p>
            <code className="bg-base-200 p-2 rounded block break-all text-xs">
              https://api.YOUR_DOMAIN/api/webhooks/agents/{'<HOOK_ID>'}
            </code>
            <p className="mt-2">
              Pass the token in the Authorization header as a Bearer token.
            </p>
          </div>
        </div>
      </Modal>

      {/* Delete Modal */}
      <Modal
        open={deleteModalOpen}
        onOpenChange={(open) => !open && setDeleteModalOpen(false)}
        title="Delete Webhook"
        sizeClassName="max-w-md"
        actions={[
          {
            label: 'Cancel',
            variant: 'ghost',
            onClick: () => setDeleteModalOpen(false),
          },
          {
            label: deleting ? 'Deleting...' : 'Delete',
            variant: 'error',
            onClick: handleDelete,
            disabled: deleting,
          },
        ]}
      >
        <p>
          Are you sure you want to delete the webhook{' '}
          <strong>"{hookToDelete?.label}"</strong>? Any external services using
          this webhook will immediately fail.
        </p>
      </Modal>
    </div>
  );
}
