import React, { forwardRef } from 'react';
import clsx from 'clsx';
import { twMerge } from 'tailwind-merge';

export interface CheckboxProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'size' | 'type'> {
  /** Checkbox label text */
  label?: string;
  /** Description text displayed below the label */
  description?: string;
  /** Error message displayed below the checkbox */
  error?: string;
  /** Container className for the form-control wrapper */
  containerClassName?: string;
  /** Checkbox size variant */
  checkboxSize?: 'xs' | 'sm' | 'md' | 'lg';
  /** Checkbox color variant */
  checkboxColor?:
    | 'primary'
    | 'secondary'
    | 'accent'
    | 'info'
    | 'success'
    | 'warning'
    | 'error';
  /** Show indeterminate state */
  indeterminate?: boolean;
}

/**
 * Checkbox Molecule
 *
 * A reusable checkbox component that follows DaisyUI patterns.
 * Provides consistent styling with label and description support.
 *
 * @example
 * ```tsx
 * <Checkbox
 *   label="I agree to the terms"
 *   description="You must accept our terms to continue"
 *   required
 * />
 * ```
 */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(
  (
    {
      label,
      description,
      error,
      containerClassName,
      checkboxSize,
      checkboxColor,
      disabled,
      className,
      indeterminate,
      ...inputProps
    },
    ref
  ) => {
    const hasError = !!error;

    // Handle indeterminate state via ref
    const inputRef = React.useRef<HTMLInputElement>(null);
    React.useImperativeHandle(ref, () => inputRef.current!);

    React.useEffect(() => {
      if (inputRef.current) {
        inputRef.current.indeterminate = !!indeterminate;
      }
    }, [indeterminate]);

    // Build checkbox classes using DaisyUI pattern
    const checkboxClasses = twMerge(
      'checkbox',
      className,
      clsx({
        'checkbox-xs': checkboxSize === 'xs',
        'checkbox-sm': checkboxSize === 'sm',
        'checkbox-md': checkboxSize === 'md',
        'checkbox-lg': checkboxSize === 'lg',
        'checkbox-primary': checkboxColor === 'primary',
        'checkbox-secondary': checkboxColor === 'secondary',
        'checkbox-accent': checkboxColor === 'accent',
        'checkbox-info': checkboxColor === 'info',
        'checkbox-success': checkboxColor === 'success',
        'checkbox-warning': checkboxColor === 'warning',
        'checkbox-error': checkboxColor === 'error' || hasError,
      })
    );

    return (
      <div className={twMerge('form-control', containerClassName)}>
        <label
          className={clsx(
            'label cursor-pointer justify-start gap-3',
            disabled && 'cursor-not-allowed opacity-60'
          )}
        >
          <input
            ref={inputRef}
            type="checkbox"
            className={checkboxClasses}
            disabled={disabled}
            aria-invalid={hasError}
            aria-describedby={
              error || description ? `${inputProps.id}-helper` : undefined
            }
            {...inputProps}
          />
          {(label || description) && (
            <div className="flex flex-col">
              {label && <span className="label-text font-medium">{label}</span>}
              {description && (
                <span className="label-text-alt text-base-content/60">
                  {description}
                </span>
              )}
            </div>
          )}
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

Checkbox.displayName = 'Checkbox';

export default Checkbox;
