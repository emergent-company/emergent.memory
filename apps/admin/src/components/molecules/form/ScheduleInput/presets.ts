/** Schedule presets with common cron expressions */
export interface SchedulePreset {
  /** Display label */
  label: string;
  /** Cron expression */
  value: string;
  /** Category for grouping */
  category?: 'frequent' | 'hourly' | 'daily' | 'weekly';
}

/** Default schedule presets */
export const SCHEDULE_PRESETS: SchedulePreset[] = [
  // Frequent
  { label: 'Every 15 minutes', value: '*/15 * * * *', category: 'frequent' },
  { label: 'Every 30 minutes', value: '*/30 * * * *', category: 'frequent' },

  // Hourly
  { label: 'Every hour', value: '0 * * * *', category: 'hourly' },
  { label: 'Every 2 hours', value: '0 */2 * * *', category: 'hourly' },
  { label: 'Every 4 hours', value: '0 */4 * * *', category: 'hourly' },
  { label: 'Every 6 hours', value: '0 */6 * * *', category: 'hourly' },
  { label: 'Every 12 hours', value: '0 */12 * * *', category: 'hourly' },

  // Daily
  { label: 'Daily at midnight', value: '0 0 * * *', category: 'daily' },
  { label: 'Daily at 6 AM', value: '0 6 * * *', category: 'daily' },
  { label: 'Daily at 9 AM', value: '0 9 * * *', category: 'daily' },
  { label: 'Daily at noon', value: '0 12 * * *', category: 'daily' },
  { label: 'Daily at 6 PM', value: '0 18 * * *', category: 'daily' },

  // Weekly
  { label: 'Weekly on Sunday', value: '0 0 * * 0', category: 'weekly' },
  { label: 'Weekly on Monday', value: '0 0 * * 1', category: 'weekly' },
  { label: 'Weekly on Friday', value: '0 18 * * 5', category: 'weekly' },
  { label: 'Weekdays at 9 AM', value: '0 9 * * 1-5', category: 'weekly' },
];

/** Custom preset value for when user wants to enter their own cron */
export const CUSTOM_PRESET_VALUE = '__custom__';
