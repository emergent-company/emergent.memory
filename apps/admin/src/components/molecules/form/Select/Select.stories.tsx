import type { Meta, StoryObj } from '@storybook/react';
import { Select } from './Select';

const meta: Meta<typeof Select> = {
  title: 'Molecules/Form/Select',
  component: Select,
  parameters: {
    layout: 'centered',
  },
  argTypes: {
    selectSize: {
      control: 'select',
      options: ['xs', 'sm', 'md', 'lg'],
    },
    selectColor: {
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
type Story = StoryObj<typeof Select>;

const countryOptions = [
  { value: 'us', label: 'United States' },
  { value: 'uk', label: 'United Kingdom' },
  { value: 'ca', label: 'Canada' },
  { value: 'au', label: 'Australia' },
  { value: 'de', label: 'Germany' },
  { value: 'fr', label: 'France' },
];

export const Default: Story = {
  args: {
    placeholder: 'Select an option',
    options: countryOptions,
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithLabel: Story = {
  args: {
    label: 'Country',
    placeholder: 'Select your country',
    options: countryOptions,
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithDescription: Story = {
  args: {
    label: 'Country',
    placeholder: 'Select your country',
    options: countryOptions,
    description: 'This determines your tax region',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithAltLabels: Story = {
  args: {
    label: 'Timezone',
    labelAlt: 'Required',
    placeholder: 'Select timezone',
    options: [
      { value: 'utc', label: 'UTC' },
      { value: 'est', label: 'Eastern Time (EST)' },
      { value: 'pst', label: 'Pacific Time (PST)' },
      { value: 'cet', label: 'Central European Time (CET)' },
    ],
    description: 'Used for scheduling',
    descriptionAlt: 'Can be changed later',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const Required: Story = {
  args: {
    label: 'Priority',
    placeholder: 'Select priority',
    options: [
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
      { value: 'critical', label: 'Critical' },
    ],
    required: true,
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithError: Story = {
  args: {
    label: 'Category',
    placeholder: 'Select category',
    options: [
      { value: 'tech', label: 'Technology' },
      { value: 'health', label: 'Healthcare' },
      { value: 'finance', label: 'Finance' },
    ],
    error: 'Please select a category',
    defaultValue: '',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithSuccess: Story = {
  args: {
    label: 'Plan',
    options: [
      { value: 'free', label: 'Free' },
      { value: 'pro', label: 'Pro' },
      { value: 'enterprise', label: 'Enterprise' },
    ],
    success: 'Great choice! Pro plan selected.',
    defaultValue: 'pro',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithWarning: Story = {
  args: {
    label: 'Data Retention',
    options: [
      { value: '7', label: '7 days' },
      { value: '30', label: '30 days' },
      { value: '90', label: '90 days' },
      { value: '365', label: '1 year' },
    ],
    warning: 'Shorter retention may impact analytics',
    defaultValue: '7',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const WithDisabledOptions: Story = {
  args: {
    label: 'Subscription',
    placeholder: 'Choose a plan',
    options: [
      { value: 'free', label: 'Free' },
      { value: 'starter', label: 'Starter - $9/mo' },
      { value: 'pro', label: 'Pro - $29/mo' },
      {
        value: 'enterprise',
        label: 'Enterprise (Contact Sales)',
        disabled: true,
      },
    ],
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const Disabled: Story = {
  args: {
    label: 'Region',
    options: countryOptions,
    disabled: true,
    defaultValue: 'us',
    description: 'Region cannot be changed',
  },
  render: (args) => (
    <div className="w-full max-w-xs">
      <Select {...args} />
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-xs">
      <Select
        label="Extra Small"
        selectSize="xs"
        placeholder="xs size"
        options={countryOptions}
      />
      <Select
        label="Small"
        selectSize="sm"
        placeholder="sm size"
        options={countryOptions}
      />
      <Select
        label="Medium (Default)"
        selectSize="md"
        placeholder="md size"
        options={countryOptions}
      />
      <Select
        label="Large"
        selectSize="lg"
        placeholder="lg size"
        options={countryOptions}
      />
    </div>
  ),
};

export const Colors: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-xs">
      <Select
        label="Primary"
        selectColor="primary"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Secondary"
        selectColor="secondary"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Accent"
        selectColor="accent"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Info"
        selectColor="info"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Success"
        selectColor="success"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Warning"
        selectColor="warning"
        options={countryOptions}
        defaultValue="us"
      />
      <Select
        label="Error"
        selectColor="error"
        options={countryOptions}
        defaultValue="us"
      />
    </div>
  ),
};

export const FormExample: Story = {
  render: () => (
    <form className="space-y-4 w-full max-w-md p-6 bg-base-200 rounded-lg">
      <h2 className="text-xl font-bold mb-4">Company Profile</h2>

      <Select
        label="Industry"
        placeholder="Select your industry"
        options={[
          { value: 'tech', label: 'Technology' },
          { value: 'finance', label: 'Finance' },
          { value: 'healthcare', label: 'Healthcare' },
          { value: 'retail', label: 'Retail' },
          { value: 'manufacturing', label: 'Manufacturing' },
        ]}
        required
      />

      <Select
        label="Company Size"
        labelAlt="Required"
        placeholder="Select company size"
        options={[
          { value: '1-10', label: '1-10 employees' },
          { value: '11-50', label: '11-50 employees' },
          { value: '51-200', label: '51-200 employees' },
          { value: '201-500', label: '201-500 employees' },
          { value: '500+', label: '500+ employees' },
        ]}
        description="This helps us recommend the right plan"
        required
      />

      <Select
        label="Preferred Contact Method"
        options={[
          { value: 'email', label: 'Email' },
          { value: 'phone', label: 'Phone' },
          { value: 'slack', label: 'Slack' },
        ]}
        defaultValue="email"
      />

      <div className="flex justify-end gap-2 mt-6">
        <button type="button" className="btn btn-ghost">
          Cancel
        </button>
        <button type="submit" className="btn btn-primary">
          Save Profile
        </button>
      </div>
    </form>
  ),
};
