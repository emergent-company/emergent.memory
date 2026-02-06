import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { TextArea } from './TextArea';

const meta: Meta<typeof TextArea> = {
  title: 'Molecules/Form/TextArea',
  component: TextArea,
  parameters: {
    layout: 'centered',
  },
  argTypes: {
    textareaSize: {
      control: 'select',
      options: ['xs', 'sm', 'md', 'lg'],
    },
    textareaColor: {
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
type Story = StoryObj<typeof TextArea>;

export const Default: Story = {
  args: {
    placeholder: 'Type your message here...',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithLabel: Story = {
  args: {
    label: 'Bio',
    placeholder: 'Tell us about yourself...',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithDescription: Story = {
  args: {
    label: 'Description',
    placeholder: 'Enter a description...',
    description: 'This will be displayed on your public profile',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithCharacterCount: Story = {
  args: {
    label: 'Tweet',
    placeholder: "What's happening?",
    maxLength: 280,
    showCharCount: true,
    rows: 3,
  },
  render: (args) => {
    const [value, setValue] = useState('');
    return (
      <div className="w-full max-w-md">
        <TextArea
          {...args}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      </div>
    );
  },
};

export const WithAltLabels: Story = {
  args: {
    label: 'Notes',
    labelAlt: 'Optional',
    placeholder: 'Add any additional notes...',
    description: 'Internal notes only',
    descriptionAlt: 'Max 1000 chars',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const Required: Story = {
  args: {
    label: 'Feedback',
    placeholder: 'Please share your feedback...',
    required: true,
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithError: Story = {
  args: {
    label: 'Message',
    placeholder: 'Enter your message',
    error: 'Message is required',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithSuccess: Story = {
  args: {
    label: 'Review',
    defaultValue:
      'This product exceeded my expectations! Great quality and fast shipping.',
    success: 'Thank you for your review!',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const WithWarning: Story = {
  args: {
    label: 'Content',
    defaultValue: 'This is some content that might need review...',
    warning: 'This content will be publicly visible',
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const AutoResize: Story = {
  args: {
    label: 'Auto-resizing textarea',
    placeholder: 'Start typing and watch it grow...',
    autoResize: true,
    minRows: 2,
    maxRows: 8,
    description: 'This textarea grows with content (2-8 rows)',
  },
  render: (args) => {
    const [value, setValue] = useState('');
    return (
      <div className="w-full max-w-md">
        <TextArea
          {...args}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      </div>
    );
  },
};

export const Disabled: Story = {
  args: {
    label: 'System Notes',
    defaultValue: 'This field is managed by the system and cannot be edited.',
    disabled: true,
    rows: 3,
    description: 'Contact support to modify',
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const ReadOnly: Story = {
  args: {
    label: 'Terms & Conditions',
    defaultValue: `By using this service, you agree to our terms and conditions. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`,
    readOnly: true,
    rows: 4,
  },
  render: (args) => (
    <div className="w-full max-w-md">
      <TextArea {...args} />
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-md">
      <TextArea
        label="Extra Small"
        textareaSize="xs"
        placeholder="xs size"
        rows={2}
      />
      <TextArea
        label="Small"
        textareaSize="sm"
        placeholder="sm size"
        rows={2}
      />
      <TextArea
        label="Medium (Default)"
        textareaSize="md"
        placeholder="md size"
        rows={2}
      />
      <TextArea
        label="Large"
        textareaSize="lg"
        placeholder="lg size"
        rows={2}
      />
    </div>
  ),
};

export const Colors: Story = {
  render: () => (
    <div className="space-y-4 w-full max-w-md">
      <TextArea
        label="Primary"
        textareaColor="primary"
        placeholder="Primary color"
        rows={2}
      />
      <TextArea
        label="Secondary"
        textareaColor="secondary"
        placeholder="Secondary color"
        rows={2}
      />
      <TextArea
        label="Accent"
        textareaColor="accent"
        placeholder="Accent color"
        rows={2}
      />
      <TextArea
        label="Info"
        textareaColor="info"
        placeholder="Info color"
        rows={2}
      />
      <TextArea
        label="Success"
        textareaColor="success"
        placeholder="Success color"
        rows={2}
      />
      <TextArea
        label="Warning"
        textareaColor="warning"
        placeholder="Warning color"
        rows={2}
      />
      <TextArea
        label="Error"
        textareaColor="error"
        placeholder="Error color"
        rows={2}
      />
    </div>
  ),
};

export const FormExample: Story = {
  render: () => {
    const [bio, setBio] = useState('');
    const [notes, setNotes] = useState('');

    return (
      <form className="space-y-4 w-full max-w-lg p-6 bg-base-200 rounded-lg">
        <h2 className="text-xl font-bold mb-4">Support Ticket</h2>

        <TextArea
          label="Subject"
          placeholder="Brief description of the issue"
          required
          rows={1}
        />

        <TextArea
          label="Description"
          labelAlt="Required"
          placeholder="Please describe your issue in detail..."
          description="Include any error messages or steps to reproduce"
          required
          autoResize
          minRows={4}
          maxRows={12}
          value={bio}
          onChange={(e) => setBio(e.target.value)}
        />

        <TextArea
          label="Additional Notes"
          placeholder="Any other information..."
          maxLength={500}
          showCharCount
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          rows={3}
        />

        <div className="flex justify-end gap-2 mt-6">
          <button type="button" className="btn btn-ghost">
            Cancel
          </button>
          <button type="submit" className="btn btn-primary">
            Submit Ticket
          </button>
        </div>
      </form>
    );
  },
};
