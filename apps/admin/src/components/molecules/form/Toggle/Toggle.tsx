import React, { forwardRef } from 'react';
import clsx from 'clsx';
import { twMerge } from 'tailwind-merge';

export interface ToggleProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'size' | 'type'> {
  /** Toggle label text */
  label?: string;
  /** Description text displayed below or beside the label */
  description?: string;
  /** Error message displayed below the toggle */
  error?: string;
  /** Container className for the form-control wrapper */
  containerClassName?: string;
  /** Toggle size variant */
  toggleSize?: 'xs' | 'sm' | 'md' | 'lg';
  /** Toggle color variant */
  toggleColor?:
    | 'primary'
    | 'secondary'
    | 'accent'
    | 'info'
    | 'success'
    | 'warning'
    | 'error';
  /** Layout direction */
  layout?: 'horizontal' | 'vertical';
  /** Position of toggle relative to label (only for horizontal layout) */
  togglePosition?: 'left' | 'right';
}

/**
 * Toggle Molecule
 *
 * A reusable toggle switch component that follows DaisyUI patterns.
 * Provides consistent styling with label and description support.
 * Supports both horizontal and vertical layouts.
 *
 * @example
 * ```tsx
 * <Toggle
 *   label="Enable notifications"
 *   description="Receive push notifications"
 *   toggleColor="primary"
 * />
 * ```
 */
export const Toggle = forwardRef<HTMLInputElement, ToggleProps>(
  (
    {
      label,
      description,
      error,
      containerClassName,
      toggleSize,
      toggleColor,
      disabled,
      className,
      layout = 'horizontal',
      togglePosition = 'left',
      ...inputProps
    },
    ref
  ) => {
    const hasError = !!error;

    // Build toggle classes using DaisyUI pattern
    const toggleClasses = twMerge(
      'toggle',
      className,
      clsx({
        'toggle-xs': toggleSize === 'xs',
        'toggle-sm': toggleSize === 'sm',
        'toggle-md': toggleSize === 'md',
        'toggle-lg': toggleSize === 'lg',
        'toggle-primary': toggleColor === 'primary',
        'toggle-secondary': toggleColor === 'secondary',
        'toggle-accent': toggleColor === 'accent',
        'toggle-info': toggleColor === 'info',
        'toggle-success': toggleColor === 'success',
        'toggle-warning': toggleColor === 'warning',
        'toggle-error': toggleColor === 'error' || hasError,
      })
    );

    const toggleElement = (
      <input
        ref={ref}
        type="checkbox"
        className={toggleClasses}
        disabled={disabled}
        aria-invalid={hasError}
        aria-describedby={
          error || description ? `${inputProps.id}-helper` : undefined
        }
        {...inputProps}
      />
    );

    const labelContent = (label || description) && (
      <div className="flex flex-col">
        {label && <span className="label-text font-medium">{label}</span>}
        {description && (
          <span className="label-text-alt text-base-content/60">
            {description}
          </span>
        )}
      </div>
    );

    if (layout === 'vertical') {
      return (
        <div className={twMerge('form-control', containerClassName)}>
          {(label || description) && (
            <label className="label">
              {label && <span className="label-text font-medium">{label}</span>}
            </label>
          )}
          <label
            className={clsx(
              'cursor-pointer',
              disabled && 'cursor-not-allowed opacity-60'
            )}
          >
            {toggleElement}
          </label>
          {description && (
            <label className="label">
              <span className="label-text-alt text-base-content/60">
                {description}
              </span>
            </label>
          )}
          {error && (
            <label className="label pt-0">
              <span
                id={inputProps.id ? `${inputProps.id}-helper` : undefined}
                className="label-text-alt text-error"
              >
                {error}
              </span>
            </label>
          )}
        </div>
      );
    }

    // Horizontal layout (default)
    return (
      <div className={twMerge('form-control', containerClassName)}>
        <label
          className={clsx(
            'label cursor-pointer justify-start gap-3',
            disabled && 'cursor-not-allowed opacity-60',
            togglePosition === 'right' && 'flex-row-reverse justify-end'
          )}
        >
          {toggleElement}
          {labelContent}
        </label>
        {error && (
          <label className="label pt-0">
            <span
              id={inputProps.id ? `${inputProps.id}-helper` : undefined}
              className="label-text-alt text-error"
            >
              {error}
            </span>
          </label>
        )}
      </div>
    );
  }
);

Toggle.displayName = 'Toggle';

export default Toggle;
