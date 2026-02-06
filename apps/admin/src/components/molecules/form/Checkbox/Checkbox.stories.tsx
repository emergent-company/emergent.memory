import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Checkbox } from './Checkbox';

const meta: Meta<typeof Checkbox> = {
  title: 'Molecules/Form/Checkbox',
  component: Checkbox,
  parameters: {
    layout: 'centered',
  },
  argTypes: {
    checkboxSize: {
      control: 'select',
      options: ['xs', 'sm', 'md', 'lg'],
    },
    checkboxColor: {
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
  },
};

export default meta;
type Story = StoryObj<typeof Checkbox>;

export const Default: Story = {
  args: {
    label: 'Accept terms and conditions',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
    </div>
  ),
};

export const WithDescription: Story = {
  args: {
    label: 'Subscribe to newsletter',
    description: "We'll send you weekly updates about new features",
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
    </div>
  ),
};

export const Checked: Story = {
  args: {
    label: 'Remember me',
    defaultChecked: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
    </div>
  ),
};

export const WithError: Story = {
  args: {
    label: 'I agree to the terms of service',
    error: 'You must accept the terms to continue',
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
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
      <Checkbox {...args} />
    </div>
  ),
};

export const DisabledChecked: Story = {
  args: {
    label: 'Required permission',
    description: 'This permission cannot be disabled',
    disabled: true,
    defaultChecked: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
    </div>
  ),
};

export const Indeterminate: Story = {
  args: {
    label: 'Select all items',
    description: 'Some items are selected',
    indeterminate: true,
  },
  render: (args) => (
    <div className="w-full max-w-sm">
      <Checkbox {...args} />
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-sm">
      <Checkbox label="Extra Small" checkboxSize="xs" />
      <Checkbox label="Small" checkboxSize="sm" />
      <Checkbox label="Medium (Default)" checkboxSize="md" />
      <Checkbox label="Large" checkboxSize="lg" />
    </div>
  ),
};

export const Colors: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-sm">
      <Checkbox label="Primary" checkboxColor="primary" defaultChecked />
      <Checkbox label="Secondary" checkboxColor="secondary" defaultChecked />
      <Checkbox label="Accent" checkboxColor="accent" defaultChecked />
      <Checkbox label="Info" checkboxColor="info" defaultChecked />
      <Checkbox label="Success" checkboxColor="success" defaultChecked />
      <Checkbox label="Warning" checkboxColor="warning" defaultChecked />
      <Checkbox label="Error" checkboxColor="error" defaultChecked />
    </div>
  ),
};

export const CheckboxGroup: Story = {
  render: () => {
    const [selected, setSelected] = useState<string[]>(['email']);

    const handleChange = (value: string) => {
      setSelected((prev) =>
        prev.includes(value)
          ? prev.filter((v) => v !== value)
          : [...prev, value]
      );
    };

    return (
      <div className="w-full max-w-sm p-4 bg-base-200 rounded-lg">
        <h3 className="font-semibold mb-3">Notification Preferences</h3>
        <div className="space-y-2">
          <Checkbox
            label="Email notifications"
            description="Receive updates via email"
            checked={selected.includes('email')}
            onChange={() => handleChange('email')}
            checkboxColor="primary"
          />
          <Checkbox
            label="Push notifications"
            description="Receive push notifications on your device"
            checked={selected.includes('push')}
            onChange={() => handleChange('push')}
            checkboxColor="primary"
          />
          <Checkbox
            label="SMS notifications"
            description="Receive text messages for urgent alerts"
            checked={selected.includes('sms')}
            onChange={() => handleChange('sms')}
            checkboxColor="primary"
          />
        </div>
        <p className="text-sm text-base-content/60 mt-3">
          Selected: {selected.length > 0 ? selected.join(', ') : 'none'}
        </p>
      </div>
    );
  },
};

export const FormExample: Story = {
  render: () => {
    const [agreed, setAgreed] = useState(false);
    const [marketing, setMarketing] = useState(false);

    return (
      <form className="space-y-4 w-full max-w-md p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Create Account</h2>

        <div className="space-y-2">
          <input
            type="text"
            placeholder="Full Name"
            className="input input-bordered w-full"
          />
          <input
            type="email"
            placeholder="Email"
            className="input input-bordered w-full"
          />
          <input
            type="password"
            placeholder="Password"
            className="input input-bordered w-full"
          />
        </div>

        <div className="divider"></div>

        <Checkbox
          label="I agree to the Terms of Service and Privacy Policy"
          checked={agreed}
          onChange={(e) => setAgreed(e.target.checked)}
          checkboxColor="primary"
          error={!agreed ? 'You must agree to continue' : undefined}
        />

        <Checkbox
          label="Send me marketing emails"
          description="Stay up to date with news, offers, and tips"
          checked={marketing}
          onChange={(e) => setMarketing(e.target.checked)}
        />

        <div className="flex justify-end gap-2 mt-6">
          <button type="button" className="btn btn-ghost">
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={!agreed}>
            Create Account
          </button>
        </div>
      </form>
    );
  },
};
