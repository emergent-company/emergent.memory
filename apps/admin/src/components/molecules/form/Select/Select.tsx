import React, { forwardRef } from 'react';
import clsx from 'clsx';
import { twMerge } from 'tailwind-merge';

export interface SelectOption {
  /** Option value */
  value: string;
  /** Display label */
  label: string;
  /** Disable this option */
  disabled?: boolean;
}

export interface SelectProps
  extends Omit<React.SelectHTMLAttributes<HTMLSelectElement>, 'size'> {
  /** Field label text */
  label?: string;
  /** Secondary label text (shown on right side) */
  labelAlt?: string;
  /** Helper text displayed below the select */
  description?: string;
  /** Secondary helper text (shown on right side below select) */
  descriptionAlt?: string;
  /** Error message displayed below the select */
  error?: string;
  /** Success message displayed below the select */
  success?: string;
  /** Warning message displayed below the select */
  warning?: string;
  /** Mark field as required with asterisk */
  required?: boolean;
  /** Container className for the form-control wrapper */
  containerClassName?: string;
  /** Select size variant */
  selectSize?: 'xs' | 'sm' | 'md' | 'lg';
  /** Select color variant */
  selectColor?:
    | 'primary'
    | 'secondary'
    | 'accent'
    | 'info'
    | 'success'
    | 'warning'
    | 'error';
  /** Show bordered style */
  bordered?: boolean;
  /** Show ghost style */
  ghost?: boolean;
  /** Options to display */
  options?: SelectOption[];
  /** Placeholder text (shown as first disabled option) */
  placeholder?: string;
}

/**
 * Select Molecule
 *
 * A reusable select dropdown component that follows DaisyUI patterns.
 * Provides consistent styling for labels, selects, descriptions, and validation states.
 *
 * @example
 * ```tsx
 * <Select
 *   label="Country"
 *   placeholder="Select a country"
 *   options={[
 *     { value: 'us', label: 'United States' },
 *     { value: 'uk', label: 'United Kingdom' },
 *   ]}
 *   required
 * />
 * ```
 */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  (
    {
      label,
      labelAlt,
      description,
      descriptionAlt,
      error,
      success,
      warning,
      required,
      containerClassName,
      selectSize,
      selectColor,
      bordered = true,
      ghost = false,
      disabled,
      className,
      options = [],
      placeholder,
      children,
      ...selectProps
    },
    ref
  ) => {
    // Determine validation state
    const hasError = !!error;
    const hasSuccess = !!success;
    const hasWarning = !!warning;

    // Build select classes using DaisyUI pattern
    const selectClasses = twMerge(
      'select',
      'w-full',
      className,
      clsx({
        'select-bordered': bordered,
        'select-ghost': ghost,
        'select-xs': selectSize === 'xs',
        'select-sm': selectSize === 'sm',
        'select-md': selectSize === 'md',
        'select-lg': selectSize === 'lg',
        'select-primary': selectColor === 'primary',
        'select-secondary': selectColor === 'secondary',
        'select-accent': selectColor === 'accent',
        'select-info': selectColor === 'info',
        'select-success':
          selectColor === 'success' || (hasSuccess && !selectColor),
        'select-warning':
          selectColor === 'warning' || (hasWarning && !selectColor),
        'select-error': selectColor === 'error' || (hasError && !selectColor),
        'bg-base-200/50 cursor-not-allowed': disabled,
      })
    );

    // Helper text content and styling
    const primaryHelper = error || success || warning || description;
    const secondaryHelper = descriptionAlt;

    const primaryHelperClass = clsx(
      'label-text-alt',
      hasError && 'text-error',
      hasSuccess && 'text-success',
      hasWarning && 'text-warning',
      !hasError && !hasSuccess && !hasWarning && 'text-base-content/60'
    );

    return (
      <div className={twMerge('form-control', containerClassName)}>
        {/* Top label row - follows DaisyUI pattern */}
        {(label || labelAlt) && (
          <label className="label">
            {label && (
              <span className="label-text font-medium">
                {label}
                {required && <span className="text-error ml-1">*</span>}
              </span>
            )}
            {labelAlt && <span className="label-text-alt">{labelAlt}</span>}
          </label>
        )}

        {/* Select element */}
        <select
          ref={ref}
          className={selectClasses}
          disabled={disabled}
          aria-invalid={hasError}
          aria-describedby={
            primaryHelper ? `${selectProps.id}-helper` : undefined
          }
          {...selectProps}
        >
          {placeholder && (
            <option value="" disabled>
              {placeholder}
            </option>
          )}
          {options.map((option) => (
            <option
              key={option.value}
              value={option.value}
              disabled={option.disabled}
            >
              {option.label}
            </option>
          ))}
          {children}
        </select>

        {/* Bottom helper text row - follows DaisyUI pattern */}
        {(primaryHelper || secondaryHelper) && (
          <label className="label">
            {primaryHelper && (
              <span
                id={selectProps.id ? `${selectProps.id}-helper` : undefined}
                className={primaryHelperClass}
              >
                {primaryHelper}
              </span>
            )}
            {secondaryHelper && (
              <span className="label-text-alt text-base-content/60">
                {secondaryHelper}
              </span>
            )}
          </label>
        )}
      </div>
    );
  }
);

Select.displayName = 'Select';

export default Select;
