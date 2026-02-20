import { useState, useMemo } from 'react';
import { useNavigate } from 'react-router';
import { Icon } from '@/components/atoms/Icon';
import { PageContainer } from '@/components/organisms';
import { useApi } from '@/hooks/use-api';
import { useToast } from '@/hooks/use-toast';
import { useConfig } from '@/contexts/config';
import {
  createAgentsClient,
  type CreateAgentPayload,
  type AgentTriggerType,
  type AgentExecutionMode,
  type ReactionConfig,
  type AgentCapabilities,
} from '@/api/agents';

/**
 * Agent template definition
 */
interface AgentTemplate {
  id: string;
  name: string;
  description: string;
  icon: string;
  strategyType: string;
  triggerType: AgentTriggerType;
  executionMode: AgentExecutionMode;
  cronSchedule: string;
  reactionConfig?: ReactionConfig;
  capabilities?: AgentCapabilities;
  config?: Record<string, any>;
}

/**
 * Predefined agent templates
 */
const AGENT_TEMPLATES: AgentTemplate[] = [
  {
    id: 'merge-suggestion',
    name: 'Merge Suggestion Agent',
    description:
      'Identifies potential duplicate entities and creates merge suggestions for human review',
    icon: 'lucide--git-merge',
    strategyType: 'merge-suggestion',
    triggerType: 'schedule',
    executionMode: 'suggest',
    cronSchedule: '0 * * * *',
    config: {
      similarityThreshold: 0.8,
      maxSuggestionsPerRun: 10,
    },
  },
  {
    id: 'scheduled-task',
    name: 'Scheduled Task Agent',
    description:
      'Runs a scheduled task on a cron schedule to perform automated maintenance',
    icon: 'lucide--clock',
    strategyType: 'scheduled-task',
    triggerType: 'schedule',
    executionMode: 'execute',
    cronSchedule: '0 0 * * *',
    config: {},
  },
  {
    id: 'reaction-handler',
    name: 'Reaction Agent',
    description:
      'Automatically responds to graph object events (create, update, delete)',
    icon: 'lucide--zap',
    strategyType: 'reaction-handler',
    triggerType: 'reaction',
    executionMode: 'execute',
    cronSchedule: '0 * * * *',
    reactionConfig: {
      objectTypes: [],
      events: ['created', 'updated'],
      concurrencyStrategy: 'skip',
      ignoreAgentTriggered: true,
      ignoreSelfTriggered: true,
    },
    capabilities: {
      canCreateObjects: true,
      canUpdateObjects: true,
      canDeleteObjects: false,
      canCreateRelationships: true,
      allowedObjectTypes: null,
    },
  },
  {
    id: 'manual-task',
    name: 'Manual Agent',
    description:
      'Agent that only runs when manually triggered by an admin user',
    icon: 'lucide--hand',
    strategyType: 'manual-task',
    triggerType: 'manual',
    executionMode: 'execute',
    cronSchedule: '0 * * * *',
    config: {},
  },
  {
    id: 'webhook-handler',
    name: 'Webhook Agent',
    description:
      'Agent that runs in response to incoming webhook calls from external systems',
    icon: 'lucide--webhook',
    strategyType: 'webhook-handler',
    triggerType: 'webhook',
    executionMode: 'execute',
    cronSchedule: '0 * * * *',
    config: {},
  },
];

type Step = 'template' | 'configure';

/**
 * Create new agent page
 */
export default function NewAgentPage() {
  const navigate = useNavigate();
  const { apiBase, fetchJson } = useApi();
  const { showToast } = useToast();
  const { config } = useConfig();

  const client = useMemo(
    () => createAgentsClient(apiBase, fetchJson),
    [apiBase, fetchJson]
  );

  const [step, setStep] = useState<Step>('template');
  const [selectedTemplate, setSelectedTemplate] =
    useState<AgentTemplate | null>(null);
  const [saving, setSaving] = useState(false);

  // Form state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [cronSchedule, setCronSchedule] = useState('0 * * * *');
  const [enabled, setEnabled] = useState(true);
  const [prompt, setPrompt] = useState('');

  // Select template and advance to configure step
  const handleSelectTemplate = (template: AgentTemplate) => {
    setSelectedTemplate(template);
    setName(template.name);
    setDescription(template.description);
    setCronSchedule(template.cronSchedule);
    setPrompt(''); // Reset prompt for new template
    setStep('configure');
  };

  // Go back to template selection
  const handleBack = () => {
    setStep('template');
    setSelectedTemplate(null);
  };

  // Create the agent
  const handleCreate = async () => {
    if (!selectedTemplate) return;

    if (!config.activeProjectId) {
      showToast({
        message: 'No active project selected',
        variant: 'error',
      });
      return;
    }

    // Validate prompt is required for reaction-handler
    if (
      selectedTemplate.strategyType === 'reaction-handler' &&
      !prompt.trim()
    ) {
      showToast({
        message: 'Prompt is required for reaction agents',
        variant: 'error',
      });
      return;
    }

    setSaving(true);
    try {
      const payload: CreateAgentPayload = {
        projectId: config.activeProjectId,
        name,
        strategyType: selectedTemplate.strategyType,
        description: description || undefined,
        cronSchedule,
        enabled,
        triggerType: selectedTemplate.triggerType,
        executionMode: selectedTemplate.executionMode,
        reactionConfig: selectedTemplate.reactionConfig || null,
        capabilities: selectedTemplate.capabilities || null,
        config: selectedTemplate.config || {},
        // Include prompt for reaction-handler agents
        ...(selectedTemplate.strategyType === 'reaction-handler' &&
        prompt.trim()
          ? { prompt: prompt.trim() }
          : {}),
      };

      const agent = await client.createAgent(payload);

      showToast({
        message: `Agent "${agent.name}" created successfully`,
        variant: 'success',
      });

      // Navigate to detail page
      navigate(`/admin/agents/${agent.id}`);
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Failed to create agent',
        variant: 'error',
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageContainer maxWidth="4xl" testId="page-agents-new">
      {/* Header */}
      <div className="mb-6">
        <button
          className="btn btn-ghost btn-sm mb-2"
          onClick={() => navigate('/admin/agents')}
        >
          <Icon icon="lucide--arrow-left" className="size-4" />
          Back to Agents
        </button>
        <h1 className="font-bold text-2xl">Create New Agent</h1>
        <p className="mt-1 text-base-content/70">
          {step === 'template'
            ? 'Select a template to get started'
            : 'Configure your agent settings'}
        </p>
      </div>

      {/* Steps indicator */}
      <ul className="steps steps-horizontal mb-8 w-full">
        <li className={`step ${step === 'template' ? 'step-primary' : ''}`}>
          Select Template
        </li>
        <li className={`step ${step === 'configure' ? 'step-primary' : ''}`}>
          Configure
        </li>
      </ul>

      {/* Step 1: Template Selection */}
      {step === 'template' && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {AGENT_TEMPLATES.map((template) => (
            <button
              key={template.id}
              className="card bg-base-100 shadow-md border border-base-300 hover:border-primary hover:shadow-lg transition-all text-left cursor-pointer"
              onClick={() => handleSelectTemplate(template)}
            >
              <div className="card-body">
                <div className="flex items-start gap-4">
                  <div className="p-3 rounded-lg bg-primary/10 text-primary">
                    <Icon icon={template.icon} className="size-6" />
                  </div>
                  <div className="flex-1">
                    <h3 className="font-semibold text-lg">{template.name}</h3>
                    <p className="text-base-content/70 text-sm mt-1">
                      {template.description}
                    </p>
                    <div className="flex gap-2 mt-3">
                      <span className="badge badge-ghost badge-sm">
                        {template.triggerType}
                      </span>
                      <span className="badge badge-ghost badge-sm">
                        {template.executionMode}
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            </button>
          ))}
        </div>
      )}

      {/* Step 2: Configure */}
      {step === 'configure' && selectedTemplate && (
        <div className="card bg-base-100 shadow-md border border-base-300">
          <div className="card-body">
            {/* Template info */}
            <div className="flex items-center gap-3 mb-6 pb-4 border-b border-base-300">
              <div className="p-2 rounded-lg bg-primary/10 text-primary">
                <Icon icon={selectedTemplate.icon} className="size-5" />
              </div>
              <div>
                <p className="font-medium">{selectedTemplate.name}</p>
                <p className="text-sm text-base-content/60">
                  Strategy: {selectedTemplate.strategyType}
                </p>
              </div>
            </div>

            {/* Form */}
            <div className="space-y-4">
              {/* Name */}
              <div className="form-control">
                <label className="label">
                  <span className="label-text font-medium">Agent Name</span>
                </label>
                <input
                  type="text"
                  className="input input-bordered"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="My Agent"
                />
              </div>

              {/* Description */}
              <div className="form-control">
                <label className="label">
                  <span className="label-text font-medium">Description</span>
                  <span className="label-text-alt">Optional</span>
                </label>
                <textarea
                  className="textarea textarea-bordered"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="What does this agent do?"
                  rows={2}
                />
              </div>

              {/* Cron Schedule (only for schedule trigger) */}
              {selectedTemplate.triggerType === 'schedule' && (
                <div className="form-control">
                  <label className="label">
                    <span className="label-text font-medium">
                      Cron Schedule
                    </span>
                  </label>
                  <input
                    type="text"
                    className="input input-bordered font-mono"
                    value={cronSchedule}
                    onChange={(e) => setCronSchedule(e.target.value)}
                    placeholder="0 * * * *"
                  />
                  <label className="label">
                    <span className="label-text-alt">
                      Example: &quot;0 * * * *&quot; = every hour, &quot;*/5 * *
                      * *&quot; = every 5 minutes
                    </span>
                  </label>
                </div>
              )}

              {/* Prompt (required for reaction-handler) */}
              {selectedTemplate.strategyType === 'reaction-handler' && (
                <div className="form-control">
                  <label className="label">
                    <span className="label-text font-medium">
                      Agent Prompt
                      <span className="text-error ml-1">*</span>
                    </span>
                  </label>
                  <textarea
                    className="textarea textarea-bordered font-mono text-sm"
                    value={prompt}
                    onChange={(e) => setPrompt(e.target.value)}
                    placeholder="You are an agent that responds to graph object events. When a new object is created or updated, analyze it and..."
                    rows={6}
                  />
                  <label className="label">
                    <span className="label-text-alt">
                      Instructions for the AI agent on how to handle events. Be
                      specific about what actions to take.
                    </span>
                  </label>
                </div>
              )}

              {/* Enabled */}
              <div className="form-control">
                <label className="label cursor-pointer justify-start gap-3">
                  <input
                    type="checkbox"
                    className="toggle toggle-primary"
                    checked={enabled}
                    onChange={(e) => setEnabled(e.target.checked)}
                  />
                  <span className="label-text font-medium">
                    Enable agent after creation
                  </span>
                </label>
              </div>
            </div>

            {/* Actions */}
            <div className="card-actions justify-between mt-6 pt-4 border-t border-base-300">
              <button className="btn btn-ghost" onClick={handleBack}>
                <Icon icon="lucide--arrow-left" className="size-4" />
                Back
              </button>
              <button
                className="btn btn-primary"
                onClick={handleCreate}
                disabled={
                  saving ||
                  !name.trim() ||
                  (selectedTemplate.strategyType === 'reaction-handler' &&
                    !prompt.trim())
                }
              >
                {saving ? (
                  <>
                    <span className="loading loading-spinner loading-sm" />
                    Creating...
                  </>
                ) : (
                  <>
                    <Icon icon="lucide--plus" className="size-4" />
                    Create Agent
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </PageContainer>
  );
}
