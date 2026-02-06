import React, { useState, useEffect, useMemo, useCallback } from 'react';
import clsx from 'clsx';
import { twMerge } from 'tailwind-merge';
import cronstrue from 'cronstrue';
import cronParser from 'cron-parser';
import { Icon } from '@/components/atoms/Icon';
import {
  SCHEDULE_PRESETS,
  CUSTOM_PRESET_VALUE,
  type SchedulePreset,
} from './presets';

export interface ScheduleInputProps {
  /** Current cron expression value */
  value?: string;
  /** Callback when value changes */
  onChange?: (value: string | null) => void;
  /** Field label text */
  label?: string;
  /** Helper text displayed below the input */
  description?: string;
  /** Error message displayed below the input */
  error?: string;
  /** Mark field as required */
  required?: boolean;
  /** Container className */
  containerClassName?: string;
  /** Input size variant */
  size?: 'sm' | 'md' | 'lg';
  /** Disable the input */
  disabled?: boolean;
  /** Allow manual/disabled state (null value) */
  allowManual?: boolean;
  /** Label for manual mode */
  manualLabel?: string;
  /** Label for scheduled mode */
  scheduledLabel?: string;
  /** Custom presets (defaults to SCHEDULE_PRESETS) */
  presets?: SchedulePreset[];
  /** Show next run time preview */
  showNextRun?: boolean;
  /** Number of next runs to show */
  nextRunCount?: number;
  /** Timezone for next run calculation (defaults to local) */
  timezone?: string;
}

/**
 * Validates a cron expression
 */
function validateCron(expression: string): { valid: boolean; error?: string } {
  if (!expression || !expression.trim()) {
    return { valid: false, error: 'Cron expression is required' };
  }

  try {
    cronParser.parse(expression.trim());
    return { valid: true };
  } catch (err) {
    return { valid: false, error: (err as Error).message };
  }
}

/**
 * Get human-readable description of cron expression
 */
function getCronDescription(expression: string): string | null {
  try {
    return cronstrue.toString(expression, {
      throwExceptionOnParseError: true,
      use24HourTimeFormat: false,
    });
  } catch {
    return null;
  }
}

/**
 * Get next run times for a cron expression
 */
function getNextRuns(
  expression: string,
  count: number = 3,
  timezone?: string
): Date[] {
  try {
    const options: { tz?: string; currentDate?: Date } = {};
    if (timezone) {
      options.tz = timezone;
    }

    const interval = cronParser.parse(expression, options);
    const runs: Date[] = [];

    for (let i = 0; i < count; i++) {
      runs.push(interval.next().toDate());
    }

    return runs;
  } catch {
    return [];
  }
}

/**
 * Format a date for display
 */
function formatNextRun(date: Date): string {
  const now = new Date();
  const diffMs = date.getTime() - now.getTime();
  const diffMins = Math.round(diffMs / 60000);
  const diffHours = Math.round(diffMs / 3600000);
  const diffDays = Math.round(diffMs / 86400000);

  let relative: string;
  if (diffMins < 60) {
    relative = `in ${diffMins} minute${diffMins !== 1 ? 's' : ''}`;
  } else if (diffHours < 24) {
    relative = `in ${diffHours} hour${diffHours !== 1 ? 's' : ''}`;
  } else if (diffDays < 7) {
    relative = `in ${diffDays} day${diffDays !== 1 ? 's' : ''}`;
  } else {
    relative = date.toLocaleDateString();
  }

  const time = date.toLocaleString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });

  return `${time} (${relative})`;
}

/**
 * ScheduleInput Molecule
 *
 * A cron-based schedule input component with preset options,
 * custom cron expression support, human-readable preview, and next run times.
 *
 * @example
 * ```tsx
 * <ScheduleInput
 *   label="Sync Schedule"
 *   value={schedule}
 *   onChange={setSchedule}
 *   allowManual
 *   showNextRun
 * />
 * ```
 */
export const ScheduleInput: React.FC<ScheduleInputProps> = ({
  value,
  onChange,
  label,
  description,
  error: externalError,
  required,
  containerClassName,
  size = 'md',
  disabled,
  allowManual = true,
  manualLabel = 'Manual',
  scheduledLabel = 'Scheduled',
  presets = SCHEDULE_PRESETS,
  showNextRun = true,
  nextRunCount = 3,
  timezone,
}) => {
  // Determine if we're in manual mode (null value) or scheduled mode
  const isManual = value === null || value === undefined;

  // Track if we're using a preset or custom cron
  const [isCustom, setIsCustom] = useState(false);
  const [customCron, setCustomCron] = useState('');
  const [internalError, setInternalError] = useState<string | undefined>();

  // Find matching preset
  const matchingPreset = useMemo(() => {
    if (isManual || !value) return null;
    return presets.find((p) => p.value === value);
  }, [value, isManual, presets]);

  // Initialize custom state from value
  useEffect(() => {
    if (value && !matchingPreset) {
      setIsCustom(true);
      setCustomCron(value);
    } else if (matchingPreset) {
      setIsCustom(false);
    }
  }, [value, matchingPreset]);

  // Get cron description and next runs
  const cronDescription = useMemo(() => {
    if (isManual || !value) return null;
    return getCronDescription(value);
  }, [value, isManual]);

  const nextRuns = useMemo(() => {
    if (isManual || !value || !showNextRun) return [];
    return getNextRuns(value, nextRunCount, timezone);
  }, [value, isManual, showNextRun, nextRunCount, timezone]);

  // Handle mode toggle (manual <-> scheduled)
  const handleModeChange = useCallback(
    (scheduled: boolean) => {
      if (scheduled) {
        // Switch to scheduled - use first preset
        const defaultPreset = presets[0]?.value || '0 * * * *';
        onChange?.(defaultPreset);
      } else {
        // Switch to manual
        onChange?.(null);
      }
    },
    [onChange, presets]
  );

  // Handle preset selection
  const handlePresetChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const selectedValue = e.target.value;

      if (selectedValue === CUSTOM_PRESET_VALUE) {
        setIsCustom(true);
        setCustomCron(value || '');
      } else {
        setIsCustom(false);
        setInternalError(undefined);
        onChange?.(selectedValue);
      }
    },
    [value, onChange]
  );

  // Handle custom cron input
  const handleCustomCronChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const newValue = e.target.value;
      setCustomCron(newValue);

      const validation = validateCron(newValue);
      if (validation.valid) {
        setInternalError(undefined);
        onChange?.(newValue.trim());
      } else {
        setInternalError(validation.error);
      }
    },
    [onChange]
  );

  const hasError = !!(externalError || internalError);
  const displayError = externalError || internalError;

  const sizeClasses = {
    sm: 'select-sm input-sm text-sm',
    md: '',
    lg: 'select-lg input-lg text-lg',
  };

  return (
    <div className={twMerge('form-control', containerClassName)}>
      {/* Label */}
      {label && (
        <label className="label">
          <span className="label-text font-medium">
            {label}
            {required && <span className="text-error ml-1">*</span>}
          </span>
        </label>
      )}

      {/* Mode toggle (if allowManual) */}
      {allowManual && (
        <div className="flex gap-2 mb-2">
          <button
            type="button"
            className={clsx(
              'btn btn-sm flex-1',
              isManual ? 'btn-primary' : 'btn-ghost'
            )}
            onClick={() => handleModeChange(false)}
            disabled={disabled}
          >
            <Icon icon="lucide--hand" className="size-4" />
            {manualLabel}
          </button>
          <button
            type="button"
            className={clsx(
              'btn btn-sm flex-1',
              !isManual ? 'btn-primary' : 'btn-ghost'
            )}
            onClick={() => handleModeChange(true)}
            disabled={disabled}
          >
            <Icon icon="lucide--clock" className="size-4" />
            {scheduledLabel}
          </button>
        </div>
      )}

      {/* Schedule configuration (only shown when scheduled) */}
      {!isManual && (
        <div className="space-y-2">
          {/* Preset selector */}
          <select
            className={twMerge(
              'select select-bordered w-full',
              sizeClasses[size],
              hasError && 'select-error'
            )}
            value={isCustom ? CUSTOM_PRESET_VALUE : value || ''}
            onChange={handlePresetChange}
            disabled={disabled}
          >
            <optgroup label="Frequent">
              {presets
                .filter((p) => p.category === 'frequent')
                .map((preset) => (
                  <option key={preset.value} value={preset.value}>
                    {preset.label}
                  </option>
                ))}
            </optgroup>
            <optgroup label="Hourly">
              {presets
                .filter((p) => p.category === 'hourly')
                .map((preset) => (
                  <option key={preset.value} value={preset.value}>
                    {preset.label}
                  </option>
                ))}
            </optgroup>
            <optgroup label="Daily">
              {presets
                .filter((p) => p.category === 'daily')
                .map((preset) => (
                  <option key={preset.value} value={preset.value}>
                    {preset.label}
                  </option>
                ))}
            </optgroup>
            <optgroup label="Weekly">
              {presets
                .filter((p) => p.category === 'weekly')
                .map((preset) => (
                  <option key={preset.value} value={preset.value}>
                    {preset.label}
                  </option>
                ))}
            </optgroup>
            <optgroup label="Advanced">
              <option value={CUSTOM_PRESET_VALUE}>
                Custom cron expression...
              </option>
            </optgroup>
          </select>

          {/* Custom cron input */}
          {isCustom && (
            <div className="relative">
              <input
                type="text"
                className={twMerge(
                  'input input-bordered w-full font-mono',
                  sizeClasses[size],
                  hasError && 'input-error'
                )}
                placeholder="* * * * * (min hour day month weekday)"
                value={customCron}
                onChange={handleCustomCronChange}
                disabled={disabled}
              />
              <a
                href="https://crontab.guru/"
                target="_blank"
                rel="noopener noreferrer"
                className="absolute right-3 top-1/2 -translate-y-1/2 text-base-content/40 hover:text-primary"
                title="Cron expression help"
              >
                <Icon icon="lucide--help-circle" className="size-4" />
              </a>
            </div>
          )}

          {/* Human-readable description */}
          {cronDescription && !hasError && (
            <div className="flex items-center gap-2 text-sm text-base-content/70 bg-base-200 rounded-lg px-3 py-2">
              <Icon icon="lucide--info" className="size-4 shrink-0" />
              <span>{cronDescription}</span>
            </div>
          )}

          {/* Next run times */}
          {showNextRun && nextRuns.length > 0 && !hasError && (
            <div className="text-xs text-base-content/60 space-y-1">
              <div className="font-medium">Next runs:</div>
              {nextRuns.map((run, i) => (
                <div key={i} className="flex items-center gap-2 pl-2">
                  <Icon icon="lucide--chevron-right" className="size-3" />
                  {formatNextRun(run)}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Manual mode message */}
      {isManual && allowManual && (
        <div className="flex items-center gap-2 text-sm text-base-content/60 bg-base-200 rounded-lg px-3 py-2">
          <Icon icon="lucide--info" className="size-4 shrink-0" />
          <span>No automatic sync. Trigger manually when needed.</span>
        </div>
      )}

      {/* Helper/error text */}
      {(displayError || description) && (
        <label className="label">
          <span
            className={clsx(
              'label-text-alt',
              hasError ? 'text-error' : 'text-base-content/60'
            )}
          >
            {displayError || description}
          </span>
        </label>
      )}
    </div>
  );
};

ScheduleInput.displayName = 'ScheduleInput';

export default ScheduleInput;
