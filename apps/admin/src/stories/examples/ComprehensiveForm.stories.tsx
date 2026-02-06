import type { Meta, StoryObj } from '@storybook/react';
import React, { useState } from 'react';
import { FormField } from '@/components/molecules/FormField';
import {
  Select,
  TextArea,
  Checkbox,
  Toggle,
  ScheduleInput,
} from '@/components/molecules/form';
import { FileUploader } from '@/components/molecules/FileUploader';
import { Button } from '@/components/atoms/Button';
import { Icon } from '@/components/atoms/Icon';

const meta: Meta = {
  title: 'Examples/Comprehensive Form',
  parameters: {
    layout: 'padded',
  },
};

export default meta;

export const CompleteProfile: StoryObj = {
  render: () => {
    const [formData, setFormData] = useState({
      fullName: '',
      email: '',
      password: '',
      phone: '',
      website: '',
      country: 'us',
      timezone: 'pst',
      bio: '',
      profilePhoto: [] as any[],
      darkMode: true,
      publicProfile: true,
      notifications: ['email', 'push'],
      syncSchedule: '0 0 * * *',
      marketingEmails: false,
    });

    const [isLoading, setIsLoading] = useState(false);

    const handleChange = (key: string, value: any) => {
      setFormData((prev) => ({ ...prev, [key]: value }));
    };

    const handleNotificationToggle = (value: string) => {
      setFormData((prev) => ({
        ...prev,
        notifications: prev.notifications.includes(value)
          ? prev.notifications.filter((v) => v !== value)
          : [...prev.notifications, value],
      }));
    };

    const handleSubmit = (e: React.FormEvent) => {
      e.preventDefault();
      setIsLoading(true);
      setTimeout(() => {
        setIsLoading(false);
        alert(
          'Form submitted successfully!\n' + JSON.stringify(formData, null, 2)
        );
      }, 1500);
    };

    const handleReset = () => {
      if (confirm('Are you sure you want to reset all fields?')) {
        setFormData({
          fullName: '',
          email: '',
          password: '',
          phone: '',
          website: '',
          country: 'us',
          timezone: 'pst',
          bio: '',
          profilePhoto: [],
          darkMode: true,
          publicProfile: true,
          notifications: ['email', 'push'],
          syncSchedule: '0 0 * * *',
          marketingEmails: false,
        });
      }
    };

    return (
      <div className="max-w-4xl mx-auto py-8">
        <div className="bg-base-100 shadow-xl rounded-2xl overflow-hidden border border-base-200">
          <div className="bg-primary p-8 text-primary-content">
            <h1 className="text-3xl font-bold">Account Settings</h1>
            <p className="opacity-80 mt-2">
              Manage your professional profile and system preferences.
            </p>
          </div>

          <form onSubmit={handleSubmit} className="p-8 space-y-12">
            {/* Section 1: Identity */}
            <section>
              <div className="flex items-center gap-2 mb-6 text-primary">
                <Icon icon="lucide--user" className="size-6" />
                <h2 className="text-xl font-semibold">Identity & Branding</h2>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
                <div className="md:col-span-1">
                  <label className="label">
                    <span className="label-text font-medium">
                      Profile Photo
                    </span>
                  </label>
                  <FileUploader
                    files={formData.profilePhoto}
                    onupdatefiles={(fileItems) =>
                      handleChange('profilePhoto', fileItems)
                    }
                    labelIdle='Drop photo or <span class="filepond--label-action">Browse</span>'
                    imagePreviewHeight={170}
                    imageCropAspectRatio="1:1"
                    imageResizeTargetWidth={200}
                    imageResizeTargetHeight={200}
                    stylePanelLayout="compact circle"
                    styleLoadIndicatorPosition="center bottom"
                    styleProgressIndicatorPosition="right bottom"
                    styleButtonRemoveItemPosition="left bottom"
                    styleButtonProcessItemPosition="right bottom"
                  />
                  <p className="text-xs text-base-content/50 mt-2 text-center">
                    Upload a high-resolution square image.
                  </p>
                </div>

                <div className="md:col-span-2 space-y-4">
                  <FormField
                    label="Full Name"
                    placeholder="John Doe"
                    leftIcon="lucide--user"
                    required
                    value={formData.fullName}
                    onChange={(e) => handleChange('fullName', e.target.value)}
                  />
                  <FormField
                    label="Email Address"
                    type="email"
                    placeholder="john@example.com"
                    leftIcon="lucide--mail"
                    required
                    value={formData.email}
                    onChange={(e) => handleChange('email', e.target.value)}
                    description="Used for account login and official communications."
                  />
                  <FormField
                    label="Password"
                    type="password"
                    placeholder="••••••••"
                    leftIcon="lucide--lock"
                    rightIcon="lucide--eye"
                    required
                    value={formData.password}
                    onChange={(e) => handleChange('password', e.target.value)}
                    description="At least 8 characters with one special symbol."
                  />
                </div>
              </div>
            </section>

            <div className="divider"></div>

            {/* Section 2: Details */}
            <section>
              <div className="flex items-center gap-2 mb-6 text-primary">
                <Icon icon="lucide--map-pin" className="size-6" />
                <h2 className="text-xl font-semibold">Profile Details</h2>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <Select
                  label="Country"
                  options={[
                    { value: 'us', label: 'United States' },
                    { value: 'uk', label: 'United Kingdom' },
                    { value: 'ca', label: 'Canada' },
                    { value: 'de', label: 'Germany' },
                    { value: 'fr', label: 'France' },
                  ]}
                  value={formData.country}
                  onChange={(e) => handleChange('country', e.target.value)}
                  required
                />
                <Select
                  label="Preferred Timezone"
                  options={[
                    { value: 'pst', label: 'Pacific Time (PST)' },
                    { value: 'est', label: 'Eastern Time (EST)' },
                    { value: 'utc', label: 'Coordinated Universal Time (UTC)' },
                    { value: 'cet', label: 'Central European Time (CET)' },
                  ]}
                  value={formData.timezone}
                  onChange={(e) => handleChange('timezone', e.target.value)}
                  required
                />
                <div className="md:col-span-2">
                  <TextArea
                    label="Bio"
                    placeholder="Tell us about your background and interests..."
                    autoResize
                    minRows={4}
                    showCharCount
                    maxLength={500}
                    value={formData.bio}
                    onChange={(e) => handleChange('bio', e.target.value)}
                    description="Brief description for your public profile."
                  />
                </div>
              </div>
            </section>

            <div className="divider"></div>

            {/* Section 3: Automation & Scheduling */}
            <section>
              <div className="flex items-center gap-2 mb-6 text-primary">
                <Icon icon="lucide--clock" className="size-6" />
                <h2 className="text-xl font-semibold">
                  Automation & Scheduling
                </h2>
              </div>

              <div className="bg-base-200/50 p-6 rounded-xl space-y-6">
                <ScheduleInput
                  label="Data Sync Schedule"
                  description="Choose how frequently your external data sources should be synchronized."
                  value={formData.syncSchedule}
                  onChange={(val) => handleChange('syncSchedule', val)}
                  allowManual
                  showNextRun
                  nextRunCount={3}
                />
              </div>
            </section>

            <div className="divider"></div>

            {/* Section 4: Preferences */}
            <section>
              <div className="flex items-center gap-2 mb-6 text-primary">
                <Icon icon="lucide--settings" className="size-6" />
                <h2 className="text-xl font-semibold">System Preferences</h2>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-x-12 gap-y-6">
                <Toggle
                  label="Dark Mode"
                  description="Use the dark color palette across the UI."
                  checked={formData.darkMode}
                  onChange={(e) => handleChange('darkMode', e.target.checked)}
                  toggleColor="primary"
                  togglePosition="right"
                />
                <Toggle
                  label="Public Profile"
                  description="Allow others to see your profile information."
                  checked={formData.publicProfile}
                  onChange={(e) =>
                    handleChange('publicProfile', e.target.checked)
                  }
                  toggleColor="primary"
                  togglePosition="right"
                />

                <div className="md:col-span-2 mt-4">
                  <label className="label">
                    <span className="label-text font-medium text-lg">
                      Notifications
                    </span>
                  </label>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mt-2">
                    <Checkbox
                      label="Email"
                      description="Account activity"
                      checked={formData.notifications.includes('email')}
                      onChange={() => handleNotificationToggle('email')}
                    />
                    <Checkbox
                      label="Push"
                      description="Browser alerts"
                      checked={formData.notifications.includes('push')}
                      onChange={() => handleNotificationToggle('push')}
                    />
                    <Checkbox
                      label="SMS"
                      description="Critical updates"
                      checked={formData.notifications.includes('sms')}
                      onChange={() => handleNotificationToggle('sms')}
                    />
                  </div>
                </div>
              </div>
            </section>

            <div className="divider"></div>

            {/* Section 5: Legal */}
            <section className="space-y-4">
              <Checkbox
                label="I agree to receive occasional marketing and product update emails."
                checked={formData.marketingEmails}
                onChange={(e) =>
                  handleChange('marketingEmails', e.target.checked)
                }
                checkboxColor="primary"
              />
              <p className="text-xs text-base-content/50">
                By clicking "Save Configuration", you acknowledge that you have
                read and agreed to our
                <a href="#" className="link link-primary ml-1">
                  Terms of Service
                </a>{' '}
                and
                <a href="#" className="link link-primary ml-1">
                  Privacy Policy
                </a>
                .
              </p>
            </section>

            {/* Form Actions */}
            <div className="flex flex-col sm:flex-row justify-between items-center gap-4 pt-8 border-t border-base-200 mt-12">
              <Button
                type="button"
                variant="ghost"
                color="error"
                onClick={handleReset}
                disabled={isLoading}
              >
                Reset to Defaults
              </Button>

              <div className="flex gap-3 w-full sm:w-auto">
                <Button
                  type="button"
                  variant="ghost"
                  className="flex-1 sm:flex-none"
                  disabled={isLoading}
                >
                  Discard
                </Button>
                <Button
                  type="submit"
                  color="primary"
                  loading={isLoading}
                  className="flex-1 sm:flex-none px-8"
                  startIcon={!isLoading && <Icon icon="lucide--save" />}
                >
                  Save Configuration
                </Button>
              </div>
            </div>
          </form>
        </div>
      </div>
    );
  },
};
