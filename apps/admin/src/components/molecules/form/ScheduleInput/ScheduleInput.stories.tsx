import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { ScheduleInput } from './ScheduleInput';

const meta: Meta<typeof ScheduleInput> = {
  title: 'Molecules/Form/ScheduleInput',
  component: ScheduleInput,
  parameters: {
    layout: 'centered',
  },
  argTypes: {
    size: {
      control: 'select',
      options: ['sm', 'md', 'lg'],
    },
  },
};

export default meta;
type Story = StoryObj<typeof ScheduleInput>;

export const Default: Story = {
  args: {
    label: 'Sync Schedule',
  },
  render: (args) => {
    const [value, setValue] = useState<string | null>('0 * * * *');
    return (
      <div className="w-full max-w-md">
        <ScheduleInput
          {...args}
          value={value ?? undefined}
          onChange={setValue}
        />
        <div className="mt-4 text-sm text-base-content/60">
          Value: <code>{JSON.stringify(value)}</code>
        </div>
      </div>
    );
  },
};

export const WithManualMode: Story = {
  args: {
    label: 'Data Sync',
    allowManual: true,
    description: 'Choose when to sync data from this source',
  },
  render: (args) => {
    const [value, setValue] = useState<string | null>(null);
    return (
      <div className="w-full max-w-md">
        <ScheduleInput
          {...args}
          value={value ?? undefined}
          onChange={setValue}
        />
        <div className="mt-4 text-sm text-base-content/60">
          Mode: {value === null ? 'Manual' : 'Scheduled'}
          <br />
          Value: <code>{JSON.stringify(value)}</code>
        </div>
      </div>
    );
  },
};

export const ScheduledMode: Story = {
  args: {
    label: 'Backup Schedule',
    allowManual: false,
    showNextRun: true,
    nextRunCount: 5,
  },
  render: (args) => {
    const [value, setValue] = useState<string | null>('0 0 * * *');
    return (
      <div className="w-full max-w-md">
        <ScheduleInput
          {...args}
          value={value ?? undefined}
          onChange={setValue}
        />
      </div>
    );
  },
};

export const CustomCron: Story = {
  args: {
    label: 'Custom Schedule',
    allowManual: false,
  },
  render: (args) => {
    const [value, setValue] = useState<string | null>('30 4 * * 1-5');
    return (
      <div className="w-full max-w-md">
        <ScheduleInput
          {...args}
          value={value ?? undefined}
          onChange={setValue}
        />
        <div className="mt-4 text-sm text-base-content/60">
          This cron runs at 4:30 AM on weekdays
        </div>
      </div>
    );
  },
};

export const WithError: Story = {
  args: {
    label: 'Schedule',
    error: 'Schedule is required for automated syncs',
    required: true,
  },
  render: (args) => {
    const [value, setValue] = useState<string | null>('0 * * * *');
    return (
      <div className="w-full max-w-md">
        <ScheduleInput
          {...args}
          value={value ?? undefined}
          onChange={setValue}
        />
      </div>
    );
  },
};

export const Disabled: Story = {
  args: {
    label: 'Schedule (Locked)',
    disabled: true,
    description: 'Contact admin to modify schedule',
  },
  render: (args) => {
    return (
      <div className="w-full max-w-md">
        <ScheduleInput {...args} value="0 9 * * 1-5" />
      </div>
    );
  },
};

export const Sizes: Story = {
  render: () => {
    const [value1, setValue1] = useState<string | null>('0 * * * *');
    const [value2, setValue2] = useState<string | null>('0 * * * *');
    const [value3, setValue3] = useState<string | null>('0 * * * *');

    return (
      <div className="space-y-6 w-full max-w-md">
        <ScheduleInput
          label="Small"
          size="sm"
          allowManual={false}
          showNextRun={false}
          value={value1 ?? undefined}
          onChange={setValue1}
        />
        <ScheduleInput
          label="Medium (Default)"
          size="md"
          allowManual={false}
          showNextRun={false}
          value={value2 ?? undefined}
          onChange={setValue2}
        />
        <ScheduleInput
          label="Large"
          size="lg"
          allowManual={false}
          showNextRun={false}
          value={value3 ?? undefined}
          onChange={setValue3}
        />
      </div>
    );
  },
};

export const IntegrationFormExample: Story = {
  render: () => {
    const [schedule, setSchedule] = useState<string | null>('0 */6 * * *');
    const [name, setName] = useState('Google Drive Sync');
    const [enabled, setEnabled] = useState(true);

    return (
      <form className="space-y-4 w-full max-w-lg p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Integration Settings</h2>

        <div className="form-control">
          <label className="label">
            <span className="label-text font-medium">Integration Name</span>
          </label>
          <input
            type="text"
            className="input input-bordered"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className="form-control">
          <label className="label cursor-pointer justify-start gap-3">
            <input
              type="checkbox"
              className="toggle toggle-primary"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            <span className="label-text">Enable integration</span>
          </label>
        </div>

        <ScheduleInput
          label="Sync Schedule"
          description="How often should we sync data from this source?"
          value={schedule ?? undefined}
          onChange={setSchedule}
          allowManual
          manualLabel="Manual sync"
          scheduledLabel="Auto sync"
          showNextRun
          nextRunCount={3}
          disabled={!enabled}
        />

        <div className="flex justify-end gap-2 mt-6">
          <button type="button" className="btn btn-ghost">
            Cancel
          </button>
          <button type="submit" className="btn btn-primary">
            Save Settings
          </button>
        </div>
      </form>
    );
  },
};

export const AgentScheduleExample: Story = {
  render: () => {
    const [schedule, setSchedule] = useState<string | null>('0 9 * * 1-5');

    return (
      <form className="space-y-4 w-full max-w-lg p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Agent Configuration</h2>

        <div className="form-control">
          <label className="label">
            <span className="label-text font-medium">Agent Name</span>
          </label>
          <input
            type="text"
            className="input input-bordered"
            defaultValue="Daily Report Generator"
          />
        </div>

        <ScheduleInput
          label="Execution Schedule"
          description="When should this agent run automatically?"
          value={schedule ?? undefined}
          onChange={setSchedule}
          allowManual
          manualLabel="On-demand only"
          scheduledLabel="Scheduled runs"
          showNextRun
          nextRunCount={5}
          required
        />

        <div className="divider"></div>

        <div className="text-sm">
          <div className="font-medium mb-2">Schedule Summary</div>
          <ul className="list-disc list-inside text-base-content/70 space-y-1">
            <li>Mode: {schedule ? 'Scheduled' : 'Manual'}</li>
            {schedule && (
              <li>
                Cron:{' '}
                <code className="bg-base-300 px-1 rounded">{schedule}</code>
              </li>
            )}
          </ul>
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <button type="button" className="btn btn-ghost">
            Cancel
          </button>
          <button type="submit" className="btn btn-primary">
            Save Agent
          </button>
        </div>
      </form>
    );
  },
};
