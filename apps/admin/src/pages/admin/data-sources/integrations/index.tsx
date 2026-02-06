import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { Icon } from '@/components/atoms/Icon';
import { Spinner } from '@/components/atoms/Spinner';
import { PageContainer } from '@/components/organisms';
import { useApi } from '@/hooks/use-api';
import { useConfig } from '@/contexts/config';
import { useToast } from '@/hooks/use-toast';
import { useExpandedRows } from '@/hooks/use-expanded-rows';
import { Modal } from '@/components/organisms/Modal/Modal';
import { SyncConfigurationsModal } from './SyncConfigurationsModal';
import type { SyncConfiguration } from './sync-config-types';

/**
 * Data source integration from the API (with optional configurations)
 */
interface DataSourceIntegration {
  id: string;
  projectId: string;
  providerType: string;
  sourceType: string;
  name: string;
  description?: string | null;
  syncMode: 'manual' | 'recurring';
  syncIntervalMinutes?: number | null;
  lastSyncedAt?: string | null;
  nextSyncAt?: string | null;
  status: 'active' | 'error' | 'disabled';
  errorMessage?: string | null;
  lastErrorAt?: string | null;
  errorCount: number;
  metadata: Record<string, any>;
  hasConfig: boolean;
  createdAt: string;
  updatedAt: string;
  configurations?: SyncConfiguration[];
}

/**
 * Provider metadata from the API
 */
interface ProviderMetadata {
  providerType: string;
  displayName: string;
  description: string;
  sourceType: string;
  icon: string;
  configSchema: Record<string, any>;
}

const STATUS_CONFIG = {
  active: { label: 'Active', color: 'success', icon: 'lucide--check-circle' },
  error: { label: 'Error', color: 'error', icon: 'lucide--alert-circle' },
  disabled: { label: 'Disabled', color: 'ghost', icon: 'lucide--pause-circle' },
} as const;

const PROVIDER_ICONS: Record<string, string> = {
  imap: 'lucide--mail',
  gmail_api: 'simple-icons--gmail',
  default: 'lucide--plug-2',
};

export default function IntegrationsListPage() {
  const navigate = useNavigate();
  const { apiBase, fetchJson } = useApi();
  const { config } = useConfig();
  const { showToast } = useToast();

  const [integrations, setIntegrations] = useState<DataSourceIntegration[]>([]);
  const [providers, setProviders] = useState<ProviderMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  // Expanded rows state
  const { isExpanded, toggle: toggleExpanded } = useExpandedRows<string>();

  // Sync state
  const [syncing, setSyncing] = useState<Set<string>>(new Set());

  // Delete modal state
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [integrationToDelete, setIntegrationToDelete] =
    useState<DataSourceIntegration | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Sync configurations modal state
  const [configModalOpen, setConfigModalOpen] = useState(false);
  const [configModalIntegration, setConfigModalIntegration] =
    useState<DataSourceIntegration | null>(null);

  // Load integrations and providers
  useEffect(() => {
    let cancelled = false;

    if (!config.activeProjectId) {
      setLoading(false);
      return () => {
        cancelled = true;
      };
    }

    async function load() {
      setLoading(true);
      setError(null);
      try {
        // Fetch integrations (with configurations) and providers in parallel
        const [integrationsRes, providersRes] = await Promise.all([
          fetchJson<DataSourceIntegration[]>(
            `${apiBase}/api/data-source-integrations?includeConfigurations=true`
          ),
          fetchJson<ProviderMetadata[]>(
            `${apiBase}/api/data-source-integrations/providers`
          ),
        ]);

        if (!cancelled) {
          setIntegrations(integrationsRes);
          setProviders(providersRes);
        }
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : 'Failed to load';
        if (!cancelled) setError(msg);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  }, [apiBase, fetchJson, config.activeProjectId]);

  // Refresh integrations
  const refreshIntegrations = useCallback(async () => {
    if (!config.activeProjectId) return;

    try {
      const result = await fetchJson<DataSourceIntegration[]>(
        `${apiBase}/api/data-source-integrations?includeConfigurations=true`
      );
      setIntegrations(result);
    } catch (e) {
      console.error('Failed to refresh integrations:', e);
    }
  }, [apiBase, fetchJson, config.activeProjectId]);

  // Handle sync for integration (default config)
  const handleSync = async (integration: DataSourceIntegration) => {
    setSyncing((prev) => new Set(prev).add(integration.id));

    try {
      await fetchJson(
        `${apiBase}/api/data-source-integrations/${integration.id}/sync`,
        {
          method: 'POST',
        }
      );

      showToast({
        message: `Sync started for "${integration.name}"`,
        variant: 'success',
      });

      // Refresh after a short delay to show updated status
      setTimeout(refreshIntegrations, 2000);
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Sync failed',
        variant: 'error',
      });
    } finally {
      setSyncing((prev) => {
        const next = new Set(prev);
        next.delete(integration.id);
        return next;
      });
    }
  };

  // Handle sync for specific configuration
  const handleSyncConfiguration = async (
    integration: DataSourceIntegration,
    cfg: SyncConfiguration
  ) => {
    const syncKey = `${integration.id}:${cfg.id}`;
    setSyncing((prev) => new Set(prev).add(syncKey));

    try {
      await fetchJson(
        `${apiBase}/api/data-source-integrations/${integration.id}/sync-configurations/${cfg.id}/run`,
        {
          method: 'POST',
        }
      );

      showToast({
        message: `Sync started for "${cfg.name}"`,
        variant: 'success',
      });

      // Refresh after a short delay to show updated status
      setTimeout(refreshIntegrations, 2000);
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Sync failed',
        variant: 'error',
      });
    } finally {
      setSyncing((prev) => {
        const next = new Set(prev);
        next.delete(syncKey);
        return next;
      });
    }
  };

  // Handle test connection
  const handleTestConnection = async (integration: DataSourceIntegration) => {
    try {
      const result = await fetchJson<{ success: boolean; error?: string }>(
        `${apiBase}/api/data-source-integrations/${integration.id}/test-connection`,
        { method: 'POST' }
      );

      if (result.success) {
        showToast({
          message: `Connection to "${integration.name}" successful!`,
          variant: 'success',
        });
      } else {
        showToast({
          message: result.error || 'Connection test failed',
          variant: 'error',
        });
      }
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Connection test failed',
        variant: 'error',
      });
    }
  };

  // Handle delete
  const handleDelete = (integration: DataSourceIntegration) => {
    setIntegrationToDelete(integration);
    setDeleteModalOpen(true);
  };

  const confirmDelete = async () => {
    if (!integrationToDelete) return;

    setDeleting(true);
    try {
      await fetchJson(
        `${apiBase}/api/data-source-integrations/${integrationToDelete.id}`,
        { method: 'DELETE' }
      );

      showToast({
        message: `"${integrationToDelete.name}" deleted successfully`,
        variant: 'success',
      });

      setDeleteModalOpen(false);
      setIntegrationToDelete(null);
      await refreshIntegrations();
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Delete failed',
        variant: 'error',
      });
    } finally {
      setDeleting(false);
    }
  };

  // Open config modal for integration
  const openConfigModal = (integration: DataSourceIntegration) => {
    setConfigModalIntegration(integration);
    setConfigModalOpen(true);
  };

  // Handle configuration run from modal
  const handleConfigurationRun = (cfg: SyncConfiguration) => {
    if (configModalIntegration) {
      handleSyncConfiguration(configModalIntegration, cfg);
    }
    setConfigModalOpen(false);
  };

  // Get provider display name
  const getProviderName = (providerType: string): string => {
    const provider = providers.find((p) => p.providerType === providerType);
    return provider?.displayName || providerType;
  };

  // Get provider icon
  const getProviderIcon = (providerType: string): string => {
    const provider = providers.find((p) => p.providerType === providerType);
    return (
      provider?.icon || PROVIDER_ICONS[providerType] || PROVIDER_ICONS.default
    );
  };

  // Render provider icon (handles both image paths and icon classes)
  const renderProviderIcon = (providerType: string, size = 'size-5') => {
    const icon = getProviderIcon(providerType);
    const provider = providers.find((p) => p.providerType === providerType);

    if (icon.startsWith('/')) {
      return (
        <img
          src={icon}
          alt={provider?.displayName || providerType}
          className={`${size} object-contain`}
        />
      );
    }
    return <Icon icon={icon} className={size} />;
  };

  // Get configuration summary for display
  const getConfigSummary = (cfg: SyncConfiguration, sourceType: string) => {
    const parts: string[] = [];
    const opts = cfg.options;

    const itemType =
      sourceType === 'email'
        ? 'emails'
        : sourceType === 'drive'
        ? 'files'
        : sourceType === 'clickup-document'
        ? 'docs'
        : 'items';

    parts.push(`${opts.limit || 100} ${itemType}`);

    if (opts.selectedFolders && opts.selectedFolders.length > 0) {
      parts.push(`${opts.selectedFolders.length} folder(s)`);
    }

    if (opts.filters && Object.keys(opts.filters).length > 0) {
      const filterCount = Object.values(opts.filters).filter(
        (v) => v !== undefined && v !== '' && v !== null
      ).length;
      if (filterCount > 0) {
        parts.push(`${filterCount} filter(s)`);
      }
    }

    return parts.join(' • ');
  };

  // Filter integrations by search query
  const filteredIntegrations = useMemo(() => {
    if (!searchQuery.trim()) return integrations;

    const q = searchQuery.toLowerCase();
    return integrations.filter(
      (i) =>
        i.name.toLowerCase().includes(q) ||
        (i.description && i.description.toLowerCase().includes(q)) ||
        i.providerType.toLowerCase().includes(q) ||
        getProviderName(i.providerType).toLowerCase().includes(q)
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [integrations, searchQuery, providers]);

  // Render status badge
  const renderStatus = (integration: DataSourceIntegration) => {
    const statusConfig = STATUS_CONFIG[integration.status];
    return (
      <div className="flex items-center gap-2">
        <Icon
          icon={statusConfig.icon}
          className={`size-4 text-${statusConfig.color}`}
        />
        <span className={`badge badge-${statusConfig.color} badge-sm`}>
          {statusConfig.label}
        </span>
      </div>
    );
  };

  return (
    <PageContainer maxWidth="full" className="px-4" testId="page-integrations">
      {/* Header */}
      <div className="flex justify-between items-start mb-6">
        <div>
          <h1 className="font-bold text-2xl inline-flex items-center gap-2">
            <Icon icon="lucide--plug-2" className="size-6" />
            Data Source Integrations
            {!loading && (
              <span className="badge badge-ghost badge-lg font-normal">
                {integrations.length}
              </span>
            )}
          </h1>
          <p className="mt-1 text-base-content/70">
            Connect external data sources to import documents automatically
          </p>
        </div>
        <button
          className="btn btn-primary"
          onClick={() => navigate('/admin/data-sources/integrations/new')}
        >
          <Icon icon="lucide--plus" className="size-4" />
          Add Integration
        </button>
      </div>

      {/* Error state */}
      {error && (
        <div className="alert alert-error mb-4">
          <Icon icon="lucide--alert-circle" className="size-5" />
          <span>{error}</span>
          <button
            className="btn btn-sm btn-ghost"
            onClick={refreshIntegrations}
          >
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
            placeholder="Search integrations..."
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
                <th className="w-10"></th>
                <th className="w-64">Integration</th>
                <th className="w-28">Status</th>
                <th className="w-32">Sync Mode</th>
                <th className="w-40">Last Synced</th>
                <th className="w-28">Created</th>
                <th className="w-28">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filteredIntegrations.length === 0 && (
                <tr>
                  <td
                    colSpan={7}
                    className="text-center py-12 text-base-content/70"
                  >
                    <Icon
                      icon="lucide--plug-2"
                      className="size-10 mx-auto mb-3 opacity-30"
                    />
                    <div>
                      {searchQuery
                        ? 'No matching integrations'
                        : 'No integrations yet'}
                    </div>
                  </td>
                </tr>
              )}
              {filteredIntegrations.map((integration) => {
                const rowExpanded = isExpanded(integration.id);
                const configs = integration.configurations || [];
                const hasConfigs = configs.length > 0;
                const isSyncing = syncing.has(integration.id);

                return (
                  <Fragment key={integration.id}>
                    {/* Integration Row */}
                    <tr className="hover">
                      <td>
                        <button
                          className="btn btn-ghost btn-xs"
                          onClick={() => toggleExpanded(integration.id)}
                          title={
                            rowExpanded
                              ? 'Collapse configurations'
                              : 'Expand configurations'
                          }
                        >
                          <Icon
                            icon={
                              rowExpanded
                                ? 'lucide--chevron-down'
                                : 'lucide--chevron-right'
                            }
                            className="size-4"
                          />
                        </button>
                      </td>
                      <td>
                        <div
                          className="flex items-center gap-3 cursor-pointer"
                          onClick={() =>
                            navigate(
                              `/admin/data-sources/integrations/${integration.id}`
                            )
                          }
                        >
                          <div className="p-2 rounded-lg bg-base-200">
                            {renderProviderIcon(integration.providerType)}
                          </div>
                          <div>
                            <div className="font-medium">
                              {integration.name}
                            </div>
                            <div className="text-sm text-base-content/60">
                              {getProviderName(integration.providerType)}
                              {hasConfigs && (
                                <span className="ml-2 text-base-content/40">
                                  • {configs.length} config
                                  {configs.length !== 1 ? 's' : ''}
                                </span>
                              )}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td>{renderStatus(integration)}</td>
                      <td>
                        <span className="text-sm text-base-content/70 capitalize">
                          {integration.syncMode}
                          {integration.syncMode === 'recurring' &&
                            integration.syncIntervalMinutes && (
                              <span className="text-base-content/50">
                                {' '}
                                ({integration.syncIntervalMinutes}m)
                              </span>
                            )}
                        </span>
                      </td>
                      <td>
                        {integration.lastSyncedAt ? (
                          <span
                            className="text-sm text-base-content/70"
                            title={new Date(
                              integration.lastSyncedAt
                            ).toISOString()}
                          >
                            {new Date(
                              integration.lastSyncedAt
                            ).toLocaleString()}
                          </span>
                        ) : (
                          <span className="text-base-content/40">Never</span>
                        )}
                      </td>
                      <td>
                        <span className="text-sm text-base-content/70">
                          {new Date(integration.createdAt).toLocaleDateString()}
                        </span>
                      </td>
                      <td>
                        <div className="dropdown dropdown-end">
                          <div
                            tabIndex={0}
                            role="button"
                            className="btn btn-ghost btn-sm"
                          >
                            <Icon
                              icon="lucide--more-vertical"
                              className="size-4"
                            />
                          </div>
                          <ul
                            tabIndex={0}
                            className="dropdown-content z-[1] menu p-2 shadow bg-base-200 rounded-box w-48"
                          >
                            <li>
                              <button
                                onClick={() =>
                                  navigate(
                                    `/admin/data-sources/integrations/${integration.id}`
                                  )
                                }
                              >
                                <Icon icon="lucide--edit" className="size-4" />
                                Edit
                              </button>
                            </li>
                            <li>
                              <button
                                onClick={() => handleSync(integration)}
                                disabled={
                                  integration.status === 'disabled' || isSyncing
                                }
                              >
                                <Icon
                                  icon="lucide--refresh-cw"
                                  className={`size-4 ${
                                    isSyncing ? 'animate-spin' : ''
                                  }`}
                                />
                                {isSyncing ? 'Syncing...' : 'Sync Now'}
                              </button>
                            </li>
                            <li>
                              <button
                                onClick={() =>
                                  handleTestConnection(integration)
                                }
                              >
                                <Icon
                                  icon="lucide--plug-zap"
                                  className="size-4"
                                />
                                Test Connection
                              </button>
                            </li>
                            <li>
                              <button
                                onClick={() => openConfigModal(integration)}
                              >
                                <Icon
                                  icon="lucide--settings-2"
                                  className="size-4"
                                />
                                Configurations
                              </button>
                            </li>
                            <div className="divider my-1"></div>
                            <li>
                              <button
                                className="text-error"
                                onClick={() => handleDelete(integration)}
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
                      </td>
                    </tr>

                    {/* Configuration Rows (when expanded) */}
                    {rowExpanded && (
                      <>
                        {configs.map((cfg) => {
                          const configSyncKey = `${integration.id}:${cfg.id}`;
                          const isConfigSyncing = syncing.has(configSyncKey);

                          return (
                            <tr key={cfg.id} className="hover bg-base-200/30">
                              <td></td>
                              <td className="pl-12">
                                <div className="flex items-center gap-2">
                                  <Icon
                                    icon="lucide--settings-2"
                                    className="size-4 text-base-content/50"
                                  />
                                  <div>
                                    <div className="flex items-center gap-2">
                                      <span className="font-medium text-sm">
                                        {cfg.name}
                                      </span>
                                      {cfg.isDefault && (
                                        <span className="badge badge-primary badge-xs">
                                          Default
                                        </span>
                                      )}
                                    </div>
                                    <div className="text-xs text-base-content/50">
                                      {getConfigSummary(
                                        cfg,
                                        integration.sourceType
                                      )}
                                    </div>
                                  </div>
                                </div>
                              </td>
                              <td colSpan={3}>
                                {cfg.description && (
                                  <span className="text-sm text-base-content/60 line-clamp-1">
                                    {cfg.description}
                                  </span>
                                )}
                              </td>
                              <td colSpan={2}>
                                <div className="flex items-center gap-1 justify-end">
                                  <button
                                    className="btn btn-ghost btn-xs"
                                    onClick={() => openConfigModal(integration)}
                                    title="Edit configuration"
                                  >
                                    <Icon
                                      icon="lucide--pencil"
                                      className="size-3"
                                    />
                                  </button>
                                  <button
                                    className="btn btn-primary btn-xs"
                                    onClick={() =>
                                      handleSyncConfiguration(integration, cfg)
                                    }
                                    disabled={
                                      integration.status === 'disabled' ||
                                      isConfigSyncing
                                    }
                                    title="Run this configuration"
                                  >
                                    {isConfigSyncing ? (
                                      <Spinner size="xs" />
                                    ) : (
                                      <>
                                        <Icon
                                          icon="lucide--play"
                                          className="size-3"
                                        />
                                        Run
                                      </>
                                    )}
                                  </button>
                                </div>
                              </td>
                            </tr>
                          );
                        })}

                        {/* Add Configuration Row */}
                        <tr className="hover bg-base-200/30">
                          <td></td>
                          <td className="pl-12" colSpan={6}>
                            <button
                              className="btn btn-ghost btn-sm text-primary"
                              onClick={() => openConfigModal(integration)}
                            >
                              <Icon icon="lucide--plus" className="size-4" />
                              Add Configuration
                            </button>
                          </td>
                        </tr>
                      </>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      <Modal
        open={deleteModalOpen}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteModalOpen(false);
            setIntegrationToDelete(null);
          }
        }}
        title="Delete Integration"
        sizeClassName="max-w-md"
        actions={[
          {
            label: 'Cancel',
            variant: 'ghost',
            onClick: () => {
              setDeleteModalOpen(false);
              setIntegrationToDelete(null);
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
            <strong>&quot;{integrationToDelete?.name}&quot;</strong>?
          </p>
          <div className="alert alert-warning">
            <Icon icon="lucide--alert-triangle" className="size-5" />
            <span>
              This will not delete documents that were already imported from
              this integration.
            </span>
          </div>
        </div>
      </Modal>

      {/* Sync Configurations Modal */}
      {configModalIntegration && (
        <SyncConfigurationsModal
          open={configModalOpen}
          integrationId={configModalIntegration.id}
          integrationName={configModalIntegration.name}
          sourceType={configModalIntegration.sourceType}
          apiBase={apiBase}
          fetchJson={fetchJson}
          onClose={() => {
            setConfigModalOpen(false);
            setConfigModalIntegration(null);
            // Refresh to pick up any changes
            refreshIntegrations();
          }}
          onConfigurationRun={handleConfigurationRun}
        />
      )}
    </PageContainer>
  );
}
