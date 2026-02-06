import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Toggle } from './Toggle';

const meta: Meta<typeof Toggle> = {
  title: 'Molecules/Form/Toggle',
  component: Toggle,
  parameters: {
    layout: 'centered',
  },
  argTypes: {
    toggleSize: {
      control: 'select',
      options: ['xs', 'sm', 'md', 'lg'],
    },
    toggleColor: {
      control: 'select',
      options: [
        'primary',
        'secondary',
        'accent',
        'info',
        'success',
        'warning',
        'error',
      ],
    },
    layout: {
      control: 'radio',
      options: ['horizontal', 'vertical'],
    },
    togglePosition: {
      control: 'radio',
      options: ['left', 'right'],
    },
  },
};

export default meta;
type Story = StoryObj<typeof Toggle>;

export const Default: Story = {
  args: {
    label: 'Enable feature',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const WithDescription: Story = {
  args: {
    label: 'Dark mode',
    description: 'Use dark theme across the application',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const Checked: Story = {
  args: {
    label: 'Notifications enabled',
    defaultChecked: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const ToggleOnRight: Story = {
  args: {
    label: 'Auto-save',
    description: 'Automatically save changes',
    togglePosition: 'right',
    defaultChecked: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const VerticalLayout: Story = {
  args: {
    label: 'Two-factor authentication',
    description: 'Add an extra layer of security to your account',
    layout: 'vertical',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const WithError: Story = {
  args: {
    label: 'Accept cookies',
    error: 'You must accept cookies to use this feature',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const Disabled: Story = {
  args: {
    label: 'Premium feature',
    description: 'Upgrade to enable this feature',
    disabled: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const DisabledChecked: Story = {
  args: {
    label: 'Required setting',
    description: 'This setting cannot be changed',
    disabled: true,
    defaultChecked: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Toggle {...args} />
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-sm">
      <Toggle label="Extra Small" toggleSize="xs" />
      <Toggle label="Small" toggleSize="sm" />
      <Toggle label="Medium (Default)" toggleSize="md" />
      <Toggle label="Large" toggleSize="lg" />
    </div>
  ),
};

export const Colors: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-sm">
      <Toggle label="Primary" toggleColor="primary" defaultChecked />
      <Toggle label="Secondary" toggleColor="secondary" defaultChecked />
      <Toggle label="Accent" toggleColor="accent" defaultChecked />
      <Toggle label="Info" toggleColor="info" defaultChecked />
      <Toggle label="Success" toggleColor="success" defaultChecked />
      <Toggle label="Warning" toggleColor="warning" defaultChecked />
      <Toggle label="Error" toggleColor="error" defaultChecked />
    </div>
  ),
};

export const SettingsPanel: Story = {
  render: () => {
    const [settings, setSettings] = useState({
      darkMode: true,
      notifications: true,
      autoSave: false,
      analytics: true,
      twoFactor: false,
    });

    const handleToggle = (key: keyof typeof settings) => {
      setSettings((prev) => ({ ...prev, [key]: !prev[key] }));
    };

    return (
      <div className="w-full max-w-md p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Settings</h2>

        <div className="space-y-1 divide-y divide-base-300">
          <Toggle
            label="Dark mode"
            description="Use dark theme"
            checked={settings.darkMode}
            onChange={() => handleToggle('darkMode')}
            toggleColor="primary"
            togglePosition="right"
          />
          <Toggle
            label="Push notifications"
            description="Receive notifications on your device"
            checked={settings.notifications}
            onChange={() => handleToggle('notifications')}
            toggleColor="primary"
            togglePosition="right"
          />
          <Toggle
            label="Auto-save"
            description="Automatically save changes as you work"
            checked={settings.autoSave}
            onChange={() => handleToggle('autoSave')}
            toggleColor="primary"
            togglePosition="right"
          />
          <Toggle
            label="Analytics"
            description="Help us improve by sharing usage data"
            checked={settings.analytics}
            onChange={() => handleToggle('analytics')}
            toggleColor="primary"
            togglePosition="right"
          />
          <Toggle
            label="Two-factor authentication"
            description="Add an extra layer of security"
            checked={settings.twoFactor}
            onChange={() => handleToggle('twoFactor')}
            toggleColor="success"
            togglePosition="right"
          />
        </div>
      </div>
    );
  },
};

export const FormExample: Story = {
  render: () => {
    const [isScheduled, setIsScheduled] = useState(false);
    const [isPublic, setIsPublic] = useState(true);

    return (
      <form className="space-y-4 w-full max-w-md p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Publish Settings</h2>

        <div className="space-y-4">
          <Toggle
            label="Public visibility"
            description="Anyone can view this content"
            checked={isPublic}
            onChange={(e) => setIsPublic(e.target.checked)}
            toggleColor="primary"
          />

          <Toggle
            label="Schedule publication"
            description="Publish at a specific date and time"
            checked={isScheduled}
            onChange={(e) => setIsScheduled(e.target.checked)}
            toggleColor="primary"
          />

          {isScheduled && (
            <div className="pl-12 space-y-2">
              <input
                type="date"
                className="input input-bordered input-sm w-full"
              />
              <input
                type="time"
                className="input input-bordered input-sm w-full"
              />
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <button type="button" className="btn btn-ghost">
            Cancel
          </button>
          <button type="submit" className="btn btn-primary">
            {isScheduled ? 'Schedule' : 'Publish Now'}
          </button>
        </div>
      </form>
    );
  },
};
