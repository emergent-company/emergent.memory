import type { Meta, StoryObj } from '@storybook/react';
import React, { useState } from 'react';
import { Modal } from '@/components/organisms/Modal';
import { FormField } from '@/components/molecules/FormField';
import { Select, Toggle } from '@/components/molecules/form';
import { Button } from '@/components/atoms/Button';
import { Icon } from '@/components/atoms/Icon';

const meta: Meta = {
  title: 'Examples/Modal Form',
  parameters: {
    layout: 'centered',
  },
};

export default meta;

export const CreateResourceModal: StoryObj = {
  render: () => {
    const [isOpen, setIsOpen] = useState(false);
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [formData, setFormData] = useState({
      name: '',
      type: 'document',
      visibility: 'private',
      notifications: true,
    });

    const handleOpen = () => setIsOpen(true);
    const handleClose = () => {
      setIsOpen(false);
      // Reset form on close
      setFormData({
        name: '',
        type: 'document',
        visibility: 'private',
        notifications: true,
      });
    };

    const handleChange = (key: string, value: any) => {
      setFormData((prev) => ({ ...prev, [key]: value }));
    };

    const handleSubmit = () => {
      if (!formData.name) {
        alert('Please enter a resource name');
        return;
      }

      setIsSubmitting(true);
      // Simulate API call
      setTimeout(() => {
        setIsSubmitting(false);
        setIsOpen(false);
        alert(`Resource "${formData.name}" created successfully!`);
      }, 1000);
    };

    return (
      <div>
        <Button
          color="primary"
          onClick={handleOpen}
          startIcon={<Icon icon="lucide--plus" />}
        >
          Create New Resource
        </Button>

        <Modal
          open={isOpen}
          onOpenChange={(open) => !isSubmitting && setIsOpen(open)}
          title="Create New Resource"
          description="Enter the details below to initialize a new system resource."
          sizeClassName="max-w-md"
          actions={[
            {
              label: 'Cancel',
              variant: 'ghost',
              onClick: handleClose,
              disabled: isSubmitting,
            },
            {
              label: isSubmitting ? 'Creating...' : 'Create Resource',
              variant: 'primary',
              onClick: handleSubmit,
              disabled: isSubmitting,
              autoFocus: true,
            },
          ]}
        >
          <div className="space-y-4 pt-2">
            <FormField
              label="Resource Name"
              placeholder="e.g. Q4 Analysis Report"
              required
              value={formData.name}
              onChange={(e) => handleChange('name', e.target.value)}
              disabled={isSubmitting}
              autoFocus
            />

            <Select
              label="Resource Type"
              options={[
                { value: 'document', label: 'Document' },
                { value: 'collection', label: 'Collection' },
                { value: 'agent', label: 'AI Agent' },
                { value: 'tool', label: 'Custom Tool' },
              ]}
              value={formData.type}
              onChange={(e) => handleChange('type', e.target.value)}
              disabled={isSubmitting}
            />

            <div className="bg-base-200/50 p-4 rounded-lg space-y-4">
              <label className="label py-0">
                <span className="label-text font-medium text-xs uppercase tracking-wider opacity-50">
                  Privacy & Settings
                </span>
              </label>

              <div className="flex flex-col gap-3">
                <Toggle
                  label="Publicly visible"
                  checked={formData.visibility === 'public'}
                  onChange={(e) =>
                    handleChange(
                      'visibility',
                      e.target.checked ? 'public' : 'private'
                    )
                  }
                  toggleSize="sm"
                  toggleColor="primary"
                  disabled={isSubmitting}
                />

                <Toggle
                  label="Enable notifications"
                  checked={formData.notifications}
                  onChange={(e) =>
                    handleChange('notifications', e.target.checked)
                  }
                  toggleSize="sm"
                  toggleColor="primary"
                  disabled={isSubmitting}
                />
              </div>
            </div>
          </div>
        </Modal>
      </div>
    );
  },
};
